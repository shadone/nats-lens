package perf

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/shadone/nats-lens/internal/store"
)

// The harness fills a store's missing asset counts within its thread
// setting, and times the fill apart from the open.
func TestHarnessFillsAfterTheSettings(t *testing.T) {
	if testing.Short() {
		t.Skip("generates a store: run without -short")
	}
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	p := Profiles["smoke"]
	path := filepath.Join(t.TempDir(), "smoke.duckdb")
	if _, err := Generate(ctx, p, path, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`DELETE FROM epoch_asset_counts`); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	threads := -1
	orig := fillAssetCounts
	t.Cleanup(func() { fillAssetCounts = orig })
	fillAssetCounts = func(st *store.Store, ctx context.Context) (int64, error) {
		if threads >= 0 {
			t.Error("the harness filled twice")
		}
		if err := st.DB().QueryRow(`SELECT current_setting('threads')::INTEGER`).Scan(&threads); err != nil {
			return 0, err
		}
		return orig(st, ctx)
	}
	h, stop, err := openHarness(ctx, LoadOptions{Profile: p, Path: path, Threads: 3, Log: log})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if threads != 3 {
		t.Errorf("the fill ran with %d threads, want the setting's 3", threads)
	}
	var missing int
	if err := h.st.DB().QueryRow(`SELECT count(*) FROM epochs WHERE epoch NOT IN (SELECT epoch FROM epoch_asset_counts)`).Scan(&missing); err != nil {
		t.Fatal(err)
	}
	if missing != 0 || h.fillTook <= 0 {
		t.Errorf("%d epochs without counts, fill took %s", missing, h.fillTook)
	}
}
