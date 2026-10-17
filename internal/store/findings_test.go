package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

// addEpoch stores a bare epoch row starting at at.
func addEpoch(t *testing.T, s *Store, epoch int64, at time.Time) {
	t.Helper()
	if _, err := s.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, 1, 1, ['nats-0'], ['nats-0'], [], 0, false, [], NULL)`,
		epoch, at, at); err != nil {
		t.Fatal(err)
	}
}

func okRun(code string, rows ...FindingRow) CheckRun {
	return CheckRun{Code: code, EntityType: "stream", Status: RunOK, Rows: rows}
}

func frow(key, sev, detail string) FindingRow {
	return FindingRow{EntityKey: key, Severity: sev, Detail: detail}
}

func apply(t *testing.T, s *Store, epoch int64, runs ...CheckRun) ApplyResult {
	t.Helper()
	res, err := s.ApplyCheckRuns(context.Background(), epoch, runs)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func transitionKinds(t *testing.T, s *Store, id string) []string {
	t.Helper()
	ts, err := s.FindingTransitions(context.Background(), TransitionQuery{FindingID: id})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for i := len(ts) - 1; i >= 0; i-- {
		out = append(out, ts[i].Kind)
	}
	return out
}

func TestFindingLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	for i := int64(1); i <= 5; i++ {
		addEpoch(t, s, i, t0.Add(time.Duration(i)*time.Minute))
	}
	id := FindingID("stream-001", "APP/jobs")
	if r := apply(t, s, 1, okRun("stream-001", frow("APP/jobs", "warning", "first"))); r.Firing != 1 {
		t.Fatalf("epoch 1: %+v", r)
	}
	if r := apply(t, s, 2, okRun("stream-001", frow("APP/jobs", "warning", "second"))); r.Firing+r.Changed+r.Resolved != 0 {
		t.Fatalf("epoch 2 unchanged severity made transitions: %+v", r)
	}
	if r := apply(t, s, 3, okRun("stream-001", frow("APP/jobs", "critical", "third"))); r.Changed != 1 {
		t.Fatalf("epoch 3: %+v", r)
	}
	f, err := s.Finding(ctx, id, t0)
	if err != nil {
		t.Fatal(err)
	}
	if f.State != StateFiring || f.Severity != "critical" || f.Detail != "third" || f.SinceEpoch != 1 ||
		f.LastSeenEpoch != 3 || f.LastTransition != TransitionChanged || !f.Since.Equal(t0.Add(time.Minute)) {
		t.Fatalf("after change: %+v", f)
	}
	if r := apply(t, s, 4, okRun("stream-001")); r.Resolved != 1 {
		t.Fatalf("epoch 4: %+v", r)
	}
	f, _ = s.Finding(ctx, id, t0)
	if f.State != StateResolved || !f.ResolvedAt.Equal(t0.Add(4*time.Minute)) || f.Detail != "third" {
		t.Fatalf("after resolve: %+v", f)
	}
	apply(t, s, 5, okRun("stream-001", frow("APP/jobs", "info", "again")))
	f, _ = s.Finding(ctx, id, t0)
	if f.State != StateFiring || f.SinceEpoch != 5 || !f.ResolvedAt.IsZero() || f.Severity != "info" {
		t.Fatalf("after firing again: %+v", f)
	}
	want := []string{"firing", "changed", "resolved", "firing"}
	if got := transitionKinds(t, s, id); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] ||
		got[2] != want[2] || got[3] != want[3] {
		t.Fatalf("transitions = %v, want %v", got, want)
	}
	if _, err := s.ApplyCheckRuns(ctx, 5, nil); !errors.Is(err, ErrChecksApplied) {
		t.Fatalf("second apply for epoch 5: %v", err)
	}
}

// A check that failed to run leaves its findings firing; a disabled one
// resolves them.
func TestFindingErrorAndDisabled(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	for i := int64(1); i <= 3; i++ {
		addEpoch(t, s, i, t0.Add(time.Duration(i)*time.Minute))
	}
	apply(t, s, 1, okRun("stream-001", frow("A/x", "warning", "d")), okRun("stream-002", frow("A/y", "warning", "d")))
	apply(t, s, 2, CheckRun{Code: "stream-001", EntityType: "stream", Status: RunError, Error: "boom"},
		CheckRun{Code: "stream-002", EntityType: "stream", Status: RunDisabled})
	x, _ := s.Finding(ctx, "stream-001:A/x", t0)
	y, _ := s.Finding(ctx, "stream-002:A/y", t0)
	if x.State != StateFiring || y.State != StateResolved {
		t.Fatalf("x %s (want firing after an error), y %s (want resolved when disabled)", x.State, y.State)
	}
	runs, err := s.CheckRuns(ctx, t0, t0.Add(time.Hour))
	if err != nil || len(runs) != 4 {
		t.Fatalf("runs = %+v, %v", runs, err)
	}
	if runs[2].Status != RunError || runs[2].Error != "boom" || runs[3].Status != RunDisabled {
		t.Fatalf("runs of epoch 2 = %+v", runs[2:])
	}
}

// Four transitions inside an hour make a finding flapping; once they age
// out of the hour it is not flapping any more.
// A finding flaps when it fired and resolved more than three times in the
// hour: five transitions are two cycles, nine are four. Once the cycles age
// out of the hour it does not flap any more.
func TestFindingFlapping(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	for i := int64(1); i <= 10; i++ {
		addEpoch(t, s, i, t0.Add(time.Duration(i)*5*time.Minute))
	}
	addEpoch(t, s, 11, t0.Add(3*time.Hour))
	firing := okRun("stream-001", frow("A/x", "warning", "d"))
	step := func(i int64) {
		if i%2 == 1 {
			apply(t, s, i, firing)
		} else {
			apply(t, s, i, okRun("stream-001"))
		}
	}
	for i := int64(1); i <= 5; i++ {
		step(i)
	}
	f, _ := s.Finding(ctx, "stream-001:A/x", t0)
	if f.TransitionsLastHour != 5 || f.Flapping {
		t.Fatalf("after two cycles: %d transitions, flapping=%v", f.TransitionsLastHour, f.Flapping)
	}
	for i := int64(6); i <= 9; i++ {
		step(i)
	}
	f, _ = s.Finding(ctx, "stream-001:A/x", t0)
	if f.TransitionsLastHour != 9 || !f.Flapping {
		t.Fatalf("after four cycles: %d transitions, flapping=%v", f.TransitionsLastHour, f.Flapping)
	}
	apply(t, s, 10, firing)
	apply(t, s, 11, firing)
	f, _ = s.Finding(ctx, "stream-001:A/x", t0)
	if f.TransitionsLastHour != 0 || f.Flapping {
		t.Fatalf("two hours later: %d flapping=%v", f.TransitionsLastHour, f.Flapping)
	}
}

// Mutes apply when findings are read: one for the entity wins over one for
// the whole check, expired and lifted mutes do not apply, and resolved
// findings carry no mute. The tally counts muted findings apart.
func TestFindingMutesAndTally(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	addEpoch(t, s, 1, t0)
	addEpoch(t, s, 2, t0.Add(time.Minute))
	apply(t, s, 1, okRun("stream-001", frow("A/x", "critical", "d"), frow("A/y", "warning", "d"), frow("A/z", "info", "d")),
		okRun("stream-002", frow("A/x", "warning", "d")))
	apply(t, s, 2, okRun("stream-001", frow("A/x", "critical", "d"), frow("A/y", "warning", "d"), frow("A/z", "info", "d")),
		okRun("stream-002"))
	now := t0.Add(2 * time.Minute)
	mk := func(code, et, ek string, exp time.Time) Mute {
		m, err := s.CreateMute(ctx, Mute{CheckCode: code, EntityType: et, EntityKey: ek, Reason: "r", MutedBy: "op",
			CreatedAt: t0, ExpiresAt: exp})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	whole := mk("stream-001", "", "", now.Add(time.Hour))
	one := mk("stream-001", "stream", "A/y", now.Add(time.Hour))
	mk("stream-001", "stream", "A/z", now.Add(-time.Second))
	lifted := mk("stream-002", "", "", now.Add(time.Hour))
	if _, err := s.UnmuteMute(ctx, lifted.ID, "op", now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	fs, err := s.Findings(ctx, now, FindingQuery{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, f := range fs {
		if f.Mute != nil {
			got[f.ID] = f.Mute.ID
		}
	}
	if got["stream-001:A/x"] != whole.ID || got["stream-001:A/y"] != one.ID || got["stream-001:A/z"] != whole.ID ||
		got["stream-002:A/x"] != 0 {
		t.Fatalf("mutes applied = %v (whole %d, one %d)", got, whole.ID, one.ID)
	}
	tally, err := s.Tally(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if tally != (FindingTally{Muted: 3, Resolved24h: 1}) {
		t.Fatalf("tally = %+v", tally)
	}
	firing, err := s.Findings(ctx, now, FindingQuery{FiringOnly: true, CheckCode: "stream-002"})
	if err != nil || len(firing) != 0 {
		t.Fatalf("firing stream-002 = %+v, %v", firing, err)
	}
}

// Resolved findings go with the retention sweep; firing ones stay.
func TestSweepResolvedFindings(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	addEpoch(t, s, 1, t0)
	addEpoch(t, s, 2, t0.Add(time.Minute))
	apply(t, s, 1, okRun("stream-001", frow("A/x", "warning", "d"), frow("A/y", "warning", "d")))
	apply(t, s, 2, okRun("stream-001", frow("A/x", "warning", "d")))
	addEpoch(t, s, 3, t0.Add(48*time.Hour))
	if _, err := s.Sweep(ctx, t0.Add(48*time.Hour), time.Hour); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM findings`); n != 1 {
		t.Fatalf("findings after sweep = %d, want the firing one", n)
	}
	// The firing finding keeps the transition that started its episode.
	if n := count(t, s.DB(), `SELECT count(*) FROM finding_transitions WHERE finding_id = 'stream-001:A/x' AND kind = 'firing'`); n != 1 ||
		count(t, s.DB(), `SELECT count(*) FROM finding_transitions`) != 1 {
		t.Fatalf("transitions after sweep: want only A/x's firing")
	}
}

