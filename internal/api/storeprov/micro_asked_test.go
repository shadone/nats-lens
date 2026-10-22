package storeprov_test

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// expected applies api's Go rules to what the fixture wrote for one
// endpoint, for a page whose epoch started at epochAt and whose range ends
// at end: the window's increase over the instances of the epoch; the
// level over the hourly rollup of the 7 days before the window; and, when
// slow, the slow run's start from the rollup's hours up to the epoch's
// hour and every instance's collections over api.MicroSlowSpan.
func (f *mcFixture) expected(key string, epochAt, end time.Time) (api.MicroIncrease, *float64, *time.Time) {
	from := end.Add(-api.RateSpan)
	cur := map[string]bool{}
	var win []api.MicroReport
	for _, r := range f.reports[key] {
		if r.At.Equal(epochAt) {
			cur[r.Instance] = true
		}
		if !r.At.Before(from) && r.At.Before(end) {
			win = append(win, r)
		}
	}
	w := api.MicroWindowIncrease(win, cur)
	var hourly []float64
	var upTo []api.MicroAverage
	for _, h := range f.hours[key] {
		if !h.At.Before(from.Add(-api.MicroLevelSpan)) && !h.At.Add(time.Hour).After(from) {
			hourly = append(hourly, h.Ms)
		}
		if !h.At.Before(epochAt.Add(-api.MicroLevelSpan)) && !h.At.After(epochAt) {
			upTo = append(upTo, h)
		}
	}
	level := api.MicroLevel(hourly)
	if w.Requests <= 0 {
		return w, level, nil
	}
	avg := w.ProcessingNs / w.Requests / 1e6
	if !api.MicroIsSlow(&avg, level) {
		return w, level, nil
	}
	from, to, ok := api.MicroSlowSpan(upTo, *level, epochAt)
	if !ok {
		return w, level, api.MicroSlowStart(upTo, nil, *level, epochAt)
	}
	var near []api.MicroReport
	for _, r := range f.reports[key] {
		if !r.At.Before(from.Add(-time.Hour)) && !r.At.After(epochAt) && r.At.Before(to) {
			near = append(near, r)
		}
	}
	var cols []api.MicroAverage
	for _, c := range api.MicroCollectionAverages(api.MicroDeltas(near)) {
		if !c.At.Before(from) {
			cols = append(cols, c)
		}
	}
	return w, level, api.MicroSlowStart(upTo, cols, *level, epochAt)
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
	slowCreate := false
	for _, back := range []time.Duration{0, 5 * time.Minute, 10 * time.Minute, 70 * time.Minute, 110 * time.Minute, 160 * time.Minute, 3 * time.Hour,
		20 * time.Hour} {
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
				slowCreate = slowCreate || (e.GetName() == "create" && e.GetSlowSince() != nil)
				var avg, share *float64
				if w.Requests > 0 {
					avg, share = ptrTo(w.ProcessingNs/w.Requests/1e6), ptrTo(w.Errors/w.Requests)
				}
				if !optEqual(e.RequestsPerSecond, w.PerSecond) || !optEqual(e.AverageMs, avg) || !optEqual(e.ErrorShare, share) ||
					!optEqual(e.LevelMs, level) || (since == nil) != (e.SlowSince == nil) || (since != nil && !e.GetSlowSince().AsTime().Equal(*since)) {
					t.Errorf("%v before last, %s.%s: rate %s avg %s share %s level %s since %v; want %s %s %s %s %v", back, s.GetName(), e.GetName(),
						optStr(e.RequestsPerSecond), optStr(e.AverageMs), optStr(e.ErrorShare), optStr(e.LevelMs), e.GetSlowSince().AsTime(),
						optStr(w.PerSecond), optStr(avg), optStr(share), optStr(level), since)
				}
				checked++
			}
		}
	}
	if !slowCreate {
		t.Error("create, on two subjects, is never slow: its slow run is not checked")
	}
	if checked < 20 {
		t.Errorf("checked %d endpoints", checked)
	}
}

func ptrTo(v float64) *float64 { return &v }

