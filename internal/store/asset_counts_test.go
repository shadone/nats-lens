package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// assetCountRows returns epoch_asset_counts as one string per row, in
// epoch order.
func assetCountRows(t *testing.T, s *Store) []string {
	t.Helper()
	rows, err := s.DB().Query(`SELECT format('{} {} {} {} {} {} {}', epoch, streams, kv_buckets, object_stores,
    durable_consumers, assets_replicated, assets_single) FROM epoch_asset_counts ORDER BY epoch`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func equalRows(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Each epoch's write stores its asset counts. Opening a store whose
// counts are gone leaves them missing; the fill gives the same ones again.
func TestAssetCountsWrittenAndFilledAfterReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db.duckdb")
	s, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0.Add(time.Duration(i)*30*time.Second))); err != nil {
			t.Fatal(err)
		}
	}
	written := assetCountRows(t, s)
	if len(written) != 3 {
		t.Fatalf("asset counts after three epochs: %v", written)
	}
	// The fixture's two servers report streams and durable consumers.
	var streams, durable int
	if err := s.DB().QueryRow(`SELECT streams + kv_buckets + object_stores, durable_consumers FROM epoch_asset_counts WHERE epoch = 1`).
		Scan(&streams, &durable); err != nil {
		t.Fatal(err)
	}
	if streams == 0 || durable == 0 {
		t.Fatalf("epoch 1 counts %d streams and %d durable consumers", streams, durable)
	}
	if _, err := s.DB().Exec(`DELETE FROM epoch_asset_counts WHERE epoch <> 2`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := assetCountRows(t, s); len(got) != 1 {
		t.Errorf("opening filled counts: %v", got)
	}
	if _, err := s.FillAssetCounts(ctx); err != nil {
		t.Fatal(err)
	}
	if got := assetCountRows(t, s); !equalRows(got, written) {
		t.Errorf("after the fill:\n%v\nwant\n%v", got, written)
	}
}

// The fill covers every epoch without counts across several batches, and
// leaves the epochs that have counts alone.
func TestAssetCountsFillInBatches(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0)); err != nil {
		t.Fatal(err)
	}
	// Epochs 2 to n copy epoch 1's reports, as the perf generator does,
	// without counts; epoch 40 has a count no rule gives, which the fill
	// must keep.
	n := 2*assetCountBatch + 17
	db := s.DB()
	for _, q := range []string{
		`INSERT INTO epochs SELECT t.* REPLACE (i AS epoch, t.started_at + to_seconds(30 * (i - 1)) AS started_at)
FROM epochs t, range(2, ? + 1) r(i) WHERE t.epoch = 1`,
		`INSERT INTO stream_stats SELECT t.* REPLACE (i AS epoch) FROM stream_stats t, range(2, ? + 1) r(i) WHERE t.epoch = 1`,
		`INSERT INTO consumer_stats SELECT t.* REPLACE (i AS epoch) FROM consumer_stats t, range(2, ? + 1) r(i) WHERE t.epoch = 1`,
		`UPDATE stream_config SET last_epoch = ? WHERE last_epoch = 1`,
		`UPDATE consumer_config SET last_epoch = ? WHERE last_epoch = 1`,
	} {
		if _, err := db.Exec(q, n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO epoch_asset_counts VALUES (40, 99, 99, 99, 99, 99, 99)`); err != nil {
		t.Fatal(err)
	}
	filled, err := s.FillAssetCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if filled != int64(n-2) {
		t.Errorf("filled %d epochs, want %d", filled, n-2)
	}
	var epochs, distinct, kept int
	if err := db.QueryRow(`SELECT count(*), count(DISTINCT (streams, kv_buckets, object_stores, durable_consumers, assets_replicated,
    assets_single)) FILTER (WHERE epoch <> 40), count(*) FILTER (WHERE epoch = 40 AND streams = 99) FROM epoch_asset_counts`).
		Scan(&epochs, &distinct, &kept); err != nil {
		t.Fatal(err)
	}
	if epochs != n || distinct != 1 || kept != 1 {
		t.Errorf("%d epochs with counts, %d distinct counts, epoch 40 kept %d; want %d, 1, 1", epochs, distinct, kept, n)
	}
	if filled, err = s.FillAssetCounts(ctx); err != nil || filled != 0 {
		t.Errorf("a second fill filled %d (%v), want none", filled, err)
	}
}

// The retention sweep deletes the counts of the epochs it deletes.
func TestAssetCountsSwept(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	for i := range 3 {
		if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0.Add(time.Duration(i)*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Sweep(ctx, t0.Add(2*time.Hour+time.Minute), 90*time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := count(t, s.DB(), `SELECT count(*) FROM epoch_asset_counts`); got != 2 {
		t.Errorf("%d epochs with counts after the sweep, want 2", got)
	}
	if got := count(t, s.DB(), `SELECT count(*) FROM epoch_asset_counts WHERE epoch NOT IN (SELECT epoch FROM epochs)`); got != 0 {
		t.Errorf("%d counts of swept epochs left", got)
	}
}

// The fill takes the write lock for one batch at a time, so an epoch
// write waits for one batch, not for the whole fill.
func TestAssetCountsFillReleasesTheLockBetweenBatches(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0)); err != nil {
		t.Fatal(err)
	}
	n := 2*assetCountBatch + 2
	if _, err := s.DB().Exec(`INSERT INTO epochs SELECT t.* REPLACE (i AS epoch, t.started_at + to_seconds(30 * (i - 1)) AS started_at)
FROM epochs t, range(2, ? + 1) r(i) WHERE t.epoch = 1`, n); err != nil {
		t.Fatal(err)
	}
	var batches, free int
	afterAssetCountBatch = func() {
		batches++
		if s.mu.m.TryLock() {
			free++
			s.mu.m.Unlock()
		}
	}
	t.Cleanup(func() { afterAssetCountBatch = nil })
	if _, err := s.FillAssetCounts(ctx); err != nil {
		t.Fatal(err)
	}
	if batches != 3 || free != 3 {
		t.Errorf("%d batches, the lock free after %d; want 3 and 3", batches, free)
	}
}
