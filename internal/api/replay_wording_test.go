package api_test

import (
	"testing"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// A meta election that keeps the leader changes nothing: no step, no
// change counted.
func TestReplayMetaElectionKeepingTheLeaderIsNoStep(t *testing.T) {
	f := facts()
	f.EventsCaptured = true
	f.MetaLeaderAtA = "nats-eu-1"
	f.Events = []*v1.Event{ev(rAt(4, 30), "meta.leader_elected", "nats-eu-1")}
	res := api.FinishReplay(f)
	if ls := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_LEADERS); len(ls) != 0 || res.GetCounts().GetMetaLeaderChanges() != 0 {
		t.Errorf("leader steps %v, counts %v", ls, res.GetCounts())
	}
}
