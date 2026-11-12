package runner

import (
	"context"
	"testing"
	"time"
)

// After the fill at open failed, an epoch has no stream samples or asset
// counts, and every later epoch gets no stream samples either. The
// runner's maintenance after the sweep fills them, and the epochs after it
// get their samples when written.
func TestMaintenanceRetriesAFailedFill(t *testing.T) {
	ctx := context.Background()
	r := newRunner(t)
	if _, err := r.Once(ctx); err != nil {
		t.Fatal(err)
	}
	db := r.Store.DB()
	// The state a failed fill at open leaves: epoch 1 as a release before
	// both tables wrote it.
	for _, q := range []string{`DELETE FROM stream_samples`, `DELETE FROM stream_sample_heads`, `DELETE FROM stream_sample_epochs`,
		`DELETE FROM epoch_asset_counts`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := r.Once(ctx); err != nil {
		t.Fatal(err)
	}
	count := func(q string) int {
		t.Helper()
		var n int
		if err := db.QueryRow(q).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	const unsampled = `SELECT count(*) FROM epochs WHERE epoch NOT IN (SELECT epoch FROM stream_sample_epochs)`
	const uncounted = `SELECT count(*) FROM epochs WHERE epoch NOT IN (SELECT epoch FROM epoch_asset_counts)`
	if n := count(unsampled); n != 2 {
		t.Fatalf("%d epochs without samples before the retry, want 2", n)
	}
	if _, err := r.maintain(ctx, time.Now(), 0); err != nil {
		t.Fatal(err)
	}
	if n := count(unsampled); n != 0 {
		t.Errorf("%d epochs without samples after the retry", n)
	}
	if n := count(uncounted); n != 0 {
		t.Errorf("%d epochs without asset counts after the retry", n)
	}
	if _, err := r.Once(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(unsampled); n != 0 {
		t.Errorf("the epoch written after the retry has no samples (%d epochs without)", n)
	}
}
