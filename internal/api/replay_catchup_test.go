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
		{"missing report", missing, "Replicas of orders. 1 stream was current by 09:21:30, but when it caught up was not observed: the replica was not reported behind or current from 09:20:30.", rAt(21, 30)},
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
	// Not observed at B and none behind there: the span ends at the last
	// collection behind, not open at B.
	if len(cs) != 1 || cs[0].GetTitle() != "nats-2 falls behind on 1 consumer" || cs[0].GetOpenAtB() || !cs[0].GetEnd().AsTime().Equal(rAt(40, 0)) ||
		cs[0].GetSeverity() != v1.Severity_SEVERITY_UNSPECIFIED ||
		cs[0].GetBody() != "Replicas of worker on orders. 1 consumer not observed at B: the replica was not reported behind or current from 09:40:30." {
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
	runs = append(runs, current(run("nats-3", streamEnt("orders"), 130, rAt(15, 0), 131, rAt(15, 30), 2, 1000, 10), 132, rAt(16, 0)))
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
	if res := api.FinishReplay(f); !res.GetStepsTruncated() || !res.GetCatchUpsTruncated() || res.GetCompleteUntil() != nil {
		t.Errorf("truncated %v, complete until %v", res.GetStepsTruncated(), res.GetCompleteUntil())
	}
}

// Two restarts of one server 30 minutes apart, each followed by a short
// catch-up, and a stream-003 firing between them: two steps, each restart
// tied to its own, and the firing a finding step of its own.
func TestReplayCatchUpTwoEpisodes(t *testing.T) {
	f := facts()
	f.Collected = true
	f.Restarts = []api.ReplayRestart{
		{Server: "nats-1", OldID: "A1", NewID: "B1", LastSeen: rAt(10, 0), FirstSeen: rAt(11, 0), OldEpoch: 120, NewEpoch: 122},
		{Server: "nats-1", OldID: "B1", NewID: "C1", LastSeen: rAt(40, 0), FirstSeen: rAt(41, 0), OldEpoch: 180, NewEpoch: 182},
	}
	orders := streamEnt("orders")
	f.CatchUps = []api.ReplayCatchUp{
		current(run("nats-1", orders, 122, rAt(11, 0), 124, rAt(12, 0), 3, 500, 100), 125, rAt(12, 30)),
		current(run("nats-1", orders, 182, rAt(41, 0), 184, rAt(42, 0), 3, 700, 100), 185, rAt(42, 30)),
	}
	f.Transitions = []api.ReplayTransition{tr("stream-003:APP/orders", "stream-003", orders, rAt(25, 0), firing, warning)}
	res := api.FinishReplay(f)
	var got []string
	for _, s := range catchUpOf(t, res) {
		got = append(got, s.GetTime().AsTime().Format("15:04:05")+"-"+s.GetEnd().AsTime().Format("15:04:05")+" "+s.GetBody())
	}
	want := []string{
		"09:11:00-09:12:30 Replicas of orders. Current again by 09:12:30. It took 2 min and was at most 500 operations behind.",
		"09:41:00-09:42:30 Replicas of orders. Current again by 09:42:30. It took 2 min and was at most 700 operations behind.",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("catch-ups:\n%s", strings.Join(got, "\n"))
	}
	var restarts []string
	for _, s := range stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART) {
		restarts = append(restarts, s.GetBody())
	}
	if strings.Join(restarts, "|") != "Back at 09:11:00. Its replicas were current again by 09:12:30.|Back at 09:41:00. Its replicas were current again by 09:42:30." {
		t.Errorf("restarts = %q", restarts)
	}
	if fs := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_FINDING); len(fs) != 1 {
		t.Errorf("finding steps = %v", fs)
	}
}

// Each distinct reason that hides an end is told with its assets.
func TestReplayCatchUpEachReason(t *testing.T) {
	f := facts()
	gap := current(run("nats-2", streamEnt("pay"), 120, rAt(10, 0), 140, rAt(20, 0), 21, 4000, 3000), 141, rAt(25, 0))
	one := run("nats-2", streamEnt("orders"), 120, rAt(10, 0), 140, rAt(20, 0), 21, 4000, 3000)
	one.Next, one.NextEpoch, one.Current, one.CurrentEpoch = rAt(20, 30), 141, rAt(21, 0), 142
	f.CatchUps = []api.ReplayCatchUp{gap, one}
	cs := catchUpOf(t, api.FinishReplay(f))
	want := "Replicas of orders and pay. 2 streams were current by 09:25:00, but when they caught up was not observed: " +
		"the replica was not reported behind or current in the collection of 09:20:30 (orders); no collection from 09:20:00 to 09:25:00 (pay)."
	if len(cs) != 1 || cs[0].GetBody() != want {
		t.Errorf("steps = %v\nwant %q", cs, want)
	}
}

