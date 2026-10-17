package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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
	closeSystems(rts, time.Time{})
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
	closeSystems(rts, time.Time{})
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
	defer closeSystems(rts, time.Time{})
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

// The marker is written before the database, so an open that fails after
// it still leaves the directory marked, and the next start under another
// id is refused instead of adopting the database.
func TestMarkerWrittenBeforeTheDatabase(t *testing.T) {
	ctx := context.Background()
	cfg := listConfig(t, t.TempDir(), "shop-prod")
	sys := cfg.ResolvedSystems()[0]
	rt := newSystemRuntime(cfg, sys, quietLog())
	if err := rt.open(ctx, 0); err == nil {
		closeSystems([]*systemRuntime{rt}, time.Time{})
		t.Fatal("open with 0 threads succeeded")
	}
	closeSystems([]*systemRuntime{rt}, time.Time{})
	if got := readMarker(t, sys.DataDir); got != "shop-prod" {
		t.Fatalf("marker %q after a failed open", got)
	}
}

// The stores close in parallel and closeSystems returns by the deadline
// even when a close takes longer.
func TestCloseSystemsInParallelByDeadline(t *testing.T) {
	ctx := context.Background()
	cfg := listConfig(t, t.TempDir(), "shop-prod", "shop-staging", "shop-edge")
	slow := func(st *store.Store) error {
		time.Sleep(400 * time.Millisecond)
		return st.Close()
	}
	defer func(old func(*store.Store) error) { closeStore = old }(closeStore)
	closeStore = slow

	rts, err := openSystems(ctx, cfg, cfg.ResolvedSystems(), quietLog())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	closeSystems(rts, time.Now().Add(5*time.Second))
	if took := time.Since(start); took > time.Second {
		t.Errorf("three 400 ms closes took %v: not in parallel", took)
	}

	var closed atomic.Int32
	closeStore = func(st *store.Store) error {
		defer closed.Add(1)
		time.Sleep(2 * time.Second)
		return st.Close()
	}
	rts, err = openSystems(ctx, cfg, cfg.ResolvedSystems(), quietLog())
	if err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	closeSystems(rts, time.Now().Add(200*time.Millisecond))
	if took := time.Since(start); took > time.Second {
		t.Errorf("closeSystems took %v past a 200 ms deadline", took)
	}
	// The abandoned closes finish before the temp dir goes.
	waitFor(t, "the abandoned closes", 10*time.Second, func() bool { return closed.Load() == 3 })
}

// A system refused at once stops the others' opens instead of waiting for
// them, for example for a long rebuild.
func TestOpenSystemsStopsAtTheFirstFailure(t *testing.T) {
	cfg := listConfig(t, t.TempDir(), "shop-prod", "shop-staging")
	defer func(old func(*systemRuntime, context.Context, int) error) { openRuntime = old }(openRuntime)
	openRuntime = func(rt *systemRuntime, ctx context.Context, threads int) error {
		if rt.sys.ID == "shop-prod" {
			return errors.New("refused")
		}
		<-ctx.Done() // a rebuild that only ends when cancelled
		return ctx.Err()
	}
	done := make(chan error, 1)
	go func() {
		_, err := openSystems(context.Background(), cfg, cfg.ResolvedSystems(), quietLog())
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "refused") {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("openSystems waited for the other system after a refusal")
	}
}

// The default archive directory inside the data directory is not warned
// about; an archive_dir set inside a data directory is.
func TestArchiveInsideDataDirWarnedOnlyWhenSet(t *testing.T) {
	ctx := context.Background()
	open := func(cfg config.Config) string {
		t.Helper()
		var buf bytes.Buffer
		rts, err := openSystems(ctx, cfg, cfg.ResolvedSystems(), slog.New(slog.NewTextHandler(&buf, nil)))
		if err != nil {
			t.Fatal(err)
		}
		closeSystems(rts, time.Time{})
		return buf.String()
	}
	const warning = "archive_dir is inside data_dir"
	if logs := open(listConfig(t, t.TempDir(), "shop-prod")); strings.Contains(logs, warning) {
		t.Errorf("list config with the default archive: warned\n%s", logs)
	}
	if logs := open(testConfigNoArchive(t.TempDir())); strings.Contains(logs, warning) {
		t.Errorf("single-system config with the default archive: warned\n%s", logs)
	}
	set := listConfig(t, t.TempDir(), "shop-prod")
	set.Systems[0].ArchiveDir = filepath.Join(set.DataDir, "shop-prod", "parquet")
	if logs := open(set); !strings.Contains(logs, warning) {
		t.Errorf("archive_dir set inside the data dir: no warning\n%s", logs)
	}
	legacy := testConfigNoArchive(t.TempDir())
	legacy.ArchiveDir = filepath.Join(legacy.DataDir, "parquet")
	if logs := open(legacy); !strings.Contains(logs, warning) {
		t.Errorf("single-system archive_dir set inside data_dir: no warning\n%s", logs)
	}
}

func testConfigNoArchive(dir string) config.Config {
	cfg := testConfig(dir)
	cfg.ArchiveDir = ""
	return cfg
}

// A stop signal while the stores open is a clean exit, logged at info.
func TestStopDuringStartIsClean(t *testing.T) {
	cfg := listConfig(t, t.TempDir(), "shop-prod", "shop-staging")
	cfg.Listen = ""
	defer func(old func(*systemRuntime, context.Context, int) error) { openRuntime = old }(openRuntime)
	ctx, cancel := context.WithCancel(context.Background())
	openRuntime = func(rt *systemRuntime, ctx context.Context, threads int) error {
		cancel() // the signal arrives during a rebuild
		<-ctx.Done()
		return ctx.Err()
	}
	var buf bytes.Buffer
	err := runConfig(ctx, "run", cfg, "", slog.New(slog.NewJSONHandler(&buf, nil)), nil)
	if err != nil {
		t.Fatalf("err = %v, want a clean exit", err)
	}
	if !strings.Contains(buf.String(), `"level":"INFO","msg":"stopped before the start completed"`) {
		t.Fatalf("no info line:\n%s", buf.String())
	}
}
