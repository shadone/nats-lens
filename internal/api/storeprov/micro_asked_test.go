package storeprov_test

import (
	"math"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// expected applies api's Go rules to the reports the fixture wrote for one
// endpoint, for a page whose epoch started at epochAt and whose range ends
// at end: the window's increase over the instances of the epoch, the
// level over the 7 days before the window and, when slow, the slow run's
// start over the same days up to the epoch.
func (f *mcFixture) expected(key string, epochAt, end time.Time) (api.MicroIncrease, *float64, *time.Time) {
	from := end.Add(-api.RateSpan)
	cur := map[string]bool{}
	var win, lvl, slow []api.MicroReport
	for _, r := range f.reports[key] {
		if r.At.Equal(epochAt) {
			cur[r.Instance] = true
		}
		if !r.At.Before(from) && r.At.Before(end) {
			win = append(win, r)
		}
		if !r.At.Before(from.Add(-api.MicroLevelSpan)) && r.At.Before(from) {
			lvl = append(lvl, r)
		}
		if !r.At.Before(from.Add(-api.MicroLevelSpan)) && !r.At.After(epochAt) {
			slow = append(slow, r)
		}
	}
	w := api.MicroWindowIncrease(win, cur)
	var hourly []float64
	for _, h := range api.MicroHourlyAverages(api.MicroDeltas(lvl)) {
		hourly = append(hourly, h.Ms)
	}
	level := api.MicroLevel(hourly)
	var since *time.Time
	if w.Requests > 0 {
		avg := w.ProcessingNs / w.Requests / 1e6
		if api.MicroIsSlow(&avg, level) {
			since = api.MicroSlowSince(api.MicroCollectionAverages(api.MicroDeltas(slow)), *level)
		}
	}
	return w, level, since
}

func closeEnough(a, b float64) bool { return math.Abs(a-b) <= 1e-9*math.Max(1, math.Abs(b)) }

func optEqual(a, b *float64) bool {
	return (a == nil) == (b == nil) && (a == nil || closeEnough(*a, *b))
}

// The store's SQL gives what api's Go rules give over the same reports:
// the rate over each instance's newest run, the average time, the error
// share, the level and the slow run's start, at the newest epoch and at
// past ones (before the slow run, inside the hang, across the restart).
func TestMicroServicesAskedFollowTheRules(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	p, f := microConfProvider(t, last)
	_, c := apitest.Serve(t, p)
	checked := 0
	for _, back := range []time.Duration{0, 5 * time.Minute, 10 * time.Minute, 70 * time.Minute, 110 * time.Minute, 3 * time.Hour, 20 * time.Hour} {
		end := last.Add(30*time.Second - back)
		res, err := c.GetMicroServices(ctx, connect.NewRequest(&v1.GetMicroServicesRequest{
			Range: &v1.TimeRange{Start: timestamppb.New(end.Add(-time.Hour)), End: timestamppb.New(end)}}))
		if err != nil {
			t.Fatal(err)
		}
		epochAt := res.Msg.GetEpochAt().AsTime()
		for _, s := range res.Msg.GetServices() {
			for _, e := range s.GetEndpoints() {
				w, level, since := f.expected(s.GetName()+"/"+e.GetName(), epochAt, end)
				var avg, share *float64
				if w.Requests > 0 {
					avg, share = ptrTo(w.ProcessingNs/w.Requests/1e6), ptrTo(w.Errors/w.Requests)
				}
				if !optEqual(e.RequestsPerSecond, w.PerSecond) || !optEqual(e.AverageMs, avg) || !optEqual(e.ErrorShare, share) ||
					!optEqual(e.LevelMs, level) || (since == nil) != (e.SlowSince == nil) || (since != nil && !e.GetSlowSince().AsTime().Equal(*since)) {
					t.Errorf("%v before last, %s.%s: rate %v avg %v share %v level %v since %v; want %v %v %v %v %v", back, s.GetName(), e.GetName(),
						e.RequestsPerSecond, e.AverageMs, e.ErrorShare, e.LevelMs, e.GetSlowSince(), w.PerSecond, avg, share, level, since)
				}
				checked++
			}
		}
	}
	if checked < 20 {
		t.Errorf("checked %d endpoints", checked)
	}
}

func ptrTo(v float64) *float64 { return &v }

// The asked level's story at the newest epoch and in the hang.
func TestMicroServicesAskedFromStore(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	p, _ := microConfProvider(t, last)
	_, c := apitest.Serve(t, p)
	res, err := c.GetMicroServices(ctx, connect.NewRequest(&v1.GetMicroServicesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	m := res.Msg
	var names []string
	for _, s := range m.GetServices() {
		names = append(names, s.GetAccount()+"/"+s.GetName()+":"+s.GetNote().String())
	}
	// pricing takes 60 requests a second, orders 24.
	want := []string{"APP/pricing:MICRO_NOTE_MIXED_VERSIONS", "APP/orders:MICRO_NOTE_SLOW_ENDPOINT", "APP/labels:MICRO_NOTE_NO_ANSWER",
		"APP/inventory:MICRO_NOTE_HEALTHY", "PROD/catalog:MICRO_NOTE_NOT_ASKED"}
	if !slices.Equal(names, want) {
		t.Errorf("services\n%v\nwant\n%v", names, want)
	}
	o := microNamed(m, "APP", "orders")
	if o.GetInstances() != 2 || o.GetDescription() != "Order intake and lookup" || o.GetVersions()[0].GetVersion() != "1.4.2" ||
		!slices.Equal(o.GetConnectionNames(), []string{"order-service"}) || o.GetInstanceList()[0].GetStarted() == nil ||
		o.GetInstanceList()[0].GetServer() != "nats-0" || !o.GetAnswered() || o.GetAccountName() != "shop" {
		t.Errorf("orders = %v", o)
	}
	if get := o.GetEndpoints()[0]; get.GetName() != "get" || !get.GetSlow() || !get.GetSlowSince().AsTime().Equal(last.Add(-55*time.Minute)) ||
		get.GetSubject() != "orders.get.*" || get.GetQueueGroup() != "q" {
		t.Errorf("get = %v", get)
	}
	if create := o.GetEndpoints()[1]; create.GetLastError() != "409 duplicate order id" || create.GetLastErrorInstance() != "O2" ||
		math.Abs(create.GetErrorShare()-0.01) > 0.001 {
		t.Errorf("create = %v", create)
	}
	pr := microNamed(m, "APP", "pricing")
	if pr.GetInstances() != 3 || len(pr.GetMissing()) != 0 || pr.GetVersions()[0].GetInstances() != 2 || math.Abs(pr.GetRequestsPerSecond()-60) > 0.1 {
		t.Errorf("pricing = %v", pr)
	}
	if l := microNamed(m, "APP", "labels"); !l.GetAsked() || l.GetAnswered() || l.GetInstances() != 1 || l.GetConnectionNames()[0] != "label-printer" {
		t.Errorf("labels = %v", l)
	}
	if len(m.GetAccounts()) != 3 || m.GetAccounts()[0].GetAccount() != "APP" || m.GetAccounts()[0].GetStats().GetReplies() != 7 ||
		m.GetAccounts()[0].GetInfo().GetState() != v1.MicroRequestState_MICRO_REQUEST_STATE_OK || m.GetAccounts()[1].GetAccount() != "PROD" ||
		m.GetAccounts()[1].GetAsked() || m.GetAccounts()[2].GetStats().GetState() != v1.MicroRequestState_MICRO_REQUEST_STATE_ERROR ||
		m.GetAccounts()[2].GetStats().GetError() == "" {
		t.Errorf("accounts = %v", m.GetAccounts())
	}

	// In the hang: I2 missing since the first epoch without it, still
	// subscribed, by its connection.
	end := last.Add(-110 * time.Minute)
	hung, err := c.GetMicroServices(ctx, connect.NewRequest(&v1.GetMicroServicesRequest{Account: "APP",
		Range: &v1.TimeRange{Start: timestamppb.New(end.Add(-time.Hour)), End: timestamppb.New(end)}}))
	if err != nil {
		t.Fatal(err)
	}
	var inv *v1.MicroService
	for _, s := range hung.Msg.GetServices() {
		if s.GetName() == "inventory" {
			inv = s
		}
	}
	if inv == nil || inv.GetNote() != v1.MicroNote_MICRO_NOTE_INSTANCE_MISSING || inv.GetInstances() != 1 || len(inv.GetMissing()) != 1 {
		t.Fatalf("inventory in the hang = %v", inv)
	}
	if g := inv.GetMissing()[0]; g.GetId() != "I2" || !g.GetStillSubscribed() || !g.GetSince().AsTime().Equal(last.Add(-130*time.Minute)) ||
		!g.GetLastSeen().AsTime().Equal(last.Add(-135*time.Minute)) || g.GetConnectionName() != "inventory-service" {
		t.Errorf("missing = %v", g)
	}
}
