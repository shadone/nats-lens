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

// rA is moment A of the replay tests; B is an hour later.
var rA = time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)

func rAt(min, sec int) time.Time {
	return rA.Add(time.Duration(min)*time.Minute + time.Duration(sec)*time.Second)
}

func facts() *api.ReplayFacts {
	return &api.ReplayFacts{A: rA, B: rAt(60, 0), Until: rAt(60, 0).Add(api.ReplayLookahead), Interval: 30 * time.Second,
		RestartWindow: 10 * time.Minute, EpochA: 100, EpochAAt: rA.Add(-10 * time.Second), EpochB: 220, EpochBAt: rAt(59, 50)}
}

var nextEvent = 0

func ev(at time.Time, kind, server string, f ...func(*v1.Event)) *v1.Event {
	nextEvent++
	e := &v1.Event{Id: fmt.Sprint(nextEvent), Time: timestamppb.New(at), Kind: kind, Server: server, Summary: kind + " " + server}
	for _, fn := range f {
		fn(e)
	}
	return e
}

func onAsset(account, stream, consumer string) func(*v1.Event) {
	return func(e *v1.Event) { e.Account, e.Stream, e.Consumer = account, stream, consumer }
}

func streamEnt(name string) *v1.EntityRef {
	return &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "APP/" + name, Name: name, Account: "APP", Stream: name}
}

func consumerEnt(stream, name string) *v1.EntityRef {
	return &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: "APP/" + stream + "/" + name, Name: name, Account: "APP",
		Stream: stream, Consumer: name}
}

func tr(id, check string, e *v1.EntityRef, at time.Time, st v1.FindingState, sev v1.Severity) api.ReplayTransition {
	return api.ReplayTransition{FindingID: id, Check: check, CheckName: check + " name", Entity: e, Time: at, State: st, Severity: sev,
		Detail: "detail of " + id}
}

const (
	firing   = v1.FindingState_FINDING_STATE_FIRING
	resolved = v1.FindingState_FINDING_STATE_RESOLVED
	warning  = v1.Severity_SEVERITY_WARNING
	critical = v1.Severity_SEVERITY_CRITICAL
	info     = v1.Severity_SEVERITY_INFO
)

func stepsOf(res *v1.GetReplayResponse, kind v1.ReplayStepKind) []*v1.ReplayStep {
	var out []*v1.ReplayStep
	for _, s := range res.GetSteps() {
		if s.GetKind() == kind {
			out = append(out, s)
		}
	}
	return out
}

func i32(v int32) *int32 { return &v }

// noCause fails on wording that claims a cause.
func noCause(t *testing.T, res *v1.GetReplayResponse) {
	t.Helper()
	for _, s := range res.GetSteps() {
		for _, w := range []string{"because", "caused", "due to", "from disk"} {
			if strings.Contains(strings.ToLower(s.GetTitle()+" "+s.GetBody()), w) {
				t.Errorf("step %q says %q: %s", s.GetTitle(), w, s.GetBody())
			}
		}
		if s.GetEvidenceCount() == 0 && s.GetKind() != v1.ReplayStepKind_REPLAY_STEP_KIND_GAP {
			t.Errorf("step %q has no evidence", s.GetTitle())
		}
	}
}

