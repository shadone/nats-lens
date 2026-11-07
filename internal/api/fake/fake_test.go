package fake_test

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
)

var ctx = context.Background()

func client(t *testing.T, opts ...connect.ClientOption) natslensv1connect.LensServiceClient {
	t.Helper()
	_, c := apitest.Serve(t, fake.New(false), opts...)
	return c
}

func at(h, m, s int) time.Time { return time.Date(2026, 10, 17, h, m, s, 0, time.UTC) }

func codeOf(err error) connect.Code {
	var ce *connect.Error
	if errors.As(err, &ce) {
		return ce.Code()
	}
	return connect.CodeUnknown
}

// must returns the message of a call that has to succeed; a failed call
// panics, which fails the test with the error and the caller's line.
func must[T any](res *connect.Response[T], err error) *T {
	if err != nil {
		panic(err)
	}
	return res.Msg
}

func TestOverviewReproducesTheReferenceScenario(t *testing.T) {
	o := must(client(t).GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{})))
	if o.GetCluster() != "example-prod" || len(o.GetServers()) != 3 {
		t.Fatalf("cluster %q, %d servers", o.GetCluster(), len(o.GetServers()))
	}
	if !o.GetRange().GetEnd().AsTime().Equal(fake.FixedNow) || !o.GetRange().GetStart().AsTime().Equal(fake.FixedNow.Add(-24*time.Hour)) {
		t.Fatalf("range = %v", o.GetRange())
	}
	tl := o.GetTally()
	if tl.GetCritical() != 1 || tl.GetWarning() != 4 || tl.GetInfo() != 1 || tl.GetMuted() != 2 || tl.GetFlapping() != 1 || tl.GetResolved_24H() != 8 {
		t.Fatalf("tally = %v", tl)
	}
	w := o.GetWorstFinding()
	if w.GetCheckCode() != "consumer-010" || w.GetCause().GetKind() != "consumer.deleted" || w.GetRemediation() == "" || len(w.GetRelatedConfig()) != 6 {
		t.Fatalf("worst = %v", w)
	}
	if got := o.GetServers()[0]; got.GetRole() != v1.ServerRole_SERVER_ROLE_META_LEADER || got.GetUptime().AsDuration() != 5*time.Hour+11*time.Minute+56*time.Second {
		t.Fatalf("nats-0 = %v", got)
	}
	if len(o.GetTiles()) != 9 || len(o.GetWindows()) != 1 || !o.GetWindows()[0].GetStart().AsTime().Equal(at(9, 16, 1)) {
		t.Fatalf("%d tiles, windows %v", len(o.GetTiles()), o.GetWindows())
	}
	want := map[string]float64{"servers": 3, "clients": 187, "meta_leader": 2, "consumers": 13}
	for _, tile := range o.GetTiles() {
		if v, ok := want[tile.GetId()]; ok && tile.GetValue() != v {
			t.Errorf("tile %s = %v, want %v", tile.GetId(), tile.GetValue(), v)
		}
		if n := len(tile.GetSeries().GetPoints()); n != 96 {
			t.Errorf("tile %s has %d points, want 96", tile.GetId(), n)
		}
	}
}

