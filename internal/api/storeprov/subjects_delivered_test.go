package storeprov_test

import (
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// deliveredProvider is the subjects store with the servers' lists of
// epoch prev (before the collection at 12) holding the delivered counts
// below, and the collection's counts set:
//   - orders.place, workers, on nats-0: cid 1 from 1,000 to 2,200 (4 a
//     second over 5 minutes), cid 2 from 500 to 1,400 (3 a second); on
//     nats-2, whose list at 12 was cut, cid 7.
//   - _INBOX.aaa.* of api-gateway on nats-0: 10 to 70 (0.2 a second);
//     _INBOX.bbb.* of checkout-service on nats-1: folded count 2 then 1.
//   - > of audit-tap in SYS on nats-1: 5,000 then 100, as the
//     subscription was made again.
func deliveredProvider(t *testing.T, last time.Time, prev int64) *storeprov.Provider {
	t.Helper()
	p := subjectsProvider(t, last)
	r := rows{t, p.St}
	for _, x := range []struct {
		id            string
		total, listed int64
	}{{"S0", 12, 12}, {"S1", 3, 3}, {"S2", 9000, 9000}} {
		r.add("subscription_lists", map[string]any{"epoch": prev, "server_id": x.id, "total": x.total, "listed": x.listed})
	}
	sub := func(id, account, subject, queue string, cid, subs, before, now int64) {
		cols := map[string]any{"epoch": prev, "server_id": id, "account": account, "subject": subject, "cid": cid, "subs": subs, "msgs": before}
		if queue != "" {
			cols["queue"] = queue
		}
		r.add("subscriptions", cols)
		if _, err := p.St.DB().Exec(`UPDATE subscriptions SET msgs = ? WHERE epoch = ? AND server_id = ? AND subject = ? AND cid = ?`,
			now, subjCollection, id, subject, cid); err != nil {
			t.Fatal(err)
		}
	}
	sub("S0", "APP", "orders.place", "workers", 1, 1, 1000, 2200)
	sub("S0", "APP", "orders.place", "workers", 2, 1, 500, 1400)
	sub("S2", "APP", "orders.place", "workers", 7, 1, 100, 900)
	sub("S0", "APP", "_INBOX.aaa.*", "", 4, 1, 10, 70)
	sub("S1", "APP", "_INBOX.bbb.*", "", 6, 2, 10, 70)
	sub("S1", "SYS", ">", "", 3, 1, 5000, 100)
	return p
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func ratesText(rs []*v1.ConnectionRate) []string {
	var out []string
	for _, c := range rs {
		v := "-"
		if c.PerSecond != nil {
			v = fmt.Sprintf("%.2f", c.GetPerSecond())
		}
		out = append(out, fmt.Sprintf("%s %s %d %s", c.GetName(), c.GetServerName(), c.GetCid(), v))
	}
	return out
}

// A row's delivered rate is the sum of its measured connections'; a
// connection on a cut list, with another folded count or with a count
// that went down has none; the tester gives the same per subject and
// queue.
func TestSubjectsDeliveredRates(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	_, c := apitest.Serve(t, deliveredProvider(t, last, subjCollection-1))
	res := must(c.GetSubjects(ctx, connect.NewRequest(&v1.GetSubjectsRequest{})))
	q := subjRow(res, "APP", "orders.place")
	if q == nil || q.DeliveredPerSecond == nil || !near(q.GetDeliveredPerSecond(), 7) || q.GetMeasuredConnections() != 2 ||
		q.GetMoreConnectionRates() != 0 {
		t.Fatalf("orders.place = %v", q)
	}
	if got, want := ratesText(q.GetConnectionRates()), []string{"order-service nats-0 1 4.00", "order-service nats-0 2 3.00",
		"order-service nats-2 7 -"}; !slices.Equal(got, want) {
		t.Errorf("orders.place rates %v, want %v", got, want)
	}
	in := subjRow(res, "APP", api.InboxPattern)
	if in == nil || !near(in.GetDeliveredPerSecond(), 0.2) || in.GetMeasuredConnections() != 1 ||
		!slices.Equal(ratesText(in.GetConnectionRates()), []string{"api-gateway nats-0 4 0.20", "checkout-service nats-1 6 -"}) {
		t.Errorf("inbox = %v", in)
	}
	if w := subjRow(res, "SYS", ">"); w == nil || w.DeliveredPerSecond != nil || w.GetMeasuredConnections() != 0 || len(w.GetConnectionRates()) != 1 {
		t.Errorf("> = %v", w)
	}
	// A stream row has none.
	if o := subjRow(res, "APP", "orders"); o.DeliveredPerSecond != nil || len(o.GetConnectionRates()) != 0 {
		t.Errorf("orders = %v", o)
	}
	m := must(c.MatchSubject(ctx, connect.NewRequest(&v1.MatchSubjectRequest{Account: "APP", Subject: "orders.place"})))
	subs := api.MatchedSubscribers(m)
	if len(subs) != 1 || !near(subs[0].GetDeliveredPerSecond(), 7) || subs[0].GetMeasuredConnections() != 2 ||
		!slices.Equal(ratesText(subs[0].GetConnectionRates()), ratesText(q.GetConnectionRates())) {
		t.Errorf("tester = %v", subs)
	}
}

// Lists further apart than three subscription intervals give no rate; a
// longer interval lets them; a cut earlier list gives none.
func TestSubjectsDeliveredRateBounds(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	rowOf := func(p *storeprov.Provider) *v1.SubjectRow {
		_, c := apitest.Serve(t, p)
		return subjRow(must(c.GetSubjects(ctx, connect.NewRequest(&v1.GetSubjectsRequest{}))), "APP", "orders.place")
	}
	// Epoch 8 lies 20 minutes before 12: more than 3 times 5 minutes.
	if q := rowOf(deliveredProvider(t, last, subjCollection-4)); q.DeliveredPerSecond != nil || q.GetMeasuredConnections() != 0 {
		t.Errorf("20 minutes apart: %v", q)
	}
	// Three intervals of 10 minutes: 1,200 messages in 20 minutes is 1 a second.
	p := deliveredProvider(t, last, subjCollection-4)
	p.SubscriptionsInterval = 10 * time.Minute
	if q := rowOf(p); !near(q.GetDeliveredPerSecond(), 1.75) || q.GetMeasuredConnections() != 2 {
		t.Errorf("with a 10 minute interval: %v", q)
	}
	// Exactly three intervals apart still counts.
	if q := rowOf(deliveredProvider(t, last, subjCollection-3)); !near(q.GetDeliveredPerSecond(), 7.0/3) {
		t.Errorf("15 minutes apart: %v", q)
	}
	p = deliveredProvider(t, last, subjCollection-1)
	if _, err := p.St.DB().Exec(`UPDATE subscription_lists SET listed = total - 1 WHERE epoch = ? AND server_id = 'S0'`, subjCollection-1); err != nil {
		t.Fatal(err)
	}
	if q := rowOf(p); q.DeliveredPerSecond != nil {
		t.Errorf("earlier list cut: %v", q)
	}
	// The server's newest earlier list counts, not an older one.
	p = deliveredProvider(t, last, subjCollection-2)
	r := rows{t, p.St}
	r.add("subscription_lists", map[string]any{"epoch": subjCollection - 1, "server_id": "S0", "total": 1, "listed": 1})
	if q := rowOf(p); q.DeliveredPerSecond != nil {
		t.Errorf("newer list without the subscriptions: %v", q)
	}
}

// The store's SQL gives the rates api.DeliveredRates and
// api.GroupSubscriptions give.
func TestSubjectsDeliveredRatesAgreeWithRules(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	p := deliveredProvider(t, last, subjCollection-1)
	db := p.St.DB()
	read := func(epoch int64) []api.SubscriptionFact {
		var out []api.SubscriptionFact
		q, err := db.Query(`SELECT server_id, account, subject, coalesce(queue, ''), cid, subs, msgs FROM subscriptions WHERE epoch = ?`, epoch)
		if err != nil {
			t.Fatal(err)
		}
		defer q.Close()
		for q.Next() {
			var s api.SubscriptionFact
			if err := q.Scan(&s.Server, &s.Account, &s.Subject, &s.Queue, &s.CID, &s.Subs, &s.Msgs); err != nil {
				t.Fatal(err)
			}
			out = append(out, s)
		}
		return out
	}
	lists := func(epoch int64) map[string]api.ServerList {
		out := map[string]api.ServerList{}
		q, err := db.Query(`SELECT l.server_id, e.started_at, l.listed >= l.total FROM subscription_lists l JOIN epochs e USING (epoch) WHERE l.epoch = ?`, epoch)
		if err != nil {
			t.Fatal(err)
		}
		defer q.Close()
		for q.Next() {
			var id string
			var l api.ServerList
			if err := q.Scan(&id, &l.At, &l.Complete); err != nil {
				t.Fatal(err)
			}
			out[id] = l
		}
		return out
	}
	subs := api.DeliveredRates(read(subjCollection), read(subjCollection-1), lists(subjCollection), lists(subjCollection-1), api.RateGap(0))
	conns := map[string]api.ServerConnections{
		"S0": {Complete: true, Names: map[uint64]string{1: "order-service", 2: "order-service", 4: "api-gateway", 5: "inventory-service", 8: "mailer-service"}},
		"S1": {Complete: true, Names: map[uint64]string{6: "checkout-service", 3: "audit-tap"}},
		"S2": {Complete: false, Names: map[uint64]string{7: "order-service"}},
	}
	deliver := map[[2]string]bool{{"APP", "deliver.mailer"}: true, {"APP", "_INBOX.alerts1"}: true, {"APP", "_INBOX.w1"}: true}
	want := api.GroupSubscriptions(subs, conns, deliver)
	_, c := apitest.Serve(t, p)
	res := must(c.GetSubjects(ctx, connect.NewRequest(&v1.GetSubjectsRequest{})))
	server := map[string]string{"S0": "nats-0", "S1": "nats-1", "S2": "nats-2"}
	for _, g := range want.Groups {
		r := subjRow(res, g.Account, g.Subject)
		var rates []string
		for _, x := range g.Rates {
			v := "-"
			if x.PerSecond != nil {
				v = fmt.Sprintf("%.2f", *x.PerSecond)
			}
			rates = append(rates, fmt.Sprintf("%s %s %d %s", x.Name, server[x.Server], x.CID, v))
		}
		if r == nil || (r.DeliveredPerSecond == nil) != (g.PerSecond == nil) || (g.PerSecond != nil && !near(r.GetDeliveredPerSecond(), *g.PerSecond)) ||
			r.GetMeasuredConnections() != g.Measured || !slices.Equal(ratesText(r.GetConnectionRates()), rates) {
			t.Errorf("%s %s: store %v, rules %v %d %v", g.Account, g.Subject, r, g.PerSecond, g.Measured, rates)
		}
	}
}
