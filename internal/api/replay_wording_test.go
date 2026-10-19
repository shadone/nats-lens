package api_test

import (
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

var ts = timestamppb.New

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

// A Kubernetes step's body never repeats its title or its one event's
// summary, which the evidence already shows; a pod failure keeps its
// reason, and a step inside a Kubernetes window names it.
func TestReplayKubernetesBodyAddsToTitle(t *testing.T) {
	f := facts()
	named := func(e *v1.Event) {
		e.Raw, e.Summary = `{"node":"gke-a","pool":"pool-1"}`, "node gke-a cordoned and drained"
	}
	unnamed := func(e *v1.Event) { e.Summary = "pool-1 upgrade complete, 3 of 3 nodes" }
	reason := func(e *v1.Event) { e.Summary = "pod nats-4: 0/4 nodes are available" }
	f.Events = []*v1.Event{
		ev(rAt(5, 0), "k8s.node.drained", "", named),
		ev(rAt(20, 0), "k8s.node.upgrade", "", unnamed),
		ev(rAt(40, 0), "k8s.pod.failed_scheduling", "nats-4", reason),
	}
	f.Windows = []*v1.TimeWindow{{Start: ts(rAt(4, 0)), End: ts(rAt(25, 0)), Label: "GKE node upgrade"}}
	ks := stepsOf(api.FinishReplay(f), v1.ReplayStepKind_REPLAY_STEP_KIND_KUBERNETES)
	if len(ks) != 3 {
		t.Fatalf("steps = %v", ks)
	}
	for i, s := range ks[:2] {
		body := strings.ToLower(s.GetBody())
		if strings.Contains(body, strings.ToLower(s.GetTitle())) || strings.Contains(body, strings.ToLower(f.Events[i].GetSummary())) {
			t.Errorf("step %q repeats itself: %q", s.GetTitle(), s.GetBody())
		}
		if !strings.Contains(s.GetBody(), "GKE node upgrade") {
			t.Errorf("step %q does not name its window: %q", s.GetTitle(), s.GetBody())
		}
	}
	if ks[2].GetBody() != "pod nats-4: 0/4 nodes are available" {
		t.Errorf("failure body = %q", ks[2].GetBody())
	}
}

// The evidence of a folded step tells its pieces apart.
func TestReplayEvidenceLabelsDistinct(t *testing.T) {
	f := facts()
	for i := range 5 {
		f.Events = append(f.Events, ev(rAt(12, 0).Add(time.Duration(i)*90*time.Second), "k8s.pod.failed_scheduling", "nats-4"))
	}
	f.Transitions = []api.ReplayTransition{
		tr("stream-001:APP/a", "stream-001", streamEnt("a"), rAt(30, 0), firing, critical),
		tr("stream-001:APP/b", "stream-001", streamEnt("b"), rAt(30, 0), firing, critical),
	}
	for _, s := range api.FinishReplay(f).GetSteps() {
		seen := map[string]bool{}
		for _, e := range s.GetEvidence() {
			if seen[e.GetLabel()] {
				t.Errorf("step %q: label %q twice", s.GetTitle(), e.GetLabel())
			}
			seen[e.GetLabel()] = true
		}
	}
}

// A gap open at B names no time after B: neither its body nor its
// evidence, which leaves out the first collection after B.
func TestReplayOpenGapNamesNothingAfterB(t *testing.T) {
	f := facts()
	gapEv := ev(rAt(50, 0), "collector.gap", "", func(e *v1.Event) {
		e.Summary = "no capture for 40m, from 09:50:00 to 10:30:00"
		e.ReceivedAt = ts(rAt(90, 0))
	})
	f.Gaps = []api.ReplayGap{
		{Kind: api.GapCollector, Start: rAt(50, 0), End: rAt(90, 0), Event: gapEv},
		{Kind: api.GapCollections, Start: rAt(50, 0), End: rAt(90, 0), BeforeEpoch: 160, BeforeAt: rAt(50, 0), AfterEpoch: 161, AfterAt: rAt(90, 0)},
	}
	gs := stepsOf(api.FinishReplay(f), v1.ReplayStepKind_REPLAY_STEP_KIND_GAP)
	if len(gs) != 1 {
		t.Fatalf("gaps = %v", gs)
	}
	g := gs[0]
	text := g.GetBody()
	for _, e := range g.GetEvidence() {
		text += " " + e.GetLabel()
		if e.GetTime().AsTime().After(f.B) {
			t.Errorf("evidence after B: %v", e)
		}
	}
	if strings.Contains(text, "10:30") || g.GetBody() == "" {
		t.Errorf("gap names a time after B: body %q, evidence %v", g.GetBody(), g.GetEvidence())
	}
	if g.GetEvidenceCount() != 2 {
		t.Errorf("evidence = %v; want the gap event and the collection before", g.GetEvidence())
	}
}

// A restart step is a warning, as a stop is.
func TestReplayRestartIsAWarning(t *testing.T) {
	f := facts()
	f.Restarts = []api.ReplayRestart{{Server: "nats-1", LastSeen: rAt(10, 0), FirstSeen: rAt(11, 30), OldEpoch: 120, NewEpoch: 123}}
	rs := stepsOf(api.FinishReplay(f), v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART)
	if len(rs) != 1 || rs[0].GetSeverity() != warning {
		t.Errorf("restart steps = %v", rs)
	}
}