func TestReplayRestartTakesInItsEvidence(t *testing.T) {
	f := facts()
	f.EventsCaptured, f.Collected = true, true
	led := streamEnt("orders")
	f.Restarts = []api.ReplayRestart{{Server: "nats-1", OldID: "OLD", NewID: "NEW", LastSeen: rAt(10, 0), FirstSeen: rAt(11, 30),
		Started: rAt(11, 5), OldEpoch: 120, NewEpoch: 123, OldVersion: "2.11.8", NewVersion: "2.12.2", LedBefore: []*v1.EntityRef{led},
		StreamsBefore: i32(4), ConsumersBefore: i32(11), StreamsAfter: i32(4), ConsumersAfter: i32(9)}}
	lame := ev(rAt(9, 40), "server.lameduck", "nats-1")
	kill := ev(rAt(9, 45), "k8s.pod.killing", "nats-1")
	elected := ev(rAt(9, 50), "stream.leader_elected", "nats-2", onAsset("APP", "orders", ""))
	other := ev(rAt(30, 0), "stream.leader_elected", "nats-0", onAsset("APP", "payments", ""))
	f.Events = []*v1.Event{other, elected, kill, lame}
	f.LeadersAtA = []api.ReplayLeader{{Entity: led, Server: "nats-1"}, {Entity: streamEnt("payments"), Server: "nats-0"}}
	f.Transitions = []api.ReplayTransition{tr("server-004:nats-1", "server-004", &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER,
		Key: "nats-1", Name: "nats-1"}, rAt(11, 30), firing, info)}
	res := api.FinishReplay(f)
	noCause(t, res)
	rs := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART)
	if len(rs) != 1 {
		t.Fatalf("restart steps = %v", rs)
	}
	r := rs[0]
	if r.GetTitle() != "nats-1 restarts for an upgrade from 2.11.8 to 2.12.2" || !r.GetTime().AsTime().Equal(rAt(10, 0)) ||
		!r.GetEnd().AsTime().Equal(rAt(11, 30)) {
		t.Errorf("restart = %q %v–%v", r.GetTitle(), r.GetTime().AsTime(), r.GetEnd().AsTime())
	}
	want := "Lame duck mode announced at 09:09:40. Back at 09:11:30 with 4 streams and 9 consumers, 2 consumers fewer than before the restart. " +
		"Leaders it held were elected on other servers: 1 on nats-2."
	if r.GetBody() != want {
		t.Errorf("body = %q\nwant %q", r.GetBody(), want)
	}
	kinds := map[v1.ReplayEvidenceKind]int{}
	for _, e := range r.GetEvidence() {
		kinds[e.GetKind()]++
	}
	// The announcement, the pod event, the election; two collections; the
	// server-004 firing.
	if kinds[v1.ReplayEvidenceKind_REPLAY_EVIDENCE_KIND_EVENT] != 3 || kinds[v1.ReplayEvidenceKind_REPLAY_EVIDENCE_KIND_COLLECTION] != 2 ||
		kinds[v1.ReplayEvidenceKind_REPLAY_EVIDENCE_KIND_FINDING] != 1 || r.GetEvidenceCount() != 6 {
		t.Errorf("evidence = %v", r.GetEvidence())
	}
	if r.GetId() != "restart:event:"+lame.GetId() {
		t.Errorf("id = %q", r.GetId())
	}
	// The election the restart took in and the pod event are no steps of
	// their own; the other election is.
	ls := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_LEADERS)
	if len(ls) != 1 || ls[0].GetTitle() != "Leader of payments elected on nats-0" || ls[0].GetBody() != "" {
		t.Errorf("leader steps = %v", ls)
	}
	if ks := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_KUBERNETES); len(ks) != 0 {
		t.Errorf("k8s steps = %v", ks)
	}
	c := res.GetCounts()
	if c.GetServersRestarted() != 1 || c.GetLeaderElections() != 2 || c.GetLeadersMoved() != 1 || c.LeadersMoved == nil {
		t.Errorf("counts = %v", c)
	}
}

func TestReplayRestartWithoutAnnouncement(t *testing.T) {
	f := facts()
	f.EventsCaptured = true
	f.Restarts = []api.ReplayRestart{{Server: "nats-0", LastSeen: rAt(5, 0), FirstSeen: rAt(6, 0), OldEpoch: 110, NewEpoch: 112}}
	res := api.FinishReplay(f)
	r := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART)[0]
	if r.GetTitle() != "nats-0 restarts" || r.GetBody() != "No lame duck or shutdown event was captured. Back at 09:06:00." {
		t.Errorf("restart = %q: %q", r.GetTitle(), r.GetBody())
	}
	// Without event capture the step does not claim none was sent.
	f.EventsCaptured = false
	r = stepsOf(api.FinishReplay(f), v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART)[0]
	if r.GetBody() != "Back at 09:06:00." {
		t.Errorf("body without events = %q", r.GetBody())
	}
}

func TestReplayStopAndAnswerAgainAfterB(t *testing.T) {
	f := facts()
	f.EventsCaptured = true
	f.Stops = []api.ReplayStop{{Server: "nats-us-4", LastSeen: rAt(20, 30), LastEpoch: 141, BackAt: rAt(75, 0), BackEpoch: 250}}
	f.Events = []*v1.Event{ev(rAt(20, 10), "server.shutdown", "nats-us-4")}
	res := api.FinishReplay(f)
	noCause(t, res)
	steps := res.GetSteps()
	if len(steps) != 2 {
		t.Fatalf("steps = %v", steps)
	}
	s, back := steps[0], steps[1]
	if s.GetTitle() != "nats-us-4 stops answering" || !s.GetOpenAtB() || s.GetEnd() != nil || s.GetSeverity() != warning ||
		s.GetBody() != "Shutdown announced at 09:20:10. Last answered at 09:20:30; not answering at B." {
		t.Errorf("stop = %v", s)
	}
	if !back.GetOutsideRange() || back.GetKind() != v1.ReplayStepKind_REPLAY_STEP_KIND_RESOLVED || back.GetTitle() != "nats-us-4 answers again" {
		t.Errorf("back = %v", back)
	}
}

