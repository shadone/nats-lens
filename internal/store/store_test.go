package store

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestMigrationsApplyOnceAndPersist(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if got := count(t, s.DB(), `SELECT count(*) FROM migrations`); got != len(all) {
		t.Fatalf("migrations applied = %d, want %d", got, len(all))
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenPath(ctx, filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	if got := count(t, s.DB(), `SELECT count(*) FROM migrations`); got != len(all) {
		t.Fatalf("migrations after reopen = %d", got)
	}
}

func TestNewerSchemaRejected(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db.duckdb")
	s, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`INSERT INTO migrations (version, name) VALUES (9999, 'future')`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := OpenPath(ctx, path); err == nil {
		t.Fatal("expected an error for a schema newer than the binary")
	}
}

func TestEveryTableAndColumnHasAComment(t *testing.T) {
	s := openMem(t)
	rows, err := s.DB().Query(`
SELECT c.table_name, c.column_name FROM duckdb_columns() c
JOIN duckdb_tables() t ON t.table_oid = c.table_oid
WHERE NOT t.internal AND (c.comment IS NULL OR c.comment = '')
UNION ALL
SELECT table_name, '' FROM duckdb_tables()
WHERE NOT internal AND (comment IS NULL OR comment = '')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var tbl, col string
		if err := rows.Scan(&tbl, &col); err != nil {
			t.Fatal(err)
		}
		t.Errorf("missing comment: %s.%s", tbl, col)
	}
}

func TestEveryEpochTableHasEpochAndServerID(t *testing.T) {
	s := openMem(t)
	for _, tbl := range epochTables {
		n := count(t, s.DB(), `SELECT count(*) FROM duckdb_columns()
WHERE table_name = ? AND column_name IN ('epoch', 'server_id')`, tbl)
		if n != 2 {
			t.Errorf("%s lacks epoch or server_id", tbl)
		}
	}
}

func TestLastSeenServers(t *testing.T) {
	s := openMem(t)
	if _, err := s.WriteSnapshot(context.Background(), fixtureSnapshot(t, t0)); err != nil {
		t.Fatal(err)
	}
	got, err := s.LastSeenServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got["nats-0"].Equal(t0) {
		t.Fatalf("last seen = %v", got)
	}
}

func TestSweep(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	for i := range 3 {
		if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0.Add(time.Duration(i)*24*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	now := t0.Add(2*24*time.Hour + time.Hour)
	res, err := s.Sweep(ctx, now, 36*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if res.Cutoff != 2 || res.Epochs != 1 {
		t.Fatalf("sweep = %+v", res)
	}
	db := s.DB()
	if n := count(t, db, `SELECT count(*) FROM epochs`); n != 2 {
		t.Errorf("epochs left = %d", n)
	}
	for _, tbl := range append(append([]string{}, epochTables...), accountEpochTables...) {
		if n := count(t, db, "SELECT count(*) FROM "+tbl+" WHERE epoch < 2"); n != 0 {
			t.Errorf("%s keeps %d old rows", tbl, n)
		}
	}
	// Configuration rows that continued into kept epochs stay.
	if n := count(t, db, `SELECT count(*) FROM stream_config`); n == 0 {
		t.Error("stream_config emptied by the sweep")
	}

	// A window with no epochs at all removes everything, and numbering continues.
	if _, err := s.Sweep(ctx, now.Add(30*24*time.Hour), time.Hour); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range append(append(append([]string{"epochs"}, epochTables...), accountEpochTables...), rangeTables...) {
		if n := count(t, db, "SELECT count(*) FROM "+tbl); n != 0 {
			t.Errorf("%s keeps %d rows", tbl, n)
		}
	}
	e, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, now))
	if err != nil {
		t.Fatal(err)
	}
	if e != 4 {
		t.Fatalf("epoch after full sweep = %d, want 4", e)
	}
}

// Every table with epoch columns is in one of the sweep lists, so a new
// table cannot outlive the retention window unnoticed.
func TestSweepCoversEveryEpochTable(t *testing.T) {
	s := openMem(t)
	// The notify tables are swept by SweepNotifications.
	swept := map[string]bool{"epochs": true}
	for _, x := range slices.Concat(epochTables, accountEpochTables, rangeTables, checkTables, derivedEpochTables, notifyTables) {
		swept[x] = true
	}
	rows, err := s.DB().Query(`SELECT DISTINCT c.table_name FROM duckdb_columns() c
JOIN duckdb_tables() t ON t.table_oid = c.table_oid
WHERE NOT t.internal AND c.column_name IN ('epoch', 'last_epoch', 'first_epoch')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var tbl string
		if err := rows.Scan(&tbl); err != nil {
			t.Fatal(err)
		}
		if !swept[tbl] {
			t.Errorf("table %s has epoch columns but is not swept", tbl)
		}
	}
}

// Mutes have no epoch columns and are never swept: they are history the
// operator wrote, kept after expiry and unmute.
func TestSweepKeepsMutes(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMute(ctx, Mute{CheckCode: "stream-025", Reason: "known", MutedBy: "op",
		CreatedAt: t0, ExpiresAt: t0.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	for _, x := range append(append([]string{"epochs"}, epochTables...), rangeTables...) {
		if x == "mutes" {
			t.Fatal("mutes is in a sweep list")
		}
	}
	if _, err := s.Sweep(ctx, t0.Add(30*24*time.Hour), time.Hour); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM mutes`); n != 1 {
		t.Errorf("mutes after a full sweep = %d, want 1", n)
	}
}

// archiveExempt lists the tables the archive does not export, with the
// reason.
var archiveExempt = map[string]string{
	"notify_identity":      "local database incarnation; a rebuild must generate a different delivery identity",
	"notify_delivery_rate": "local delivery watermarks; an archive rebuild starts a new baseline",
	"archive_hours":        "records what was exported; a rebuild writes it from the loaded files",
	"migrations":           "migration bookkeeping; a new database applies the migrations itself",
	"inventory":            "loaded from inventory_file on every start",
	"epoch_asset_counts":   "derived from the archived reports and configurations; a rebuild computes it again",
	"notify_channels":      "what was sent to a receiver is not what NATS showed; after a rebuild every channel starts from a new baseline",
	"notify_state":         "what was sent to a receiver is not what NATS showed; after a rebuild every channel starts from a new baseline",
	"notify_outbox":        "what was sent to a receiver is not what NATS showed; after a rebuild every channel starts from a new baseline",
}

// Every table is archived or listed as exempt, so that a new table is not
// lost on a rebuild unnoticed.
func TestEveryTableArchivedOrExempt(t *testing.T) {
	s := openMem(t)
	archived := map[string]bool{}
	for _, name := range ArchiveTableNames() {
		archived[name] = true
	}
	rows, err := s.DB().Query(`SELECT table_name FROM duckdb_tables() WHERE NOT internal`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var tbl string
		if err := rows.Scan(&tbl); err != nil {
			t.Fatal(err)
		}
		seen[tbl] = true
		if archived[tbl] && archiveExempt[tbl] != "" {
			t.Errorf("table %s is both archived and exempt", tbl)
		}
		if !archived[tbl] && archiveExempt[tbl] == "" {
			t.Errorf("table %s is neither archived nor listed as exempt", tbl)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"mutes", "findings", "finding_transitions", "check_runs"} {
		if !archived[tbl] {
			t.Errorf("%s is not archived", tbl)
		}
	}
	for tbl := range archiveExempt {
		if !seen[tbl] {
			t.Errorf("exempt table %s does not exist", tbl)
		}
	}
}