func TestListFindingsFiltersAndPages(t *testing.T) {
	c := client(t)
	list := func(req *v1.ListFindingsRequest) *v1.ListFindingsResponse {
		return must(c.ListFindings(ctx, connect.NewRequest(req)))
	}
	if got := list(&v1.ListFindingsRequest{}); got.GetTotalCount() != 6 || got.GetFindings()[0].GetCheckCode() != "consumer-010" {
		t.Fatalf("default: %d, first %s", got.GetTotalCount(), got.GetFindings()[0].GetCheckCode())
	}
	all := list(&v1.ListFindingsRequest{IncludeMuted: true})
	// Muted last: stream-006 under its mute, stream-005 on
	// KV_customer-config under its acceptance.
	if all.GetTotalCount() != 8 || all.GetFindings()[6].GetMute().GetMutedBy() != "alice" || !all.GetFindings()[7].GetMute().GetAccepted() {
		t.Fatalf("with muted: %v", all.GetFindings())
	}
	res := list(&v1.ListFindingsRequest{States: []v1.FindingState{v1.FindingState_FINDING_STATE_RESOLVED}})
	if res.GetTotalCount() != 8 {
		t.Fatalf("resolved = %d", res.GetTotalCount())
	}
	arch := list(&v1.ListFindingsRequest{Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "APP/order-archive"}})
	if arch.GetTotalCount() != 2 {
		t.Fatalf("stream order-archive and its consumers: %v", arch.GetFindings())
	}
	var seen []string
	token := ""
	for {
		p := list(&v1.ListFindingsRequest{IncludeMuted: true, PageSize: 2, PageToken: token})
		for _, f := range p.GetFindings() {
			seen = append(seen, f.GetId())
		}
		if token = p.GetNextPageToken(); token == "" {
			break
		}
	}
	if len(seen) != 8 {
		t.Fatalf("paged %v", seen)
	}
	_, err := c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{PageToken: "%%%"}))
	if codeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("bad token: %v", err)
	}
}

func TestGetFinding(t *testing.T) {
	c := client(t)
	f := must(c.GetFinding(ctx, connect.NewRequest(&v1.GetFindingRequest{Id: "consumer-002:APP/media-windows/media-chunk-pool"})))
	if f.GetFinding().GetState() != v1.FindingState_FINDING_STATE_FLAPPING || len(f.GetTransitions()) != 9 {
		t.Fatalf("consumer-002: %v, %d transitions", f.GetFinding().GetState(), len(f.GetTransitions()))
	}
	_, err := c.GetFinding(ctx, connect.NewRequest(&v1.GetFindingRequest{Id: "nope"}))
	if codeOf(err) != connect.CodeNotFound {
		t.Fatalf("unknown id: %v", err)
	}
}

func TestMuteRoundTrip(t *testing.T) {
	c := client(t)
	e := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: "APP/order-jobs/resize-worker"}
	req := connect.NewRequest(&v1.MuteFindingRequest{CheckCode: "consumer-009", Entity: e, Reason: "expected during the resync",
		ExpiresAt: timestamppb.New(fake.FixedNow.Add(time.Hour)), MutedBy: "op"})
	// Without IAP configured the unsigned IAP headers are ignored.
	req.Header().Set("X-Goog-Authenticated-User-Email", "accounts.google.com:ceo@example.com")
	m := must(c.MuteFinding(ctx, req)).GetMute()
	if m.GetMutedBy() != "op" || m.GetId() == "" {
		t.Fatalf("mute = %v", m)
	}
	tl := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{}))).GetTally()
	if tl.GetWarning() != 3 || tl.GetMuted() != 3 {
		t.Fatalf("tally after mute = %v", tl)
	}
	if n := len(must(c.ListMutes(ctx, connect.NewRequest(&v1.ListMutesRequest{}))).GetMutes()); n != 3 {
		t.Fatalf("active mutes = %d", n)
	}
	un := must(c.UnmuteFinding(ctx, connect.NewRequest(&v1.UnmuteFindingRequest{MuteId: m.GetId(), UnmutedBy: "op"}))).GetMute()
	if un.GetUnmutedAt() == nil || un.GetUnmutedBy() != "op" {
		t.Fatalf("unmuted = %v", un)
	}
	if tl := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{}))).GetTally(); tl.GetWarning() != 4 || tl.GetMuted() != 2 {
		t.Fatalf("tally after unmute = %v", tl)
	}
	_, err := c.UnmuteFinding(ctx, connect.NewRequest(&v1.UnmuteFindingRequest{MuteId: m.GetId(), UnmutedBy: "op"}))
	if codeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("second unmute: %v", err)
	}
	if n := len(must(c.ListMutes(ctx, connect.NewRequest(&v1.ListMutesRequest{IncludeInactive: true}))).GetMutes()); n != 3 {
		t.Fatalf("all mutes = %d", n)
	}
	for name, bad := range map[string]*v1.MuteFindingRequest{
		"no expiry":   {CheckCode: "x", Reason: "r", MutedBy: "op"},
		"past expiry": {CheckCode: "x", Reason: "r", MutedBy: "op", ExpiresAt: timestamppb.New(fake.FixedNow.Add(-time.Minute))},
		"no reason":   {CheckCode: "x", MutedBy: "op", ExpiresAt: timestamppb.New(fake.FixedNow.Add(time.Minute))},
		"no user":     {CheckCode: "x", Reason: "r", ExpiresAt: timestamppb.New(fake.FixedNow.Add(time.Minute))},
	} {
		if _, err := c.MuteFinding(ctx, connect.NewRequest(bad)); codeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestStatusTimeline(t *testing.T) {
	tl := must(client(t).GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{})))
	if tl.GetBucketCount() != 96 || len(tl.GetRows()) != 12 {
		t.Fatalf("%d buckets, %d rows", tl.GetBucketCount(), len(tl.GetRows()))
	}
	first := tl.GetRows()[0]
	if first.GetCheckCode() != "consumer-010" || first.GetNow() != v1.Status_STATUS_CRITICAL || len(first.GetCells()) != 96 {
		t.Fatalf("first row %v", first)
	}
	if got := tl.GetRows()[5].GetNow(); got != v1.Status_STATUS_OK {
		t.Fatalf("sixth row now = %v, want ok (five fail now)", got)
	}
	if len(tl.GetKubernetes()) != 15 || len(tl.GetWindows()) != 1 || len(tl.GetNats()) != 48 {
		t.Fatalf("lanes %d k8s, %d nats, %d windows", len(tl.GetKubernetes()), len(tl.GetNats()), len(tl.GetWindows()))
	}
	one := must(client(t).GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: "nats-1"},
	})))
	if len(one.GetRows()) != 2 {
		t.Fatalf("nats-1 rows = %v", one.GetRows())
	}
}