// optStr shows an optional number, or unset.
func optStr(v *float64) string {
	if v == nil {
		return "unset"
	}
	return fmt.Sprintf("%.6g", *v)
}

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
	// pricing takes 60 requests a second, orders 26, search 1.
	want := []string{"APP/pricing:MICRO_NOTE_MIXED_VERSIONS", "APP/orders:MICRO_NOTE_SLOW_ENDPOINT", "APP/search:MICRO_NOTE_SLOW_ENDPOINT", "APP/labels:MICRO_NOTE_NO_ANSWER",
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
	// create: 2 a second on orders.create, 1% failing, and 1 on
	// orders.create.v2, per instance.
	if create := o.GetEndpoints()[1]; create.GetLastError() != "409 duplicate order id" || create.GetLastErrorInstance() != "O2" ||
		math.Abs(create.GetErrorShare()-0.02/3) > 0.001 || math.Abs(create.GetRequestsPerSecond()-6) > 0.01 {
		t.Errorf("create = %v", create)
	}
	pr := microNamed(m, "APP", "pricing")
	if pr.GetInstances() != 3 || len(pr.GetMissing()) != 0 || pr.GetVersions()[0].GetInstances() != 2 || math.Abs(pr.GetRequestsPerSecond()-60) > 0.1 {
		t.Errorf("pricing = %v", pr)
	}
	// search was idle for three hours before its slow hour: the run starts
	// at its first collection after them.
	if q := microNamed(m, "APP", "search").GetEndpoints()[0]; !q.GetSlow() || !q.GetSlowSince().AsTime().Equal(last.Add(-55*time.Minute)) {
		t.Errorf("search.query = %v", q)
	}
	if l := microNamed(m, "APP", "labels"); !l.GetAsked() || l.GetAnswered() || l.GetInstances() != 1 || l.GetConnectionNames()[0] != "label-printer" {
		t.Errorf("labels = %v", l)
	}
	if len(m.GetAccounts()) != 3 || m.GetAccounts()[0].GetAccount() != "APP" || m.GetAccounts()[0].GetStats().GetReplies() != 8 ||
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

// a deploy 2 hours before last replaced the orders instances with
// new IDs and a new start. The endpoint's 7-day level should still come
// from its history (the plan: "the median of the hourly averages of the 7
// days before the 15 minutes" of the endpoint), so get is still slow.
func TestMicroLevelSurvivesRedeploy(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	p, f := microConfProvider(t, last)
	cut := last.Add(-2 * time.Hour)
	for _, tbl := range []string{"micro_endpoints", "micro_stats", "micro_instances"} {
		if _, err := f.st.DB().Exec(`UPDATE `+tbl+` SET instance_id = instance_id || 'old'
			WHERE service = 'orders' AND epoch IN (SELECT epoch FROM epochs WHERE started_at < ?)`, cut); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.st.DB().Exec(`UPDATE micro_stats SET started = ? WHERE service = 'orders'
		AND epoch IN (SELECT epoch FROM epochs WHERE started_at >= ?)`, cut, cut); err != nil {
		t.Fatal(err)
	}
	_, c := apitest.Serve(t, p)
	res, err := c.GetMicroServices(ctx, connect.NewRequest(&v1.GetMicroServicesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	o := microNamed(res.Msg, "APP", "orders")
	get := o.GetEndpoints()[0]
	for _, e := range o.GetEndpoints() {
		if e.GetName() == "get" {
			get = e
		}
	}
	if get.LevelMs == nil || !get.GetSlow() || get.GetSlowSince() == nil {
		t.Errorf("after a redeploy 2h ago: get level %v slow %v avg %v note %v", get.LevelMs, get.GetSlow(), get.AverageMs, o.GetNote())
	}
}

// the account lookup was refused, so the collector recorded APP's
// replies under its label. Unscoped, FinishMicro aliases the label to the
// seen key; scoped to the key, the page should say the same.
func TestMicroLabelScopedToKey(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	p, f := microConfProvider(t, last)
	for _, tbl := range []string{"micro_endpoints", "micro_stats", "micro_instances", "micro_requests", "micro_endpoint_hours"} {
		if _, err := f.st.DB().Exec(`UPDATE ` + tbl + ` SET account = 'app' WHERE account = 'APP'`); err != nil {
			t.Fatal(err)
		}
	}
	_, c := apitest.Serve(t, p)
	all, err := c.GetMicroServices(ctx, connect.NewRequest(&v1.GetMicroServicesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := c.GetMicroServices(ctx, connect.NewRequest(&v1.GetMicroServicesRequest{Account: "APP"}))
	if err != nil {
		t.Fatal(err)
	}
	a, s := microNamed(all.Msg, "APP", "orders"), microNamed(scoped.Msg, "APP", "orders")
	t.Logf("unscoped: asked %v answered %v note %v; scoped: asked %v answered %v note %v", a.GetAsked(), a.GetAnswered(), a.GetNote(),
		s.GetAsked(), s.GetAnswered(), s.GetNote())
	if a.GetNote() != s.GetNote() || a.GetAnswered() != s.GetAnswered() {
		t.Errorf("scoped to the key differs from unscoped")
	}
}

// The page does not depend on the database session's time zone: natslens
// may run on a laptop that is not in UTC. Every page end of the rule test
// gives the same answer in UTC, Auckland, Los Angeles and Berlin.
func TestMicroServicesZoneIndependent(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	p, f := microConfProvider(t, last)
	_, c := apitest.Serve(t, p)
	pages := func() []*v1.GetMicroServicesResponse {
		var out []*v1.GetMicroServicesResponse
		for _, back := range []time.Duration{0, 70 * time.Minute, 110 * time.Minute, 20 * time.Hour} {
			end := last.Add(30*time.Second - back)
			res, err := c.GetMicroServices(ctx, connect.NewRequest(&v1.GetMicroServicesRequest{
				Range: &v1.TimeRange{Start: timestamppb.New(end.Add(-time.Hour)), End: timestamppb.New(end)}}))
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, res.Msg)
		}
		return out
	}
	setZone := func(zone string) {
		t.Helper()
		if _, err := f.st.DB().Exec(`SET GLOBAL TimeZone = '` + zone + `'`); err != nil {
			t.Fatal(err)
		}
	}
	setZone("UTC")
	want := pages()
	if want[0].GetServices()[0].GetEndpoints()[0].GetSlowSince() == nil && want[0].GetServices()[1].GetEndpoints()[0].GetSlowSince() == nil {
		t.Fatal("no slow run to compare")
	}
	for _, zone := range []string{"Pacific/Auckland", "America/Los_Angeles", "Europe/Berlin"} {
		setZone(zone)
		for i, got := range pages() {
			if !proto.Equal(got, want[i]) {
				g, w := protojson.Format(got), protojson.Format(want[i])
				gl, wl := strings.Split(g, "\n"), strings.Split(w, "\n")
				for j := range min(len(gl), len(wl)) {
					if gl[j] != wl[j] {
						t.Errorf("%s, page %d, line %d: %s; in UTC: %s", zone, i, j, gl[j], wl[j])
						break
					}
				}
			}
		}
	}
	setZone("UTC")
}

// An instance's first STATS replies came before INFO gave the endpoint its
// subject: the rows without a subject and those with one are one series,
// as the hourly rollup pairs them, so the rate is the instance's, not
// twice it.
func TestMicroSubjectFirstAppearing(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	const n = 30
	at := func(e int64) time.Time { return last.Add(-time.Duration(n-e) * 30 * time.Second) }
	started := at(1).Add(-time.Hour)
	for e := int64(1); e <= n; e++ {
		if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, 1, 1, [], [], [], 0, false, [], NULL)`, e, at(e),
			at(e).Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		r.add("micro_requests", map[string]any{"epoch": e, "label": "app", "account": "APP", "request": "STATS", "state": "ok", "replies": 1})
		r.add("micro_stats", map[string]any{"epoch": e, "account": "APP", "service": "orders", "instance_id": "O1", "started": started})
		// 10 requests a second at 2 ms; the subject from the 20th epoch on.
		req := int64(at(e).Sub(started).Seconds() * 10)
		cols := map[string]any{"epoch": e, "account": "APP", "service": "orders", "instance_id": "O1", "endpoint": "get", "num_requests": req,
			"num_errors": 0, "processing_time_ns": req * 2_000_000, "average_processing_time_ns": 2_000_000}
		if e >= 20 {
			cols["subject"] = "orders.get"
		}
		r.add("micro_endpoints", cols)
	}
	clock := last.Add(30 * time.Second)
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Clock: func() time.Time { return clock },
		Retention: 14 * day})
	res, err := c.GetMicroServices(ctx, connect.NewRequest(&v1.GetMicroServicesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	e := res.Msg.GetServices()[0].GetEndpoints()[0]
	if math.Abs(e.GetRequestsPerSecond()-10) > 0.01 || math.Abs(e.GetAverageMs()-2) > 0.001 {
		t.Errorf("get: %.2f req/s at %.3f ms, want 10 at 2", e.GetRequestsPerSecond(), e.GetAverageMs())
	}
}
