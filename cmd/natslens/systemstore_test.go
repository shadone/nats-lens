package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/events"
	"github.com/shadone/nats-lens/internal/store"
)

func listConfig(t *testing.T, root string, ids ...string) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = root
	for _, id := range ids {
		cfg.Systems = append(cfg.Systems, config.SystemConfig{ID: id, NATS: config.NATS{URL: "nats://127.0.0.1:1"}})
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func readMarker(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, markerFile))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m.ID
}

// Each system's data directory is marked with its id on first open.
func TestOpenSystemsMarksDataDirs(t *testing.T) {
	ctx := context.Background()
	cfg := listConfig(t, t.TempDir(), "shop-prod", "shop-staging")
	rts, err := openSystems(ctx, cfg, cfg.ResolvedSystems(), quietLog())
	if err != nil {
		t.Fatal(err)
	}
	closeSystems(rts)
	for _, s := range cfg.ResolvedSystems() {
		if got := readMarker(t, s.DataDir); got != s.ID {
			t.Errorf("%s marked %q", s.DataDir, got)
		}
	}
	// A second start opens the same directories.
	rts, err = openSystems(ctx, cfg, cfg.ResolvedSystems(), quietLog())
	if err != nil {
		t.Fatal(err)
	}
	closeSystems(rts)
}

// A data directory marked for another system refuses the start before a
// database is opened in it, and no store stays open.
func TestOpenSystemsRefusesAnotherSystemsDir(t *testing.T) {
	ctx := context.Background()
	cfg := listConfig(t, t.TempDir(), "shop-prod", "shop-staging")
	sys := cfg.ResolvedSystems()
	if err := os.MkdirAll(sys[1].DataDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sys[1].DataDir, markerFile), []byte(`{"id":"shop-edge"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := openSystems(ctx, cfg, sys, quietLog())
	if err == nil || !strings.Contains(err.Error(), "shop-edge") || !strings.Contains(err.Error(), "shop-staging") {
		t.Fatalf("err = %v, want a refusal naming both systems", err)
	}
	if _, err := os.Stat(filepath.Join(sys[1].DataDir, store.FileName)); !os.IsNotExist(err) {
		t.Errorf("a database was created in the refused directory: %v", err)
	}
	// shop-prod's store was closed again: it opens now.
	st, err := store.Open(ctx, sys[0].DataDir)
	if err != nil {
		t.Fatalf("shop-prod left open: %v", err)
	}
	st.Close()
}

// A single-system configuration opens the database already in data_dir
// in place and adopts the directory.
func TestLegacyConfigOpensDatabaseInPlace(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t.TempDir())
	st, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	if err := st.WriteEvents(ctx, []events.Event{{Time: at, ReceivedAt: at, Source: events.SourceCollector, Kind: "collector.started", Summary: "s"}}); err != nil {
		t.Fatal(err)
	}
	st.Close()
	rts, err := openSystems(ctx, cfg, cfg.ResolvedSystems(), quietLog())
	if err != nil {
		t.Fatal(err)
	}
	defer closeSystems(rts)
	if len(rts) != 1 || rts[0].sys.ID != config.DefaultSystemID {
		t.Fatalf("runtimes %+v", rts)
	}
	if n := countEvents(t, rts[0].st); n != 1 {
		t.Fatalf("events = %d, want the one already stored", n)
	}
	if got := readMarker(t, cfg.DataDir); got != config.DefaultSystemID {
		t.Errorf("marker %q", got)
	}
}

// A single-system database left in the root data_dir is reported when the
// listed systems do not use it.
func TestUnusedLegacyDatabaseWarned(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	st, err := store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	cfg := listConfig(t, root, "shop-prod")
	var buf bytes.Buffer
	warnUnusedDatabase(cfg, slog.New(slog.NewTextHandler(&buf, nil)))
	if !strings.Contains(buf.String(), "no system uses it") {
		t.Fatalf("no warning: %q", buf.String())
	}
	buf.Reset()
	cfg.Systems[0].DataDir = root
	warnUnusedDatabase(cfg, slog.New(slog.NewTextHandler(&buf, nil)))
	if buf.Len() != 0 {
		t.Fatalf("warned although shop-prod uses it: %q", buf.String())
	}
}

// Two systems sharing an archive directory refuse the start.
func TestRunRefusesSharedArchiveDir(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "natslens.yaml")
	body := "data_dir: " + filepath.Join(dir, "data") + `
listen: ""
systems:
  - { id: shop-prod, archive_dir: ` + filepath.Join(dir, "archive") + ` }
  - { id: shop-staging, archive_dir: ` + filepath.Join(dir, "archive") + ` }
`
	if err := os.WriteFile(conf, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	err := run(context.Background(), []string{"run", "-config", conf})
	if err == nil || !strings.Contains(err.Error(), "archive") {
		t.Fatalf("err = %v, want the shared archive refused", err)
	}
}

func TestThreadsPerStore(t *testing.T) {
	for _, c := range []struct{ procs, n, want int }{{8, 1, 8}, {8, 2, 4}, {8, 3, 2}, {2, 3, 2}, {1, 1, 2}} {
		if got := threadsPerStore(c.procs, c.n); got != c.want {
			t.Errorf("threadsPerStore(%d, %d) = %d, want %d", c.procs, c.n, got, c.want)
		}
	}
}
