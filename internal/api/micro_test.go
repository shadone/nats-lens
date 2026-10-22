package api_test

import (
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

var microT0 = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

func at(sec int) time.Time { return microT0.Add(time.Duration(sec) * time.Second) }

func fptr(v float64) *float64 { return &v }

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestMicroInstanceOf(t *testing.T) {
	for _, c := range []struct {
		subject, name, id string
		ok                bool
	}{
		{"$SRV.PING.orders.ABC", "orders", "ABC", true},
		{"$SRV.STATS.orders.ABC", "orders", "ABC", true},
		{"$SRV.INFO.orders.ABC", "orders", "ABC", true},
		{"$SRV.PING.orders", "", "", false},
		{"$SRV.PING", "", "", false},
		{"$SRV.HELLO.orders.ABC", "", "", false},
		{"$SRV.PING.*.ABC", "", "", false},
		{"$SRV.PING.orders.>", "", "", false},
		{"$SRV.PING.orders.ABC.x", "", "", false},
		{"SRV.PING.orders.ABC", "", "", false},
		{"$SRV.PING..ABC", "", "", false},
	} {
		name, id, ok := api.MicroInstanceOf(c.subject)
		if name != c.name || id != c.id || ok != c.ok {
			t.Errorf("%q: %q %q %v", c.subject, name, id, ok)
		}
	}
}

// Seen services group an account's $SRV subscriptions by name and
// instance; the connection is named when CONNZ lists it, and a
// subscription of the same instance on a listed connection fills it in.
func TestSeenMicro(t *testing.T) {
	subs := []api.SubscriptionFact{
		{Server: "n1", Account: "APP", Subject: "$SRV.PING", CID: 5},
		{Server: "n1", Account: "APP", Subject: "$SRV.PING.orders", CID: 5},
		{Server: "n1", Account: "APP", Subject: "$SRV.PING.orders.B", CID: 5},
		{Server: "n1", Account: "APP", Subject: "$SRV.INFO.orders.B", CID: 5},
		{Server: "n2", Account: "APP", Subject: "$SRV.STATS.orders.A", CID: 9},
		{Server: "n3", Account: "APP", Subject: "$SRV.PING.pricing.C", CID: 1},
		{Server: "n1", Account: "OTHER", Subject: "$SRV.PING.orders.D", CID: 6},
		{Server: "n1", Account: "APP", Subject: "orders.create", Queue: "q", CID: 5},
	}
	conns := map[string]api.ServerConnections{
		"n1": {Complete: true, Names: map[uint64]string{5: "order-service", 6: ""}},
		"n2": {Complete: true, Names: map[uint64]string{9: "order-service"}},
	}
	got := api.SeenMicro(subs, conns)
	want := []api.MicroServiceFact{
		{Account: "APP", Name: "orders", Instances: []api.MicroInstanceFact{
			{ID: "A", Connection: "order-service", Server: "n2"}, {ID: "B", Connection: "order-service", Server: "n1"}}},
		{Account: "APP", Name: "pricing", Instances: []api.MicroInstanceFact{{ID: "C"}}},
		{Account: "OTHER", Name: "orders", Instances: []api.MicroInstanceFact{{ID: "D", Server: "n1"}}},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("seen =\n%v\nwant\n%v", got, want)
	}
}

// report is one instance's counters: req requests taking ms each, errs
// of them failed.
func report(id string, started time.Time, sec int, req, errs int64, ms float64) api.MicroReport {
	return api.MicroReport{Instance: id, Started: started, At: at(sec), Requests: req, Errors: errs, ProcessingNs: int64(float64(req) * ms * 1e6)}
}

// The rate is per instance of the epoch over its newest run, summed: a
// gone instance is left out, a restart (a new start time) or a counter
// that fell starts a new run, and a run of one report has no rate.
func TestMicroWindowIncrease(t *testing.T) {
	old, restart := at(-3600), at(400)
	reports := []api.MicroReport{
		// A: 10 a second throughout, 1 error in 10.
		report("A", old, 0, 1000, 100, 2), report("A", old, 300, 4000, 400, 2), report("A", old, 600, 7000, 700, 2),
		// B was replaced by C: left out.
		report("B", old, 0, 500, 0, 2), report("B", old, 300, 2000, 0, 2),
		// C started at 400 with B's share: 5 a second.
		report("C", restart, 450, 250, 0, 4), report("C", restart, 600, 1000, 0, 4),
		// D restarted in place at 400 with the same ID: its newest run
		// counts, 2 a second.
		report("D", old, 0, 9000, 0, 1), report("D", old, 300, 9600, 0, 1), report("D", restart, 450, 100, 0, 1), report("D", restart, 600, 400, 0, 1),
		// E's counter fell (a reset without a new start time): from 500 on.
		report("E", old, 0, 9000, 0, 1), report("E", old, 300, 50, 0, 1), report("E", old, 500, 250, 0, 1), report("E", old, 600, 350, 0, 1),
		// F answered once.
		report("F", restart, 600, 100, 0, 1),
	}
	cur := map[string]bool{"A": true, "C": true, "D": true, "E": true, "F": true}
	got := api.MicroWindowIncrease(reports, cur)
	if got.PerSecond == nil || !near(*got.PerSecond, 10+5+2+1) {
		t.Fatalf("rate = %v", got.PerSecond)
	}
	if !near(got.Requests, 6000+750+300+300) || !near(got.Errors, 600) {
		t.Errorf("requests %v, errors %v", got.Requests, got.Errors)
	}
	wantNs := 6000*2e6 + 750*4e6 + 300*1e6 + 100*1e6 + 200*1e6
	// E's run from 300 holds 50, 250, 350: an increase of 300 at 1 ms.
	if !near(got.ProcessingNs, wantNs) {
		t.Errorf("processing %v, want %v", got.ProcessingNs, wantNs)
	}
	if none := api.MicroWindowIncrease(reports, map[string]bool{"F": true}); none.PerSecond != nil || none.Requests != 0 {
		t.Errorf("one report = %+v", none)
	}
}

// The level is the median of the hourly averages, each the hour's
// processing time over its requests; fewer than 24 hours give none.
func TestMicroLevel(t *testing.T) {
	var reports []api.MicroReport
	start := microT0.Add(-30 * time.Hour)
	var req, ns int64
	for m := 0; m <= 30*60; m += 5 {
		ms := 4.0
		if m >= 25*60 {
			ms = 40 // the last 5 hours are slow
		}
		r := api.MicroReport{Instance: "A", Started: start, At: start.Add(time.Duration(m) * time.Minute), Requests: req, ProcessingNs: ns}
		reports = append(reports, r)
		req += 600
		ns += int64(600 * ms * 1e6)
	}
	hourly := api.MicroHourlyAverages(api.MicroDeltas(reports))
	if len(hourly) != 31 {
		t.Fatalf("%d hours", len(hourly))
	}
	var vals []float64
	for _, h := range hourly {
		vals = append(vals, h.Ms)
	}
	if l := api.MicroLevel(vals); l == nil || !near(*l, 4) {
		t.Errorf("level = %v", l)
	}
	if l := api.MicroLevel(vals[:23]); l != nil {
		t.Errorf("23 hours give level %v", *l)
	}
	if l := api.MicroLevel([]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24}); l == nil || *l != 12.5 {
		t.Errorf("even median = %v", l)
	}
}

