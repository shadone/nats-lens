package api_test

import (
	"strings"
	"testing"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// dumpCatchUps writes the restart and catch-up steps, one line each.
func dumpCatchUps(t *testing.T, res *v1.GetReplayResponse) string {
	t.Helper()
	var out []string
	for _, s := range res.GetSteps() {
		if s.GetKind() == v1.ReplayStepKind_REPLAY_STEP_KIND_CATCH_UP || s.GetKind() == v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART {
			end := ""
			if s.GetEnd() != nil {
				end = s.GetEnd().AsTime().Format("15:04:05")
			}
			out = append(out, strings.TrimPrefix(s.GetKind().String(), "REPLAY_STEP_KIND_")+" "+s.GetTime().AsTime().Format("15:04:05")+"-"+end+" "+s.GetTitle()+" | "+s.GetBody())
		}
	}
	return strings.Join(out, "\n")
}

// One replica current while another still behind; a third starting
// meanwhile joins the same catch-up.
func TestReplayCatchUpLongCatchUpNotAGap(t *testing.T) {
	f := facts()
	f.CatchUps = []api.ReplayCatchUp{
		current(run("nats-1", streamEnt("x"), 120, rAt(10, 0), 124, rAt(12, 0), 5, 500, 100), 125, rAt(12, 30)),
		current(run("nats-1", streamEnt("y"), 120, rAt(10, 0), 140, rAt(20, 0), 21, 900, 100), 141, rAt(20, 30)),
		current(run("nats-1", streamEnt("z"), 130, rAt(15, 0), 132, rAt(16, 0), 3, 300, 100), 133, rAt(16, 30)),
	}
	res := api.FinishReplay(f)
	if cs := catchUpOf(t, res); len(cs) != 1 {
		t.Errorf("\n%s", dumpCatchUps(t, res))
	}
}

// A replica behind across a restart of its server, and another starting
// after the restart.
func TestReplayCatchUpRestartDuringCatchUp(t *testing.T) {
	f := facts()
	f.Collected = true
	f.Restarts = []api.ReplayRestart{{Server: "nats-1", OldID: "A", NewID: "B", LastSeen: rAt(12, 0), FirstSeen: rAt(13, 0), OldEpoch: 124, NewEpoch: 126}}
	f.CatchUps = []api.ReplayCatchUp{
		current(run("nats-1", streamEnt("x"), 120, rAt(10, 0), 132, rAt(16, 0), 11, 5000, 100), 133, rAt(16, 30)),
		current(run("nats-1", streamEnt("w"), 126, rAt(13, 0), 128, rAt(14, 0), 3, 500, 100), 129, rAt(14, 30)),
	}
	res := api.FinishReplay(f)
	want := "RESTART 09:12:00-09:13:00 nats-1 restarts | Back at 09:13:00. Its replicas were current again by 09:16:30."
	if got := dumpCatchUps(t, res); !strings.Contains(got, want) {
		t.Errorf("got\n%s\nwant %s", got, want)
	}
}

// A replica behind from the server's lame duck mode, before its old
// process last answered, until after the restart: the restart tells it.
func TestReplayCatchUpFromLameDuck(t *testing.T) {
	f := facts()
	f.Collected = true
	f.Restarts = []api.ReplayRestart{{Server: "nats-1", OldID: "A", NewID: "B", LastSeen: rAt(12, 0), FirstSeen: rAt(13, 0), OldEpoch: 124, NewEpoch: 126}}
	f.CatchUps = []api.ReplayCatchUp{current(run("nats-1", streamEnt("x"), 120, rAt(10, 0), 128, rAt(14, 0), 7, 5000, 100), 129, rAt(14, 30))}
	want := "RESTART 09:12:00-09:13:00 nats-1 restarts | Back at 09:13:00. Its replicas were current again by 09:14:30."
	if got := dumpCatchUps(t, api.FinishReplay(f)); !strings.Contains(got, want) {
		t.Errorf("got\n%s\nwant %s", got, want)
	}
}

// Three restarts in a row, each with overlapping catch-ups.
func TestReplayCatchUpThreeRestarts(t *testing.T) {
	f := facts()
	f.Collected = true
	f.Restarts = []api.ReplayRestart{
		{Server: "nats-1", OldID: "A", NewID: "B", LastSeen: rAt(10, 0), FirstSeen: rAt(11, 0), OldEpoch: 120, NewEpoch: 122},
		{Server: "nats-1", OldID: "B", NewID: "C", LastSeen: rAt(13, 0), FirstSeen: rAt(14, 0), OldEpoch: 126, NewEpoch: 128},
		{Server: "nats-1", OldID: "C", NewID: "D", LastSeen: rAt(16, 0), FirstSeen: rAt(17, 0), OldEpoch: 132, NewEpoch: 134},
	}
	f.CatchUps = []api.ReplayCatchUp{
		current(run("nats-1", streamEnt("a"), 122, rAt(11, 0), 124, rAt(12, 0), 3, 500, 100), 125, rAt(12, 30)),
		current(run("nats-1", streamEnt("b"), 128, rAt(14, 0), 130, rAt(15, 0), 3, 600, 100), 131, rAt(15, 30)),
		current(run("nats-1", streamEnt("c"), 134, rAt(17, 0), 136, rAt(18, 0), 3, 700, 100), 137, rAt(18, 30)),
	}
	res := api.FinishReplay(f)
	got := dumpCatchUps(t, res)
	for _, want := range []string{"Back at 09:11:00. Its replicas were current again by 09:12:30.", "Back at 09:14:00. Its replicas were current again by 09:15:30.",
		"Back at 09:17:00. Its replicas were current again by 09:18:30."} {
		if !strings.Contains(got, want) {
			t.Errorf("got\n%s\nwant %s", got, want)
		}
	}
	if cs := catchUpOf(t, res); len(cs) != 3 {
		t.Errorf("catch-ups = %d", len(cs))
	}
}

// The keep rule's boundaries: two collections with a peak of 100 kept; n1 at FirstSeen+3 intervals kept, +3
// intervals and 1 s dropped, 1 s before FirstSeen dropped.
func TestReplayCatchUpKeepBoundaries(t *testing.T) {
	f := facts()
	f.Restarts = []api.ReplayRestart{{Server: "nats-1", LastSeen: rAt(10, 0), FirstSeen: rAt(11, 0), OldEpoch: 120, NewEpoch: 122}}
	f.CatchUps = []api.ReplayCatchUp{
		current(run("nats-2", streamEnt("c"), 140, rAt(20, 0), 141, rAt(20, 30), 2, api.CatchUpMinLag, 10), 142, rAt(21, 0)),
		current(run("nats-1", streamEnt("a"), 125, rAt(12, 30), 125, rAt(12, 30), 1, 10, 10), 126, rAt(13, 0)),
		current(run("nats-1", streamEnt("b"), 125, rAt(12, 31), 125, rAt(12, 31), 1, 10, 10), 126, rAt(13, 0)),
		current(run("nats-1", streamEnt("d"), 121, rAt(10, 59), 121, rAt(10, 59), 1, 10, 10), 122, rAt(11, 0)),
	}
	res := api.FinishReplay(f)
	var names []string
	for _, s := range catchUpOf(t, res) {
		for _, e := range s.GetEntities()[1:] {
			names = append(names, e.GetName())
		}
	}
	if strings.Join(names, ",") != "a,c" {
		t.Errorf("kept %v\n%s", names, dumpCatchUps(t, res))
	}
}

// A catch-up reported current at the old process's last answer was over
// before the restart: the restart does not name it.
func TestReplayCatchUpOverBeforeTheRestart(t *testing.T) {
	f := facts()
	f.Collected = true
	f.Restarts = []api.ReplayRestart{{Server: "nats-1", OldID: "A", NewID: "B", LastSeen: rAt(12, 0), FirstSeen: rAt(13, 0), OldEpoch: 124, NewEpoch: 126}}
	f.CatchUps = []api.ReplayCatchUp{current(run("nats-1", streamEnt("x"), 120, rAt(10, 0), 123, rAt(11, 30), 4, 5000, 100), 124, rAt(12, 0))}
	want := "RESTART 09:12:00-09:13:00 nats-1 restarts | Back at 09:13:00."
	if got := dumpCatchUps(t, api.FinishReplay(f)); !strings.Contains(got, want+"\n") && !strings.HasSuffix(got, want) {
		t.Errorf("got\n%s\nwant %s", got, want)
	}
}

// A replica behind at the old process's last answer and then never
// reported again makes the restart's catch-up not observed, though a later
// one was seen current.
func TestReplayCatchUpHiddenEndBeforeAnObservedOne(t *testing.T) {
	f := facts()
	f.Collected = true
	f.Restarts = []api.ReplayRestart{{Server: "nats-1", OldID: "A", NewID: "B", LastSeen: rAt(12, 0), FirstSeen: rAt(13, 0), OldEpoch: 124, NewEpoch: 126}}
	h := run("nats-1", streamEnt("h"), 126, rAt(13, 0), 128, rAt(14, 0), 3, 5000, 100)
	h.Next, h.NextEpoch = rAt(14, 30), 129
	f.CatchUps = []api.ReplayCatchUp{h, current(run("nats-1", streamEnt("o"), 130, rAt(15, 0), 132, rAt(16, 0), 3, 500, 100), 133, rAt(16, 30))}
	want := "Back at 09:13:00. When its replicas were current again was not observed."
	if got := dumpCatchUps(t, api.FinishReplay(f)); !strings.Contains(got, want) {
		t.Errorf("got\n%s\nwant %s", got, want)
	}
}

// A replica behind at the old process's last answer, last reported
// behind exactly then and never again: still under way at the restart.
func TestReplayCatchUpBehindAtTheLastAnswer(t *testing.T) {
	f := facts()
	f.Collected = true
	f.Restarts = []api.ReplayRestart{{Server: "nats-1", OldID: "A", NewID: "B", LastSeen: rAt(12, 0), FirstSeen: rAt(13, 0), OldEpoch: 124, NewEpoch: 126}}
	h := run("nats-1", streamEnt("h"), 120, rAt(10, 0), 124, rAt(12, 0), 5, 5000, 100)
	h.Next, h.NextEpoch = rAt(12, 30), 125
	f.CatchUps = []api.ReplayCatchUp{h}
	want := "Back at 09:13:00. When its replicas were current again was not observed."
	if got := dumpCatchUps(t, api.FinishReplay(f)); !strings.Contains(got, want) {
		t.Errorf("got\n%s\nwant %s", got, want)
	}
}