// Value, cause and related configuration come back as written.
func TestFindingFieldsRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	addEpoch(t, s, 1, t0)
	v := 0.25
	apply(t, s, 1, okRun("stream-001", FindingRow{EntityKey: "A/x", Severity: "warning", Detail: "d", Value: &v,
		CauseEventID: 42, RelatedConfig: map[string]string{"max_age": "7d"}, RelatedSource: "inventory"}))
	f, err := s.Finding(ctx, "stream-001:A/x", t0)
	if err != nil {
		t.Fatal(err)
	}
	if f.Value == nil || *f.Value != v || f.CauseEventID != 42 || f.RelatedConfig["max_age"] != "7d" || f.RelatedSource != "inventory" {
		t.Fatalf("finding = %+v", f)
	}
}

// The findings of a check that is no longer run (removed or renamed)
// resolve.
func TestFindingOfRemovedCheckResolves(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	addEpoch(t, s, 1, t0)
	addEpoch(t, s, 2, t0.Add(time.Minute))
	apply(t, s, 1, okRun("stream-001", frow("A/x", "warning", "d")), okRun("stream-099", frow("A/x", "warning", "d")))
	if r := apply(t, s, 2, okRun("stream-001", frow("A/x", "warning", "d"))); r.Resolved != 1 {
		t.Fatalf("epoch 2: %+v", r)
	}
	if f, _ := s.Finding(ctx, "stream-099:A/x", t0); f.Firing() {
		t.Fatal("finding of a removed check still fires")
	}
}

