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
	"github.com/shadone/nats-lens/internal/store"
)

// stripColumns rewrites an archived Parquet file without the named
// columns, as a file written before they existed.
func stripColumns(t *testing.T, st *store.Store, file, cols string) {
	t.Helper()
	old := file + ".old.parquet"
	q := fmt.Sprintf(`COPY (SELECT * EXCLUDE (%s) FROM read_parquet('%s', hive_partitioning = false)) TO '%s' (FORMAT parquet)`,
		cols, file, old)
	if _, err := st.DB().Exec(q); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(old, file); err != nil {
		t.Fatal(err)
	}
}

func hourFile(t *testing.T, dir, table string) string {
	t.Helper()
	files, err := (archive.LocalDir{Dir: dir}).List(context.Background(), table+"/date=2026-10-16/hour=22.")
	if err != nil || len(files) != 1 {
		t.Fatalf("%s files %v, err %v", table, files, err)
	}
	return filepath.Join(dir, files[0])
}

// The archive carries account_details and the certificate expiry through
// export and rebuild; server_stats and connections files written before
// migration 0028 load with NULL in the new columns.
func TestArchiveAccountDetailsAndCerts(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	if _, err := st.WriteSnapshot(context.Background(), store.FixtureSnapshot(t, h0.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if n := exported(t, newArchiver(st, dir), now); n != 1 {
		t.Fatalf("exported %d hours", n)
	}
	rb := rebuilt(t, dir, now)
	if n := scalar[int](t, rb, `SELECT count(*) FROM account_details WHERE account = 'AFAKEAPPACCOUNT' AND limit_connections = 2000`); n != 1 {
		t.Errorf("rebuilt account_details rows = %d, want 1", n)
	}
	if n := scalar[int](t, rb, `SELECT count(*) FROM server_stats WHERE tls_cert_not_after IS NOT NULL`); n != 2 {
		t.Errorf("rebuilt certificate expiries = %d, want 2", n)
	}

	stripColumns(t, st, hourFile(t, dir, "server_stats"), `tls_cert_not_after, cluster_tls_cert_not_after,
  gateway_tls_cert_not_after, leafnode_tls_cert_not_after, websocket_tls_cert_not_after, mqtt_tls_cert_not_after`)
	stripColumns(t, st, hourFile(t, dir, "connections"), `user_jwt, jwt_expires`)
	rb = rebuilt(t, dir, now)
	var certs sql.NullTime
	if err := rb.DB().QueryRow(`SELECT max(tls_cert_not_after) FROM server_stats`).Scan(&certs); err != nil || certs.Valid {
		t.Errorf("certificates from a file before 0028 = %v, %v; want NULL", certs, err)
	}
	if n := scalar[int](t, rb, `SELECT count(*) FROM server_stats`); n != 2 {
		t.Errorf("server_stats rows from a file before 0028 = %d, want 2", n)
	}
	if n := scalar[int](t, rb, `SELECT count(*) FROM connections WHERE user_jwt IS NULL AND jwt_expires IS NULL`); n != 2 {
		t.Errorf("connections from a file before 0028 with NULL user JWT = %d, want 2", n)
	}
}
