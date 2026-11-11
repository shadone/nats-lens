package api_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func rateOf(s api.SubscriptionFact) string {
	if s.PerSecond == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f", *s.PerSecond)
}

// Each rule of a delivered rate: the server's lists both complete and in
// reach, the subscription in both with the same folded count and a count
// that did not go down.
func TestDeliveredRates(t *testing.T) {
	at := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	sub := func(server, subject string, cid uint64, subs, msgs int64) api.SubscriptionFact {
		return api.SubscriptionFact{Server: server, Account: "APP", Subject: subject, CID: cid, Subs: subs, Msgs: msgs}
	}
	prev := []api.SubscriptionFact{
		sub("s1", "a", 1, 1, 100), sub("s1", "b", 1, 2, 100), sub("s1", "c", 1, 1, 900), sub("s1", "d", 2, 1, 0),
		sub("s2", "a", 1, 1, 100), sub("s3", "a", 1, 1, 100), sub("s4", "a", 1, 1, 100), sub("s5", "a", 1, 1, 100),
	}
	cur := []api.SubscriptionFact{
		sub("s1", "a", 1, 1, 400), // 300 in 5 minutes: 1 a second
		sub("s1", "b", 1, 1, 400), // folded count changed
		sub("s1", "c", 1, 1, 100), // went down: made again
		sub("s1", "d", 3, 1, 600), // another connection
		sub("s1", "e", 1, 1, 600), // not in the earlier list
		sub("s2", "a", 1, 1, 400), // the current list was cut
		sub("s3", "a", 1, 1, 400), // the earlier list was cut
		sub("s4", "a", 1, 1, 400), // the lists lie 20 minutes apart
		sub("s5", "a", 1, 1, 100), // nothing delivered
		sub("s6", "a", 1, 1, 400), // no earlier list
	}
	curLists := map[string]api.ServerList{}
	prevLists := map[string]api.ServerList{}
	for _, s := range []string{"s1", "s2", "s3", "s4", "s5", "s6"} {
		curLists[s] = api.ServerList{At: at, Complete: s != "s2"}
		if s != "s6" {
			prevLists[s] = api.ServerList{At: at.Add(-5 * time.Minute), Complete: s != "s3"}
		}
	}
	prevLists["s4"] = api.ServerList{At: at.Add(-20 * time.Minute), Complete: true}
	var got []string
	for _, s := range api.DeliveredRates(cur, prev, curLists, prevLists, api.RateGap(0)) {
		got = append(got, rateOf(s))
	}
	if want := []string{"1.00", "-", "-", "-", "-", "-", "-", "-", "0.00", "-"}; !slices.Equal(got, want) {
		t.Errorf("rates %v, want %v", got, want)
	}
	// With a 10-minute interval, 20 minutes is in reach.
	if r := api.DeliveredRates(cur[7:8], prev, curLists, prevLists, api.RateGap(10*time.Minute)); rateOf(r[0]) != "0.25" {
		t.Errorf("s4 with a 10 minute interval: %s", rateOf(r[0]))
	}
}

// A connection's rate is the sum of its subscriptions' in the group, none
// when one of them has none; the group's the sum of its measured
// connections'.
func TestGroupDeliveredRates(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	conns := map[string]api.ServerConnections{"s1": {Complete: true, Names: map[uint64]string{1: "order-service", 2: "order-service", 3: "api-gateway"}}}
	subs := []api.SubscriptionFact{
		{Server: "s1", Account: "APP", Subject: "orders.place", Queue: "workers", CID: 1, Subs: 1, PerSecond: f(4)},
		{Server: "s1", Account: "APP", Subject: "orders.place", Queue: "workers", CID: 2, Subs: 1, PerSecond: f(3)},
		{Server: "s1", Account: "APP", Subject: "orders.place", Queue: "workers", CID: 3, Subs: 1},
		// Two inboxes of one connection, one without a rate.
		{Server: "s1", Account: "APP", Subject: "_INBOX.a.*", CID: 1, Subs: 1, PerSecond: f(1)},
		{Server: "s1", Account: "APP", Subject: "_INBOX.b.*", CID: 1, Subs: 1},
		{Server: "s1", Account: "APP", Subject: "_INBOX.c.*", CID: 2, Subs: 1, PerSecond: f(0.5)},
		{Server: "s1", Account: "APP", Subject: "_INBOX.d.*", CID: 2, Subs: 1, PerSecond: f(0.25)},
	}
	text := func(g api.SubscriptionGroup) string {
		s := fmt.Sprintf("%d measured", g.Measured)
		if g.PerSecond != nil {
			s += fmt.Sprintf(", %.2f", *g.PerSecond)
		}
		for _, r := range g.Rates {
			v := "-"
			if r.PerSecond != nil {
				v = fmt.Sprintf("%.2f", *r.PerSecond)
			}
			s += fmt.Sprintf("; %s %s %d %s", r.Name, r.Server, r.CID, v)
		}
		return s
	}
	got := groupBy(api.GroupSubscriptions(subs, conns, nil).Groups)
	if g, want := text(got["APP orders.place workers"]), "2 measured, 7.00; order-service s1 1 4.00; order-service s1 2 3.00; api-gateway s1 3 -"; g != want {
		t.Errorf("queue group: %s, want %s", g, want)
	}
	if g, want := text(got["APP _INBOX.> "]), "1 measured, 0.75; order-service s1 2 0.75; order-service s1 1 -"; g != want {
		t.Errorf("inboxes: %s, want %s", g, want)
	}
	if g, want := text(groupBy(api.CoreGroups(subs, conns))["APP orders.place workers"]), text(got["APP orders.place workers"]); g != want {
		t.Errorf("tester: %s, want %s", g, want)
	}
}

// A row lists at most MaxConnectionRates connections, and counts the rest.
func TestConnectionRatesCut(t *testing.T) {
	var subs []api.SubscriptionFact
	names := map[uint64]string{}
	for i := range uint64(25) {
		v := float64(i)
		subs = append(subs, api.SubscriptionFact{Server: "s1", Account: "APP", Subject: "_INBOX.x" + fmt.Sprint(i), CID: i, Subs: 1, PerSecond: &v})
		names[i] = "c"
	}
	f := &api.SubjectsFacts{Epoch: 1, Subscriptions: &api.SubscriptionFacts{
		Groups: api.GroupSubscriptions(subs, map[string]api.ServerConnections{"s1": {Complete: true, Names: names}}, nil).Groups}}
	res := api.FinishSubjects(f, "", v1.SubjectFilter_SUBJECT_FILTER_UNSPECIFIED)
	r := res.GetRows()[0]
	if len(r.GetConnectionRates()) != api.MaxConnectionRates || r.GetMoreConnectionRates() != 5 || r.GetConnectionRates()[0].GetPerSecond() != 24 ||
		r.GetMeasuredConnections() != 25 || r.GetDeliveredPerSecond() != 300 {
		t.Errorf("row = %v", r)
	}
}
