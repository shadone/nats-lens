package store_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/archive"
	"github.com/shadone/nats-lens/internal/events"
	"github.com/shadone/nats-lens/internal/store"
)

func openMem(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.OpenPath(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func write(t *testing.T, st *store.Store, evs ...events.Event) {
	t.Helper()
	if err := st.WriteEvents(context.Background(), evs); err != nil {
		t.Fatal(err)
	}
}

func exported(t *testing.T, a *archive.Archiver, at time.Time) int {
	t.Helper()
	n, err := a.Export(context.Background(), at)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func rebuilt(t *testing.T, dir string, at time.Time) *store.Store {
	t.Helper()
	rb := openMem(t)
	if err := newArchiver(rb, dir).Rebuild(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	return rb
}

// An event received in an hour but written after the hour was exported
// makes the hour pending again.
func TestLateEventReexportsHour(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	a := newArchiver(st, dir)
	write(t, st, event(h0.Add(time.Minute), "early"))
	if n := exported(t, a, now); n != 1 {
		t.Fatalf("first export = %d hours", n)
	}
	write(t, st, event(h0.Add(59*time.Minute), "late"))
	if n := exported(t, a, now.Add(10*time.Minute)); n != 1 {
		t.Fatalf("hour with a late event exported %d times, want once more", n)
	}
	if n := exported(t, a, now.Add(20*time.Minute)); n != 0 {
		t.Fatalf("unchanged hour exported again: %d", n)
	}
	if n := scalar[int](t, rebuilt(t, dir, now), `SELECT count(*) FROM events`); n != 2 {
		t.Fatalf("events after rebuild = %d, want 2", n)
	}
}

// The open hour is exported provisionally at most every ProvisionalEvery,
// and finally once closed.
func TestOpenHourProvisional(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	a := newArchiver(st, dir)
	a.ProvisionalEvery = 5 * time.Minute
	write(t, st, event(h2.Add(time.Minute), "a"))
	if n := exported(t, a, now); n != 1 {
		t.Fatalf("open hour exported %d times, want a provisional export", n)
	}
	if final := scalar[bool](t, st, `SELECT final FROM archive_hours WHERE hour = ?`, h2); final {
		t.Fatal("open hour marked final")
	}
	write(t, st, event(h2.Add(31*time.Minute), "b"))
	if n := exported(t, a, now.Add(time.Minute)); n != 0 {
		t.Fatalf("provisional export repeated within ProvisionalEvery: %d", n)
	}
	if n := exported(t, a, now.Add(6*time.Minute)); n != 1 {
		t.Fatalf("changed open hour not exported after ProvisionalEvery: %d", n)
	}
	rb := rebuilt(t, dir, now.Add(7*time.Minute))
	if n := scalar[int](t, rb, `SELECT count(*) FROM events`); n != 2 {
		t.Fatalf("rebuild from a provisional hour = %d events, want 2", n)
	}
	if final := scalar[bool](t, rb, `SELECT final FROM archive_hours WHERE hour = ?`, h2); final {
		t.Fatal("rebuilt provisional hour marked final")
	}
	closed := h2.Add(time.Hour + 2*time.Minute)
	if n := exported(t, a, closed); n != 1 {
		t.Fatalf("closed hour not exported finally: %d", n)
	}
	if final := scalar[bool](t, st, `SELECT final FROM archive_hours WHERE hour = ?`, h2); !final {
		t.Fatal("closed hour not marked final")
	}
}

// Files of an hour without a manifest are an export that did not finish
// and are not loaded.
func TestRebuildNeedsManifest(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	write(t, st, event(h0.Add(time.Minute), "a"), event(h1.Add(time.Minute), "b"))
	if n := exported(t, newArchiver(st, dir), now); n != 2 {
		t.Fatalf("exported %d", n)
	}
	if err := os.Remove(filepath.Join(dir, filepath.FromSlash(archive.ManifestRelPath(h1)))); err != nil {
		t.Fatal(err)
	}
	rb := rebuilt(t, dir, now)
	if n := scalar[int](t, rb, `SELECT count(*) FROM events`); n != 1 {
		t.Fatalf("events = %d, want only the hour with a manifest", n)
	}
}

// The state object written on each tick carries numbers handed out after
// the last exported hour; a rebuild continues after them.
func TestStateRaisesSequences(t *testing.T) {
	ctx := context.Background()
	st, dir := openMem(t), t.TempDir()
	a := newArchiver(st, dir)
	write(t, st, event(h0.Add(time.Minute), "a"))
	if _, err := st.WriteSnapshot(ctx, store.FixtureSnapshot(t, h0.Add(2*time.Minute))); err != nil {
		t.Fatal(err)
	}
	// Written in the open hour, not exported (no provisional exports).
	write(t, st, event(h2.Add(time.Minute), "b"), event(h2.Add(time.Minute), "c"))
	if _, err := st.WriteSnapshot(ctx, store.FixtureSnapshot(t, h2.Add(2*time.Minute))); err != nil {
		t.Fatal(err)
	}
	if err := a.Tick(ctx, now); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, archive.StateRelPath))
	if err != nil {
		t.Fatal(err)
	}
	var state archive.State
	if err := json.Unmarshal(b, &state); err != nil || state.MaxEpoch != 2 || state.MaxEventID != 3 {
		t.Fatalf("state = %+v, %v", state, err)
	}
	rb := rebuilt(t, dir, now)
	epoch, err := rb.WriteSnapshot(ctx, store.FixtureSnapshot(t, now))
	if err != nil {
		t.Fatal(err)
	}
	if epoch != 3 {
		t.Errorf("next epoch = %d, want 3 (after the state's 2, not the loaded 1)", epoch)
	}
	write(t, rb, event(now, "d"))
	if id := scalar[int](t, rb, `SELECT max(id) FROM events`); id != 4 {
		t.Errorf("next event id = %d, want 4", id)
	}
}

// A mute created after the last export is in no archived file; the state
// written at shutdown carries its ID, and a rebuild never hands it out
// again.
func TestStateRaisesMuteSequence(t *testing.T) {
	ctx := context.Background()
	st, dir := openMem(t), t.TempDir()
	a := newArchiver(st, dir)
	write(t, st, event(h0.Add(time.Minute), "a"))
	mute := store.Mute{CheckCode: "stream-025", Reason: "known", MutedBy: "op", CreatedAt: h0, ExpiresAt: h0.Add(48 * time.Hour)}
	if _, err := st.CreateMute(ctx, mute); err != nil {
		t.Fatal(err)
	}
	if err := a.Tick(ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateMute(ctx, mute); err != nil {
		t.Fatal(err)
	}
	if err := a.WriteState(ctx, now); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, archive.StateRelPath))
	if err != nil {
		t.Fatal(err)
	}
	var state archive.State
	if err := json.Unmarshal(b, &state); err != nil || state.MaxMuteID != 2 {
		t.Fatalf("state = %+v, %v", state, err)
	}
	rb := rebuilt(t, dir, now)
	if n := scalar[int](t, rb, `SELECT count(*) FROM mutes`); n != 1 {
		t.Fatalf("rebuilt mutes = %d, want the archived one", n)
	}
	m, err := rb.CreateMute(ctx, mute)
	if err != nil || m.ID != 3 {
		t.Errorf("next mute id = %d, %v; want 3 (after the state's 2, not the loaded 1)", m.ID, err)
	}
}

// A tick exports at most MaxHoursPerTick hours, oldest first.
func TestExportIsBoundedPerTick(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	a := newArchiver(st, dir)
	a.MaxHoursPerTick = 1
	write(t, st, event(h0.Add(time.Minute), "a"), event(h1.Add(time.Minute), "b"))
	if n := exported(t, a, now); n != 1 {
		t.Fatalf("first tick exported %d", n)
	}
	if got := scalar[time.Time](t, st, `SELECT hour FROM archive_hours`); !got.Equal(h0) {
		t.Fatalf("first hour exported = %v, want %v", got, h0)
	}
	if n := exported(t, a, now); n != 1 {
		t.Fatalf("second tick exported %d", n)
	}
}

// A Kubernetes Event updated in a later hour moves its row there: the old
// hour is exported again without it, and a rebuild holds the row once,
// with its latest count.
func TestUpdatedK8sEventReexportsBothHours(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	a := newArchiver(st, dir)
	k := func(at time.Time, n string) events.Event {
		return events.Event{Time: at, ReceivedAt: at, Source: events.SourceK8s, Kind: "k8s.event", Summary: "x" + n,
			Raw: json.RawMessage(`{"uid":"u1","count":` + n + `}`)}
	}
	write(t, st, k(h0.Add(50*time.Minute), "1"))
	if n := exported(t, a, now); n != 1 {
		t.Fatalf("first export = %d hours", n)
	}
	write(t, st, k(h0.Add(70*time.Minute), "2"))
	if n := exported(t, a, now.Add(2*time.Hour)); n != 2 {
		t.Fatalf("exported %d hours after the update, want the old and the new hour", n)
	}
	rb := rebuilt(t, dir, now.Add(2*time.Hour))
	if n := scalar[int](t, rb, `SELECT count(*) FROM events`); n != 1 {
		t.Fatalf("events after rebuild = %d, want 1", n)
	}
	if c := scalar[string](t, rb, `SELECT json_extract_string(raw, '$.count') FROM events`); c != "2" {
		t.Fatalf("count after rebuild = %s, want 2", c)
	}
}

// When both hours' files hold a moved Kubernetes Event (the old hour was
// not exported again before the database was lost), the rebuild keeps the
// copy received last instead of failing on the duplicate id.
func TestRebuildKeepsTheLatestCopyOfAnEvent(t *testing.T) {
	ctx := context.Background()
	st, dir := openMem(t), t.TempDir()
	k := func(at time.Time, n string) events.Event {
		return events.Event{Time: h0.Add(50 * time.Minute), ReceivedAt: at, Source: events.SourceK8s, Kind: "k8s.event", Summary: "x" + n,
			Raw: json.RawMessage(`{"uid":"u1","count":` + n + `}`)}
	}
	export := func(hour time.Time, name string) store.RebuildHour {
		files := map[string]string{}
		e, err := st.ExportHour(ctx, hour, func(table string) string {
			p := filepath.Join(dir, name+"-"+table+".parquet")
			files[table] = p
			return p
		})
		if err != nil {
			t.Fatal(err)
		}
		out := store.RebuildHour{Files: map[string]string{}, Final: true}
		for _, f := range e.Files {
			out.Files[f.Table] = f.Path
		}
		return out
	}
	write(t, st, k(h0.Add(50*time.Minute), "1"))
	first := export(h0, "h0")
	write(t, st, k(h0.Add(70*time.Minute), "2"))
	second := export(h0.Add(time.Hour), "h1")
	rb := openMem(t)
	if err := rb.Rebuild(ctx, map[time.Time]store.RebuildHour{h0: first, h0.Add(time.Hour): second}, store.MaxIDs{}, now); err != nil {
		t.Fatal(err)
	}
	if c := scalar[string](t, rb, `SELECT json_extract_string(raw, '$.count') FROM events`); c != "2" {
		t.Fatalf("count after rebuild = %s, want the latest copy's 2", c)
	}
}

// The loopback decision stored with an epoch survives the archive.
func TestArchiveKeepsLoopbackDecision(t *testing.T) {
	ctx := context.Background()
	st, dir := openMem(t), t.TempDir()
	for e, at := range map[int64]time.Time{1: h0.Add(time.Minute), 2: h0.Add(2 * time.Minute)} {
		if _, err := st.DB().Exec(`INSERT INTO epochs (epoch, started_at, finished_at, duration_ms, expected_servers, observed_servers,
  expected_names, observed_names, missing_names, endpoint_failures, partial, errors) VALUES (?, ?, ?, 0, 1, 1, ['nats-0'], ['nats-0'], [], 0, false, [])`,
			e, at, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetLoopbackTooling(ctx, 1, false); err != nil {
		t.Fatal(err)
	}
	if n := exported(t, newArchiver(st, dir), now); n != 1 {
		t.Fatalf("exported %d hours", n)
	}
	rb := rebuilt(t, dir, now)
	if v := scalar[string](t, rb, `SELECT string_agg(coalesce(loopback_tooling::VARCHAR, 'null'), ',' ORDER BY epoch) FROM epochs`); v != "false,null" {
		t.Fatalf("loopback decisions after rebuild = %s, want false,null", v)
	}
}
