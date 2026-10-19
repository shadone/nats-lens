package fake_test

import (
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func replay(t *testing.T, system string, a, b time.Time) *v1.GetReplayResponse {
	t.Helper()
	return must(demo(t).GetReplay(ctx, connect.NewRequest(&v1.GetReplayRequest{System: system, A: timestamppb.New(a),
		B: timestamppb.New(b)})))
}

// example-global's pool-us upgrade over its window: three restarts with
// their lame duck announcements and the leaders they gave up, two
// durables gone without a delete event after nats-us-2's restart, the
// quorum loss, nats-us-4 stopping with its bucket, and consumer-013
// resolving after B.
func TestDemoReplayGlobal(t *testing.T) {
	res := replay(t, fake.GlobalSystem, at(8, 40, 12), at(9, 20, 30))
	var titles []string
	for _, s := range res.GetSteps() {
		titles = append(titles, s.GetTitle())
	}
	want := []string{
		"Node gke-example-pool-us-17bd drained",
		"nats-us-1 restarts",
		"Node gke-example-pool-us-17bd upgraded",
		"Node gke-example-pool-us-a904 drained",
		"nats-us-2 restarts",
		"Node gke-example-pool-us-a904 upgraded",
		"2 durables on inventory-changes gone without a delete event",
		"Stream inventory-changes has no quorum for 2 min",
		"Node gke-example-pool-us-5e21 drained",
		"nats-us-3 restarts",
		"Node gke-example-pool-us-5e21 upgraded",
		"Node gke-example-pool-us-c3f8 drained",
		"nats-us-4 stops answering",
		"Stream KV_label-templates not reported at B",
		"2 leaders elected on nats-us-2",
		"The pod of nats-us-4 cannot be scheduled (5 times, 09:12–09:18)",
		"Stream missing on an assigned server: return-requests",
		"Durable consumer disappeared resolved for 2 consumers",
	}
	if strings.Join(titles, "\n") != strings.Join(want, "\n") {
		t.Errorf("steps:\n%s", strings.Join(titles, "\n"))
	}
	us2 := res.GetSteps()[4].GetBody()
	if us2 != "Lame duck mode announced at 08:56:21. Back at 08:58:56 with 2 streams and 3 consumers, 2 consumers fewer than before the restart. "+
		"Leaders it held were elected on other servers: 2 on nats-us-4." {
		t.Errorf("nats-us-2 body = %q", us2)
	}
	c := res.GetCounts()
	if c.GetServersRestarted() != 3 || c.GetServersStopped() != 1 || c.GetConsumersGoneWithoutDelete() != 2 || c.GetAssetsUnknownAtB() != 1 ||
		c.GetLeadersMoved() != 9 || c.MetaLeaderChanges == nil || c.GetMetaLeaderChanges() != 0 || c.GetNodesUpgraded() != 3 ||
		c.GetNodesDrained() != 4 || c.GetNodePoolsUpgraded() != 1 {
		t.Errorf("counts = %v", c)
	}
	gone := map[string]*v1.GoneAsset{}
	for _, g := range res.GetGone() {
		gone[g.GetEntity().GetKey()] = g
	}
	if g := gone["APP/inventory-changes/restock-alerts"]; g == nil || strings.Join(g.GetServersAtA(), ",") != "nats-us-0,nats-us-1,nats-us-2" ||
		g.GetDeleteEvent() != nil || len(g.GetFindingIds()) != 1 {
		t.Errorf("restock-alerts = %v", g)
	}
	if g := gone["APP/KV_label-templates"]; g == nil || strings.Join(g.GetNotAnsweringAtB(), ",") != "nats-us-4" {
		t.Errorf("KV_label-templates = %v", g)
	}
}

// example-prod's upgrade: analytics-aggregator, held by the three
// servers at A, was deleted by analytics-worker; nothing else is gone.
func TestDemoReplayProdGoneWithDelete(t *testing.T) {
	res := replay(t, fake.Cluster, at(9, 10, 0), at(9, 55, 0))
	c := res.GetCounts()
	if c.GetAssetsGoneWithDelete() != 1 || c.ConsumersGoneWithoutDelete == nil || c.GetConsumersGoneWithoutDelete() != 0 ||
		c.GetServersRestarted() != 3 || c.GetMetaLeaderChanges() != 1 {
		t.Errorf("counts = %v", c)
	}
	gone := res.GetGone()
	if len(gone) != 1 || gone[0].GetEntity().GetKey() != "APP/order-archive/analytics-aggregator" || len(gone[0].GetServersAtA()) != 3 ||
		gone[0].GetDeleteEvent() == nil || !strings.HasPrefix(gone[0].GetDeleteClient(), "analytics-worker") {
		t.Errorf("gone = %v", gone)
	}
}

// example-staging stopped collecting three hours before now: a gap open
// at B.
func TestDemoReplayStagingGap(t *testing.T) {
	res := replay(t, fake.StagingSystem, at(10, 30, 0), at(14, 30, 0))
	steps := res.GetSteps()
	if len(steps) != 1 || steps[0].GetKind() != v1.ReplayStepKind_REPLAY_STEP_KIND_GAP || !steps[0].GetOpenAtB() || res.GetEpochB() != 0 {
		t.Errorf("steps = %v, epoch B %d", steps, res.GetEpochB())
	}
}

// The last hour of the range of example-staging, whose data ended three
// hours ago: no collection near A or B, so one gap from A, open at B, as
// the store reads it.
func TestDemoReplayStagingGapAfterTheData(t *testing.T) {
	res := replay(t, fake.StagingSystem, at(13, 30, 0), at(14, 30, 0))
	steps := res.GetSteps()
	if len(steps) != 1 || steps[0].GetTitle() != "No collections since 13:30:00, still at B" || !steps[0].GetOpenAtB() {
		t.Errorf("steps = %v", steps)
	}
}

// Before example-staging's history starts there is no collection at A,
// so no leaders at A; it does not watch Kubernetes.
func TestDemoReplayFactsBeforeTheHistory(t *testing.T) {
	p := fake.Demo(false)[2].Provider
	f, err := p.Replay(ctx, &v1.GetReplayRequest{A: timestamppb.New(at(4, 0, 0)), B: timestamppb.New(at(6, 0, 0))})
	if err != nil {
		t.Fatal(err)
	}
	if f.EpochA != 0 || len(f.LeadersAtA) != 0 || f.Kubernetes {
		t.Errorf("epoch A %d, leaders at A %v, Kubernetes %v", f.EpochA, f.LeadersAtA, f.Kubernetes)
	}
}

// The demo's node events name their nodes and pools, so its node counts
// are set as the store's are: three nodes of pool-1 drained and upgraded.
func TestDemoReplayNodeCounts(t *testing.T) {
	c := replay(t, fake.Cluster, at(9, 10, 0), at(9, 55, 0)).GetCounts()
	if c.GetNodesDrained() != 3 || c.GetNodesUpgraded() != 3 || c.GetNodePoolsUpgraded() != 1 {
		t.Errorf("counts = %v", c)
	}
}

// A B one second after nats-us-1's new process started, before it was
// collected: nats-us-1 is a stop that answers again after B, as the store
// reads it.
func TestDemoReplayProcessCollectedAfterBIsAStop(t *testing.T) {
	res := replay(t, fake.GlobalSystem, at(8, 30, 0), at(8, 46, 11))
	var titles []string
	for _, s := range res.GetSteps() {
		if s.GetKind() == v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART || s.GetOutsideRange() {
			titles = append(titles, s.GetTitle())
		}
	}
	if len(titles) != 2 || titles[0] != "nats-us-1 stops answering" || titles[1] != "nats-us-1 answers again" ||
		res.GetCounts().GetServersStopped() != 1 || res.GetCounts().GetServersRestarted() != 0 {
		t.Errorf("steps %q, counts %v", titles, res.GetCounts())
	}
}

// The two durables gone after nats-us-2's restart are listed for a range
// that ends before it and not after, on Consumers and Placement alike,
// and their consumer-013 findings resolved an hour later.
func TestDemoVanishedDurablesFollowTheRange(t *testing.T) {
	c := demo(t)
	before := &v1.TimeRange{Start: tsAt(8, 0, 0), End: tsAt(8, 50, 0)}
	after := &v1.TimeRange{Start: tsAt(9, 0, 0), End: tsAt(14, 30, 0)}
	listed := func(r *v1.TimeRange) (consumers, placed bool) {
		for _, x := range must(c.ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{System: fake.GlobalSystem, Range: r,
			Stream: "inventory-changes"}))).GetConsumers() {
			consumers = consumers || x.GetEntity().GetConsumer() == "restock-alerts"
		}
		for _, s := range must(c.GetPlacement(ctx, connect.NewRequest(&v1.GetPlacementRequest{System: fake.GlobalSystem, Range: r}))).GetStreams() {
			for _, x := range s.GetConsumers() {
				placed = placed || x.GetEntity().GetConsumer() == "restock-alerts"
			}
		}
		return consumers, placed
	}
	if c1, p1 := listed(before); !c1 || !p1 {
		t.Errorf("before the restart: listed %v, placed %v", c1, p1)
	}
	if c2, p2 := listed(after); c2 || p2 {
		t.Errorf("after the restart: listed %v, placed %v", c2, p2)
	}
	f := must(c.GetFinding(ctx, connect.NewRequest(&v1.GetFindingRequest{System: fake.GlobalSystem,
		Id: "consumer-013:APP/inventory-changes/restock-alerts"}))).GetFinding()
	if f.GetState() != v1.FindingState_FINDING_STATE_RESOLVED || !f.GetResolvedAt().AsTime().Equal(at(9, 58, 56)) {
		t.Errorf("finding = %v", f)
	}
}