// Slow is at least 3 times the level and at least 5 ms above it.
func TestMicroIsSlow(t *testing.T) {
	for _, c := range []struct {
		avg, level *float64
		slow       bool
	}{
		{fptr(41), fptr(4.1), true},
		{fptr(9), fptr(3), true},     // 3 times and 6 ms above
		{fptr(7.9), fptr(3), false},  // under 5 ms above
		{fptr(3), fptr(0.5), false},  // 6 times, 2.5 ms above
		{fptr(5.5), fptr(0.5), true}, // 11 times, 5 ms above
		{fptr(20), fptr(7), false},   // under 3 times
		{fptr(21), fptr(7), true},
		{nil, fptr(1), false},
		{fptr(100), nil, false},
	} {
		if got := api.MicroIsSlow(c.avg, c.level); got != c.slow {
			t.Errorf("avg %v level %v: %v", c.avg, c.level, got)
		}
	}
}

// The slow run is the run of collections, each slow, that ends at the
// newest slow one.
func TestMicroSlowSince(t *testing.T) {
	avgs := func(ms ...float64) []api.MicroAverage {
		var out []api.MicroAverage
		for i, v := range ms {
			out = append(out, api.MicroAverage{At: at(30 * i), Ms: v})
		}
		return out
	}
	for _, c := range []struct {
		ms   []float64
		want int
	}{
		{[]float64{4, 40, 4, 40, 41, 42}, 3},
		{[]float64{4, 40, 40, 40, 4}, 1},
		{[]float64{40, 40}, 0},
		{[]float64{4, 4}, -1},
		{nil, -1},
	} {
		got := api.MicroSlowSince(avgs(c.ms...), 4)
		switch {
		case c.want < 0 && got != nil:
			t.Errorf("%v: %v", c.ms, got)
		case c.want >= 0 && (got == nil || !got.Equal(at(30*c.want))):
			t.Errorf("%v: %v, want %v", c.ms, got, at(30*c.want))
		}
	}
	// Collection averages sum the instances' deltas of one collection.
	reports := []api.MicroReport{report("A", microT0, 0, 0, 0, 1), report("A", microT0, 30, 100, 0, 1), report("B", microT0, 0, 0, 0, 9),
		report("B", microT0, 30, 100, 0, 9)}
	if a := api.MicroCollectionAverages(api.MicroDeltas(reports)); len(a) != 1 || !near(a[0].Ms, 5) || !a[0].At.Equal(at(30)) {
		t.Errorf("collection averages %v", a)
	}
}

