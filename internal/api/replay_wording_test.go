package api_test

import (
	"fmt"
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
	if ks[2].GetBody() != "0/4 nodes are available." {
		t.Errorf("failure body = %q", ks[2].GetBody())
	}
}

// A pod failure's body is its reason as a sentence, from the Event's note
// when the payload has one, then the window it lies in.
func TestReplayPodFailureReasonInAWindow(t *testing.T) {
	f := facts()
	f.Events = []*v1.Event{
		ev(rAt(10, 0), "k8s.pod.failed_scheduling", "nats-4", func(e *v1.Event) {
			e.Summary = "pod nats-4 FailedScheduling: 0/4 nodes are available: 4 Insufficient memory (x3)"
			e.Raw = `{"reason":"FailedScheduling","note":"0/4 nodes are available: 4 Insufficient memory"}`
		}),
		ev(rAt(40, 0), "k8s.pod.backoff", "nats-4", func(e *v1.Event) { e.Summary = "pod nats-4 BackOff: back-off restarting failed container (x2)" }),
	}
	f.Windows = []*v1.TimeWindow{{Start: ts(rAt(4, 0)), End: ts(rAt(25, 0)), Label: "GKE node upgrade"}}
	ks := stepsOf(api.FinishReplay(f), v1.ReplayStepKind_REPLAY_STEP_KIND_KUBERNETES)
	if len(ks) != 2 || ks[0].GetBody() != "0/4 nodes are available: 4 Insufficient memory. Within the GKE node upgrade window, 09:04–09:25." ||
		ks[1].GetBody() != "Back-off restarting failed container." {
		t.Errorf("steps = %v", ks)
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

// Resolves folded at one time say that time once.
func TestReplayFoldedResolvesAtOneTime(t *testing.T) {
	f := facts()
	f.Transitions = []api.ReplayTransition{
		tr("consumer-013:APP/o/a", "consumer-013", consumerEnt("o", "a"), rAt(10, 0), firing, critical),
		tr("consumer-013:APP/o/b", "consumer-013", consumerEnt("o", "b"), rAt(10, 0), firing, critical),
		tr("consumer-013:APP/o/a", "consumer-013", consumerEnt("o", "a"), rAt(30, 0), resolved, critical),
		tr("consumer-013:APP/o/b", "consumer-013", consumerEnt("o", "b"), rAt(30, 0), resolved, critical),
	}
	rs := stepsOf(api.FinishReplay(f), v1.ReplayStepKind_REPLAY_STEP_KIND_RESOLVED)
	if len(rs) != 1 || !strings.HasSuffix(rs[0].GetBody(), "Resolved at 09:30:00.") {
		t.Errorf("resolved = %v", rs)
	}
}

// A stopped server's leaders elected elsewhere go into its stop step,
// as a restart's do.
func TestReplayStopTakesInItsLeaders(t *testing.T) {
	f := facts()
	f.EventsCaptured = true
	f.Stops = []api.ReplayStop{{Server: "nats-4", LastSeen: rAt(10, 0), LastEpoch: 120, LedBefore: []*v1.EntityRef{streamEnt("orders")}}}
	f.Events = []*v1.Event{
		ev(rAt(10, 5), "server.shutdown", "nats-4"),
		ev(rAt(10, 6), "stream.leader_elected", "nats-2", onAsset("APP", "orders", "")),
	}
	res := api.FinishReplay(f)
	rs := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART)
	if len(rs) != 1 || !strings.Contains(rs[0].GetBody(), "Leaders it held were elected on other servers: 1 on nats-2.") {
		t.Errorf("stop = %v", rs)
	}
	if ls := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_LEADERS); len(ls) != 0 {
		t.Errorf("leader steps = %v", ls)
	}
}

