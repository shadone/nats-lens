package store

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Epochs written while the fill runs, and a counts row that appears
// for an epoch of a later batch after the missing list was read, do not
// make a batch fail on the primary key.
func TestAssetCountsFillRacesEpochWrites(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0)); err != nil {
		t.Fatal(err)
	}
	n := 3*assetCountBatch + 5
	db := s.DB()
	for _, q := range []string{
		`INSERT INTO epochs SELECT t.* REPLACE (i AS epoch, t.started_at + to_seconds(30 * (i - 1)) AS started_at)
FROM epochs t, range(2, ? + 1) r(i) WHERE t.epoch = 1`,
		`INSERT INTO stream_stats SELECT t.* REPLACE (i AS epoch) FROM stream_stats t, range(2, ? + 1) r(i) WHERE t.epoch = 1`,
	} {
		if _, err := db.Exec(q, n); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := restartSequence(ctx, tx, "epoch_seq", "SELECT max(epoch) + 1 FROM epochs"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// A writer that stores epochs throughout the fill.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var written int
	var werr error
	wg.Go(func() {
		for i := 1; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0.Add(time.Duration(n+i)*30*time.Second))); err != nil {
				werr = err
				return
			}
			written++
		}
	})
	batches := 0
	afterAssetCountBatch = func() {
		batches++
		if batches == 1 {
			// An epoch of the second batch gets its counts after the
			// missing list was read.
			if _, err := db.Exec(`INSERT INTO epoch_asset_counts VALUES (?, 1, 1, 1, 1, 1, 1)`, assetCountBatch+10); err != nil {
				t.Error(err)
			}
		}
	}
	t.Cleanup(func() { afterAssetCountBatch = nil })
	filled, err := s.FillAssetCounts(ctx)
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatalf("fill: %v", err)
	}
	if werr != nil {
		t.Fatalf("epoch write: %v", werr)
	}
	if filled != int64(n-2) {
		t.Errorf("filled %d, want %d", filled, n-2)
	}
	if got := count(t, db, `SELECT count(*) FROM epochs WHERE epoch NOT IN (SELECT epoch FROM epoch_asset_counts)`); got != 0 {
		t.Errorf("%d epochs without counts", got)
	}
	t.Logf("%d batches, %d epochs written during the fill", batches, written)
}
