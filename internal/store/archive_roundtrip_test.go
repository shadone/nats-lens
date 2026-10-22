package store_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/archive"
	"github.com/shadone/nats-lens/internal/events"
	"github.com/shadone/nats-lens/internal/store"
)

var (
	h0  = time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)
	h1  = h0.Add(time.Hour)
	h2  = h0.Add(2 * time.Hour)
	now = h2.Add(30 * time.Minute)
)

func event(at time.Time, kind string) events.Event {
	return events.Event{Time: at, ReceivedAt: at, Source: events.SourceSys, Kind: kind, ServerName: "nats-0",
		Summary: kind, Raw: json.RawMessage(`{"k":"` + kind + `"}`)}
}

func newArchiver(st *store.Store, dir string) *archive.Archiver {
	return &archive.Archiver{
		Store: st, Dest: archive.LocalDir{Dir: dir}, Retention: 14 * 24 * time.Hour, Grace: time.Minute,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func scalar[T any](t *testing.T, st *store.Store, q string, args ...any) T {
	t.Helper()
	var v T
	if err := st.DB().QueryRow(q, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return v
}

// Export closed hours, rebuild a new database from the files and compare
// it with the original, table by table.
func TestArchiveRoundTrip(t *testing.T) {
	ctx := context.Background()
	dataDir, archiveDir := t.TempDir(), t.TempDir()
	st, err := store.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	// Epochs 1 and 2 in h0, 3 in h1, 4 in the open hour h2.
	for _, at := range []time.Time{h0.Add(10 * time.Minute), h0.Add(40 * time.Minute), h1.Add(5 * time.Minute), h2.Add(time.Minute)} {
		if _, err := st.WriteSnapshot(ctx, store.FixtureSnapshot(t, at)); err != nil {
			t.Fatal(err)
		}
	}
	// Findings change in every epoch: A fires, changes and resolves; B
	// fires in epoch 2 and resolves in the open hour.
	row := func(key, sev string) store.FindingRow {
		return store.FindingRow{EntityKey: key, Severity: sev, Detail: key + " " + sev}
	}
	for epoch := int64(1); epoch <= 4; epoch++ {
		rows := map[int64][]store.FindingRow{
			1: {row("APP/a", "warning")}, 2: {row("APP/a", "critical"), row("APP/b", "info")},
			3: {row("APP/b", "info")},
		}[epoch]
		if _, err := st.ApplyCheckRuns(ctx, epoch, []store.CheckRun{{Code: "stream-001", EntityType: "stream", Status: store.RunOK, Rows: rows}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.WriteEvents(ctx, []events.Event{
		event(h0.Add(time.Minute), "client.connect"), event(h1.Add(59*time.Minute), "consumer.deleted"),
		event(h2.Add(time.Minute), "server.shutdown"),
	}); err != nil {
		t.Fatal(err)
	}

	lifted, err := st.CreateMute(ctx, store.Mute{CheckCode: "stream-025", Reason: "known", MutedBy: "op",
		CreatedAt: h0, ExpiresAt: h0.Add(48 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateMute(ctx, store.Mute{CheckCode: "server-018", EntityType: "server", EntityKey: "nats-0",
		Reason: "noisy", MutedBy: "op", CreatedAt: h0, ExpiresAt: h0.Add(24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}

	a := newArchiver(st, archiveDir)
	if n, err := a.Export(ctx, now); err != nil || n != 2 {
		t.Fatalf("exported %d hours, err %v; want h0 and h1", n, err)
	}
	if n, err := a.Export(ctx, now); err != nil || n != 0 {
		t.Fatalf("second run exported %d hours, err %v; want none", n, err)
	}
	for _, rel := range []string{
		"epochs/date=2026-10-16/hour=22.", "server_stats/date=2026-10-16/hour=23.",
		"streams/date=2026-10-16/hour=22.", "consumer_config/date=2026-10-16/hour=23.",
		"events/date=2026-10-16/hour=23.",
	} {
		if got, _ := (archive.LocalDir{Dir: archiveDir}).List(ctx, rel); len(got) != 1 {
			t.Errorf("files %s* = %v, want one", rel, got)
		}
	}
	if got, _ := (archive.LocalDir{Dir: archiveDir}).List(ctx, "epochs/date=2026-10-17/hour=00."); len(got) != 0 {
		t.Error("the open hour was exported")
	}
	// Re-exporting replaces the hour's files and leaves no staged files.
	// h1 is exported again after a mute was lifted: h0 keeps the active
	// copy, and the rebuild must keep the lifted one.
	if err := a.ExportHour(ctx, h0, true, now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UnmuteMute(ctx, lifted.ID, "lead", h1.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := a.ExportHour(ctx, h1, true, now); err != nil {
		t.Fatal(err)
	}
	if staged, _ := os.ReadDir(filepath.Join(archiveDir, ".staging")); len(staged) != 0 {
		t.Errorf("staged files left: %v", staged)
	}
	origPath := filepath.Join(dataDir, store.FileName)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	rebuiltDir := t.TempDir()
	rb, err := store.Open(ctx, rebuiltDir)
	if err != nil {
		t.Fatal(err)
	}
	defer rb.Close()
	// A session time zone other than UTC must not shift any stored time.
	if _, err := rb.DB().Exec(`SET GLOBAL TimeZone = 'Europe/Copenhagen'`); err != nil {
		t.Fatal(err)
	}
	if err := newArchiver(rb, archiveDir).Rebuild(ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, err := rb.DB().Exec(`ATTACH '` + origPath + `' AS orig (READ_ONLY)`); err != nil {
		t.Fatal(err)
	}
	// The original's rows that belong to the exported hours.
	inHours := `epoch IN (SELECT epoch FROM orig.epochs WHERE started_at < '2026-10-17 00:00:00')`
	for _, table := range store.ArchiveTableNames() {
		where := inHours
		switch table {
		case "events":
			where = `received_at < '2026-10-17 00:00:00'`
		case "micro_endpoint_hours":
			where = `hour < '2026-10-17 00:00:00'`
		case "servers", "accounts", "streams", "consumers", "raft_groups", "stream_config", "consumer_config", "mutes", "findings":
			where = "true"
		}
		missing := scalar[int](t, rb, `SELECT count(*) FROM (SELECT * FROM orig.`+table+` WHERE `+where+` EXCEPT ALL SELECT * FROM `+table+`)`)
		extra := scalar[int](t, rb, `SELECT count(*) FROM (SELECT * FROM `+table+` EXCEPT ALL SELECT * FROM orig.`+table+` WHERE `+where+`)`)
		total := scalar[int](t, rb, `SELECT count(*) FROM `+table)
		if missing != 0 || extra != 0 {
			t.Errorf("%s: %d rows missing, %d extra after rebuild", table, missing, extra)
		}
		t.Logf("%s: %d rows", table, total)
	}
	if n := scalar[int](t, rb, `SELECT count(*) FROM epochs`); n != 3 {
		t.Errorf("rebuilt epochs = %d, want 3", n)
	}
	if n := scalar[int](t, rb, `SELECT count(*) FROM archive_hours WHERE rows > 0 AND files > 0
AND hour IN ('2026-10-16 22:00:00', '2026-10-16 23:00:00')`); n != 2 {
		t.Errorf("archive_hours after rebuild = %d, want h0 and h1", n)
	}
	// Numbering continues after the loaded rows.
	epoch, err := rb.WriteSnapshot(ctx, store.FixtureSnapshot(t, now))
	if err != nil {
		t.Fatal(err)
	}
	if epoch != 4 {
		t.Errorf("next epoch = %d, want 4", epoch)
	}
	// The loaded events are the two closed-hour events and the events of
	// the finding transitions in those hours; numbering continues after
	// the highest of them.
	loaded := scalar[int](t, rb, `SELECT max(id) FROM events`)
	if err := rb.WriteEvents(ctx, []events.Event{event(now, "collector.started")}); err != nil {
		t.Fatal(err)
	}
	if id := scalar[int](t, rb, `SELECT max(id) FROM events`); id != loaded+1 {
		t.Errorf("next event id = %d, want %d", id, loaded+1)
	}
	if n := scalar[int](t, rb, `SELECT count(*) FROM events WHERE source = 'finding'`); n != 4 {
		t.Errorf("finding events after rebuild = %d, want one per transition of the closed hours", n)
	}
	if n := scalar[int](t, rb, `SELECT count(*) FROM mutes WHERE unmuted_by = 'lead'`); n != 1 {
		t.Errorf("lifted mutes after rebuild = %d, want 1", n)
	}
	if n := scalar[int](t, rb, `SELECT count(*) FROM finding_transitions`); n != 4 {
		t.Errorf("transitions after rebuild = %d, want the 4 of the closed hours", n)
	}
	m, err := rb.CreateMute(ctx, store.Mute{CheckCode: "x-001", Reason: "r", MutedBy: "op", CreatedAt: now, ExpiresAt: now.Add(time.Hour)})
	if err != nil || m.ID != 3 {
		t.Errorf("next mute id = %d, %v; want 3", m.ID, err)
	}
}

func TestRebuildOutsideRetentionIsEmpty(t *testing.T) {
	ctx := context.Background()
	archiveDir := t.TempDir()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.WriteSnapshot(ctx, store.FixtureSnapshot(t, h0.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if _, err := newArchiver(st, archiveDir).Export(ctx, now); err != nil {
		t.Fatal(err)
	}
	rb, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer rb.Close()
	if err := newArchiver(rb, archiveDir).Rebuild(ctx, now.Add(30*24*time.Hour)); err != archive.ErrEmptyArchive {
		t.Fatalf("rebuild past retention: %v", err)
	}
	// An hour before the retention window is not exported: the sweep may
	// have removed part of it.
	if n, err := newArchiver(st, t.TempDir()).Export(ctx, now.Add(30*24*time.Hour)); err != nil || n != 0 {
		t.Fatalf("exported %d hours outside retention, err %v", n, err)
	}
}