func TestFreshnessFollowsTheRange(t *testing.T) {
	c := client(t)
	if f := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{}))).GetFreshness(); f.GetEpoch() != 86400 || f.GetAge().AsDuration() != 4*time.Second {
		t.Fatalf("now: %v", f)
	}
	past := &v1.TimeRange{Start: timestamppb.New(at(8, 0, 0)), End: timestamppb.New(at(10, 0, 0))}
	o := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{Range: past})))
	// The range's newest epoch is the last before 10:00 on the 30-second
	// grid of the newest, 540 epochs back, as the store reports it.
	if f := o.GetFreshness(); f.GetEpoch() != 85860 || !f.GetEpochAt().AsTime().Equal(at(9, 59, 56)) ||
		o.GetServers()[0].GetUptime().AsDuration() != 41*time.Minute+56*time.Second { // as of that epoch
		t.Fatalf("08:00-10:00: freshness %v, nats-0 uptime %v", f, o.GetServers()[0].GetUptime().AsDuration())
	}
	// A range before the history holds no epoch.
	old := &v1.TimeRange{Start: timestamppb.New(fake.FixedNow.Add(-60 * 24 * time.Hour)), End: timestamppb.New(fake.FixedNow.Add(-40 * 24 * time.Hour))}
	if f := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{Range: old}))).GetFreshness(); f.GetEpoch() != 0 || f.GetEpochAt() != nil {
		t.Fatalf("before the history: freshness %v", f)
	}
}

