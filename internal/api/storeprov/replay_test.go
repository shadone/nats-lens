package storeprov_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The replay fixture: epochs every 30 seconds from rT (epoch 1) to epoch
// 50, without epochs 25 to 28. nats-0 answers throughout; nats-1 restarts
// for an upgrade after epoch 10 (its new process answers from epoch 12,
// after a lame duck announcement); nats-2 answers up to epoch 20 and
// again from epoch 46, after B. A is epoch 3, B epoch 40.
var rT = time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)

const rAcc = "AAPP"

func rEpoch(n int) time.Time { return rT.Add(time.Duration(n-1) * 30 * time.Second) }

func rEpochs() []int {
	var out []int
	for n := 1; n <= 50; n++ {
		if n < 25 || n > 28 {
			out = append(out, n)
		}
	}
	return out
}

func replayStore(t *testing.T) (*storeprov.Provider, rows) {
	t.Helper()
	st := emptyStore(t)
	r := rows{t, st}
	proc := func(id, name, version string, first, last int, start time.Time) {
		r.add("servers", map[string]any{"server_id": id, "server_name": name, "version": version, "start_time": start,
			"first_epoch": first, "last_epoch": last, "first_seen_at": rEpoch(first), "last_seen_at": rEpoch(last)})
	}
	proc("S0", "nats-0", "2.12.2", 1, 50, rT.Add(-24*time.Hour))
	proc("S1OLD", "nats-1", "2.11.8", 1, 10, rT.Add(-24*time.Hour))
	proc("S1NEW", "nats-1", "2.12.2", 12, 50, rEpoch(11).Add(10*time.Second))
	proc("S2", "nats-2", "2.12.2", 1, 20, rT.Add(-24*time.Hour))
	proc("S2NEW", "nats-2", "2.12.2", 46, 50, rEpoch(45).Add(10*time.Second))
	answering := func(n int) []string {
		var ids []string
		for _, p := range []struct {
			id          string
			first, last int
		}{{"S0", 1, 50}, {"S1OLD", 1, 10}, {"S1NEW", 12, 50}, {"S2", 1, 20}, {"S2NEW", 46, 50}} {
			if n >= p.first && n <= p.last {
				ids = append(ids, p.id)
			}
		}
		return ids
	}
	nameOf := map[string]string{"S0": "nats-0", "S1OLD": "nats-1", "S1NEW": "nats-1", "S2": "nats-2", "S2NEW": "nats-2"}
	for _, n := range rEpochs() {
		ids := answering(n)
		var names []string
		for _, id := range ids {
			names = append(names, nameOf[id])
		}
		slices.Sort(names)
		if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, 3, ?, ['nats-0', 'nats-1', 'nats-2'], ?, [], 0, false, [], NULL)`,
			n, rEpoch(n), rEpoch(n), len(ids), names); err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			r.add("epoch_endpoints", map[string]any{"epoch": n, "server_id": id, "endpoint": "JSZ", "state": "ok"})
			r.add("server_stats", map[string]any{"epoch": n, "server_id": id, "meta_leader": "nats-1"})
		}
	}
	stream := func(n int, id, name string, leader bool) {
		r.add("stream_stats", map[string]any{"epoch": n, "server_id": id, "account": rAcc, "stream": name, "is_leader": leader})
	}
	cons := func(n int, id, name string, leader bool) {
		r.add("consumer_stats", map[string]any{"epoch": n, "server_id": id, "account": rAcc, "stream": "ORDERS", "consumer": name,
			"is_leader": leader})
	}
	// At A: ORDERS led by nats-1 on all three, KV_labels on nats-2 alone,
	// durables restock and agg and an ephemeral on nats-0 and nats-1.
	for _, n := range []int{3, 10} {
		for _, id := range []string{"S0", "S1OLD", "S2"} {
			stream(n, id, "ORDERS", id == "S1OLD")
		}
		stream(n, "S2", "KV_labels", true)
		for _, id := range []string{"S0", "S1OLD"} {
			for _, c := range []string{"restock", "agg", "eph"} {
				cons(n, id, c, id == "S0")
			}
		}
	}
	// After nats-1's restart, and at B: ORDERS on nats-0 and nats-1, no
	// KV_labels (nats-2 does not answer), and none of the consumers but
	// one durable that stays.
	for _, n := range []int{12, 40} {
		for _, id := range []string{"S0", "S1NEW"} {
			stream(n, id, "ORDERS", id == "S0")
			cons(n, id, "keep", id == "S0")
		}
	}
	for _, c := range []string{"restock", "agg", "keep"} {
		r.add("consumer_config", map[string]any{"server_id": "S0", "account": rAcc, "stream": "ORDERS", "consumer": c, "first_epoch": 1,
			"last_epoch": 50, "config_hash": "h", "durable_name": c, "inactive_threshold_ns": 0})
	}
	r.add("consumer_config", map[string]any{"server_id": "S0", "account": rAcc, "stream": "ORDERS", "consumer": "eph", "first_epoch": 1,
		"last_epoch": 10, "config_hash": "h"})
	id := 0
	event := func(at time.Time, source, kind string, cols map[string]any) {
		id++
		row := map[string]any{"id": id, "time": at, "received_at": at, "source": source, "kind": kind, "summary": kind}
		for k, v := range cols {
			row[k] = v
		}
		r.add("events", row)
	}
	event(rEpoch(10).Add(10*time.Second), "sys", "server.lameduck", map[string]any{"server_name": "nats-1"})
	event(rEpoch(10).Add(20*time.Second), "k8s", "k8s.pod.killing", map[string]any{"server_name": "nats-1"})
	event(rEpoch(11), "jetstream", "stream.leader_elected", map[string]any{"server_name": "nats-0", "account": rAcc, "stream": "ORDERS"})
	event(rEpoch(11).Add(5*time.Second), "jetstream", "consumer.quorum_lost", map[string]any{"server_name": "nats-0", "account": rAcc,
		"stream": "ORDERS", "consumer": "restock"})
	event(rEpoch(15), "jetstream", "consumer.deleted", map[string]any{"server_name": "nats-0", "account": rAcc, "stream": "ORDERS",
		"consumer": "agg", "client_name": "analytics-worker", "client_host": "10.20.0.12"})
	event(rEpoch(5), "k8s", "k8s.node.drained", map[string]any{"raw": `{"node":"gke-example-pool-1-7f3a","pool":"pool-1"}`})
	event(rEpoch(6), "k8s", "k8s.node.upgrade", map[string]any{"raw": `{"node":"gke-example-pool-1-7f3a","pool":"pool-1"}`})
	event(rEpoch(13), "sys", "client.disconnect", map[string]any{"client_name": "orders-api", "reason": "Server Shutdown"})
	event(rEpoch(13).Add(time.Second), "sys", "client.connect", map[string]any{"client_name": "orders-api"})
	event(rEpoch(13), "sys", "client.disconnect", map[string]any{"client_name": "natslens", "reason": "Stale Connection"})
	event(rEpoch(13).Add(time.Second), "sys", "client.connect", map[string]any{"client_name": "natslens"})
	trans := func(n int, check, typ, key, kind, sev string) {
		r.add("finding_transitions", map[string]any{"finding_id": check + ":" + key, "check_code": check, "entity_type": typ,
			"entity_key": key, "epoch": n, "time": rEpoch(n), "kind": kind, "severity": sev, "detail": check + " detail"})
	}
	trans(11, "consumer-001", "consumer", rAcc+"/ORDERS/restock", "firing", "critical")
	trans(14, "consumer-001", "consumer", rAcc+"/ORDERS/restock", "resolved", "critical")
	trans(12, "server-004", "server", "nats-1", "firing", "info")
	trans(20, "consumer-013", "consumer", rAcc+"/ORDERS/restock", "firing", "critical")
	trans(39, "stream-001", "stream", rAcc+"/ORDERS", "firing", "critical")
	trans(45, "stream-001", "stream", rAcc+"/ORDERS", "resolved", "critical")
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second, Retention: 14 * 24 * time.Hour, Live: true, Kubernetes: true,
		ToolingNames: []string{"natslens"}, Clock: func() time.Time { return rT.Add(2 * time.Hour) }}
	return p, r
}

func replayReq(a, b time.Time) *v1.GetReplayRequest {
	return &v1.GetReplayRequest{A: timestamppb.New(a), B: timestamppb.New(b)}
}

func TestReplayFactsFromStore(t *testing.T) {
	p, _ := replayStore(t)
	f, err := p.Replay(ctx, replayReq(rEpoch(3), rEpoch(40)))
	if err != nil {
		t.Fatal(err)
	}
	if f.EpochA != 3 || f.EpochB != 40 || !f.Collected || !f.EventsCaptured || !f.Kubernetes || !f.Until.Equal(rT.Add(2*time.Hour)) {
		t.Errorf("epochs %d %d, collected %v, captured %v, until %v", f.EpochA, f.EpochB, f.Collected, f.EventsCaptured, f.Until)
	}
	if len(f.Restarts) != 1 {
		t.Fatalf("restarts = %+v", f.Restarts)
	}
	rs := f.Restarts[0]
	if rs.Server != "nats-1" || rs.OldEpoch != 10 || rs.NewEpoch != 12 || rs.OldVersion != "2.11.8" || rs.NewVersion != "2.12.2" ||
		*rs.StreamsBefore != 1 || *rs.ConsumersBefore != 3 || *rs.StreamsAfter != 1 || *rs.ConsumersAfter != 1 ||
		len(rs.LedBefore) != 1 || rs.LedBefore[0].GetKey() != rAcc+"/ORDERS" {
		t.Errorf("restart = %+v", rs)
	}
	if len(f.Stops) != 1 || f.Stops[0].Server != "nats-2" || f.Stops[0].LastEpoch != 20 || f.Stops[0].BackEpoch != 46 {
		t.Errorf("stops = %+v", f.Stops)
	}
	gone := map[string]api.ReplayGone{}
	for _, g := range f.Gone {
		gone[g.Entity.GetKey()] = g
	}
	if len(gone) != 3 {
		t.Errorf("gone = %v", f.Gone)
	}
	if g := gone[rAcc+"/ORDERS/restock"]; g.Delete != nil || !slices.Equal(g.ServersAtA, []string{"nats-0", "nats-1"}) ||
		len(g.NotAnsweringAtB) != 0 || g.LastEpoch != 10 {
		t.Errorf("restock = %+v", g)
	}
	if g := gone[rAcc+"/ORDERS/agg"]; g.Delete == nil || g.Delete.GetClientName() != "analytics-worker" {
		t.Errorf("agg = %+v", g)
	}
	if g := gone[rAcc+"/KV_labels"]; !slices.Equal(g.ServersAtA, []string{"nats-2"}) || !slices.Equal(g.NotAnsweringAtB, []string{"nats-2"}) {
		t.Errorf("KV_labels = %+v", g)
	}
	if f.MetaLeaderAtA != "nats-1" || len(f.LeadersAtA) != 5 {
		t.Errorf("leaders at A = %v %v", f.MetaLeaderAtA, f.LeadersAtA)
	}
	if f.ClientReconnects != 1 {
		t.Errorf("reconnects = %d; the server shutdown counts, the tooling client does not", f.ClientReconnects)
	}
	if f.Nodes != (api.ReplayNodes{Upgraded: 1, Drained: 1, Pools: 1, PoolLabels: true}) {
		t.Errorf("nodes = %+v", f.Nodes)
	}
	var gaps []api.ReplayGap
	for _, g := range f.Gaps {
		if g.Kind == api.GapCollections {
			gaps = append(gaps, g)
		}
	}
	if len(gaps) != 1 || gaps[0].BeforeEpoch != 24 || gaps[0].AfterEpoch != 29 {
		t.Errorf("gaps = %+v", f.Gaps)
	}
}

func TestReplayStoryFromStore(t *testing.T) {
	p, _ := replayStore(t)
	f, err := p.Replay(ctx, replayReq(rEpoch(3), rEpoch(40)))
	if err != nil {
		t.Fatal(err)
	}
	res := api.FinishReplay(f)
	var lines []string
	for _, s := range res.GetSteps() {
		line := s.GetTitle()
		if s.GetOutsideRange() {
			line += " [after B]"
		}
		lines = append(lines, line)
	}
	want := []string{
		"Node gke-example-pool-1-7f3a drained",
		"Node gke-example-pool-1-7f3a upgraded",
		// Both gone steps are dated by the last report, epoch 10.
		"nats-1 restarts for an upgrade from 2.11.8 to 2.12.2",
		"Stream KV_labels not reported at B",
		"Durable restock on ORDERS gone without a delete event",
		"Consumer restock on ORDERS has no quorum for 2 min",
		"Durable agg on ORDERS deleted",
		"nats-2 stops answering",
		"No collections from 09:11:30 to 09:14:00",
		"Stream ORDERS has no quorum",
		"Stream quorum back: ORDERS [after B]",
		"nats-2 answers again [after B]",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("steps:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	restart := res.GetSteps()[2]
	if !strings.HasPrefix(restart.GetBody(), "Lame duck mode announced at 09:04:40.") ||
		!strings.Contains(restart.GetBody(), "Leaders it held were elected on other servers: 1 on nats-0.") ||
		!strings.Contains(restart.GetBody(), "2 consumers fewer than before the restart") {
		t.Errorf("restart body = %q", restart.GetBody())
	}
	c := res.GetCounts()
	if c.GetServersRestarted() != 1 || c.GetServersStopped() != 1 || c.GetConsumersGoneWithoutDelete() != 1 || c.GetAssetsGoneWithDelete() != 1 ||
		c.GetAssetsUnknownAtB() != 1 || c.GetNodePoolsUpgraded() != 1 || c.GetFindingsRaised() != 3 ||
		// Events come on connections of their own: the time without
		// collections (epochs 25 to 28) leaves the event counts set.
		c.GetLeaderElections() != 1 || c.GetLeadersMoved() != 1 || c.GetQuorumLostReports() != 1 || c.GetClientReconnects() != 1 {
		t.Errorf("counts = %v", c)
	}
}

func TestReplayRefusesAOlderThanRetention(t *testing.T) {
	p, _ := replayStore(t)
	p.Retention = time.Hour
	_, err := p.Replay(ctx, replayReq(rEpoch(3), rEpoch(40)))
	if !errors.Is(err, api.ErrInvalid) || !strings.Contains(err.Error(), "A is older than retention (1h0m0s)") {
		t.Errorf("err = %v", err)
	}
}

func TestReplayWithoutCollectionsOrKubernetes(t *testing.T) {
	p, _ := replayStore(t)
	p.Kubernetes = false
	// An hour after the last epoch: nothing collected, one open gap.
	f, err := p.Replay(ctx, replayReq(rEpoch(50).Add(10*time.Minute), rEpoch(50).Add(40*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	res := api.FinishReplay(f)
	c := res.GetCounts()
	if c.ServersRestarted != nil || c.ConsumersGoneWithoutDelete != nil || c.NodesUpgraded != nil || c.LeaderElections == nil {
		t.Errorf("counts = %v; event counts stay set, as events were captured", c)
	}
	gaps := 0
	for _, s := range res.GetSteps() {
		if s.GetKind() == v1.ReplayStepKind_REPLAY_STEP_KIND_GAP {
			gaps++
			if s.GetTitle() != "No collections since 09:24:30, still at B" || !s.GetOpenAtB() {
				t.Errorf("gap = %v", s)
			}
		}
	}
	if gaps != 1 {
		t.Errorf("steps = %v", res.GetSteps())
	}
}

// A process that started before B but was first collected after it is not
// a restart: its server stops answering and answers again after B.
func TestReplayProcessCollectedAfterBIsAStop(t *testing.T) {
	p, _ := replayStore(t)
	f, err := p.Replay(ctx, replayReq(rEpoch(3), rEpoch(45).Add(20*time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Restarts) != 1 || f.Restarts[0].Server != "nats-1" || len(f.Stops) != 1 || f.Stops[0].Server != "nats-2" ||
		f.Stops[0].BackEpoch != 46 {
		t.Errorf("restarts %+v, stops %+v", f.Restarts, f.Stops)
	}
}

// When the read limit cuts the events and transitions, the response says
// the steps are truncated and leaves the counts taken from them unset.
func TestReplayRowLimitIsReported(t *testing.T) {
	defer storeprov.SetReplayRowLimit(3)()
	p, _ := replayStore(t)
	// Up to epoch 22, before the time without collections.
	f, err := p.Replay(ctx, replayReq(rEpoch(3), rEpoch(22)))
	if err != nil {
		t.Fatal(err)
	}
	if !f.EventsTruncated || !f.TransitionsTruncated || len(f.Events) != 3 || len(f.Transitions) != 3 {
		t.Fatalf("truncated %v %v, %d events, %d transitions", f.EventsTruncated, f.TransitionsTruncated, len(f.Events), len(f.Transitions))
	}
	res := api.FinishReplay(f)
	c := res.GetCounts()
	until := f.Events[2].GetTime().AsTime()
	if f.Transitions[2].Time.Before(until) {
		until = f.Transitions[2].Time
	}
	if res.GetCompleteUntil() == nil || !res.GetCompleteUntil().AsTime().Equal(until) {
		t.Errorf("complete until %v, want the earlier last row read, %v", res.GetCompleteUntil(), until)
	}
	if !res.GetStepsTruncated() || c.LeaderElections != nil || c.LeadersMoved != nil || c.QuorumLostReports != nil ||
		c.FindingsRaised != nil || c.ClientReconnects == nil {
		t.Errorf("truncated %v, counts %v", res.GetStepsTruncated(), c)
	}
}

// A quorum loss that began before A is dated by the firing that started
// it, not by a later severity change.
func TestReplayQuorumSinceTheFiringBeforeA(t *testing.T) {
	p, r := replayStore(t)
	for _, tr := range []struct {
		n         int
		kind, sev string
	}{{1, "firing", "warning"}, {2, "changed", "critical"}} {
		r.add("finding_transitions", map[string]any{"finding_id": "stream-001:" + rAcc + "/KV_labels", "check_code": "stream-001",
			"entity_type": "stream", "entity_key": rAcc + "/KV_labels", "epoch": tr.n, "time": rEpoch(tr.n), "kind": tr.kind,
			"severity": tr.sev, "detail": "d"})
	}
	f, err := p.Replay(ctx, replayReq(rEpoch(3), rEpoch(40)))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range api.FinishReplay(f).GetSteps() {
		if s.GetTitle() == "Stream KV_labels has no quorum" {
			if !strings.HasPrefix(s.GetBody(), "Already without quorum at A (since 09:00:00)") {
				t.Errorf("body = %q", s.GetBody())
			}
			return
		}
	}
	t.Errorf("no quorum step for KV_labels")
}

// A server whose last answer was in A's own collection, which started
// before A, is a stop too.
func TestReplayStopLastSeenInAsCollection(t *testing.T) {
	p, _ := replayStore(t)
	f, err := p.Replay(ctx, replayReq(rEpoch(20).Add(10*time.Second), rEpoch(45).Add(20*time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	if f.EpochA != 20 || len(f.Stops) != 1 || f.Stops[0].Server != "nats-2" || f.Stops[0].LastEpoch != 20 || f.Stops[0].BackEpoch != 46 {
		t.Errorf("epoch A %d, stops %+v", f.EpochA, f.Stops)
	}
}

// A time nats-lens was not running that ended within the restart window
// before A is read, so a restart near A can weigh it.
func TestReplayReadsCollectorGapsBeforeA(t *testing.T) {
	p, r := replayStore(t)
	r.add("events", map[string]any{"id": 900, "time": rEpoch(1).Add(-5 * time.Minute), "received_at": rEpoch(1).Add(-time.Minute),
		"source": "collector", "kind": "collector.gap", "summary": "gap"})
	f, err := p.Replay(ctx, replayReq(rEpoch(3), rEpoch(40)))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(f.Gaps, func(g api.ReplayGap) bool { return g.Kind == api.GapCollector }) {
		t.Errorf("gaps = %+v", f.Gaps)
	}
}

// A node event that names no node leaves the node counts unset.
func TestReplayNodeCountsUnsetForAnUnnamedNode(t *testing.T) {
	p, r := replayStore(t)
	r.add("events", map[string]any{"id": 901, "time": rEpoch(7), "received_at": rEpoch(7), "source": "k8s", "kind": "k8s.node.drained",
		"summary": "node drained", "raw": `{}`})
	f, err := p.Replay(ctx, replayReq(rEpoch(3), rEpoch(40)))
	if err != nil {
		t.Fatal(err)
	}
	if c := api.FinishReplay(f).GetCounts(); !f.Nodes.Unnamed || c.NodesDrained != nil || c.NodePoolsUpgraded != nil {
		t.Errorf("nodes %+v, counts %v", f.Nodes, c)
	}
}

// A stop last seen in A's collection, which started before A, has its
// whole announcement window read: the lame duck early in it is found.
func TestReplayReadsTheAnnouncementWindowOfAStopInAsCollection(t *testing.T) {
	p, r := replayStore(t)
	r.add("events", map[string]any{"id": 902, "time": rEpoch(20).Add(-10*time.Minute + 5*time.Second),
		"received_at": rEpoch(20).Add(-10*time.Minute + 5*time.Second), "source": "sys", "kind": "server.lameduck", "summary": "lame duck",
		"server_name": "nats-2"})
	f, err := p.Replay(ctx, replayReq(rEpoch(20).Add(25*time.Second), rEpoch(40)))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range api.FinishReplay(f).GetSteps() {
		if s.GetTitle() == "nats-2 stops answering" {
			if !strings.HasPrefix(s.GetBody(), "Lame duck mode announced at 08:59:35.") {
				t.Errorf("body = %q", s.GetBody())
			}
			return
		}
	}
	t.Errorf("no stop step for nats-2")
}

// A B inside the missing epochs 25 to 28, with nats-lens not running from
// before B to after it: the gap step names nothing after B, neither the
// collector event's end nor the first collection after B.
func TestReplayOpenGapNamesNothingAfterBFromStore(t *testing.T) {
	p, r := replayStore(t)
	r.add("events", map[string]any{"id": 1000, "time": rEpoch(24).Add(5 * time.Second), "received_at": rEpoch(30), "source": "collector",
		"kind": "collector.gap", "summary": "no capture for 3m, from 09:11:35 to 09:14:30"})
	f, err := p.Replay(ctx, replayReq(rEpoch(3), rEpoch(26)))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range api.FinishReplay(f).GetSteps() {
		if s.GetKind() != v1.ReplayStepKind_REPLAY_STEP_KIND_GAP {
			continue
		}
		text := s.GetTitle() + " " + s.GetBody()
		for _, e := range s.GetEvidence() {
			text += " " + e.GetLabel()
			if e.GetTime().AsTime().After(rEpoch(26)) {
				t.Errorf("evidence after B: %v", e)
			}
		}
		if strings.Contains(text, "09:14") {
			t.Errorf("gap step names a time after B: %s", text)
		}
	}
}

// A node upgrade Event as the watcher wrote it before it named the node
// at the top of the payload: the node is the Event's object, and counts.
func TestReplayCountsANodeEventByItsObject(t *testing.T) {
	p, r := replayStore(t)
	r.add("events", map[string]any{"id": 1001, "time": rEpoch(7), "received_at": rEpoch(7), "source": "k8s", "kind": "k8s.node.upgrade",
		"summary": "node gke-example-pool-1-9b2e NodeUpgrade: upgrading", "raw": `{"reason":"NodeUpgrade","object":{"kind":"Node","name":"gke-example-pool-1-9b2e"}}`})
	f, err := p.Replay(ctx, replayReq(rEpoch(3), rEpoch(40)))
	if err != nil {
		t.Fatal(err)
	}
	// Its pool is not known, so the pools upgraded cannot be counted.
	if f.Nodes.Unnamed || f.Nodes.Upgraded != 2 || f.Nodes.PoolLabels {
		t.Errorf("nodes = %+v", f.Nodes)
	}
	// It folds with the upgrade of gke-example-pool-1-7f3a a minute
	// before: two named nodes, not two unnamed events.
	for _, s := range api.FinishReplay(f).GetSteps() {
		if strings.HasPrefix(s.GetTitle(), "2 nodes upgraded") {
			return
		}
	}
	t.Error("no step counts the two nodes")
}

// nats-2 led KV_labels when it last answered (epoch 20); its election on
// nats-0 after goes into nats-2's stop step.
func TestReplayStopLeadersFromStore(t *testing.T) {
	p, r := replayStore(t)
	r.add("stream_stats", map[string]any{"epoch": 20, "server_id": "S2", "account": rAcc, "stream": "KV_labels", "is_leader": true})
	r.add("events", map[string]any{"id": 1002, "time": rEpoch(20).Add(20 * time.Second), "received_at": rEpoch(20).Add(20 * time.Second),
		"source": "jetstream", "kind": "stream.leader_elected", "server_name": "nats-0", "account": rAcc, "stream": "KV_labels", "summary": "KV_labels -> nats-0"})
	f, err := p.Replay(ctx, replayReq(rEpoch(3), rEpoch(40)))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range api.FinishReplay(f).GetSteps() {
		if s.GetTitle() == "nats-2 stops answering" && strings.Contains(s.GetBody(), "Leaders it held were elected on other servers: 1 on nats-0.") {
			return
		}
	}
	t.Errorf("stops = %+v", f.Stops)
}

// The sys connection had no server from epoch 30 to 33: the event counts
// over A to B are unset, with that reason and span.
func TestReplayEventsConnectionLostFromStore(t *testing.T) {
	p, r := replayStore(t)
	for i, e := range []struct {
		n    int
		kind string
	}{{30, "collector.not_connected"}, {33, "collector.reconnected"}} {
		r.add("events", map[string]any{"id": 1100 + i, "time": rEpoch(e.n), "received_at": rEpoch(e.n), "source": "collector", "kind": e.kind,
			"summary": e.kind, "raw": `{"connection":"sys","reason":"no server reachable"}`})
	}
	f, err := p.Replay(ctx, replayReq(rEpoch(3), rEpoch(40)))
	if err != nil {
		t.Fatal(err)
	}
	res := api.FinishReplay(f)
	if c := res.GetCounts(); c.LeaderElections != nil || c.ClientReconnects != nil {
		t.Errorf("counts = %v", c)
	}
	lost := 0
	for _, u := range res.GetUnobserved() {
		if u.GetReason() == v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_EVENTS_CONNECTION_LOST && u.GetFrom().AsTime().Equal(rEpoch(30)) &&
			u.GetTo().AsTime().Equal(rEpoch(33)) {
			lost++
		}
	}
	if lost != 5 {
		t.Errorf("unobserved = %v", res.GetUnobserved())
	}
}

// nats-lens restarted while the sys connection had no server: the time
// without events ends when it started again.
func TestReplayEventsGapEndsAtTheNextStartFromStore(t *testing.T) {
	p, r := replayStore(t)
	r.add("events", map[string]any{"id": 1200, "time": rEpoch(30), "received_at": rEpoch(30), "source": "collector", "kind": "collector.not_connected",
		"summary": "not connected", "raw": `{"connection":"sys","reason":"no server reachable"}`})
	r.add("events", map[string]any{"id": 1201, "time": rEpoch(33), "received_at": rEpoch(33), "source": "collector", "kind": "collector.started",
		"summary": "collector started", "raw": `{}`})
	f, err := p.Replay(ctx, replayReq(rEpoch(3), rEpoch(40)))
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range api.FinishReplay(f).GetUnobserved() {
		if u.GetReason() == v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_EVENTS_CONNECTION_LOST && !u.GetTo().AsTime().Equal(rEpoch(33)) {
			t.Errorf("%v", u)
		}
	}
	if len(f.Gaps) == 0 || !slices.ContainsFunc(f.Gaps, func(g api.ReplayGap) bool { return g.Kind == api.GapEvents && g.End.Equal(rEpoch(33)) }) {
		t.Errorf("gaps = %+v", f.Gaps)
	}
}

// A sys connection that reconnects 5 seconds after its disconnect fails
// over: the event counts stay set; one that takes 15 seconds was lost.
func TestReplayEventsFailoverFromStore(t *testing.T) {
	for _, tc := range []struct {
		took time.Duration
		lost bool
	}{{5 * time.Second, false}, {15 * time.Second, true}} {
		p, r := replayStore(t)
		r.add("events", map[string]any{"id": 1300, "time": rEpoch(30), "received_at": rEpoch(30), "source": "collector", "kind": "collector.disconnected",
			"summary": "lost", "raw": `{"connection":"sys","reason":"EOF"}`})
		r.add("events", map[string]any{"id": 1301, "time": rEpoch(30).Add(tc.took), "received_at": rEpoch(30).Add(tc.took), "source": "collector",
			"kind": "collector.reconnected", "summary": "back", "raw": `{"connection":"sys","reason":""}`})
		f, err := p.Replay(ctx, replayReq(rEpoch(3), rEpoch(40)))
		if err != nil {
			t.Fatal(err)
		}
		if c := api.FinishReplay(f).GetCounts(); (c.LeaderElections == nil) != tc.lost {
			t.Errorf("reconnect after %v: counts %v", tc.took, c)
		}
	}
}

// A sys connection lost two days before A and never back stays a time
// without events however many other connection events came since.
func TestReplayOldEventsGapStaysReadFromStore(t *testing.T) {
	p, r := replayStore(t)
	lost := rT.Add(-48 * time.Hour)
	r.add("events", map[string]any{"id": 1400, "time": lost, "received_at": lost, "source": "collector", "kind": "collector.not_connected",
		"summary": "no server", "raw": `{"connection":"sys","reason":"no server reachable"}`})
	if _, err := p.St.DB().Exec(`INSERT INTO events (id, time, received_at, source, kind, summary, raw)
SELECT 2000 + i, ?::TIMESTAMP + i * INTERVAL 1 SECOND, ?::TIMESTAMP + i * INTERVAL 1 SECOND, 'collector', 'collector.disconnected', 'lost',
  '{"connection":"collector","reason":"EOF"}' FROM range(6000) t(i)`, lost.Add(time.Hour), lost.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	f, err := p.Replay(ctx, replayReq(rEpoch(3), rEpoch(40)))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(f.Gaps, func(g api.ReplayGap) bool { return g.Kind == api.GapEvents && g.Start.Equal(lost) && g.End.IsZero() }) {
		t.Errorf("gaps = %+v", f.Gaps)
	}
}

// A sys connection lost and back an hour before the read starts left no
// time without events at A.
func TestReplayEventsLossBeforeTheReadEndedFromStore(t *testing.T) {
	p, r := replayStore(t)
	lost := rT.Add(-time.Hour)
	for i, e := range []struct {
		at   time.Time
		kind string
	}{{lost, "collector.not_connected"}, {lost.Add(30 * time.Second), "collector.reconnected"}} {
		r.add("events", map[string]any{"id": 1500 + i, "time": e.at, "received_at": e.at, "source": "collector", "kind": e.kind,
			"summary": e.kind, "raw": `{"connection":"sys","reason":"x"}`})
	}
	f, err := p.Replay(ctx, replayReq(rEpoch(3), rEpoch(40)))
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(f.Gaps, func(g api.ReplayGap) bool { return g.Kind == api.GapEvents }) {
		t.Errorf("gaps = %+v", f.Gaps)
	}
}

// Without a node upgrade between A and B the pools upgraded are 0,
// observed.
func TestReplayNoUpgradeCountsNoPoolsFromStore(t *testing.T) {
	p, _ := replayStore(t)
	f, err := p.Replay(ctx, replayReq(rEpoch(10), rEpoch(40)))
	if err != nil {
		t.Fatal(err)
	}
	if c := api.FinishReplay(f).GetCounts(); c.NodePoolsUpgraded == nil || c.GetNodePoolsUpgraded() != 0 {
		t.Errorf("counts = %v, nodes %+v", c, f.Nodes)
	}
}
