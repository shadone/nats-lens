package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// These tests compare times with timestamp columns: run them with a
// non-UTC TZ as well (TZ=Asia/Kolkata, TZ=America/Los_Angeles).

// ackStore has epochs 1 to 10, one minute apart from t0, and stream-001
// on APP/jobs firing from epoch 1; a later epoch is applied by the test.
func ackStore(t *testing.T) *Store {
	t.Helper()
	s := openMem(t)
	for i := int64(1); i <= 10; i++ {
		addEpoch(t, s, i, ackEpoch(i))
	}
	apply(t, s, 1, okRun("stream-001", frow("APP/jobs", "critical", "No leader.")))
	return s
}

func ackEpoch(e int64) time.Time { return t0.Add(time.Duration(e) * time.Minute) }

var ackFinding = FindingID("stream-001", "APP/jobs")

func acknowledge(t *testing.T, s *Store, by, note string, at time.Time, quiet time.Duration) Ack {
	t.Helper()
	a := Ack{FindingID: ackFinding, AckedBy: by, Note: note, CreatedAt: at}
	if quiet > 0 {
		a.QuietUntil = at.Add(quiet)
	}
	out, err := s.Acknowledge(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func readAck(t *testing.T, s *Store, at time.Time) *Ack {
	t.Helper()
	f, err := s.Finding(context.Background(), ackFinding, at)
	if err != nil {
		t.Fatal(err)
	}
	return f.Ack
}

// An acknowledgement belongs to the finding's current episode, shows on
// the finding from the moment it was made and is counted in the tally
// without taking the finding out of its severity.
func TestAcknowledgeShowsOnTheFindingAndInTheTally(t *testing.T) {
	ctx := context.Background()
	s := ackStore(t)
	at := ackEpoch(2).Add(30 * time.Second)
	a := acknowledge(t, s, "alice@example.com", "Upgrade in progress; watching nats-2", at, 2*time.Hour)
	if a.ID != 1 || a.SinceEpoch != 1 || a.CheckCode != "stream-001" || a.EntityType != "stream" || a.EntityKey != "APP/jobs" ||
		!a.CreatedAt.Equal(at) || !a.QuietUntil.Equal(at.Add(2*time.Hour)) || !a.EndedAt.IsZero() {
		t.Fatalf("acknowledgement = %+v", a)
	}
	if got := readAck(t, s, at.Add(-time.Second)); got != nil {
		t.Errorf("acknowledgement read a second before it was made: %+v", got)
	}
	got := readAck(t, s, at)
	if got == nil || got.ID != a.ID || got.Note != "Upgrade in progress; watching nats-2" || got.AckedBy != "alice@example.com" {
		t.Fatalf("finding's acknowledgement at its time = %+v", got)
	}
	if !got.QuietAt(at.Add(119*time.Minute)) || got.QuietAt(at.Add(2*time.Hour)) {
		t.Errorf("quiet at 1h59m %v, at 2h %v; want true, false", got.QuietAt(at.Add(119*time.Minute)), got.QuietAt(at.Add(2*time.Hour)))
	}
	tally, err := s.Tally(ctx, at)
	if err != nil {
		t.Fatal(err)
	}
	if tally.Acknowledged != 1 || tally.Critical != 1 || tally.Muted != 0 {
		t.Errorf("tally = %+v, want 1 critical, 1 acknowledged", tally)
	}
}

// An acknowledgement is written as a finding.acknowledged event of the
// finding's entity.
func TestAcknowledgementIsAnEvent(t *testing.T) {
	s := ackStore(t)
	at := ackEpoch(3)
	acknowledge(t, s, "alice@example.com", "Upgrade in progress; watching nats-2", at, 30*time.Minute)
	var kind, summary, account, stream, finding, severity, detail string
	var when time.Time
	if err := s.DB().QueryRow(`SELECT kind, summary, account, stream, time, raw->>'finding_id', raw->>'severity', raw->>'detail'
FROM events WHERE source = 'finding' AND kind = 'finding.acknowledged'`).
		Scan(&kind, &summary, &account, &stream, &when, &finding, &severity, &detail); err != nil {
		t.Fatal(err)
	}
	if summary != "stream-001 acknowledged by alice@example.com: Upgrade in progress; watching nats-2" ||
		account != "APP" || stream != "jobs" || !when.Equal(at) || finding != ackFinding || severity != "critical" ||
		detail != "acknowledged by alice@example.com: Upgrade in progress; watching nats-2" {
		t.Errorf("event: %q %q %s/%s at %v, finding %s %s, detail %q", kind, summary, account, stream, when, finding, severity, detail)
	}
	if got := AckDetail("bob", ""); got != "acknowledged by bob" {
		t.Errorf("detail without a note = %q", got)
	}
}

// A new acknowledgement of the same episode ends the one before it as
// replaced, by the one who made the new one.
func TestNewAcknowledgementReplacesTheOneBefore(t *testing.T) {
	ctx := context.Background()
	s := ackStore(t)
	first := acknowledge(t, s, "alice@example.com", "on it", ackEpoch(2), 30*time.Minute)
	second := acknowledge(t, s, "bob@example.com", "still on it, 1 more hour", ackEpoch(4), time.Hour)
	if got := readAck(t, s, ackEpoch(5)); got == nil || got.ID != second.ID {
		t.Fatalf("finding's acknowledgement = %+v, want the second", got)
	}
	all, err := s.Acks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].ID != first.ID || all[0].EndReason != AckReplaced || all[0].EndedBy != "bob@example.com" ||
		!all[0].EndedAt.Equal(ackEpoch(4)) || !all[1].EndedAt.IsZero() {
		t.Errorf("acknowledgements = %+v", all)
	}
	// Read before the second was made, the first is in force.
	if got := readAck(t, s, ackEpoch(3)); got == nil || got.ID != first.ID {
		t.Errorf("acknowledgement read before the replacement = %+v, want the first", got)
	}
}

// Ending an acknowledgement releases it; it then shows on no finding and
// cannot be ended again.
func TestEndAcknowledgement(t *testing.T) {
	ctx := context.Background()
	s := ackStore(t)
	a := acknowledge(t, s, "alice@example.com", "", ackEpoch(2), 0)
	out, err := s.EndAck(ctx, a.ID, "alice@example.com", ackEpoch(3))
	if err != nil {
		t.Fatal(err)
	}
	if out.EndReason != AckReleased || out.EndedBy != "alice@example.com" || !out.EndedAt.Equal(ackEpoch(3)) {
		t.Errorf("ended = %+v", out)
	}
	if got := readAck(t, s, ackEpoch(3)); got != nil {
		t.Errorf("an ended acknowledgement shows on the finding: %+v", got)
	}
	if _, err := s.EndAck(ctx, a.ID, "bob", ackEpoch(4)); !errors.Is(err, ErrAckEnded) {
		t.Errorf("ending twice: %v, want ErrAckEnded", err)
	}
	if _, err := s.EndAck(ctx, 99, "bob", ackEpoch(4)); !errors.Is(err, ErrAckNotFound) {
		t.Errorf("ending an unknown one: %v, want ErrAckNotFound", err)
	}
}

// Only a firing finding can be acknowledged.
func TestAcknowledgeRefusesResolvedAndUnknownFindings(t *testing.T) {
	ctx := context.Background()
	s := ackStore(t)
	apply(t, s, 2, okRun("stream-001"))
	if _, err := s.Acknowledge(ctx, Ack{FindingID: ackFinding, AckedBy: "a", CreatedAt: ackEpoch(3)}); !errors.Is(err, ErrFindingNotFiring) {
		t.Errorf("resolved finding: %v, want ErrFindingNotFiring", err)
	}
	if _, err := s.Acknowledge(ctx, Ack{FindingID: "stream-001:APP/none", AckedBy: "a", CreatedAt: ackEpoch(3)}); !errors.Is(err, ErrFindingNotFound) {
		t.Errorf("unknown finding: %v, want ErrFindingNotFound", err)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM events WHERE kind = 'finding.acknowledged'`); n != 0 {
		t.Errorf("refused acknowledgements wrote %d events", n)
	}
}

// A resolve ends an acknowledgement until resolved, and the finding firing
// again is a new episode the old acknowledgement does not cover.
func TestAcknowledgementEndsWithItsEpisode(t *testing.T) {
	ctx := context.Background()
	s := ackStore(t)
	a := acknowledge(t, s, "alice@example.com", "on it", ackEpoch(2), 0)
	if got := readAck(t, s, ackEpoch(9)); got == nil || !got.QuietAt(ackEpoch(9)) {
		t.Fatalf("until resolved, before the resolve: %+v", got)
	}
	apply(t, s, 3, okRun("stream-001"))
	if got := readAck(t, s, ackEpoch(3)); got != nil {
		t.Errorf("resolved finding carries %+v", got)
	}
	apply(t, s, 4, okRun("stream-001", frow("APP/jobs", "critical", "No leader again.")))
	if got := readAck(t, s, ackEpoch(4)); got != nil {
		t.Errorf("new episode carries the old acknowledgement %+v", got)
	}
	if _, err := s.EndAck(ctx, a.ID, "bob", ackEpoch(5)); !errors.Is(err, ErrAckEnded) {
		t.Errorf("ending the old episode's: %v, want ErrAckEnded", err)
	}
	tally, err := s.Tally(ctx, ackEpoch(4))
	if err != nil {
		t.Fatal(err)
	}
	if tally.Acknowledged != 0 {
		t.Errorf("tally.Acknowledged = %d, want 0", tally.Acknowledged)
	}
}

// After its quiet runs out an acknowledgement is still in force: it
// still says who works on the finding.
func TestAcknowledgementOutlivesItsQuiet(t *testing.T) {
	s := ackStore(t)
	acknowledge(t, s, "alice@example.com", "on it", ackEpoch(2), 30*time.Minute)
	later := ackEpoch(2).Add(31 * time.Minute)
	got := readAck(t, s, later)
	if got == nil || got.QuietAt(later) {
		t.Errorf("31 minutes later: %+v, want in force and not quiet", got)
	}
}

// A past range rebuilds the findings with the acknowledgement in force at
// its end.
func TestFindingsAtAppliesAcknowledgements(t *testing.T) {
	ctx := context.Background()
	s := ackStore(t)
	a := acknowledge(t, s, "alice@example.com", "on it", ackEpoch(4), 0)
	at := func(to time.Time) *Ack {
		t.Helper()
		fs, err := s.FindingsAt(ctx, ackEpoch(1), to)
		if err != nil {
			t.Fatal(err)
		}
		if len(fs) != 1 {
			t.Fatalf("findings at %v = %d", to, len(fs))
		}
		return fs[0].Ack
	}
	if got := at(ackEpoch(4).Add(-time.Second)); got != nil {
		t.Errorf("before the acknowledgement: %+v", got)
	}
	if got := at(ackEpoch(4).Add(time.Second)); got == nil || got.ID != a.ID {
		t.Errorf("after it: %+v", got)
	}
}

// The sweep deletes the acknowledgements ended, or whose episode ended,
// before the retention window, and keeps those of an episode still
// firing however old.
func TestSweepAcknowledgements(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	for i := int64(1); i <= 10; i++ {
		addEpoch(t, s, i, ackEpoch(i))
	}
	key := func(k string) FindingRow { return frow(k, "warning", "d") }
	// keep: fires from 1 to the end. resolved: fires 1, resolves at 3.
	// refired: fires 1, resolves 2, fires again 3. late: fires 1,
	// resolves 9.
	apply(t, s, 1, okRun("stream-001", key("A/keep"), key("A/resolved"), key("A/refired"), key("A/late"), key("A/ended")))
	mk := func(k, by string) Ack {
		t.Helper()
		a, err := s.Acknowledge(ctx, Ack{FindingID: FindingID("stream-001", k), AckedBy: by, CreatedAt: ackEpoch(1)})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	for _, k := range []string{"A/keep", "A/resolved", "A/refired", "A/late", "A/ended"} {
		mk(k, k)
	}
	ended := mk("A/ended", "replacer") // ends the first A/ended one at epoch 1
	if _, err := s.EndAck(ctx, ended.ID, "op", ackEpoch(8)); err != nil {
		t.Fatal(err)
	}
	apply(t, s, 2, okRun("stream-001", key("A/keep"), key("A/resolved"), key("A/late"), key("A/ended")))
	apply(t, s, 3, okRun("stream-001", key("A/keep"), key("A/refired"), key("A/late"), key("A/ended")))
	for e := int64(4); e <= 8; e++ {
		apply(t, s, e, okRun("stream-001", key("A/keep"), key("A/refired"), key("A/late"), key("A/ended")))
	}
	apply(t, s, 9, okRun("stream-001", key("A/keep"), key("A/refired"), key("A/ended")))
	// The window starts at epoch 5.
	if _, err := s.Sweep(ctx, ackEpoch(5).Add(time.Hour), time.Hour); err != nil {
		t.Fatal(err)
	}
	all, err := s.Acks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, a := range all {
		left = append(left, a.AckedBy)
	}
	// resolved: resolved at 3; refired: its episode ended before 3;
	// A/ended's first: replaced at 1. late resolved at 9 and the
	// released one ended at 8 are inside the window.
	want := []string{"A/keep", "A/late", "replacer"}
	if len(left) != len(want) {
		t.Fatalf("left after the sweep: %v, want %v", left, want)
	}
	for i := range want {
		if left[i] != want[i] {
			t.Fatalf("left after the sweep: %v, want %v", left, want)
		}
	}
}