func TestMutePagesKeepTheirPlace(t *testing.T) {
	c := client(t)
	mute := func(code string) string {
		return must(c.MuteFinding(ctx, connect.NewRequest(&v1.MuteFindingRequest{CheckCode: code, Reason: "r", MutedBy: "op",
			ExpiresAt: timestamppb.New(fake.FixedNow.Add(time.Hour))}))).GetMute().GetId()
	}
	newest := mute("stream-001")
	first := must(c.ListMutes(ctx, connect.NewRequest(&v1.ListMutesRequest{PageSize: 1})))
	if first.GetMutes()[0].GetId() != newest || first.GetTotalCount() != 3 {
		t.Fatalf("first page = %v", first)
	}
	mute("stream-002")
	second := must(c.ListMutes(ctx, connect.NewRequest(&v1.ListMutesRequest{PageSize: 1, PageToken: first.GetNextPageToken()})))
	if len(second.GetMutes()) != 1 || !second.GetMutes()[0].GetAccepted() || second.GetNextPageToken() == "" {
		t.Fatalf("second page = %v", second)
	}
	third := must(c.ListMutes(ctx, connect.NewRequest(&v1.ListMutesRequest{PageSize: 1, PageToken: second.GetNextPageToken()})))
	if len(third.GetMutes()) != 1 || third.GetMutes()[0].GetCheckCode() != "stream-006" || third.GetNextPageToken() != "" {
		t.Fatalf("third page = %v", third)
	}
}

// Epochs are numbered from 1 at the start of the history, 30 seconds
// apart: the newest of 30 days is 86400, and ranges far back still get
// numbers of their own. ListConnections names the range's epoch too.
func TestEpochNumbersCoverTheHistory(t *testing.T) {
	c := demo(t)
	epoch := func(sys string, end time.Time) int64 {
		t.Helper()
		r := &v1.TimeRange{Start: timestamppb.New(end.Add(-time.Hour)), End: timestamppb.New(end)}
		f := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{System: sys, Range: r}))).GetFreshness()
		conns := must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{System: sys, Range: r})))
		if conns.GetEpoch() != f.GetEpoch() {
			t.Errorf("%s ..%v: connections epoch %d, freshness %d", sys, end, conns.GetEpoch(), f.GetEpoch())
		}
		return f.GetEpoch()
	}
	seen := map[int64]time.Duration{}
	for _, d := range []time.Duration{0, 10 * time.Hour, 15 * time.Hour, 48 * time.Hour, 10 * 24 * time.Hour, 29 * 24 * time.Hour} {
		n := epoch(fake.Cluster, fake.FixedNow.Add(-d))
		if prev, ok := seen[n]; ok || n < 1 {
			t.Errorf("now-%v has epoch %d, as now-%v", d, n, prev)
		}
		seen[n] = d
	}
	// The first epoch of a 30-day history is 1.
	if n := epoch(fake.Cluster, fake.FixedNow.Add(-30*24*time.Hour+time.Minute)); n != 1 && n != 2 {
		t.Errorf("first minute of the history: epoch %d", n)
	}
	// example-staging's six hours of epochs end three hours ago.
	if n := epoch(fake.StagingSystem, fake.FixedNow.Add(-3*time.Hour+time.Minute)); n != 720 {
		t.Errorf("staging newest epoch %d, want 720", n)
	}
}

// An epoch is partial when a server was down then, by the rule the
// servers_up series uses: inside a restart, not before it.
func TestPartialEpochsFollowTheDownServers(t *testing.T) {
	c := demo(t)
	for _, x := range []struct {
		sys     string
		end     time.Time
		partial bool
	}{
		{fake.Cluster, at(9, 17, 0), true},       // nats-0 down 09:16:02-09:18:00
		{fake.Cluster, at(9, 30, 0), true},       // nats-1 down from 09:29:05
		{fake.Cluster, at(9, 0, 0), false},       // before the upgrade
		{fake.Cluster, at(12, 0, 0), false},      // after it
		{fake.GlobalSystem, at(8, 45, 30), true}, // nats-us-1 down 08:44:10-08:46:10
		{fake.GlobalSystem, at(8, 30, 0), false}, // before the us upgrade
		{fake.GlobalSystem, at(9, 12, 5), false}, // nats-us-4 answered at 09:12:05
		{fake.GlobalSystem, at(9, 13, 0), true},  // and not since
		{fake.StagingSystem, fake.FixedNow, false},
	} {
		r := &v1.TimeRange{Start: timestamppb.New(x.end.Add(-time.Hour)), End: timestamppb.New(x.end)}
		f := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{System: x.sys, Range: r}))).GetFreshness()
		if f.GetPartial() != x.partial {
			t.Errorf("%s ..%s: epoch at %v partial %v, want %v", x.sys, x.end.Format("15:04:05"), f.GetEpochAt().AsTime().Format("15:04:05"), f.GetPartial(), x.partial)
		}
		if x.sys == fake.StagingSystem {
			continue
		}
		// servers_up in the 30 seconds of the epoch agrees.
		at := f.GetEpochAt().AsTime()
		sr := &v1.TimeRange{Start: timestamppb.New(at.Add(-30 * time.Second)), End: timestamppb.New(at.Add(time.Second))}
		pts := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{System: x.sys, Metric: "cluster.servers_up", Range: sr, MaxPoints: 100}))).GetSeries().GetPoints()
		total := map[string]float64{fake.Cluster: 3, fake.GlobalSystem: 24}[x.sys]
		if len(pts) == 0 || (pts[len(pts)-1].GetValue() < total) != x.partial {
			t.Errorf("%s ..%s: servers_up %v of %v, partial %v", x.sys, x.end.Format("15:04:05"), pts, total, x.partial)
		}
	}
}