// Every transition is also an event, written in the same transaction.
func TestFindingTransitionsAreEvents(t *testing.T) {
	s := openMem(t)
	for i := int64(1); i <= 3; i++ {
		addEpoch(t, s, i, t0.Add(time.Duration(i)*time.Minute))
	}
	key := "AAPP/order-jobs/resize-worker"
	run := func(rows ...FindingRow) CheckRun {
		return CheckRun{Code: "consumer-010", EntityType: "consumer", Status: RunOK, Rows: rows}
	}
	r := frow(key, "critical", "missing")
	r.CauseEventID = 42
	apply(t, s, 1, run(r))
	apply(t, s, 2, run(frow(key, "warning", "missing, a server did not answer")))
	apply(t, s, 3, run())
	rows, err := s.DB().Query(`SELECT kind, time, account, stream, consumer, summary,
    json_extract_string(raw, '$.finding_id'), json_extract_string(raw, '$.severity'), coalesce(json_extract_string(raw, '$.cause_event_id'), '')
FROM events WHERE source = 'finding' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var kind, acc, st, con, summary, id, sev, cause string
		var at time.Time
		if err := rows.Scan(&kind, &at, &acc, &st, &con, &summary, &id, &sev, &cause); err != nil {
			t.Fatal(err)
		}
		if acc != "AAPP" || st != "order-jobs" || con != "resize-worker" || id != "consumer-010:"+key {
			t.Errorf("event %s: %s/%s/%s, finding %s", kind, acc, st, con, id)
		}
		got = append(got, kind+" "+sev+" "+cause+" "+at.UTC().Format("15:04"))
		if !strings.HasPrefix(summary, "consumer-010 ") {
			t.Errorf("summary %q", summary)
		}
	}
	want := []string{"finding.firing critical 42 23:18", "finding.changed warning  23:19", "finding.resolved warning  23:20"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("finding events:\n got %q\nwant %q", got, want)
	}
}

// 3000 new findings in one epoch are written in one transaction well
// within a second: one statement per row took about 9 seconds.
func TestApplyManyNewFindingsQuickly(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	s := openMem(t)
	addEpoch(t, s, 1, t0)
	addEpoch(t, s, 2, t0.Add(time.Minute))
	var rows []FindingRow
	for i := range 3000 {
		rows = append(rows, frow(fmt.Sprintf("c%04d", i), "warning", "slow"))
	}
	start := time.Now()
	res := apply(t, s, 1, CheckRun{Code: "client-001", EntityType: "client", Status: RunOK, Rows: rows})
	took := time.Since(start)
	t.Logf("3000 new findings: %v", took)
	if res.Firing != 3000 || took > time.Second {
		t.Fatalf("firing %d in %v", res.Firing, took)
	}
	// The next epoch updates them all and resolves half.
	start = time.Now()
	res = apply(t, s, 2, CheckRun{Code: "client-001", EntityType: "client", Status: RunOK, Rows: rows[:1500]})
	t.Logf("1500 updated, 1500 resolved: %v", time.Since(start))
	if res.Resolved != 1500 || res.Firing != 0 {
		t.Fatalf("second epoch: %+v", res)
	}
	var firing, resolved, events int
	if err := s.DB().QueryRow(`SELECT count(*) FILTER (WHERE state = 'firing'), count(*) FILTER (WHERE state = 'resolved') FROM findings`).
		Scan(&firing, &resolved); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().QueryRow(`SELECT count(*) FROM events WHERE source = 'finding'`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if firing != 1500 || resolved != 1500 || events != 4500 {
		t.Fatalf("firing %d, resolved %d, finding events %d", firing, resolved, events)
	}
}

// refireStore: one finding fires in epoch 2, resolves in 3, fires again in
// 5 with another detail; epochs are a minute apart from t0+1m.
func refireStore(t *testing.T) *Store {
	t.Helper()
	s := openMem(t)
	for i := int64(1); i <= 5; i++ {
		addEpoch(t, s, i, t0.Add(time.Duration(i)*time.Minute))
	}
	run := func(rows ...FindingRow) CheckRun { return okRun("stream-001", rows...) }
	first := frow("A/s", "warning", "first")
	v := 2.0
	first.Value = &v
	apply(t, s, 1, run())
	apply(t, s, 2, run(first))
	apply(t, s, 3, run())
	apply(t, s, 4, run())
	apply(t, s, 5, run(frow("A/s", "critical", "second")))
	return s
}

// A finding that resolved and fired again counts its resolve in the
// tally's 24 hours: the findings row alone says it fires.
func TestTallyCountsResolveTransitions(t *testing.T) {
	s := refireStore(t)
	tl, err := s.Tally(context.Background(), t0.Add(6*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if tl.Resolved24h != 1 || tl.Critical != 1 {
		t.Fatalf("tally = %+v, want 1 critical and 1 resolve", tl)
	}
}

func TestFindingsAtRebuildsPastState(t *testing.T) {
	s := refireStore(t)
	ctx := context.Background()
	at := func(from, to time.Duration) []Finding {
		t.Helper()
		fs, err := s.FindingsAt(ctx, t0.Add(from), t0.Add(to))
		if err != nil {
			t.Fatal(err)
		}
		return fs
	}
	// [t0, 2m30s): fired in epoch 2, firing at the end.
	fs := at(0, 150*time.Second)
	if len(fs) != 1 || !fs[0].Firing() || fs[0].Detail != "first" || fs[0].Severity != "warning" ||
		!fs[0].Since.Equal(t0.Add(2*time.Minute)) || fs[0].Value == nil || *fs[0].Value != 2 {
		t.Fatalf("[0, 2m30s) = %+v", fs)
	}
	// [t0, 4m30s): the first episode resolved at 3m.
	fs = at(0, 270*time.Second)
	if len(fs) != 1 || fs[0].Firing() || !fs[0].ResolvedAt.Equal(t0.Add(3*time.Minute)) || !fs[0].Since.Equal(t0.Add(2*time.Minute)) {
		t.Fatalf("[0, 4m30s) = %+v", fs)
	}
	// [3m30s, 4m30s): nothing fired in it.
	if fs := at(210*time.Second, 270*time.Second); len(fs) != 0 {
		t.Fatalf("[3m30s, 4m30s) = %+v", fs)
	}
	// [2m30s, 6m): open at the start, then the second episode.
	fs = at(150*time.Second, 6*time.Minute)
	if len(fs) != 1 || !fs[0].Firing() || fs[0].Detail != "second" || !fs[0].Since.Equal(t0.Add(5*time.Minute)) || fs[0].TransitionsLastHour != 3 {
		t.Fatalf("[2m30s, 6m) = %+v", fs)
	}
	// A mute created after the range's end does not apply to it.
	if _, err := s.CreateMute(ctx, Mute{CheckCode: "stream-001", Reason: "r", MutedBy: "op", CreatedAt: t0.Add(5*time.Minute + 30*time.Second),
		ExpiresAt: t0.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if fs := at(0, 5*time.Minute+10*time.Second); fs[0].Mute != nil {
		t.Fatalf("mute applied before it existed: %+v", fs[0].Mute)
	}
	if fs := at(0, 6*time.Minute); fs[0].Mute == nil {
		t.Fatal("mute not applied after it was created")
	}
}

func TestFindingsByIDAndCause(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	addEpoch(t, s, 1, t0)
	a, b := frow("A/x", "warning", "x"), frow("A/y", "warning", "y")
	b.CauseEventID = 7
	apply(t, s, 1, okRun("stream-001", a, b))
	byID, err := s.Findings(ctx, t0, FindingQuery{IDs: []string{"stream-001:A/x"}})
	if err != nil || len(byID) != 1 || byID[0].EntityKey != "A/x" {
		t.Fatalf("by id: %+v, %v", byID, err)
	}
	byCause, err := s.Findings(ctx, t0, FindingQuery{CauseEventIDs: []int64{7, 9}})
	if err != nil || len(byCause) != 1 || byCause[0].EntityKey != "A/y" {
		t.Fatalf("by cause: %+v, %v", byCause, err)
	}
	none, err := s.Findings(ctx, t0, FindingQuery{IDs: []string{}})
	if err != nil || len(none) != 0 {
		t.Fatalf("empty ID list: %+v, %v", none, err)
	}
}

// The sweep keeps the current episode of every finding it keeps: a finding
// firing since a swept epoch still has its firing transition, so the past
// path and the timeline know when it started.
func TestSweepKeepsTheEpisodeOfKeptFindings(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	for i := int64(1); i <= 4; i++ {
		addEpoch(t, s, i, t0.Add(time.Duration(i)*time.Minute))
	}
	apply(t, s, 1, okRun("stream-001", frow("A/firing", "warning", "w")), okRun("stream-002", frow("A/gone", "info", "x")))
	apply(t, s, 2, okRun("stream-001", frow("A/firing", "critical", "c")), okRun("stream-002"))
	apply(t, s, 3, okRun("stream-001", frow("A/firing", "critical", "c")), okRun("stream-002"))
	apply(t, s, 4, okRun("stream-001", frow("A/firing", "critical", "c")), okRun("stream-002"))
	// Keep epochs 3 and 4: the firing finding's episode (epochs 1 and 2)
	// stays; stream-002's finding resolved in epoch 2 and is swept with its
	// transitions.
	if _, err := s.Sweep(ctx, t0.Add(4*time.Minute+10*time.Second), 90*time.Second); err != nil {
		t.Fatal(err)
	}
	if got := transitionKinds(t, s, "stream-001:A/firing"); strings.Join(got, ",") != "firing,changed" {
		t.Fatalf("kept transitions = %v", got)
	}
	if got := transitionKinds(t, s, "stream-002:A/gone"); len(got) != 0 {
		t.Fatalf("a swept finding kept its transitions: %v", got)
	}
	fs, err := s.FindingsAt(ctx, t0.Add(3*time.Minute), t0.Add(4*time.Minute))
	if err != nil || len(fs) != 1 || !fs[0].Since.Equal(t0.Add(time.Minute)) || fs[0].Severity != "critical" {
		t.Fatalf("findings at epoch 3 = %+v, %v", fs, err)
	}
}

// ResolvedBetween counts [from, to), like every range: a resolve exactly
// at to belongs to the next range, where the finding shows resolved.
func TestResolvedBetweenExcludesTo(t *testing.T) {
	s := refireStore(t) // resolved at t0+3m
	ctx := context.Background()
	for _, c := range []struct {
		from, to time.Duration
		want     int
	}{{2 * time.Minute, 3 * time.Minute, 0}, {3 * time.Minute, 4 * time.Minute, 1}} {
		n, err := s.ResolvedBetween(ctx, t0.Add(c.from), t0.Add(c.to))
		if err != nil || n != c.want {
			t.Errorf("[%v, %v): %d, %v; want %d", c.from, c.to, n, err, c.want)
		}
	}
}

// Transition times, since, transitioned_at and resolved_at keep the
// epoch's start to the microsecond, as DuckDB stores it.
func TestTransitionTimesKeepMicroseconds(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	at1 := time.Date(2026, 10, 17, 3, 4, 5, 123456000, time.UTC)
	at2 := at1.Add(30*time.Second + 654321*time.Microsecond)
	addEpoch(t, s, 1, at1)
	addEpoch(t, s, 2, at2)
	apply(t, s, 1, okRun("stream-001", frow("A/s", "info", "d")))
	apply(t, s, 2, okRun("stream-001"))
	f, err := s.Finding(ctx, "stream-001:A/s", at2)
	if err != nil {
		t.Fatal(err)
	}
	if !f.Since.Equal(at1) || !f.ResolvedAt.Equal(at2) || !f.TransitionedAt.Equal(at2) {
		t.Fatalf("since %v, resolved %v, transitioned %v; want %v and %v", f.Since, f.ResolvedAt, f.TransitionedAt, at1, at2)
	}
	trs, err := s.FindingTransitions(ctx, TransitionQuery{FindingID: "stream-001:A/s"})
	if err != nil || len(trs) != 2 || !trs[0].Time.Equal(at2) || !trs[1].Time.Equal(at1) {
		t.Fatalf("transitions = %+v, %v", trs, err)
	}
	var evAt time.Time
	if err := s.DB().QueryRow(`SELECT min(time) FROM events WHERE source = 'finding'`).Scan(&evAt); err != nil || !evAt.Equal(at1) {
		t.Fatalf("finding event time %v, %v; want %v", evAt, err, at1)
	}
}

// A value that is not a finite number is stored as NULL and named in the
// result; it never fails the epoch's checks.
func TestNonFiniteValueIsStoredAsNull(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	addEpoch(t, s, 1, t0)
	nan, inf, ok := math.NaN(), math.Inf(-1), 1.5
	rows := []FindingRow{
		{EntityKey: "A/nan", Severity: "info", Detail: "d", Value: &nan},
		{EntityKey: "A/inf", Severity: "info", Detail: "d", Value: &inf},
		{EntityKey: "A/ok", Severity: "info", Detail: "d", Value: &ok},
	}
	res, err := s.ApplyCheckRuns(ctx, 1, []CheckRun{okRun("stream-001", rows...)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.DroppedValues, ",") != "stream-001:A/inf,stream-001:A/nan" || res.Firing != 3 {
		t.Fatalf("result = %+v", res)
	}
	for key, want := range map[string]*float64{"A/nan": nil, "A/inf": nil, "A/ok": &ok} {
		f, err := s.Finding(ctx, "stream-001:"+key, t0)
		if err != nil || (want == nil) != (f.Value == nil) {
			t.Errorf("%s: value %v, %v", key, f.Value, err)
		}
	}
}