// A busy stream a few operations behind for two collections makes no
// step; a run without a reported lag does.
func TestReplayCatchUpMinimumLag(t *testing.T) {
	f := facts()
	small := current(run("nats-2", streamEnt("orders"), 140, rAt(20, 0), 141, rAt(20, 30), 2, api.CatchUpMinLag-1, 10), 142, rAt(21, 0))
	unknown := current(run("nats-3", streamEnt("orders"), 140, rAt(20, 0), 141, rAt(20, 30), 2, 0, 0), 142, rAt(21, 0))
	unknown.PeakLag, unknown.LastLag = nil, nil
	f.CatchUps = []api.ReplayCatchUp{small, unknown}
	cs := catchUpOf(t, api.FinishReplay(f))
	if len(cs) != 1 || cs[0].GetTitle() != "nats-3 catches up 1 stream" {
		t.Errorf("steps = %v", cs)
	}
}

// A restarted server's replica that its leader first reports offline and
// then behind in one collection still starts with the restart.
func TestReplayCatchUpOneCollectionAfterOffline(t *testing.T) {
	f := facts()
	f.Restarts = []api.ReplayRestart{{Server: "nats-1", LastSeen: rAt(10, 0), FirstSeen: rAt(11, 0), OldEpoch: 120, NewEpoch: 122}}
	f.CatchUps = []api.ReplayCatchUp{current(run("nats-1", streamEnt("orders"), 123, rAt(11, 30), 123, rAt(11, 30), 1, 40, 40), 124, rAt(12, 0))}
	if cs := catchUpOf(t, api.FinishReplay(f)); len(cs) != 1 {
		t.Errorf("steps = %v", cs)
	}
}

// A stream-003 transition whose detail names other replicas behind is
// not this server's: it stays a finding step.
func TestReplayCatchUpTakesInOnlyItsOwnLag(t *testing.T) {
	f := facts()
	orders := streamEnt("orders")
	f.CatchUps = []api.ReplayCatchUp{current(run("nats-1", orders, 120, rAt(10, 0), 124, rAt(12, 0), 5, 5000, 100), 125, rAt(12, 30))}
	other := tr("stream-003:APP/orders", "stream-003", orders, rAt(11, 0), firing, warning)
	other.Detail = "Leader nats-0 reports nats-2 4000 operations behind."
	f.Transitions = []api.ReplayTransition{other}
	res := api.FinishReplay(f)
	if fs := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_FINDING); len(fs) != 1 {
		t.Errorf("finding steps = %v", fs)
	}
	f.Transitions[0].Detail = "Leader nats-0 reports nats-1 4000 operations behind, nats-2 1200 operations behind."
	res = api.FinishReplay(f)
	if fs := stepsOf(res, v1.ReplayStepKind_REPLAY_STEP_KIND_FINDING); len(fs) != 0 || catchUpOf(t, res)[0].GetSeverity() != warning {
		t.Errorf("finding steps = %v", fs)
	}
}

// A replica behind twice in one episode took from the start of its last
// run.
func TestReplayCatchUpTookFromTheLastRun(t *testing.T) {
	f := facts()
	orders, pay := streamEnt("orders"), streamEnt("pay")
	f.CatchUps = []api.ReplayCatchUp{
		current(run("nats-1", orders, 120, rAt(10, 0), 122, rAt(11, 0), 3, 500, 100), 123, rAt(11, 30)),
		current(run("nats-1", pay, 120, rAt(10, 0), 129, rAt(14, 30), 10, 800, 100), 130, rAt(15, 0)),
		current(run("nats-1", orders, 126, rAt(13, 0), 131, rAt(15, 30), 6, 900, 100), 132, rAt(16, 0)),
	}
	cs := catchUpOf(t, api.FinishReplay(f))
	if len(cs) != 1 || !strings.HasSuffix(cs[0].GetBody(), "The slowest, orders, took 3 min and was at most 900 operations behind.") {
		t.Errorf("steps = %v", cs)
	}
}
