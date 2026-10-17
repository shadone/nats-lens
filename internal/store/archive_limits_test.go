package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/archive"
	"github.com/shadone/nats-lens/internal/collect"
)

const limitsVarz = `{"server":{"name":"nats-0","id":"NLIMITSERVER0","ver":"2.15.0"},
"data":{"server_id":"NLIMITSERVER0","server_name":"nats-0","version":"2.15.0",
"max_payload":8388608,"max_pending":67108864,"write_deadline":10000000000,"ping_interval":120000000000,"ping_max":2}}`

// archiveLimits writes one epoch with VARZ limits in h0, exports it, and
// returns the archive directory and the server_stats file of h0.
func archiveLimits(t *testing.T) (string, string) {
	t.Helper()
	st, dir := openMem(t), t.TempDir()
	snap := collect.NewSnapshot(h0.Add(time.Minute))
	if err := snap.Apply(collect.Varz, []byte(limitsVarz)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	if n := exported(t, newArchiver(st, dir), now); n != 1 {
		t.Fatalf("exported %d hours", n)
	}
	files, err := (archive.LocalDir{Dir: dir}).List(context.Background(), "server_stats/date=2026-10-16/hour=22.")
	if err != nil || len(files) != 1 {
		t.Fatalf("server_stats files %v, err %v", files, err)
	}
	return dir, filepath.Join(dir, files[0])
}

func limitsOf(t *testing.T, dir string) (payload, pingsOut sql.NullInt64) {
	t.Helper()
	rb := rebuilt(t, dir, now)
	if err := rb.DB().QueryRow(`SELECT max_payload, max_pings_out FROM server_stats`).Scan(&payload, &pingsOut); err != nil {
		t.Fatal(err)
	}
	return payload, pingsOut
}

// The archive carries the VARZ limits through export and rebuild.
func TestArchiveKeepsServerLimits(t *testing.T) {
	dir, _ := archiveLimits(t)
	if payload, pingsOut := limitsOf(t, dir); payload.Int64 != 8<<20 || pingsOut.Int64 != 2 {
		t.Fatalf("rebuilt limits %v %v, want 8 MiB and 2", payload, pingsOut)
	}
}

// A server_stats file written before migration 0025 has no limit
// columns; the rebuild loads its rows with NULL limits.
func TestRebuildBeforeServerLimitsLoadsNull(t *testing.T) {
	dir, file := archiveLimits(t)
	st := openMem(t)
	old := file + ".old.parquet"
	q := fmt.Sprintf(`COPY (SELECT * EXCLUDE (max_payload, max_pending, write_deadline_ns, ping_interval_ns, max_pings_out)
  FROM read_parquet('%s', hive_partitioning = false)) TO '%s' (FORMAT parquet)`, file, old)
	if _, err := st.DB().Exec(q); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(old, file); err != nil {
		t.Fatal(err)
	}
	payload, pingsOut := limitsOf(t, dir)
	if payload.Valid || pingsOut.Valid {
		t.Fatalf("limits from a file before 0025 = %v %v, want NULL", payload, pingsOut)
	}
}
