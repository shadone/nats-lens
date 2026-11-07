package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/archive"
	"github.com/shadone/nats-lens/internal/store"
)

// ackArchiveStore has an epoch in h0 and h1 and the finding stream-001 on
// APP/jobs firing from the first.
func ackArchiveStore(t *testing.T) *store.Store {
	t.Helper()
	st := openMem(t)
	for i, at := range []time.Time{h0.Add(time.Minute), h1.Add(time.Minute)} {
		if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, 1, 1, ['nats-0'], ['nats-0'], [], 0, false, [], NULL)`,
			i+1, at, at); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.ApplyCheckRuns(context.Background(), 1, []store.CheckRun{{Code: "stream-001", EntityType: "stream",
		Status: store.RunOK, Rows: []store.FindingRow{{EntityKey: "APP/jobs", Severity: "critical", Detail: "No leader."}}}}); err != nil {
		t.Fatal(err)
	}
	return st
}

func ackAt(t *testing.T, st *store.Store, by string, at time.Time) store.Ack {
	t.Helper()
	a, err := st.Acknowledge(context.Background(), store.Ack{FindingID: "stream-001:APP/jobs", AckedBy: by, Note: "on it",
		CreatedAt: at, QuietUntil: at.Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// Acknowledgements go through export and rebuild: the replaced one ended,
// the one in force on its finding, and the sequence continues after them.
func TestArchiveKeepsAcknowledgements(t *testing.T) {
	ctx := context.Background()
	st, dir := ackArchiveStore(t), t.TempDir()
	first := ackAt(t, st, "alice@example.com", h1.Add(2*time.Minute))
	second := ackAt(t, st, "bob@example.com", h1.Add(3*time.Minute))
	if n := exported(t, newArchiver(st, dir), now); n != 2 {
		t.Fatalf("exported %d hours, want 2", n)
	}
	rb := rebuilt(t, dir, now)
	all, err := rb.Acks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].ID != first.ID || all[0].EndReason != store.AckReplaced || all[0].EndedBy != "bob@example.com" ||
		all[1].ID != second.ID || !all[1].EndedAt.IsZero() || !all[1].QuietUntil.Equal(second.QuietUntil) || all[1].SinceEpoch != 1 {
		t.Fatalf("acknowledgements after rebuild = %+v", all)
	}
	f, err := rb.Finding(ctx, "stream-001:APP/jobs", now)
	if err != nil {
		t.Fatal(err)
	}
	if f.Ack == nil || f.Ack.ID != second.ID {
		t.Errorf("finding's acknowledgement after rebuild = %+v, want %d", f.Ack, second.ID)
	}
	if next := ackAt(t, rb, "carol", now); next.ID != second.ID+1 {
		t.Errorf("first acknowledgement after rebuild has ID %d, want %d", next.ID, second.ID+1)
	}
}

// An acknowledgement made, ended or swept after the newest hour was
// exported makes that hour pending again; older hours are not exported
// again.
func TestAcknowledgementChangeReexportsNewestHour(t *testing.T) {
	ctx := context.Background()
	st, dir := ackArchiveStore(t), t.TempDir()
	a := newArchiver(st, dir)
	if n := exported(t, a, now); n != 2 {
		t.Fatalf("first export = %d hours, want 2", n)
	}
	if n := exported(t, a, now); n != 0 {
		t.Fatalf("second export = %d hours, want none", n)
	}
	ack := ackAt(t, st, "alice@example.com", h1.Add(5*time.Minute))
	if n := exported(t, a, now.Add(time.Minute)); n != 1 {
		t.Fatalf("export after an acknowledgement = %d hours, want the newest one", n)
	}
	if n := exported(t, a, now.Add(2*time.Minute)); n != 0 {
		t.Fatalf("export with no change = %d hours, want none", n)
	}
	if _, err := st.EndAck(ctx, ack.ID, "alice@example.com", now); err != nil {
		t.Fatal(err)
	}
	if n := exported(t, a, now.Add(3*time.Minute)); n != 1 {
		t.Fatalf("export after an end = %d hours, want the newest one", n)
	}
	if got := scalar[time.Time](t, st, `SELECT exported_at FROM archive_hours WHERE hour = ?`, h0); !got.Equal(now) {
		t.Errorf("h0 exported at %v, want only the first export at %v", got, now)
	}
	// A sweep that deletes the ended acknowledgement is a change too.
	if _, err := st.DB().Exec(`DELETE FROM acknowledgements`); err != nil {
		t.Fatal(err)
	}
	if n := exported(t, a, now.Add(4*time.Minute)); n != 1 {
		t.Fatalf("export after a deletion = %d hours, want the newest one", n)
	}
	rb := rebuilt(t, dir, now.Add(4*time.Minute))
	if n := scalar[int](t, rb, `SELECT count(*) FROM acknowledgements`); n != 0 {
		// The newest hour holds none; h0's files never held one.
		t.Errorf("acknowledgements after rebuild = %d, want 0", n)
	}
}

// An archive written before migration 0037 has no acknowledgements files
// and a state without max_ack_id; it rebuilds, with no acknowledgement,
// no ack_changes on the hours and the sequence from 1.
func TestRebuildBeforeAcknowledgements(t *testing.T) {
	ctx := context.Background()
	st, dir := ackArchiveStore(t), t.TempDir()
	a := newArchiver(st, dir)
	if n := exported(t, a, now); n != 2 {
		t.Fatalf("exported %d hours, want 2", n)
	}
	if err := a.WriteState(ctx, now); err != nil {
		t.Fatal(err)
	}
	files, err := (archive.LocalDir{Dir: dir}).List(ctx, "acknowledgements/")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("an archive without acknowledgements has files %v", files)
	}
	// The state as a version before acknowledgements wrote it.
	path := filepath.Join(dir, archive.StateRelPath)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	delete(state, "max_ack_id")
	if raw, err = json.Marshal(state); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	rb := rebuilt(t, dir, now)
	if n := scalar[int](t, rb, `SELECT count(*) FROM acknowledgements`); n != 0 {
		t.Errorf("acknowledgements after rebuild = %d, want 0", n)
	}
	if n := scalar[int](t, rb, `SELECT count(*) FROM archive_hours WHERE ack_changes IS NOT NULL`); n != 0 {
		t.Errorf("rebuilt hours with ack_changes = %d, want 0", n)
	}
	if next := ackAt(t, rb, "alice@example.com", now); next.ID != 1 {
		t.Errorf("first acknowledgement after rebuild has ID %d, want 1", next.ID)
	}
}