// ListConnections lists the connections of the range's newest epoch: none
// for a server down then, and nothing at all for a range without an
// epoch. The Fleet summary counts answering servers and connections at
// the same epoch, so partial means fewer answering than expected.
func TestConnectionsAndFleetFollowTheEpoch(t *testing.T) {
	c := demo(t)
	rng := func(end time.Time) *v1.TimeRange {
		return &v1.TimeRange{Start: timestamppb.New(end.Add(-time.Hour)), End: timestamppb.New(end)}
	}
	conns := func(end time.Time, server string) *v1.ListConnectionsResponse {
		return must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{System: fake.Cluster, Range: rng(end), Server: server})))
	}
	// nats-0 was down 09:16:02-09:18:00.
	if r := conns(at(9, 17, 0), "nats-0"); r.GetEpoch() == 0 || r.GetTotalCount() != 0 {
		t.Errorf("nats-0 at 09:17: epoch %d, %d connections, want none", r.GetEpoch(), r.GetTotalCount())
	}
	if r := conns(at(9, 17, 0), "nats-1"); r.GetTotalCount() != 43 {
		t.Errorf("nats-1 at 09:17: %d connections, want 43 (the unnamed script connects at 10:05)", r.GetTotalCount())
	}
	if r := conns(fake.FixedNow.Add(-40*24*time.Hour), ""); r.GetEpoch() != 0 || r.GetTotalCount() != 0 || len(r.GetConnections()) != 0 {
		t.Errorf("before the history: epoch %d, %d connections", r.GetEpoch(), r.GetTotalCount())
	}
	for _, end := range []time.Time{at(9, 17, 0), at(9, 30, 0), at(10, 0, 0), at(8, 0, 0)} {
		res := must(c.ListSystems(ctx, connect.NewRequest(&v1.ListSystemsRequest{Range: rng(end)})))
		for _, s := range res.GetSystems() {
			if s.GetSystem().GetId() != fake.Cluster {
				continue
			}
			if partial, fewer := s.GetFreshness().GetPartial(), s.GetServersAnswering() < s.GetServers(); partial != fewer {
				t.Errorf("..%s: partial %v, %d of %d answering", end.Format("15:04"), partial, s.GetServersAnswering(), s.GetServers())
			}
			if want := map[bool]int64{true: 85, false: 187}[s.GetServersAnswering() < s.GetServers()]; end.Equal(at(9, 17, 0)) && s.GetConnections() != want {
				t.Errorf("..09:17: %d connections, want %d", s.GetConnections(), want)
			}
		}
	}
}