func TestReplayFoldsKubernetesEventsOfOneServer(t *testing.T) {
	f := facts()
	for i := range 5 {
		f.Events = append(f.Events, ev(rAt(12, 0).Add(time.Duration(i)*90*time.Second), "k8s.pod.failed_scheduling", "nats-us-4"))
	}
	f.Events = append(f.Events, ev(rAt(40, 0), "k8s.pod.failed_scheduling", "nats-us-4"))
	res := api.FinishReplay(f)
	ks := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_KUBERNETES)
	if len(ks) != 2 {
		t.Fatalf("k8s steps = %v", ks)
	}
	if ks[0].GetTitle() != "The pod of nats-us-4 cannot be scheduled (5 times, 09:12–09:18)" || ks[0].GetEvidenceCount() != 5 ||
		ks[0].GetSeverity() != warning || !ks[0].GetEnd().AsTime().Equal(rAt(18, 0)) {
		t.Errorf("folded = %v", ks[0])
	}
	if ks[1].GetTitle() != "The pod of nats-us-4 cannot be scheduled" {
		t.Errorf("second = %q", ks[1].GetTitle())
	}
}

func TestReplayFoldsQuorumAcrossEntities(t *testing.T) {
	f := facts()
	a, b, c := consumerEnt("orders", "a"), consumerEnt("orders", "b"), consumerEnt("orders", "c")
	f.Transitions = []api.ReplayTransition{
		tr("consumer-001:a", "consumer-001", a, rAt(10, 0), firing, critical),
		tr("consumer-001:a", "consumer-001", a, rAt(14, 0), resolved, critical),
		tr("consumer-001:b", "consumer-001", b, rAt(11, 0), firing, critical),
		tr("consumer-001:b", "consumer-001", b, rAt(13, 0), resolved, critical),
		// c is already without quorum at A and resolves after B.
		tr("consumer-001:c", "consumer-001", c, rAt(-5, 0), firing, critical),
		tr("consumer-001:c", "consumer-001", c, rAt(70, 0), resolved, critical),
	}
	f.Events = []*v1.Event{ev(rAt(10, 10), "consumer.quorum_lost", "nats-0", onAsset("APP", "orders", "a"))}
	res := api.FinishReplay(f)
	noCause(t, res)
	qs := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_QUORUM)
	if len(qs) != 2 {
		t.Fatalf("quorum steps = %v", qs)
	}
	open, folded := qs[0], qs[1]
	if open.GetTitle() != "Consumer c on orders has no quorum" || !open.GetOpenAtB() || !open.GetTime().AsTime().Equal(rA) ||
		open.GetBody() != "Already without quorum at A (since 08:55:00); still without quorum at B." {
		t.Errorf("open = %v", open)
	}
	if folded.GetTitle() != "2 consumers have no quorum for 4 min" || folded.GetEntityCount() != 2 ||
		!folded.GetEnd().AsTime().Equal(rAt(14, 0)) || folded.GetBody() != "a on orders and b on orders. From 09:10:00 to 09:14:00." {
		t.Errorf("folded = %v", folded)
	}
	hasAdvisory := false
	for _, e := range folded.GetEvidence() {
		hasAdvisory = hasAdvisory || e.GetKind() == v1.ReplayEvidenceKind_REPLAY_EVIDENCE_KIND_EVENT
	}
	if !hasAdvisory {
		t.Errorf("the quorum advisory is not evidence: %v", folded.GetEvidence())
	}
	last := res.GetSteps()[len(res.GetSteps())-1]
	if !last.GetOutsideRange() || last.GetTitle() != "Consumer quorum back: c on orders" {
		t.Errorf("last step = %v", last)
	}
	if n := len(stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_RESOLVED)); n != 1 {
		t.Errorf("resolved steps = %d, want only the one after B", n)
	}
}

func TestReplayFindingsFoldAndInfoIsNoStep(t *testing.T) {
	f := facts()
	f.Collected = true
	f.Transitions = []api.ReplayTransition{
		tr("consumer-009:x", "consumer-009", consumerEnt("orders", "x"), rAt(30, 0), firing, warning),
		tr("consumer-009:y", "consumer-009", consumerEnt("orders", "y"), rAt(31, 0), firing, warning),
		tr("stream-005:s", "stream-005", streamEnt("s"), rAt(32, 0), firing, info),
		tr("consumer-009:x", "consumer-009", consumerEnt("orders", "x"), rAt(45, 0), resolved, warning),
	}
	res := api.FinishReplay(f)
	fs := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_FINDING)
	if len(fs) != 1 || fs[0].GetTitle() != "consumer-009 name for 2 consumers" || fs[0].GetSeverity() != warning {
		t.Fatalf("finding steps = %v", fs)
	}
	rs := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_RESOLVED)
	if len(rs) != 1 || rs[0].GetTitle() != "consumer-009 name resolved: x on orders" || rs[0].GetOutsideRange() {
		t.Errorf("resolved steps = %v", rs)
	}
	if res.GetCounts().GetFindingsRaised() != 2 {
		t.Errorf("findings raised = %d", res.GetCounts().GetFindingsRaised())
	}
}

