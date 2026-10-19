package fake_test

import (
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

// example-global's pool-us upgrade: three restarts and nats-us-4, which
// stops answering and cannot be scheduled.
func TestDemoReplayGlobal(t *testing.T) {
	res := replay(t, fake.GlobalSystem, at(8, 40, 0), at(9, 21, 0))
	byKind := map[v1.ReplayStepKind][]string{}
	for _, s := range res.GetSteps() {
		byKind[s.GetKind()] = append(byKind[s.GetKind()], s.GetTitle())
	}
	restarts := byKind[v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART]
	if len(restarts) != 4 || restarts[3] != "nats-us-4 stops answering" || res.GetCounts().GetServersStopped() != 1 ||
		res.GetCounts().GetServersRestarted() != 3 {
		t.Errorf("restart steps %q, counts %v", restarts, res.GetCounts())
	}
	if len(byKind[v1.ReplayStepKind_REPLAY_STEP_KIND_QUORUM]) == 0 || len(byKind[v1.ReplayStepKind_REPLAY_STEP_KIND_KUBERNETES]) == 0 {
		t.Errorf("steps = %v", byKind)
	}
}

// The demo has no gone assets yet, so it does not count them.
func TestDemoReplayLeavesGoneCountsUnset(t *testing.T) {
	c := replay(t, fake.Cluster, at(9, 10, 0), at(9, 55, 0)).GetCounts()
	if c.AssetsGoneWithDelete != nil || c.ConsumersGoneWithoutDelete != nil || c.ServersRestarted == nil || c.GetServersRestarted() != 3 {
		t.Errorf("counts = %v", c)
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

// Node events without a node name are not counted, as in the store; the
// demo's node events name no node yet.
func TestDemoReplayCountsNodesByName(t *testing.T) {
	c := replay(t, fake.Cluster, at(9, 10, 0), at(9, 55, 0)).GetCounts()
	if c.NodesDrained == nil || c.GetNodesDrained() != 0 || c.GetNodesUpgraded() != 0 {
		t.Errorf("counts = %v", c)
	}
}