// Servers and accounts read the range's newest epoch too: at 09:17 nats-0
// is down, so it is missing, not present, and its 100 connections are not
// counted; before 09:18 its summary is the process it restarted from.
// Without an epoch in the range the account's counts are unset.
func TestServersAndAccountsFollowTheEpoch(t *testing.T) {
	c := demo(t)
	r := &v1.TimeRange{Start: timestamppb.New(at(8, 17, 0)), End: timestamppb.New(at(9, 17, 0))}
	sv := must(c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{System: fake.Cluster, Range: r})))
	if strings.Join(sv.GetMissing(), ",") != "nats-0" {
		t.Errorf("missing at 09:17 = %v, want nats-0", sv.GetMissing())
	}
	for _, s := range sv.GetServers() {
		if (s.GetName() == "nats-0") == s.GetPresent() || s.GetUptime().AsDuration() <= 0 {
			t.Errorf("%s at 09:17: present %v, healthz %q, uptime %v", s.GetName(), s.GetPresent(), s.GetHealthz(), s.GetUptime().AsDuration())
		}
	}
	o := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{System: fake.Cluster, Range: r})))
	if o.GetServers()[0].GetPresent() || o.GetServers()[0].GetServerId() != "NB71FAKE0OLDPROCESS" {
		t.Errorf("overview nats-0 at 09:17 = %v", o.GetServers()[0])
	}
	acc := must(c.ListAccounts(ctx, connect.NewRequest(&v1.ListAccountsRequest{System: fake.Cluster, Range: r}))).GetAccounts()
	if len(acc) != 1 || acc[0].GetConnections() != 85 {
		t.Errorf("accounts at 09:17 = %v, want 85 connections", acc)
	}
	live := &v1.TimeRange{Start: timestamppb.New(fake.FixedNow.Add(-time.Hour)), End: timestamppb.New(fake.FixedNow)}
	acc = must(c.ListAccounts(ctx, connect.NewRequest(&v1.ListAccountsRequest{System: fake.StagingSystem, Range: live}))).GetAccounts()
	if len(acc) != 1 || acc[0].Connections != nil || acc[0].Streams != nil || acc[0].Consumers != nil {
		t.Errorf("staging accounts after its stop = %v, want counts unset", acc)
	}
}

// The demo reads the Kubernetes events of a window as the store does, up
// to 15 minutes after the range: a range ending at 09:00 sees example-
// global's upgrade end at nats-us-4's pod Killing, 09:12:00, not at 09:20:30.
func TestWindowsReadAsTheStoreReadsThem(t *testing.T) {
	o := must(demo(t).GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{System: fake.GlobalSystem,
		Range: &v1.TimeRange{Start: tsAt(8, 0, 0), End: tsAt(9, 0, 0)}})))
	if ws := o.GetWindows(); len(ws) != 1 || !ws[0].GetEnd().AsTime().Equal(at(9, 12, 0)) {
		t.Errorf("windows = %v", ws)
	}
}

// An acceptance through the API keeps accept and records the stream's
// growth: in the demo its Theil-Sen growth over the 8 days before now, not
// the store's week-over-week rule.
func TestAcceptRecordsGrowth(t *testing.T) {
	c := client(t)
	accept := func(e *v1.EntityRef) *v1.Mute {
		t.Helper()
		return must(c.MuteFinding(ctx, connect.NewRequest(&v1.MuteFindingRequest{CheckCode: "stream-005", Entity: e, Accept: true,
			Reason: "kept for the account's life", MutedBy: "ops@example.com", ExpiresAt: timestamppb.New(fake.FixedNow.Add(180 * 24 * time.Hour))}))).GetMute()
	}
	m := accept(&v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "APP/order-archive"})
	// order-archive grows 96 MiB a day in the scenario.
	if !m.GetAccepted() || m.GrowthPerDay == nil || math.Abs(m.GetGrowthPerDay()-96*(1<<20)) > 2*(1<<20) {
		t.Fatalf("acceptance = %v, want growth near 96 MiB/day", m)
	}
	f := must(c.GetFinding(ctx, connect.NewRequest(&v1.GetFindingRequest{Id: "stream-005:APP/order-archive"}))).GetFinding()
	if f.GetMute().GetId() != m.GetId() || !f.GetMute().GetAccepted() {
		t.Fatalf("finding mute = %v", f.GetMute())
	}
	if s := accept(&v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: "nats-0"}); s.GrowthPerDay != nil {
		t.Errorf("server acceptance in the demo records %v, want none", s.GetGrowthPerDay())
	}
}