func microEndpoint(name string, rate, req, errs, msEach float64) api.MicroEndpointFact {
	return api.MicroEndpointFact{Name: name, Subject: "s." + name, QueueGroup: "q",
		Window: api.MicroIncrease{PerSecond: fptr(rate), Requests: req, Errors: errs, ProcessingNs: req * msEach * 1e6}}
}

func instances(version string, ids ...string) []api.MicroInstanceFact {
	var out []api.MicroInstanceFact
	for _, id := range ids {
		st := microT0.Add(-time.Hour)
		out = append(out, api.MicroInstanceFact{ID: id, Version: version, Description: "d", Started: &st})
	}
	return out
}

func findService(res *v1.GetMicroServicesResponse, account, name string) *v1.MicroService {
	for _, s := range res.GetServices() {
		if s.GetAccount() == account && s.GetName() == name {
			return s
		}
	}
	return nil
}

// FinishMicro merges the answered, seen and gone facts per service and
// applies the notes, the missing rule, the endpoints' rules and the order.
func TestFinishMicro(t *testing.T) {
	epoch := microT0
	subsAt := epoch.Add(-2 * time.Minute)
	slowSince := epoch.Add(-3 * time.Hour)
	f := &api.MicroFacts{
		Epoch: 7, EpochAt: epoch, SystemAccount: "SYS",
		Accounts: []api.MicroAccountFact{
			{Account: "APP", Info: api.MicroRequestFact{State: v1.MicroRequestState_MICRO_REQUEST_STATE_OK, At: subsAt, Replies: 9},
				Stats: api.MicroRequestFact{State: v1.MicroRequestState_MICRO_REQUEST_STATE_OK, At: epoch, Replies: 9}},
			{Account: "LOCKED", Stats: api.MicroRequestFact{State: v1.MicroRequestState_MICRO_REQUEST_STATE_ERROR, At: epoch, Error: "denied"}},
		},
		Answered: []api.MicroServiceFact{
			{Account: "APP", Name: "orders", Instances: instances("1.4.2", "O1", "O2", "O3"), Endpoints: []api.MicroEndpointFact{
				func() api.MicroEndpointFact {
					e := microEndpoint("get", 118, 1000, 0, 41)
					e.LevelMs, e.SlowSince = fptr(4.1), &slowSince
					return e
				}(),
				func() api.MicroEndpointFact {
					e := microEndpoint("create", 42, 1000, 1, 3.8)
					e.LevelMs, e.SlowSince = fptr(3.8), &slowSince // not slow: the start is dropped
					e.LastError, e.LastErrorInstance = "409 duplicate order id", "O2"
					return e
				}(),
				{Name: "idle", Subject: "s.idle"},
			}},
			{Account: "APP", Name: "pricing", Instances: append(instances("2.0.0", "P1", "P2", "P3"), instances("1.9.3", "P4")...),
				Endpoints: []api.MicroEndpointFact{microEndpoint("quote", 88, 100, 0, 2)}},
			{Account: "APP", Name: "inventory", Instances: instances("0.9.1", "I1"), Endpoints: []api.MicroEndpointFact{microEndpoint("stock", 6, 10, 0, 1)}},
			{Account: "APP", Name: "payments", Instances: instances("1.0.0", "Y1", "Y2"), Endpoints: []api.MicroEndpointFact{
				microEndpoint("a", 1, 100, 2, 1), microEndpoint("b", 1, 100, 5, 1), microEndpoint("c", 50, 100, 0, 1)}},
			{Account: "APP", Name: "rolled", Instances: instances("1.0.0", "R3", "R4")},
		},
		SubscriptionsAt: &subsAt,
		Seen: []api.MicroServiceFact{
			{Account: "APP", Name: "orders", Instances: []api.MicroInstanceFact{{ID: "O1", Connection: "order-service", Server: "n1"},
				{ID: "O2", Connection: "order-service", Server: "n2"}, {ID: "O3", Connection: "order-api", Server: "n3"}}},
			{Account: "APP", Name: "inventory", Instances: []api.MicroInstanceFact{{ID: "I1"}, {ID: "I2", Connection: "inv"}}},
			{Account: "APP", Name: "labels", Instances: []api.MicroInstanceFact{{ID: "L1", Connection: "label-printer"}}},
			{Account: "PROD", Name: "catalog", Instances: []api.MicroInstanceFact{{ID: "C1", Connection: "catalog-api"}}},
			{Account: "SYS", Name: "sysmon", Instances: []api.MicroInstanceFact{{ID: "S1"}}},
		},
		Gone: []api.MicroGoneFact{
			// Hung: still subscribed in a collection after it went missing.
			{Account: "APP", Service: "inventory", ID: "I2", LastSeen: epoch.Add(-10 * time.Minute), Since: epoch.Add(-9*time.Minute - 30*time.Second),
				Connection: "inv", InstancesThen: 2},
			// Replaced in a rolling restart: as many instances now.
			{Account: "APP", Service: "rolled", ID: "R1", LastSeen: epoch.Add(-20 * time.Minute), Since: epoch.Add(-19 * time.Minute), InstancesThen: 2},
			// Gone longer than the hour.
			{Account: "APP", Service: "rolled", ID: "R2", LastSeen: epoch.Add(-61 * time.Minute), Since: epoch.Add(-60 * time.Minute), InstancesThen: 3},
			// Scaled down from 2 to 1, gone with its connection.
			{Account: "PROD", Service: "catalog", ID: "C2", LastSeen: epoch.Add(-5 * time.Minute), Since: epoch, InstancesThen: 2},
			// A whole service gone, in an account not asked.
			{Account: "PROD", Service: "search", ID: "Z1", LastSeen: epoch.Add(-5 * time.Minute), Since: epoch, InstancesThen: 1},
		},
	}
	res := api.FinishMicro(f, "")
	var order []string
	for _, s := range res.GetServices() {
		order = append(order, s.GetAccount()+"/"+s.GetName()+":"+s.GetNote().String())
	}
	// Warning notes first, by rate (unset last), then name; the others
	// after, the system account's last.
	wantOrder := []string{
		"APP/orders:MICRO_NOTE_SLOW_ENDPOINT", "APP/pricing:MICRO_NOTE_MIXED_VERSIONS", "APP/payments:MICRO_NOTE_ERRORS",
		"APP/inventory:MICRO_NOTE_INSTANCE_MISSING", "PROD/catalog:MICRO_NOTE_INSTANCE_MISSING", "APP/labels:MICRO_NOTE_NO_ANSWER",
		"PROD/search:MICRO_NOTE_INSTANCE_MISSING", "APP/rolled:MICRO_NOTE_HEALTHY", "SYS/sysmon:MICRO_NOTE_NOT_ASKED",
	}
	if !slices.Equal(order, wantOrder) {
		t.Errorf("order =\n%v\nwant\n%v", order, wantOrder)
	}
	if res.GetServicesTotal() != 9 || res.GetEpoch() != 7 || res.GetSubscriptionsAt().AsTime() != subsAt {
		t.Errorf("total %d, epoch %d, subs %v", res.GetServicesTotal(), res.GetEpoch(), res.GetSubscriptionsAt())
	}

	o := findService(res, "APP", "orders")
	if !o.GetAsked() || !o.GetAnswered() || o.GetInstances() != 3 || o.GetDescription() != "d" || !near(o.GetRequestsPerSecond(), 160) ||
		!near(o.GetErrorShare(), 1.0/2000) || !slices.Equal(o.GetConnectionNames(), []string{"order-api", "order-service"}) {
		t.Errorf("orders = %v", o)
	}
	if in := o.GetInstanceList()[1]; in.GetId() != "O2" || in.GetConnectionName() != "order-service" || in.GetServer() != "n2" || in.GetStarted() == nil {
		t.Errorf("instance = %v", in)
	}
	var eps []string
	for _, e := range o.GetEndpoints() {
		eps = append(eps, e.GetName())
	}
	if !slices.Equal(eps, []string{"get", "create", "idle"}) || o.GetEndpointsTotal() != 3 {
		t.Errorf("endpoints %v", eps)
	}
	get, create, idle := o.GetEndpoints()[0], o.GetEndpoints()[1], o.GetEndpoints()[2]
	if !get.GetSlow() || !near(get.GetAverageMs(), 41) || get.GetSlowSince().AsTime() != slowSince || get.GetLevelMs() != 4.1 || get.GetErrorShare() != 0 {
		t.Errorf("get = %v", get)
	}
	if create.GetSlow() || create.GetSlowSince() != nil || create.GetLastError() != "409 duplicate order id" || create.GetLastErrorInstance() != "O2" ||
		!near(create.GetErrorShare(), 0.001) || create.GetErrorsHigh() {
		t.Errorf("create = %v", create)
	}
	if idle.RequestsPerSecond != nil || idle.AverageMs != nil || idle.ErrorShare != nil {
		t.Errorf("idle = %v", idle)
	}

	p := findService(res, "APP", "pricing")
	if len(p.GetVersions()) != 2 || p.GetVersions()[0].GetVersion() != "2.0.0" || p.GetVersions()[0].GetInstances() != 3 ||
		p.GetVersions()[1].GetVersion() != "1.9.3" || len(p.GetConnectionNames()) != 0 {
		t.Errorf("pricing = %v", p)
	}

	inv := findService(res, "APP", "inventory")
	if inv.GetInstances() != 1 || len(inv.GetMissing()) != 1 || inv.GetMissing()[0].GetId() != "I2" || !inv.GetMissing()[0].GetStillSubscribed() ||
		inv.GetMissing()[0].GetConnectionName() != "inv" {
		t.Errorf("inventory = %v", inv)
	}

	pay := findService(res, "APP", "payments")
	if pay.GetEndpoints()[0].GetName() != "b" || pay.GetEndpoints()[1].GetName() != "a" || !pay.GetEndpoints()[0].GetErrorsHigh() ||
		pay.GetEndpoints()[2].GetErrorsHigh() {
		t.Errorf("payments endpoints = %v", pay.GetEndpoints())
	}

	if r := findService(res, "APP", "rolled"); len(r.GetMissing()) != 0 || r.RequestsPerSecond != nil || r.ErrorShare != nil {
		t.Errorf("rolled = %v", r)
	}
	if l := findService(res, "APP", "labels"); !l.GetAsked() || l.GetAnswered() || l.GetInstances() != 1 || len(l.GetEndpoints()) != 0 {
		t.Errorf("labels = %v", l)
	}
	if c := findService(res, "PROD", "catalog"); c.GetAsked() || c.GetInstances() != 1 || len(c.GetMissing()) != 1 || c.GetMissing()[0].GetStillSubscribed() {
		t.Errorf("catalog = %v", c)
	}
	if s := findService(res, "PROD", "search"); s.GetInstances() != 0 || len(s.GetMissing()) != 1 {
		t.Errorf("search = %v", s)
	}
	if s := findService(res, "SYS", "sysmon"); !s.GetSystemAccount() {
		t.Errorf("sysmon = %v", s)
	}

	var accs []string
	for _, a := range res.GetAccounts() {
		accs = append(accs, fmt.Sprintf("%s:%v:%d:%v", a.GetAccount(), a.GetAsked(), a.GetServices(), a.GetStats().GetState()))
	}
	wantAccs := []string{"APP:true:6:MICRO_REQUEST_STATE_OK", "LOCKED:true:0:MICRO_REQUEST_STATE_ERROR",
		"PROD:false:2:MICRO_REQUEST_STATE_UNSPECIFIED", "SYS:false:1:MICRO_REQUEST_STATE_UNSPECIFIED"}
	if !slices.Equal(accs, wantAccs) {
		t.Errorf("accounts =\n%v\nwant\n%v", accs, wantAccs)
	}
	if a := res.GetAccounts()[0]; a.GetInfo().GetAt().AsTime() != subsAt || a.GetInfo().GetReplies() != 9 {
		t.Errorf("APP info = %v", a.GetInfo())
	}
	if a := res.GetAccounts()[2]; a.GetStats().GetAt() != nil {
		t.Errorf("PROD stats = %v", a.GetStats())
	}

	scoped := api.FinishMicro(f, "PROD")
	if scoped.GetServicesTotal() != 2 || len(scoped.GetAccounts()) != 1 || scoped.GetAccounts()[0].GetAccount() != "PROD" {
		t.Errorf("scoped = %v", scoped)
	}
	if e := api.FinishMicro(&api.MicroFacts{}, ""); e.GetEpoch() != 0 || len(e.GetServices()) != 0 || e.EpochAt != nil {
		t.Errorf("empty = %v", e)
	}
}