func TestReplayGoneAssets(t *testing.T) {
	f := facts()
	del := ev(rAt(31, 7), "consumer.deleted", "nats-0", onAsset("APP", "archive", "agg"), func(e *v1.Event) {
		e.ClientName, e.ClientHost = "analytics-worker", "10.20.0.12"
	})
	f.GoneRead = true
	f.Gone = []api.ReplayGone{
		{Entity: consumerEnt("inventory-changes", "restock-alerts"), ServersAtA: []string{"nats-us-0", "nats-us-1", "nats-us-2"},
			LastReported: rAt(29, 0), LastEpoch: 158, FindingIDs: []string{"consumer-013:restock"}},
		{Entity: consumerEnt("inventory-changes", "stock-audit-export"), ServersAtA: []string{"nats-us-0", "nats-us-1", "nats-us-2"},
			LastReported: rAt(29, 0), LastEpoch: 158},
		{Entity: consumerEnt("archive", "agg"), ServersAtA: []string{"nats-0"}, LastReported: rAt(31, 0), LastEpoch: 162, Delete: del},
		{Entity: streamEnt("KV_label-templates"), ServersAtA: []string{"nats-us-4"}, NotAnsweringAtB: []string{"nats-us-4"},
			LastReported: rAt(20, 0), LastEpoch: 140},
	}
	f.Transitions = []api.ReplayTransition{
		tr("consumer-013:restock", "consumer-013", consumerEnt("inventory-changes", "restock-alerts"), rAt(29, 30), firing, critical),
		tr("consumer-013:restock", "consumer-013", consumerEnt("inventory-changes", "restock-alerts"), rAt(95, 0), resolved, critical),
		// A finding the provider did not name is found by its entity.
		tr("consumer-013:stock", "consumer-013", consumerEnt("inventory-changes", "stock-audit-export"), rAt(29, 30), firing, critical),
	}
	res := api.FinishReplay(f)
	noCause(t, res)
	gs := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_GONE)
	titles := []string{}
	for _, s := range gs {
		titles = append(titles, s.GetTitle())
	}
	want := []string{"Stream KV_label-templates not reported at B", "2 durables on inventory-changes gone without a delete event",
		"Durable agg on archive deleted"}
	if strings.Join(titles, "|") != strings.Join(want, "|") {
		t.Errorf("gone titles = %q\nwant %q", titles, want)
	}
	if gs[2].GetBody() != "Deleted at 09:31:07 by analytics-worker (10.20.0.12). Held by nats-0 at A." {
		t.Errorf("deleted body = %q", gs[2].GetBody())
	}
	if gs[0].GetBody() != "Held by nats-us-4 at A; no server reports it at B, and nats-us-4 did not answer then. Last reported at 09:20:00." {
		t.Errorf("unknown body = %q", gs[0].GetBody())
	}
	// consumer-013 fired inside the gone step, so it makes no finding step;
	// its resolve after B closes the story.
	if n := len(stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_FINDING)); n != 0 {
		t.Errorf("finding steps = %d", n)
	}
	last := res.GetSteps()[len(res.GetSteps())-1]
	if !last.GetOutsideRange() || !strings.Contains(last.GetTitle(), "resolved: restock-alerts on inventory-changes") {
		t.Errorf("last = %v", last)
	}
	if ids := res.GetGone()[3].GetFindingIds(); len(ids) != 1 || ids[0] != "consumer-013:stock" {
		t.Errorf("finding IDs of stock-audit-export = %v", ids)
	}
	if len(res.GetGone()) != 4 || res.GetGone()[0].GetEntity().GetKey() != "APP/KV_label-templates" ||
		res.GetGone()[1].GetDeleteClient() != "analytics-worker (10.20.0.12)" || res.GetGone()[1].GetDeleteEvent().GetId() != del.GetId() {
		t.Errorf("gone = %v", res.GetGone())
	}
	c := res.GetCounts()
	if c.GetConsumersGoneWithoutDelete() != 2 || c.GetStreamsGoneWithoutDelete() != 0 || c.StreamsGoneWithoutDelete == nil ||
		c.GetAssetsGoneWithDelete() != 1 || c.GetAssetsUnknownAtB() != 1 {
		t.Errorf("counts = %v", c)
	}
}

