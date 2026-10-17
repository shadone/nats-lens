package main

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
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/events"
	"github.com/shadone/nats-lens/internal/store"
)

func testConfig(dir string) config.Config {
	cfg := config.Default()
	cfg.DataDir = filepath.Join(dir, "data")
	cfg.ArchiveDir = filepath.Join(dir, "archive")
	return cfg
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// writeArchive exports one closed hour holding two events into cfg's archive.
func writeArchive(t *testing.T, cfg config.Config) {
	t.Helper()
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	ev := events.Event{Time: at, ReceivedAt: at, Source: events.SourceSys, Kind: "client.connect", Summary: "s", Raw: json.RawMessage(`{}`)}
	if err := st.WriteEvents(ctx, []events.Event{ev, ev}); err != nil {
		t.Fatal(err)
	}
	a := newArchiver(cfg, st, archive.LocalDir{Dir: cfg.ArchiveDir}, quietLog())
	if n, err := a.Export(ctx, time.Now()); err != nil || n != 1 {
		t.Fatalf("exported %d, err %v", n, err)
	}
}

func countEvents(t *testing.T, st *store.Store) int {
	t.Helper()
	var n int
	if err := st.DB().QueryRow(`SELECT count(*) FROM events`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A failed rebuild leaves no database, so the next start tries again
// instead of running on an empty one.
func TestFailedRebuildIsRetried(t *testing.T) {
	cfg := testConfig(t.TempDir())
	hour := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	bad := filepath.Join(cfg.ArchiveDir, filepath.FromSlash(archive.RelPath("events", hour, "x")))
	if err := os.MkdirAll(filepath.Dir(bad), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("not parquet"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, _ := json.Marshal(archive.Manifest{Hour: hour, Final: true, Files: []archive.ManifestFile{
		{Table: "events", Path: archive.RelPath("events", hour, "x"), Rows: 1},
	}})
	mp := filepath.Join(cfg.ArchiveDir, filepath.FromSlash(archive.ManifestRelPath(hour)))
	if err := os.MkdirAll(filepath.Dir(mp), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mp, m, 0o600); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		st, _, err := openStore(context.Background(), cfg, quietLog())
		if err == nil {
			st.Close()
			t.Fatalf("start %d succeeded although the archive could not be loaded", i+1)
		}
	}
	for _, p := range []string{store.FileName, store.FileName + rebuildSuffix} {
		if _, err := os.Stat(filepath.Join(cfg.DataDir, p)); !os.IsNotExist(err) {
			t.Errorf("%s left behind after a failed rebuild: %v", p, err)
		}
	}
}

// A database file without epochs or events (left by an older version after
// a failed rebuild) is rebuilt from the archive.
func TestEmptyDatabaseIsRebuilt(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t.TempDir())
	writeArchive(t, cfg)
	empty, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	empty.Close()
	st, _, err := openStore(ctx, cfg, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if n := countEvents(t, st); n != 2 {
		t.Fatalf("events after rebuild = %d, want 2", n)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, store.FileName+rebuildSuffix)); !os.IsNotExist(err) {
		t.Errorf("side file left after a rebuild: %v", err)
	}
}

// A database with data is never replaced.
func TestDatabaseWithDataIsKept(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t.TempDir())
	writeArchive(t, cfg)
	st, _, err := openStore(ctx, cfg, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	if err := st.WriteEvents(ctx, []events.Event{{Time: at, ReceivedAt: at, Source: events.SourceCollector, Kind: "collector.started", Summary: "s"}}); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, _, err = openStore(ctx, cfg, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if n := countEvents(t, st); n != 3 {
		t.Fatalf("events = %d, want the 2 rebuilt and 1 written", n)
	}
}

func TestWithin(t *testing.T) {
	for _, c := range []struct {
		dir, parent string
		want        bool
	}{{"/d/archive", "/d", true}, {"/d", "/d", true}, {"/data2", "/data", false}, {"/a", "/d", false}} {
		if got := within(c.dir, c.parent); got != c.want {
			t.Errorf("within(%s, %s) = %v", c.dir, c.parent, got)
		}
	}
}

func TestArchiveGrace(t *testing.T) {
	cfg := config.Default()
	if g := archiveGrace(cfg); g != time.Minute {
		t.Errorf("default grace = %s, want 1m", g)
	}
	cfg.Interval = config.Duration(45 * time.Second)
	if g := archiveGrace(cfg); g != 90*time.Second {
		t.Errorf("grace = %s, want two intervals", g)
	}
	cfg.Events.FlushInterval = config.Duration(2 * time.Minute)
	if g := archiveGrace(cfg); g != 4*time.Minute {
		t.Errorf("grace = %s, want two flush intervals", g)
	}
}
