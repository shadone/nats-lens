package api_test

import (
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// run is a run of a replica behind, from epoch from (at fromAt) to last,
// reported behind in n collections.
func run(server string, e *v1.EntityRef, fromEpoch int64, fromAt time.Time, lastEpoch int64, lastAt time.Time, n int, peak, lastLag int64) api.ReplayCatchUp {
	return api.ReplayCatchUp{Server: server, Entity: e, From: fromAt, FromEpoch: fromEpoch, Last: lastAt, LastEpoch: lastEpoch, Behind: n,
		PeakLag: i64(peak), LastLag: i64(lastLag)}
}

// current ends a run in the collection after it, epoch next at at.
func current(c api.ReplayCatchUp, next int64, at time.Time) api.ReplayCatchUp {
	c.Next, c.NextEpoch, c.Current, c.CurrentEpoch = at, next, at, next
	return c
}

func catchUpOf(t *testing.T, res *v1.GetReplayResponse) []*v1.ReplayStep {
	t.Helper()
	return stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_CATCH_UP)
}

func TestReplayCatchUpAfterARestart(t *testing.T) {
	f := facts()
	f.Collected = true
	f.Restarts = []api.ReplayRestart{{Server: "nats-1", OldID: "OLD", NewID: "NEW", LastSeen: rAt(10, 0), FirstSeen: rAt(11, 30),
		Started: rAt(11, 5), OldEpoch: 120, NewEpoch: 123}}
	orders, worker := streamEnt("orders"), consumerEnt("orders", "worker")
	f.CatchUps = []api.ReplayCatchUp{
		current(run("nats-1", orders, 123, rAt(11, 30), 126, rAt(13, 0), 4, 18400, 900), 127, rAt(13, 30)),
		// One collection behind, the first of the restarted process: kept.
		current(run("nats-1", worker, 123, rAt(11, 30), 123, rAt(11, 30), 1, 40, 40), 124, rAt(12, 0)),
	}
	f.Transitions = []api.ReplayTransition{tr("stream-003:APP/orders", "stream-003", orders, rAt(12, 0), firing, warning)}
	res := api.FinishReplay(f)
	noCause(t, res)
	cs := catchUpOf(t, res)
	if len(cs) != 1 {
		t.Fatalf("catch-up steps = %v", cs)
	}
	c := cs[0]
	if c.GetTitle() != "nats-1 catches up 1 stream and 1 consumer" || !c.GetTime().AsTime().Equal(rAt(11, 30)) ||
		!c.GetEnd().AsTime().Equal(rAt(13, 30)) || c.GetOpenAtB() {
		t.Errorf("step = %q %v–%v open %v", c.GetTitle(), c.GetTime().AsTime(), c.GetEnd().AsTime(), c.GetOpenAtB())
	}
	want := "Replicas of orders and worker on orders. Current again by 09:13:30. The slowest, orders, took 2 min and was at most 18,400 operations behind."
	if c.GetBody() != want {
		t.Errorf("body = %q\nwant %q", c.GetBody(), want)
	}
	// The stream-003 firing is the step's evidence and makes it a
	// warning, not a finding step of its own.
	if c.GetSeverity() != warning {
		t.Errorf("severity = %v", c.GetSeverity())
	}
	if fs := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_FINDING); len(fs) != 0 {
		t.Errorf("finding steps = %v", fs)
	}
	var labels []string
	for _, e := range c.GetEvidence() {
		labels = append(labels, e.GetLabel())
	}
	wantEv := "collection of 09:11:30 in Placement|stream-003 stream-003 name (firing)|collection of 09:13:30 in Placement|collection of 09:13:30 for orders"
	if got := strings.Join(labels, "|"); got != wantEv {
		t.Errorf("evidence = %s\nwant %s", got, wantEv)
	}
	if e := c.GetEvidence()[3]; e.GetEntity().GetKey() != "APP/orders" || e.GetId() != "127" {
		t.Errorf("stream evidence = %v", e)
	}
	r := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART)
	if len(r) != 1 || !strings.HasSuffix(r[0].GetBody(), "Its replicas were current again by 09:13:30.") {
		t.Errorf("restart = %v", r)
	}
}