func TestReplayCountsUnsetWhenNotObserved(t *testing.T) {
	f := facts()
	f.EpochA, f.EpochB = 0, 0
	c := api.FinishReplay(f).GetCounts()
	for name, v := range map[string]*int32{"restarted": c.ServersRestarted, "gone": c.ConsumersGoneWithoutDelete, "elections": c.LeaderElections,
		"reconnects": c.ClientReconnects, "nodes": c.NodesUpgraded, "pools": c.NodePoolsUpgraded, "raised": c.FindingsRaised} {
		if v != nil {
			t.Errorf("%s = %d, want unset", name, *v)
		}
	}
	// With both collections but the gone assets not read, the gone counts
	// stay unset.
	if c := api.FinishReplay(facts()).GetCounts(); c.ConsumersGoneWithoutDelete != nil || c.AssetsGoneWithDelete != nil {
		t.Errorf("gone counts set without GoneRead: %v", c)
	}
	f = facts()
	f.Collected, f.EventsCaptured, f.Kubernetes = true, true, true
	f.Nodes = api.ReplayNodes{Upgraded: 3, Drained: 3, Pools: 1}
	c = api.FinishReplay(f).GetCounts()
	if c.ServersRestarted == nil || *c.ServersRestarted != 0 || c.LeaderElections == nil || c.GetNodesUpgraded() != 3 || c.NodePoolsUpgraded != nil {
		t.Errorf("counts = %v; pools stay unset without pool labels", c)
	}
	f.Nodes.PoolLabels = true
	if c := api.FinishReplay(f).GetCounts(); c.GetNodePoolsUpgraded() != 1 {
		t.Errorf("pools = %v", c.NodePoolsUpgraded)
	}
}

func TestReplayMetaLeaderMoves(t *testing.T) {
	f := facts()
	f.EventsCaptured = true
	f.MetaLeaderAtA = "nats-1"
	f.Events = []*v1.Event{ev(rAt(30, 11), "meta.leader_elected", "nats-0")}
	res := api.FinishReplay(f)
	ls := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_LEADERS)
	if len(ls) != 1 || ls[0].GetTitle() != "Meta leader moves from nats-1 to nats-0" || res.GetCounts().GetMetaLeaderChanges() != 1 {
		t.Errorf("meta = %v, counts %v", ls, res.GetCounts())
	}
	f.MetaLeaderAtA = ""
	res = api.FinishReplay(f)
	if t0 := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_LEADERS)[0].GetTitle(); t0 != "Meta leader elected on nats-0" ||
		res.GetCounts().GetMetaLeaderChanges() != 0 {
		t.Errorf("unknown previous: %q, %v", t0, res.GetCounts())
	}
}

func TestReplayGaps(t *testing.T) {
	f := facts()
	gapEv := ev(rAt(20, 0), "collector.gap", "", func(e *v1.Event) { e.Summary = "natslens was not running for 10m" })
	f.Gaps = []api.ReplayGap{
		{Kind: api.GapCollections, Start: rAt(20, 0), End: rAt(30, 0), BeforeEpoch: 140, BeforeAt: rAt(20, 0), AfterEpoch: 141, AfterAt: rAt(30, 0)},
		{Kind: api.GapCollector, Start: rAt(20, 0), End: rAt(30, 0), Event: gapEv},
		{Kind: api.GapCollections, Start: rAt(50, 0), BeforeEpoch: 160, BeforeAt: rAt(50, 0)},
	}
	res := api.FinishReplay(f)
	gs := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_GAP)
	if len(gs) != 2 {
		t.Fatalf("gaps = %v", gs)
	}
	if gs[0].GetTitle() != "nats-lens was not running from 09:20:00 to 09:30:00" || gs[0].GetEvidenceCount() != 3 {
		t.Errorf("collector gap = %v", gs[0])
	}
	if gs[1].GetTitle() != "No collections since 09:50:00" || !gs[1].GetOpenAtB() {
		t.Errorf("trailing gap = %v", gs[1])
	}
}

func TestReplayOrderAndCaps(t *testing.T) {
	f := facts()
	f.B = rA.Add(24 * time.Hour)
	f.Until = f.B
	// 250 neutral leader steps, each on its own server, and one gap at
	// the end: the gap is kept first.
	for i := range 250 {
		f.Events = append(f.Events, ev(rA.Add(time.Duration(i)*5*time.Minute), "stream.leader_elected", fmt.Sprintf("nats-%d", i),
			onAsset("APP", "s", "")))
	}
	f.Gaps = []api.ReplayGap{{Kind: api.GapCollections, Start: f.B.Add(-time.Minute), BeforeEpoch: 9, BeforeAt: f.B.Add(-time.Minute)}}
	// A drain and a restart at one moment: Kubernetes first.
	f.Events = append(f.Events, ev(rAt(2, 0), "k8s.node.drained", ""))
	f.Restarts = []api.ReplayRestart{{Server: "nats-x", LastSeen: rAt(2, 0), FirstSeen: rAt(3, 0), OldEpoch: 1, NewEpoch: 2}}
	res := api.FinishReplay(f)
	if len(res.GetSteps()) != 200 || !res.GetStepsTruncated() || res.GetStepsOmitted() != 53 {
		t.Fatalf("steps %d, truncated %v, omitted %d", len(res.GetSteps()), res.GetStepsTruncated(), res.GetStepsOmitted())
	}
	var kinds []v1.ReplayStepKind
	for i, s := range res.GetSteps() {
		if i > 0 && s.GetTime().AsTime().Before(res.GetSteps()[i-1].GetTime().AsTime()) {
			t.Fatalf("step %d out of order", i)
		}
		kinds = append(kinds, s.GetKind())
	}
	if len(stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_GAP)) != 1 || len(stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART)) != 1 {
		t.Errorf("the gap and the restart were not kept")
	}
	ki := func(k v1.ReplayStepKind) int {
		for i, x := range kinds {
			if x == k {
				return i
			}
		}
		return -1
	}
	if ki(v1.ReplayStepKind_REPLAY_STEP_KIND_KUBERNETES) > ki(v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART) {
		t.Errorf("Kubernetes step after the restart at the same time: %v", kinds[:5])
	}
}