// A hung instance is missing only once a subscription collection taken
// after it went missing still lists it; before that, only the count says
// so. Without a subscription collection, the count alone decides.
func TestFinishMicroStillSubscribedNeedsALaterCollection(t *testing.T) {
	epoch := microT0
	early := epoch.Add(-4 * time.Minute)
	f := &api.MicroFacts{Epoch: 1, EpochAt: epoch,
		Accounts:        []api.MicroAccountFact{{Account: "APP", Stats: api.MicroRequestFact{State: v1.MicroRequestState_MICRO_REQUEST_STATE_OK, At: epoch}}},
		Answered:        []api.MicroServiceFact{{Account: "APP", Name: "s", Instances: instances("1", "A", "B2")}},
		SubscriptionsAt: &early,
		Seen:            []api.MicroServiceFact{{Account: "APP", Name: "s", Instances: []api.MicroInstanceFact{{ID: "A"}, {ID: "B"}}}},
		Gone:            []api.MicroGoneFact{{Account: "APP", Service: "s", ID: "B", LastSeen: epoch.Add(-90 * time.Second), Since: epoch.Add(-time.Minute), InstancesThen: 2}},
	}
	if s := api.FinishMicro(f, "").GetServices()[0]; len(s.GetMissing()) != 0 {
		t.Errorf("replaced before the collection: %v", s.GetMissing())
	}
	late := epoch.Add(-30 * time.Second)
	f.SubscriptionsAt = &late
	if s := api.FinishMicro(f, "").GetServices()[0]; len(s.GetMissing()) != 1 || !s.GetMissing()[0].GetStillSubscribed() {
		t.Errorf("still subscribed after: %v", s.GetMissing())
	}
}

