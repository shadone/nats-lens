package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/archive"
)

// failManifests is a local archive whose manifest writes fail, as if the
// process died between an hour's files and its manifest.
type failManifests struct{ archive.LocalDir }

func (f failManifests) Put(ctx context.Context, rel, path string) error {
	if strings.HasPrefix(rel, "manifests/") {
		_ = os.Remove(path)
		return errors.New("crashed before the manifest")
	}
	return f.LocalDir.Put(ctx, rel, path)
}

func eventFiles(t *testing.T, dir string) []string {
	t.Helper()
	got, err := archive.LocalDir{Dir: dir}.List(context.Background(), "events/date=2026-10-16/hour=22.")
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// A re-export that stops before its manifest leaves the previous export
// whole: a rebuild loads the previous manifest's files only. The next
// export that finishes removes the files no manifest lists.
func TestReexportCrashKeepsPreviousExport(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	a := newArchiver(st, dir)
	write(t, st, event(h0.Add(time.Minute), "early"))
	if n := exported(t, a, now); n != 1 {
		t.Fatalf("first export = %d", n)
	}
	write(t, st, event(h0.Add(59*time.Minute), "late"))
	crashing := newArchiver(st, dir)
	crashing.Dest = failManifests{archive.LocalDir{Dir: dir}}
	if _, err := crashing.Export(context.Background(), now.Add(time.Minute)); err == nil {
		t.Fatal("export with a failing manifest write succeeded")
	}
	if n := scalar[int](t, rebuilt(t, dir, now), `SELECT count(*) FROM events`); n != 1 {
		t.Fatalf("rebuild after a crashed re-export = %d events, want the previous export's 1", n)
	}
	if got := eventFiles(t, dir); len(got) != 2 {
		t.Fatalf("event files after the crash = %v, want the previous and the orphaned one", got)
	}
	if n := exported(t, a, now.Add(2*time.Minute)); n != 1 {
		t.Fatalf("export after the crash = %d", n)
	}
	if got := eventFiles(t, dir); len(got) != 1 {
		t.Fatalf("event files after a finished export = %v, want only the listed one", got)
	}
	if n := scalar[int](t, rebuilt(t, dir, now), `SELECT count(*) FROM events`); n != 2 {
		t.Fatalf("rebuild = %d events, want 2", n)
	}
}

// A file that does not hold the rows its manifest lists stops the rebuild.
func TestRebuildChecksRowCounts(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	write(t, st, event(h0.Add(time.Minute), "a"))
	if n := exported(t, newArchiver(st, dir), now); n != 1 {
		t.Fatalf("export = %d", n)
	}
	mp := filepath.Join(dir, filepath.FromSlash(archive.ManifestRelPath(h0)))
	b, err := os.ReadFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	var m archive.Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for i := range m.Files {
		if m.Files[i].Table == "events" {
			m.Files[i].Rows++
		}
	}
	b, _ = json.Marshal(m)
	if err := os.WriteFile(mp, b, 0o600); err != nil {
		t.Fatal(err)
	}
	rb := openMem(t)
	err = newArchiver(rb, dir).Rebuild(context.Background(), now)
	if err == nil || !strings.Contains(err.Error(), "holds 1 rows, the manifest lists 2") {
		t.Fatalf("rebuild with a wrong row count: %v", err)
	}
}

func manifestPaths(t *testing.T, dir string, hour time.Time) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(archive.ManifestRelPath(hour))))
	if err != nil {
		t.Fatal(err)
	}
	var m archive.Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range m.Files {
		out = append(out, f.Path)
	}
	return out
}

// Two exports of an hour never share file names, even when the clock
// gives both the same time.
func TestExportIDNotReused(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	a := newArchiver(st, dir)
	fixed := time.Date(2026, 10, 17, 1, 0, 0, 0, time.UTC)
	a.Clock = func() time.Time { return fixed }
	write(t, st, event(h0.Add(time.Minute), "a"))
	exported(t, a, now)
	first := manifestPaths(t, dir, h0)
	write(t, st, event(h0.Add(2*time.Minute), "b"))
	exported(t, a, now.Add(time.Minute))
	for _, p := range manifestPaths(t, dir, h0) {
		for _, q := range first {
			if p == q {
				t.Fatalf("second export reused %s", p)
			}
		}
	}
}

// pauseManifest holds an export between its files and its manifest.
type pauseManifest struct {
	archive.LocalDir
	reached chan struct{}
	release chan struct{}
}

func (p pauseManifest) Put(ctx context.Context, rel, path string) error {
	if strings.HasPrefix(rel, "manifests/") {
		close(p.reached)
		<-p.release
	}
	return p.LocalDir.Put(ctx, rel, path)
}

// Two exporters on one hour: the one that finishes first must not remove
// the other's fresh files, or the other's manifest lists missing files and
// every rebuild fails. From the review.
func TestConcurrentExportsKeepRebuildWorking(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	newA := func() *archive.Archiver {
		a := newArchiver(st, dir)
		a.MinCleanupAge = 10 * time.Minute
		return a
	}
	write(t, st, event(h0.Add(time.Minute), "a"))
	exported(t, newA(), now)
	write(t, st, event(h0.Add(2*time.Minute), "b"))
	pm := pauseManifest{archive.LocalDir{Dir: dir}, make(chan struct{}), make(chan struct{})}
	b := newA()
	b.Dest = pm
	done := make(chan error)
	go func() { _, err := b.Export(context.Background(), now.Add(time.Minute)); done <- err }()
	<-pm.reached
	write(t, st, event(h0.Add(3*time.Minute), "c"))
	exported(t, newA(), now.Add(2*time.Minute)) // finishes first and cleans up
	close(pm.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := newArchiver(openMem(t), dir).Rebuild(context.Background(), now); err != nil {
		t.Fatalf("rebuild after overlapping exports: %v", err)
	}
}

// Cleanup removes old files of earlier exports, never a file of a later
// export, however old.
func TestCleanupKeepsLaterExports(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	a := newArchiver(st, dir)
	a.MinCleanupAge = time.Minute
	old := time.Now().Add(-time.Hour)
	put := func(id string) string {
		rel := archive.RelPath("events", h0, id)
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
		return rel
	}
	earlier := put("20000101T000000.000000000Z")
	later := put("99991231T235959.000000000Z")
	write(t, st, event(h0.Add(time.Minute), "a"))
	exported(t, a, now)
	got := eventFiles(t, dir)
	if slices.Contains(got, earlier) || !slices.Contains(got, later) {
		t.Fatalf("files after export = %v; want the earlier export removed and the later kept", got)
	}
}