func TestReplayCapsEvidenceAndEntities(t *testing.T) {
	f := facts()
	for i := range 60 {
		f.Events = append(f.Events, ev(rAt(10, 0).Add(time.Duration(i)*time.Second), "stream.leader_elected", "nats-2",
			onAsset("APP", fmt.Sprintf("s%02d", i), "")))
	}
	res := api.FinishReplay(f)
	s := res.GetSteps()[0]
	if len(res.GetSteps()) != 1 || s.GetTitle() != "60 leaders elected on nats-2" || len(s.GetEvidence()) != 10 || s.GetEvidenceCount() != 60 ||
		len(s.GetEntities()) != 50 || s.GetEntityCount() != 60 {
		t.Errorf("step = %q, evidence %d/%d, entities %d/%d", s.GetTitle(), len(s.GetEvidence()), s.GetEvidenceCount(),
			len(s.GetEntities()), s.GetEntityCount())
	}
}

// A restart takes in the server-004 transition of its own time only; a
// later critical firing of the same finding is a step of its own.
func TestReplayRestartTakesInOnlyItsOwnRestartFinding(t *testing.T) {
	f := facts()
	srv := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: "nats-1", Name: "nats-1"}
	f.Restarts = []api.ReplayRestart{{Server: "nats-1", LastSeen: rAt(10, 0), FirstSeen: rAt(11, 0), OldEpoch: 120, NewEpoch: 122}}
	f.Transitions = []api.ReplayTransition{
		tr("server-004:nats-1", "server-004", srv, rAt(11, 0), firing, warning),
		tr("server-004:nats-1", "server-004", srv, rAt(40, 0), v1.FindingState_FINDING_STATE_CHANGED, critical),
	}
	res := api.FinishReplay(f)
	fs := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_FINDING)
	if len(fs) != 1 || !fs[0].GetTime().AsTime().Equal(rAt(40, 0)) || fs[0].GetSeverity() != critical {
		t.Errorf("finding steps = %v", fs)
	}
}

// An announcement goes into one step only: a second, unannounced restart
// of the same server within the restart window does not claim it.
func TestReplayAnnouncementIsTakenOnce(t *testing.T) {
	f := facts()
	f.EventsCaptured = true
	f.Events = []*v1.Event{ev(rAt(9, 50), "server.lameduck", "nats-1")}
	f.Restarts = []api.ReplayRestart{
		{Server: "nats-1", LastSeen: rAt(15, 0), FirstSeen: rAt(16, 0), OldEpoch: 130, NewEpoch: 132},
		{Server: "nats-1", LastSeen: rAt(10, 0), FirstSeen: rAt(11, 0), OldEpoch: 120, NewEpoch: 122},
	}
	rs := stepsOf(api.FinishReplay(f), v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART)
	if len(rs) != 2 || !strings.HasPrefix(rs[0].GetBody(), "Lame duck mode announced at 09:09:50.") ||
		!strings.HasPrefix(rs[1].GetBody(), "No lame duck or shutdown event was captured.") {
		t.Errorf("restarts = %q / %q", rs[0].GetBody(), rs[1].GetBody())
	}
}

// Without a collection at B no stop can be told, so the count stays unset.
func TestReplayStopsUnsetWithoutCollectionAtB(t *testing.T) {
	f := facts()
	f.Collected, f.EpochB, f.EpochBAt = true, 0, time.Time{}
	c := api.FinishReplay(f).GetCounts()
	if c.ServersStopped != nil || c.ServersRestarted == nil {
		t.Errorf("counts = %v", c)
	}
}