// Meta elections that all keep the leader make no step, folded or not; a
// meta step has a body.
func TestReplayMetaStepsOnlyForChanges(t *testing.T) {
	f := facts()
	f.EventsCaptured = true
	f.MetaLeaderAtA = "nats-1"
	f.Events = []*v1.Event{ev(rAt(4, 30), "meta.leader_elected", "nats-1"), ev(rAt(5, 0), "meta.leader_elected", "nats-1")}
	if ls := stepsOf(api.FinishReplay(f), v1.ReplayStepKind_REPLAY_STEP_KIND_LEADERS); len(ls) != 0 {
		t.Errorf("leader steps for elections that keep the leader: %v", ls)
	}
	f.Events = append(f.Events, ev(rAt(30, 0), "meta.leader_elected", "nats-0"))
	ls := stepsOf(api.FinishReplay(f), v1.ReplayStepKind_REPLAY_STEP_KIND_LEADERS)
	if len(ls) != 1 || ls[0].GetBody() == "" {
		t.Errorf("meta move = %v", ls)
	}
}

// Folded elections that all moved the lead say so plainly.
func TestReplayFoldedElectionsAllMoved(t *testing.T) {
	f := facts()
	f.LeadersAtA = []api.ReplayLeader{{Entity: streamEnt("a"), Server: "nats-1"}, {Entity: streamEnt("b"), Server: "nats-1"}}
	f.Events = []*v1.Event{
		ev(rAt(10, 0), "stream.leader_elected", "nats-2", onAsset("APP", "a", "")),
		ev(rAt(10, 1), "stream.leader_elected", "nats-2", onAsset("APP", "b", "")),
	}
	ls := stepsOf(api.FinishReplay(f), v1.ReplayStepKind_REPLAY_STEP_KIND_LEADERS)
	if len(ls) != 1 || !strings.Contains(ls[0].GetBody(), "Both moved the lead from another server.") {
		t.Errorf("leaders = %v", ls)
	}
}

// Folded gone durables of one stream are named without the stream the
// title already names.
func TestReplayFoldedGoneNamesOnce(t *testing.T) {
	f := facts()
	f.GoneRead = true
	for _, n := range []string{"x", "y"} {
		f.Gone = append(f.Gone, api.ReplayGone{Entity: consumerEnt("orders", n), ServersAtA: []string{"nats-0", "nats-1"}, LastReported: rAt(20, 0),
			LastEpoch: 140})
	}
	gs := stepsOf(api.FinishReplay(f), v1.ReplayStepKind_REPLAY_STEP_KIND_GONE)
	if len(gs) != 1 || strings.Count(gs[0].GetTitle()+" "+gs[0].GetBody(), "orders") != 1 ||
		!strings.Contains(gs[0].GetBody(), "Held by nats-0 and nats-1 at A, by none at B.") {
		t.Errorf("gone = %v", gs)
	}
}

// Event counts follow event capture, not the collections: a time without
// collections or none at A or B leaves them set, a connection that
// captures events without a server unsets them.
func TestReplayEventCountsFollowEventCapture(t *testing.T) {
	for name, change := range map[string]func(*api.ReplayFacts){
		"no collection at A": func(f *api.ReplayFacts) { f.EpochA, f.EpochAAt = 0, time.Time{} },
		"no collection at B": func(f *api.ReplayFacts) { f.EpochB, f.EpochBAt = 0, time.Time{} },
		"collections gap": func(f *api.ReplayFacts) {
			f.Gaps = []api.ReplayGap{{Kind: api.GapCollections, Start: rAt(20, 0), End: rAt(30, 0), BeforeEpoch: 140, BeforeAt: rAt(20, 0)}}
		},
	} {
		f := facts()
		f.EventsCaptured = true
		change(f)
		if c := api.FinishReplay(f).GetCounts(); c.LeaderElections == nil || c.ClientReconnects == nil {
			t.Errorf("%s: counts %v", name, c)
		}
	}
	f := facts()
	f.EventsCaptured = true
	f.Gaps = []api.ReplayGap{{Kind: api.GapEvents, Start: rAt(20, 0), Event: ev(rAt(20, 0), "collector.connection_closed", "")}}
	res := api.FinishReplay(f)
	if c := res.GetCounts(); c.LeaderElections != nil || c.ClientReconnects != nil {
		t.Errorf("events connection lost: counts %v", c)
	}
	gs := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_GAP)
	if len(gs) != 1 || gs[0].GetGapKind() != v1.ReplayGapKind_REPLAY_GAP_KIND_EVENTS || gs[0].GetTitle() != "Events not captured since 09:20:00, still at B" {
		t.Errorf("gap steps %v", gs)
	}
}