// At most 200 services, 100 endpoints and 100 instances, with totals.
func TestFinishMicroCaps(t *testing.T) {
	f := &api.MicroFacts{Epoch: 1, EpochAt: microT0}
	for i := range 201 {
		f.Seen = append(f.Seen, api.MicroServiceFact{Account: "APP", Name: fmt.Sprintf("s%03d", i), Instances: []api.MicroInstanceFact{{ID: "x"}}})
	}
	big := api.MicroServiceFact{Account: "APP", Name: "big"}
	for i := range 101 {
		big.Endpoints = append(big.Endpoints, microEndpoint(fmt.Sprintf("e%03d", i), 1, 1, 0, 1))
		big.Instances = append(big.Instances, api.MicroInstanceFact{ID: fmt.Sprintf("i%03d", i), Connection: fmt.Sprintf("c%03d", i)})
	}
	f.Answered = []api.MicroServiceFact{big}
	res := api.FinishMicro(f, "")
	if len(res.GetServices()) != api.MaxMicroServices || res.GetServicesTotal() != 202 {
		t.Fatalf("%d services, total %d", len(res.GetServices()), res.GetServicesTotal())
	}
	b := res.GetServices()[0]
	if b.GetName() != "big" || len(b.GetEndpoints()) != 100 || b.GetEndpointsTotal() != 101 || len(b.GetInstanceList()) != 100 ||
		b.GetInstances() != 101 || len(b.GetConnectionNames()) != 3 || b.GetMoreNames() != 98 {
		t.Errorf("big: %d endpoints of %d, %d instances of %d, names %v +%d", len(b.GetEndpoints()), b.GetEndpointsTotal(),
			len(b.GetInstanceList()), b.GetInstances(), b.GetConnectionNames(), b.GetMoreNames())
	}
}

