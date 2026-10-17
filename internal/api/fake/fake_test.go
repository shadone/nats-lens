package fake_test

import (
	"context"
	"errors"
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
	if tl.GetCritical() != 1 || tl.GetWarning() != 2 || tl.GetInfo() != 1 || tl.GetMuted() != 1 || tl.GetFlapping() != 1 || tl.GetResolved_24H() != 8 {
		t.Fatalf("tally = %v", tl)
	}
	w := o.GetWorstFinding()
	if w.GetCheckCode() != "consumer-010" || w.GetCause().GetKind() != "consumer.deleted" || w.GetRemediation() == "" || len(w.GetRelatedConfig()) != 6 {
		t.Fatalf("worst = %v", w)
	}
	if got := o.GetServers()[0]; got.GetRole() != v1.ServerRole_SERVER_ROLE_META_LEADER || got.GetUptime().AsDuration() != 5*time.Hour+11*time.Minute+56*time.Second {
		t.Fatalf("nats-0 = %v", got)
	}
	if len(o.GetTiles()) != 9 || len(o.GetWindows()) != 1 || !o.GetWindows()[0].GetStart().AsTime().Equal(at(9, 14, 0)) {
		t.Fatalf("%d tiles, windows %v", len(o.GetTiles()), o.GetWindows())
	}
	want := map[string]float64{"servers": 3, "clients": 182, "meta_leader": 2, "consumers": 13}
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
	if got := list(&v1.ListFindingsRequest{}); got.GetTotalCount() != 4 || got.GetFindings()[0].GetCheckCode() != "consumer-010" {
		t.Fatalf("default: %d, first %s", got.GetTotalCount(), got.GetFindings()[0].GetCheckCode())
	}
	all := list(&v1.ListFindingsRequest{IncludeMuted: true})
	if all.GetTotalCount() != 5 || all.GetFindings()[4].GetMute().GetMutedBy() != "denis" {
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
	if len(seen) != 5 {
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
	if tl.GetWarning() != 1 || tl.GetMuted() != 2 {
		t.Fatalf("tally after mute = %v", tl)
	}
	if n := len(must(c.ListMutes(ctx, connect.NewRequest(&v1.ListMutesRequest{}))).GetMutes()); n != 2 {
		t.Fatalf("active mutes = %d", n)
	}
	un := must(c.UnmuteFinding(ctx, connect.NewRequest(&v1.UnmuteFindingRequest{MuteId: m.GetId(), UnmutedBy: "op"}))).GetMute()
	if un.GetUnmutedAt() == nil || un.GetUnmutedBy() != "op" {
		t.Fatalf("unmuted = %v", un)
	}
	if tl := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{}))).GetTally(); tl.GetWarning() != 2 || tl.GetMuted() != 1 {
		t.Fatalf("tally after unmute = %v", tl)
	}
	_, err := c.UnmuteFinding(ctx, connect.NewRequest(&v1.UnmuteFindingRequest{MuteId: m.GetId(), UnmutedBy: "op"}))
	if codeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("second unmute: %v", err)
	}
	if n := len(must(c.ListMutes(ctx, connect.NewRequest(&v1.ListMutesRequest{IncludeInactive: true}))).GetMutes()); n != 2 {
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
	if tl.GetBucketCount() != 96 || len(tl.GetRows()) != 11 {
		t.Fatalf("%d buckets, %d rows", tl.GetBucketCount(), len(tl.GetRows()))
	}
	first := tl.GetRows()[0]
	if first.GetCheckCode() != "consumer-010" || first.GetNow() != v1.Status_STATUS_CRITICAL || len(first.GetCells()) != 96 {
		t.Fatalf("first row %v", first)
	}
	if got := tl.GetRows()[4].GetNow(); got != v1.Status_STATUS_OK {
		t.Fatalf("fifth row now = %v, want ok (four fail now)", got)
	}
	if len(tl.GetKubernetes()) != 8 || len(tl.GetWindows()) != 1 || len(tl.GetNats()) != 8 {
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
	if f := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{}))).GetFreshness(); f.GetEpoch() != 1734 || f.GetAge().AsDuration() != 4*time.Second {
		t.Fatalf("now: %v", f)
	}
	past := &v1.TimeRange{Start: timestamppb.New(at(8, 0, 0)), End: timestamppb.New(at(10, 0, 0))}
	o := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{Range: past})))
	if o.GetFreshness().GetEpoch() != 0 || o.GetServers()[0].GetUptime().AsDuration() != 42*time.Minute {
		t.Fatalf("09:00-10:00: freshness %v, nats-0 uptime %v", o.GetFreshness(), o.GetServers()[0].GetUptime().AsDuration())
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
	if first.GetMutes()[0].GetId() != newest || first.GetTotalCount() != 2 {
		t.Fatalf("first page = %v", first)
	}
	mute("stream-002")
	second := must(c.ListMutes(ctx, connect.NewRequest(&v1.ListMutesRequest{PageSize: 1, PageToken: first.GetNextPageToken()})))
	if len(second.GetMutes()) != 1 || second.GetMutes()[0].GetCheckCode() != "stream-006" || second.GetNextPageToken() != "" {
		t.Fatalf("second page = %v", second)
	}
}
