package fake_test

import (
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The demo's day draws its restarts on the server lane, its elections on
// the leader lane and its client events as counts per bucket, none of
// them on the NATS lane.
func TestTimelineSubjectLanes(t *testing.T) {
	tl := must(client(t).GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{})))
	var restarts []string
	for _, e := range tl.GetServers() {
		if e.GetKind() == api.RestartKind && e.GetEventId() == "" {
			restarts = append(restarts, e.GetTime().AsTime().Format("15:04:05")+" "+e.GetLabel())
		}
	}
	if want := []string{"09:18:00 nats-0 restarted, version 2.12.2 unchanged", "09:31:02 nats-1 restarted, version 2.12.2 unchanged",
		"09:44:21 nats-2 restarted, version 2.12.2 unchanged"}; !slices.Equal(restarts, want) {
		t.Errorf("restarts = %v, want %v", restarts, want)
	}
	kinds := map[string]int{}
	for _, e := range tl.GetLeaders() {
		kinds[e.GetKind()]++
	}
	if kinds["meta.leader_elected"] != 2 || kinds["stream.leader_elected"] == 0 || kinds["consumer.leader_elected"] == 0 {
		t.Errorf("leader kinds = %v", kinds)
	}
	for _, e := range tl.GetNats() {
		if slices.Contains(api.LeaderLaneKinds, e.GetKind()) || slices.Contains(api.ServerLaneKinds, e.GetKind()) {
			t.Errorf("%s on the NATS lane too", e.GetKind())
		}
	}
	var drops, shutdowns, connects int64
	for _, b := range tl.GetClients() {
		drops, shutdowns, connects = drops+b.GetDrops(), shutdowns+b.GetShutdowns(), connects+b.GetConnects()
	}
	if drops == 0 || shutdowns == 0 || connects == 0 || !tl.GetSystemEventsCaptured() || !tl.GetJetstreamEventsCaptured() {
		t.Errorf("clients: %d connects, %d drops, %d shutdowns; captured system %v jetstream %v", connects, drops, shutdowns,
			tl.GetSystemEventsCaptured(), tl.GetJetstreamEventsCaptured())
	}

	// Before the demo's history no event was captured; the lanes say so.
	old := &v1.TimeRange{Start: timestamppb.New(fake.FixedNow.Add(-50 * 24 * time.Hour)), End: timestamppb.New(fake.FixedNow.Add(-49 * 24 * time.Hour))}
	none := must(client(t).GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{Range: old})))
	if none.GetSystemEventsCaptured() || none.GetJetstreamEventsCaptured() || len(none.GetClients())+len(none.GetLeaders())+len(none.GetServers()) != 0 {
		t.Errorf("before the history: %v", none)
	}
}

// example-staging stopped collecting three hours ago: its timeline says
// capture stopped up to its end; an entity's timeline has no subject
// lanes.
func TestTimelineCaptureGapsAndEntity(t *testing.T) {
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: fake.Demo(false)})
	tl := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{System: fake.StagingSystem})))
	gaps := tl.GetSystemCaptureGaps()
	if len(gaps) == 0 || !gaps[len(gaps)-1].GetStart().AsTime().Equal(fake.FixedNow.Add(-3*time.Hour)) || !gaps[len(gaps)-1].GetEnd().AsTime().Equal(fake.FixedNow) {
		t.Errorf("staging gaps = %v", gaps)
	}
	last := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{System: fake.StagingSystem,
		Range: &v1.TimeRange{Start: timestamppb.New(fake.FixedNow.Add(-2 * time.Hour)), End: timestamppb.New(fake.FixedNow)}})))
	if last.GetSystemEventsCaptured() || last.GetJetstreamEventsCaptured() {
		t.Errorf("last two hours of staging read as captured")
	}
	one := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{System: fake.Cluster,
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: "nats-1"}})))
	if len(one.GetServers())+len(one.GetLeaders())+len(one.GetClients()) != 0 || one.GetSystemEventsCaptured() {
		t.Errorf("nats-1 timeline has subject lanes")
	}
}