// Every gap step names its kind; other steps none.
func TestReplayGapStepsNameTheirKind(t *testing.T) {
	f := facts()
	f.Gaps = []api.ReplayGap{
		{Kind: api.GapCollections, Start: rAt(5, 0), End: rAt(8, 0), BeforeEpoch: 110, BeforeAt: rAt(5, 0), AfterEpoch: 111, AfterAt: rAt(8, 0)},
		{Kind: api.GapCollector, Start: rAt(20, 0), End: rAt(30, 0), Event: ev(rAt(20, 0), "collector.gap", "")},
		{Kind: api.GapKubernetes, Start: rAt(40, 0), End: rAt(45, 0), Event: ev(rAt(40, 0), "collector.k8s_gap", "")},
	}
	f.Events = []*v1.Event{ev(rAt(50, 0), "k8s.node.drained", "")}
	want := map[string]v1.ReplayGapKind{"No collections": v1.ReplayGapKind_REPLAY_GAP_KIND_COLLECTIONS,
		"nats-lens": v1.ReplayGapKind_REPLAY_GAP_KIND_COLLECTOR, "Kubernetes": v1.ReplayGapKind_REPLAY_GAP_KIND_KUBERNETES}
	got := map[v1.ReplayGapKind]bool{}
	for _, s := range api.FinishReplay(f).GetSteps() {
		if s.GetKind() != v1.ReplayStepKind_REPLAY_STEP_KIND_GAP {
			if s.GetGapKind() != v1.ReplayGapKind_REPLAY_GAP_KIND_UNSPECIFIED {
				t.Errorf("%q has gap kind %v", s.GetTitle(), s.GetGapKind())
			}
			continue
		}
		for prefix, k := range want {
			if strings.HasPrefix(s.GetTitle(), prefix) && s.GetGapKind() != k {
				t.Errorf("%q has gap kind %v, want %v", s.GetTitle(), s.GetGapKind(), k)
			}
		}
		got[s.GetGapKind()] = true
	}
	if len(got) != 3 || got[v1.ReplayGapKind_REPLAY_GAP_KIND_UNSPECIFIED] {
		t.Errorf("gap kinds %v", got)
	}
}

// Election evidence names the server it elected, other evidence none; a
// step gives its elections by new leader, all of them, beyond the
// evidence's 10.
func TestReplayElectionEvidenceNamesTheLeader(t *testing.T) {
	f := facts()
	f.EventsCaptured, f.Collected = true, true
	var led []*v1.EntityRef
	for i := range 12 {
		led = append(led, streamEnt(fmt.Sprintf("s%d", i)))
	}
	f.Restarts = []api.ReplayRestart{{Server: "nats-1", LastSeen: rAt(10, 0), FirstSeen: rAt(11, 30), OldEpoch: 120, NewEpoch: 123, LedBefore: led}}
	for i := range 12 {
		server := "nats-0"
		if i%3 == 0 {
			server = "nats-2"
		}
		f.Events = append(f.Events, ev(rAt(10, 5).Add(time.Duration(i)*time.Second), "stream.leader_elected", server, onAsset("APP", fmt.Sprintf("s%d", i), "")))
	}
	f.Events = append(f.Events, ev(rAt(30, 0), "meta.leader_elected", "nats-2"))
	f.MetaLeaderAtA = "nats-1"
	res := api.FinishReplay(f)
	r := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART)[0]
	var got []string
	for _, l := range r.GetLeadersTaken() {
		got = append(got, fmt.Sprintf("%s=%d", l.GetLeader(), l.GetCount()))
	}
	if strings.Join(got, ",") != "nats-0=8,nats-2=4" {
		t.Errorf("leaders taken %v", got)
	}
	for _, s := range res.GetSteps() {
		for _, e := range s.GetEvidence() {
			isElection := strings.Contains(e.GetLabel(), "leader_elected")
			if isElection != (e.GetElected() != "") {
				t.Errorf("%q: evidence %v", s.GetTitle(), e)
			}
		}
	}
	meta := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_LEADERS)
	if len(meta) != 1 || len(meta[0].GetLeadersTaken()) != 1 || meta[0].GetLeadersTaken()[0].GetLeader() != "nats-2" {
		t.Errorf("meta = %v", meta)
	}
}