func TestReplayCatchUpLeavesOutABurst(t *testing.T) {
	f := facts()
	f.CatchUps = []api.ReplayCatchUp{current(run("nats-2", streamEnt("orders"), 140, rAt(20, 0), 140, rAt(20, 0), 1, 3000, 3000), 141, rAt(20, 30))}
	if cs := catchUpOf(t, api.FinishReplay(f)); len(cs) != 0 {
		t.Errorf("a one-collection run makes a step: %v", cs)
	}
}

func TestReplayCatchUpStillBehindAtB(t *testing.T) {
	f := facts()
	f.CatchUps = []api.ReplayCatchUp{
		current(run("nats-2", streamEnt("payments"), 140, rAt(20, 0), 142, rAt(21, 0), 3, 500, 100), 143, rAt(21, 30)),
		run("nats-2", streamEnt("orders"), 140, rAt(20, 0), 220, rAt(59, 50), 80, 30000, 18400),
		run("nats-2", streamEnt("audit"), 141, rAt(20, 30), 220, rAt(59, 50), 79, 900, 700),
	}
	res := api.FinishReplay(f)
	noCause(t, res)
	cs := catchUpOf(t, res)
	if len(cs) != 1 {
		t.Fatalf("catch-up steps = %v", cs)
	}
	c := cs[0]
	if c.GetTitle() != "nats-2 still catching up at B: 2 streams behind, the largest 18,400 operations behind" || !c.GetOpenAtB() ||
		c.GetEnd() != nil || c.GetSeverity() != warning {
		t.Errorf("step = %q open %v end %v severity %v", c.GetTitle(), c.GetOpenAtB(), c.GetEnd(), c.GetSeverity())
	}
	want := "Replicas of orders, payments and audit. 1 stream current again by 09:21:30. The slowest, payments, took 2 min and was at most " +
		"500 operations behind. Behind at B: orders and audit."
	if c.GetBody() != want {
		t.Errorf("body = %q\nwant %q", c.GetBody(), want)
	}
}

func TestReplayCatchUpOneStillBehindAtB(t *testing.T) {
	f := facts()
	f.CatchUps = []api.ReplayCatchUp{run("nats-0", streamEnt("orders"), 100, f.EpochAAt, 220, rAt(59, 50), 120, 30000, 18400)}
	c := catchUpOf(t, api.FinishReplay(f))
	if len(c) != 1 || c[0].GetTitle() != "nats-0 still catching up at B: orders 18,400 operations behind" ||
		c[0].GetBody() != "Replicas of orders. Behind already in the collection at A." {
		t.Errorf("steps = %v", c)
	}
}

// A time without collections, or without the leader's report, between
// the last collection behind and the first current hides when the
// replica caught up: the step does not claim it did.
func TestReplayCatchUpEndHidden(t *testing.T) {
	gap := current(run("nats-2", streamEnt("orders"), 120, rAt(10, 0), 140, rAt(20, 0), 21, 4000, 3000), 141, rAt(25, 0))
	missing := run("nats-2", streamEnt("orders"), 120, rAt(10, 0), 140, rAt(20, 0), 21, 4000, 3000)
	missing.Next, missing.NextEpoch, missing.Current, missing.CurrentEpoch = rAt(20, 30), 141, rAt(21, 30), 143
	for _, c := range []struct {
		name string
		run  api.ReplayCatchUp
		body string
		end  time.Time
	}{
		{"gap", gap, "Replicas of orders. 1 stream was current by 09:25:00, but when it caught up was not observed: no collection from 09:20:00 to 09:25:00.", rAt(25, 0)},
		{"missing report", missing, "Replicas of orders. 1 stream was current by 09:21:30, but when it caught up was not observed: the leader did not report the replica from 09:20:30 to 09:21:30.", rAt(21, 30)},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := facts()
			f.CatchUps = []api.ReplayCatchUp{c.run}
			res := api.FinishReplay(f)
			noCause(t, res)
			cs := catchUpOf(t, res)
			if len(cs) != 1 {
				t.Fatalf("steps = %v", cs)
			}
			s := cs[0]
			if s.GetTitle() != "nats-2 falls behind on 1 stream" || s.GetBody() != c.body || !s.GetEnd().AsTime().Equal(c.end) {
				t.Errorf("step = %q %q end %v", s.GetTitle(), s.GetBody(), s.GetEnd().AsTime())
			}
		})
	}
}

