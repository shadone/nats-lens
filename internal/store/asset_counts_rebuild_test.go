package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/archive"
	"github.com/shadone/nats-lens/internal/store"
)

// The archive holds no asset counts, as hours exported before the table
// existed do not; a rebuild computes them again from the loaded reports.
func TestRebuildComputesAssetCounts(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, at := range []time.Time{h0.Add(10 * time.Minute), h0.Add(40 * time.Minute), h1.Add(5 * time.Minute)} {
		if _, err := st.WriteSnapshot(ctx, store.FixtureSnapshot(t, at)); err != nil {
			t.Fatal(err)
		}
	}
	const counts = `SELECT list(format('{} {} {} {} {} {} {}', epoch, streams, kv_buckets, object_stores, durable_consumers,
    assets_replicated, assets_single) ORDER BY epoch)::VARCHAR FROM epoch_asset_counts`
	want := scalar[string](t, st, counts)
	if scalar[int](t, st, `SELECT count(*) FROM epoch_asset_counts`) != 3 {
		t.Fatalf("written counts: %s", want)
	}
	archiveDir := t.TempDir()
	if n, err := newArchiver(st, archiveDir).Export(ctx, now); err != nil || n != 2 {
		t.Fatalf("exported %d hours, err %v; want h0 and h1", n, err)
	}
	if files, _ := (archive.LocalDir{Dir: archiveDir}).List(ctx, "epoch_asset_counts/"); len(files) != 0 {
		t.Errorf("asset counts archived: %v", files)
	}
	rb, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer rb.Close()
	if _, err := rb.DB().Exec(`SET GLOBAL TimeZone = 'Pacific/Honolulu'`); err != nil {
		t.Fatal(err)
	}
	if err := newArchiver(rb, archiveDir).Rebuild(ctx, now); err != nil {
		t.Fatal(err)
	}
	if got := scalar[string](t, rb, counts); got != want {
		t.Errorf("rebuilt counts %s, want %s", got, want)
	}
}