// A step whose evidence is capped keeps the evidence of its own first and
// fills the rest with elections, which leaders_taken counts in full: a
// restart keeps the collection it came back in.
func TestReplayCapKeepsTheStepsOwnEvidence(t *testing.T) {
	f := facts()
	f.EventsCaptured, f.Collected = true, true
	var led []*v1.EntityRef
	for i := range 12 {
		led = append(led, streamEnt(fmt.Sprintf("s%d", i)))
	}
	f.Restarts = []api.ReplayRestart{{Server: "nats-1", LastSeen: rAt(10, 0), FirstSeen: rAt(11, 30), OldEpoch: 120, NewEpoch: 123, LedBefore: led}}
	for i := range 12 {
		f.Events = append(f.Events, ev(rAt(10, 5).Add(time.Duration(i)*time.Second), "stream.leader_elected", "nats-0", onAsset("APP", fmt.Sprintf("s%d", i), "")))
	}
	f.Transitions = []api.ReplayTransition{tr("server-004:nats-1", "server-004", &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: "nats-1"},
		rAt(11, 30), firing, info)}
	r := stepsOf(api.FinishReplay(f), v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART)[0]
	var collections, findings, elections int
	for _, e := range r.GetEvidence() {
		switch {
		case e.GetKind() == v1.ReplayEvidenceKind_REPLAY_EVIDENCE_KIND_COLLECTION:
			collections++
		case e.GetKind() == v1.ReplayEvidenceKind_REPLAY_EVIDENCE_KIND_FINDING:
			findings++
		case e.GetElected() != "":
			elections++
		}
	}
	if len(r.GetEvidence()) != 10 || collections != 2 || findings != 1 || elections != 7 || r.GetEvidenceCount() != 15 {
		t.Errorf("evidence %d of %d: %d collections, %d findings, %d elections", len(r.GetEvidence()), r.GetEvidenceCount(), collections, findings, elections)
	}
	for i := 1; i < len(r.GetEvidence()); i++ {
		if r.GetEvidence()[i].GetTime().AsTime().Before(r.GetEvidence()[i-1].GetTime().AsTime()) {
			t.Errorf("evidence out of time order: %v", r.GetEvidence())
		}
	}
}

// The page tells a meta leaders step by its entities: one server; a
// stream or consumer leaders step names no server.
func TestReplayLeadersStepEntities(t *testing.T) {
	f := facts()
	f.MetaLeaderAtA = "nats-1"
	f.Events = []*v1.Event{
		ev(rAt(10, 0), "meta.leader_elected", "nats-0"),
		ev(rAt(20, 0), "stream.leader_elected", "nats-2", onAsset("APP", "orders", "")),
		ev(rAt(20, 1), "consumer.leader_elected", "nats-2", onAsset("APP", "orders", "fast")),
	}
	ls := stepsOf(api.FinishReplay(f), v1.ReplayStepKind_REPLAY_STEP_KIND_LEADERS)
	if len(ls) != 2 {
		t.Fatalf("leaders steps = %v", ls)
	}
	if es := ls[0].GetEntities(); len(es) != 1 || es[0].GetType() != v1.EntityType_ENTITY_TYPE_SERVER || es[0].GetKey() != "nats-0" {
		t.Errorf("meta step entities = %v", es)
	}
	for _, e := range ls[1].GetEntities() {
		if e.GetType() == v1.EntityType_ENTITY_TYPE_SERVER {
			t.Errorf("stream step names a server: %v", ls[1].GetEntities())
		}
	}
	if len(ls[1].GetEntities()) != 2 {
		t.Errorf("stream step entities = %v", ls[1].GetEntities())
	}
}

// An open gap's body says once what was missing.
func TestReplayOpenGapBodyIsOneSentence(t *testing.T) {
	f := facts()
	f.Gaps = []api.ReplayGap{{Kind: api.GapEvents, Start: rAt(50, 0), Event: ev(rAt(50, 0), "collector.connection_closed", "")}}
	g := stepsOf(api.FinishReplay(f), v1.ReplayStepKind_REPLAY_STEP_KIND_GAP)[0]
	if g.GetBody() != "Events were not captured from 09:50:00 up to B." {
		t.Errorf("body = %q", g.GetBody())
	}
}