// Folded node events count nodes, not events.
func TestReplayFoldedNodeEventsCountNodes(t *testing.T) {
	f := facts()
	node := func(n string) func(*v1.Event) { return func(e *v1.Event) { e.Raw = `{"node":"` + n + `"}` } }
	f.Events = []*v1.Event{
		ev(rAt(10, 0), "k8s.node.drained", "", node("gke-a")),
		ev(rAt(11, 0), "k8s.node.drained", "", node("gke-a")),
		ev(rAt(30, 0), "k8s.node.drained", "", node("gke-a")),
		ev(rAt(31, 0), "k8s.node.drained", "", node("gke-b")),
		ev(rAt(40, 0), "k8s.node.drained", "", node("gke-c")),
		ev(rAt(40, 30), "k8s.node.drained", "", node("gke-d")),
		ev(rAt(41, 0), "k8s.node.drained", "", node("gke-c")),
		ev(rAt(50, 0), "k8s.node.drained", ""),
		ev(rAt(51, 0), "k8s.node.drained", ""),
	}
	var titles []string
	for _, s := range stepsOf(api.FinishReplay(f), v1.ReplayStepKind_REPLAY_STEP_KIND_KUBERNETES) {
		titles = append(titles, s.GetTitle())
	}
	want := []string{"Node gke-a drained (2 times, 09:10–09:11)", "2 nodes drained (09:30–09:31)", "2 nodes drained (09:40–09:41)",
		"2 drain events (09:50–09:51)"}
	if strings.Join(titles, "|") != strings.Join(want, "|") {
		t.Errorf("titles = %q, want %q", titles, want)
	}
}

// While nats-lens was not running for part of A to B, the event and node
// counts stay unset; a Kubernetes watch gap unsets the node counts only.
func TestReplayCountsUnsetDuringCaptureGaps(t *testing.T) {
	f := facts()
	f.EventsCaptured, f.Kubernetes = true, true
	f.Gaps = []api.ReplayGap{{Kind: api.GapCollector, Start: rAt(20, 0), End: rAt(30, 0)}}
	c := api.FinishReplay(f).GetCounts()
	if c.LeaderElections != nil || c.ClientReconnects != nil || c.NodesDrained != nil {
		t.Errorf("counts during a collector gap = %v", c)
	}
	f.Gaps = []api.ReplayGap{{Kind: api.GapKubernetes, Start: rAt(20, 0), End: rAt(30, 0)}}
	c = api.FinishReplay(f).GetCounts()
	if c.LeaderElections == nil || c.NodesDrained != nil {
		t.Errorf("counts during a Kubernetes watch gap = %v", c)
	}
	// A gap that ended before A changes nothing.
	f.Gaps = []api.ReplayGap{{Kind: api.GapCollector, Start: rAt(-30, 0), End: rAt(-20, 0)}}
	if c := api.FinishReplay(f).GetCounts(); c.LeaderElections == nil || c.NodesDrained == nil {
		t.Errorf("counts after an earlier gap = %v", c)
	}
}

// A gone stream takes in its durables' consumer-013 findings: they make
// no finding step of their own.
func TestReplayGoneStreamTakesItsDurablesFindings(t *testing.T) {
	f := facts()
	f.GoneRead = true
	f.Gone = []api.ReplayGone{{Entity: streamEnt("orders"), ServersAtA: []string{"nats-0"}, LastReported: rAt(20, 0), LastEpoch: 140,
		DurablesWithIt: []string{"x"}}}
	f.Transitions = []api.ReplayTransition{tr("consumer-013:x", "consumer-013", consumerEnt("orders", "x"), rAt(21, 0), firing, critical),
		// y vanished before A and did not go with the stream: its finding is
		// a step of its own.
		tr("consumer-013:y", "consumer-013", consumerEnt("orders", "y"), rAt(22, 0), firing, critical)}
	res := api.FinishReplay(f)
	if fs := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_FINDING); len(fs) != 1 || !strings.Contains(fs[0].GetTitle(), "y on orders") {
		t.Errorf("finding steps = %v", fs)
	}
	if ids := res.GetGone()[0].GetFindingIds(); len(ids) != 1 || ids[0] != "consumer-013:x" || res.GetGone()[0].GetConsumersWithIt() != 1 {
		t.Errorf("finding IDs = %v", ids)
	}
}

// A step between A and B takes in nothing from after B: neither a stop's
// announcement nor a restart's pod event, election or restart finding.
func TestReplayStepsTakeInNothingAfterB(t *testing.T) {
	f := facts()
	f.EventsCaptured = true
	b := f.B
	led := streamEnt("orders")
	f.Stops = []api.ReplayStop{{Server: "nats-2", LastSeen: b.Add(-time.Minute), LastEpoch: 218}}
	f.Restarts = []api.ReplayRestart{{Server: "nats-1", LastSeen: b.Add(-time.Minute), FirstSeen: b.Add(-10 * time.Second),
		OldEpoch: 218, NewEpoch: 220, LedBefore: []*v1.EntityRef{led}}}
	f.Events = []*v1.Event{
		ev(b.Add(3*time.Minute), "server.lameduck", "nats-2"),
		ev(b.Add(20*time.Second), "k8s.pod.started", "nats-1"),
		ev(b.Add(15*time.Second), "stream.leader_elected", "nats-0", onAsset("APP", "orders", "")),
	}
	f.Transitions = []api.ReplayTransition{tr("server-004:nats-1", "server-004", &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER,
		Key: "nats-1"}, b.Add(15*time.Second), firing, info)}
	for _, s := range api.FinishReplay(f).GetSteps() {
		for _, e := range s.GetEvidence() {
			if e.GetKind() != v1.ReplayEvidenceKind_REPLAY_EVIDENCE_KIND_COLLECTION && e.GetTime().AsTime().After(b) && !s.GetOutsideRange() {
				t.Errorf("step %q takes in %q from after B", s.GetTitle(), e.GetLabel())
			}
		}
		if strings.Contains(s.GetBody(), "announced") || strings.Contains(s.GetBody(), "elected on other servers") {
			t.Errorf("step %q: %q", s.GetTitle(), s.GetBody())
		}
	}
}