// An account recorded under its configured label, as when its lookup was
// refused, is the account the subscription collection shows for its
// instances: one service, asked, not two.
func TestFinishMicroLabelIsTheSeenAccount(t *testing.T) {
	const key = "ADKEYOFTHESHOPACCOUNT"
	f := &api.MicroFacts{Epoch: 1, EpochAt: microT0,
		Accounts: []api.MicroAccountFact{{Account: "shop", Stats: api.MicroRequestFact{State: v1.MicroRequestState_MICRO_REQUEST_STATE_OK, At: microT0}}},
		Answered: []api.MicroServiceFact{{Account: "shop", Name: "orders", Instances: instances("1", "O1", "O2")}},
		Seen: []api.MicroServiceFact{{Account: key, Name: "orders", Instances: []api.MicroInstanceFact{{ID: "O1", Connection: "order-service"},
			{ID: "O2"}}}},
	}
	res := api.FinishMicro(f, "")
	if len(res.GetServices()) != 1 || res.GetServices()[0].GetAccount() != key || !res.GetServices()[0].GetAnswered() ||
		res.GetServices()[0].GetConnectionNames()[0] != "order-service" || len(res.GetAccounts()) != 1 || !res.GetAccounts()[0].GetAsked() {
		t.Errorf("response = %v", res)
	}
	if s := api.FinishMicro(f, key); len(s.GetServices()) != 1 {
		t.Errorf("scoped to the key: %v", s)
	}
}