func TestReplayCatchUpNotObservedAtB(t *testing.T) {
	f := facts()
	c := run("nats-2", consumerEnt("orders", "worker"), 120, rAt(10, 0), 180, rAt(40, 0), 61, 4000, 3000)
	c.Next, c.NextEpoch = rAt(40, 30), 181
	f.CatchUps = []api.ReplayCatchUp{c}
	f.Restarts = []api.ReplayRestart{{Server: "nats-2", LastSeen: rAt(8, 0), FirstSeen: rAt(10, 0), OldEpoch: 116, NewEpoch: 120}}
	res := api.FinishReplay(f)
	cs := catchUpOf(t, res)
	if len(cs) != 1 || cs[0].GetTitle() != "nats-2 falls behind on 1 consumer" || !cs[0].GetOpenAtB() || cs[0].GetSeverity() != v1.Severity_SEVERITY_UNSPECIFIED ||
		cs[0].GetBody() != "Replicas of worker on orders. 1 consumer not observed at B: the leader did not report the replica after 09:40:00." {
		t.Errorf("steps = %v", cs)
	}
	r := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART)
	if len(r) != 1 || !strings.HasSuffix(r[0].GetBody(), "When its replicas were current again was not observed.") {
		t.Errorf("restart = %v", r)
	}
}

func TestReplayCatchUpOneStepPerServer(t *testing.T) {
	f := facts()
	var runs []api.ReplayCatchUp
	for i := range 60 {
		name := "s" + string(rune('a'+i/26)) + string(rune('a'+i%26))
		runs = append(runs, current(run("nats-1", streamEnt(name), 120, rAt(10, 0), 122, rAt(11, 0), 3, int64(100+i), 10), 123, rAt(11, 30)))
	}
	runs = append(runs, current(run("nats-3", streamEnt("orders"), 130, rAt(15, 0), 131, rAt(15, 30), 2, 10, 10), 132, rAt(16, 0)))
	f.CatchUps = runs
	res := api.FinishReplay(f)
	cs := catchUpOf(t, res)
	if len(cs) != 2 {
		t.Fatalf("steps = %d", len(cs))
	}
	s := cs[0]
	// The server and the first 49 assets; the count covers them all.
	if s.GetTitle() != "nats-1 catches up 60 streams" || s.GetEntityCount() != 61 || len(s.GetEntities()) != 50 ||
		!strings.HasPrefix(s.GetBody(), "Replicas of saa, sab, sac and 57 more. Current again by 09:11:30. The slowest, sch,") {
		t.Errorf("step = %q %q entities %d of %d", s.GetTitle(), s.GetBody(), len(s.GetEntities()), s.GetEntityCount())
	}
}

func TestReplayCatchUpCutMarksStepsTruncated(t *testing.T) {
	f := facts()
	f.CatchUpsTruncated = true
	if res := api.FinishReplay(f); !res.GetStepsTruncated() || res.GetCompleteUntil() != nil {
		t.Errorf("truncated %v, complete until %v", res.GetStepsTruncated(), res.GetCompleteUntil())
	}
}