// When the read limit cut the events, a restart does not say that no
// announcement was captured.
func TestReplayNoAbsenceClaimWhenEventsWereCut(t *testing.T) {
	f := facts()
	f.EventsCaptured, f.EventsTruncated = true, true
	f.Restarts = []api.ReplayRestart{{Server: "nats-0", LastSeen: rAt(50, 0), FirstSeen: rAt(51, 0), OldEpoch: 200, NewEpoch: 202}}
	r := stepsOf(api.FinishReplay(f), v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART)[0]
	if strings.Contains(r.GetBody(), "No lame duck") {
		t.Errorf("body = %q", r.GetBody())
	}
}

// A time nats-lens was not running inside a restart's announcement window,
// even before A, keeps the step from saying no announcement was captured;
// the counts over A to B stay set.
func TestReplayAbsenceClaimNeedsCaptureOverTheWindow(t *testing.T) {
	f := facts()
	f.EventsCaptured = true
	f.Gaps = []api.ReplayGap{{Kind: api.GapCollector, Start: rAt(-8, 0), End: rAt(-1, 0)}}
	f.Restarts = []api.ReplayRestart{{Server: "nats-0", LastSeen: rAt(2, 0), FirstSeen: rAt(3, 0), OldEpoch: 104, NewEpoch: 106}}
	res := api.FinishReplay(f)
	if r := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART)[0]; strings.Contains(r.GetBody(), "No lame duck") {
		t.Errorf("body = %q", r.GetBody())
	}
	if res.GetCounts().LeaderElections == nil {
		t.Errorf("counts = %v", res.GetCounts())
	}
}

// A gap that starts at B is a step and unsets the event counts alike.
func TestReplayGapStartingAtB(t *testing.T) {
	f := facts()
	f.EventsCaptured = true
	f.Gaps = []api.ReplayGap{{Kind: api.GapCollector, Start: f.B, End: f.B.Add(time.Hour), Event: ev(f.B, "collector.gap", "")}}
	res := api.FinishReplay(f)
	if len(stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_GAP)) != 1 || res.GetCounts().LeaderElections != nil {
		t.Errorf("gap steps %v, counts %v", stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_GAP), res.GetCounts())
	}
}

// When the read limit cut the transitions before B, a quorum loss whose
// resolve may be in the cut rows is not said to be open at B, and the
// response says up to when the read is complete; an announcement window
// before the cut may still say none was captured.
func TestReplayReadLimitTimes(t *testing.T) {
	f := facts()
	f.EventsCaptured = true
	f.TransitionsTruncated, f.TransitionsReadUntil = true, rAt(20, 0)
	f.EventsTruncated, f.EventsReadUntil = true, rAt(50, 0)
	f.Transitions = []api.ReplayTransition{tr("stream-001:s", "stream-001", streamEnt("s"), rAt(10, 0), firing, critical)}
	f.Restarts = []api.ReplayRestart{{Server: "nats-0", LastSeen: rAt(15, 0), FirstSeen: rAt(16, 0), OldEpoch: 130, NewEpoch: 132}}
	res := api.FinishReplay(f)
	q := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_QUORUM)[0]
	if q.GetOpenAtB() || q.GetBody() != "From 09:10:00; no resolve was read before 09:20:00, where the read limit stopped." {
		t.Errorf("quorum = %v", q)
	}
	if !res.GetCompleteUntil().AsTime().Equal(rAt(20, 0)) || !res.GetStepsTruncated() {
		t.Errorf("complete until %v, truncated %v", res.GetCompleteUntil(), res.GetStepsTruncated())
	}
	if r := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART)[0]; !strings.HasPrefix(r.GetBody(), "No lame duck") {
		t.Errorf("restart = %q", r.GetBody())
	}
}

// A step whose announcement window starts before the events were read
// does not say none was captured.
func TestReplayNoAbsenceClaimBeforeTheRead(t *testing.T) {
	f := facts()
	f.EventsCaptured, f.ReadFrom = true, rAt(-5, 0)
	f.Stops = []api.ReplayStop{{Server: "nats-2", LastSeen: rA.Add(-20 * time.Second), LastEpoch: 100}}
	s := api.FinishReplay(f).GetSteps()[0]
	if strings.Contains(s.GetBody(), "No lame duck") {
		t.Errorf("body = %q", s.GetBody())
	}
}
