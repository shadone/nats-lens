package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// compatUnarchived lists the tables the hourly export leaves out, each on
// purpose: the schema record, the archive's own bookkeeping, the inventory
// file's copy, and the notification channels' local state, which a
// rebuild starts afresh, and the asset counts per epoch, which a rebuild
// computes again from the archived reports.
var compatUnarchived = []string{
	"migrations", "archive_hours", "inventory", "epoch_asset_counts",
	"notify_channels", "notify_state", "notify_outbox", "notify_identity", "notify_delivery_rate",
}

// Every table is either archived or listed in compatUnarchived, so a
// table added by a migration is not left out of the archive unnoticed.
func TestCompatEveryTableArchivedOrListed(t *testing.T) {
	s := openMem(t)
	archived := ArchiveTableNames()
	for _, table := range compatTables(t, s.DB()) {
		in, out := slices.Contains(archived, table), slices.Contains(compatUnarchived, table)
		switch {
		case in && out:
			t.Errorf("%s is archived and listed in compatUnarchived", table)
		case !in && !out:
			t.Errorf("%s is neither archived nor listed in compatUnarchived: add it to archiveTables, or to compatUnarchived with the reason", table)
		}
	}
	for _, table := range append(append([]string{}, archived...), compatUnarchived...) {
		if !slices.Contains(compatTables(t, s.DB()), table) {
			t.Errorf("%s is listed but no migration creates it", table)
		}
	}
}

// laterNotNull applies ms to a new database one by one and returns the
// columns of archived tables that a migration after the table's own makes
// NOT NULL: a column it adds as NOT NULL, or one it changes from nullable.
// A rebuild reads the hours of a table with union_by_name, so in rows from
// files written before such a column existed the column is NULL whenever
// another file has it: a default does not apply, and the load fails the
// constraint. Files written while the column was nullable can hold NULL
// in it too. A table dropped by one migration and created again under the
// same name by a later one counts as new there, so its columns are not
// checked, although older archive files hold its earlier rows.
func laterNotNull(t *testing.T, ms []migration) []string {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE migrations (version INTEGER PRIMARY KEY, name VARCHAR NOT NULL,
    applied_at TIMESTAMP NOT NULL DEFAULT current_timestamp)`); err != nil {
		t.Fatal(err)
	}
	archived := ArchiveTableNames()
	nullable := func() map[string]bool {
		out := map[string]bool{}
		rows, err := db.Query(`SELECT table_name || '.' || column_name, is_nullable FROM duckdb_columns()
WHERE database_name = 'memory' AND schema_name = 'main'`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var col string
			var null bool
			if err := rows.Scan(&col, &null); err != nil {
				t.Fatal(err)
			}
			out[col] = null
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	var found []string
	for _, m := range ms {
		tablesBefore := compatTables(t, db)
		before := nullable()
		if err := applyMigration(ctx, db, m); err != nil {
			t.Fatal(err)
		}
		after := nullable()
		cols := make([]string, 0, len(after))
		for col := range after {
			cols = append(cols, col)
		}
		slices.Sort(cols)
		for _, col := range cols {
			table, _, _ := strings.Cut(col, ".")
			if after[col] || !slices.Contains(tablesBefore, table) || !slices.Contains(archived, table) {
				continue
			}
			if was, existed := before[col]; !existed || was {
				found = append(found, col+" (migration "+m.name+")")
			}
		}
	}
	return found
}

// A column added to an archived table after the table was created is
// nullable, and stays so; a column that was nullable is never made NOT
// NULL. This is stricter than a default: see laterNotNull.
func TestCompatArchivedColumnsAddedLaterAreNullable(t *testing.T) {
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if found := laterNotNull(t, all); len(found) > 0 {
		t.Errorf("NOT NULL columns added to archived tables after their creation: %v; "+
			"a rebuild of hours archived before and after the column fails on them, default or not", found)
	}
	// The check finds such a column, with a default too.
	bad := migration{version: all[len(all)-1].version + 1, name: "not_null_on_mutes",
		sql: `ALTER TABLE mutes ADD COLUMN shop_flag INTEGER NOT NULL DEFAULT 0;`}
	if found := laterNotNull(t, append(append([]migration{}, all...), bad)); len(found) != 1 || !strings.HasPrefix(found[0], "mutes.shop_flag") {
		t.Errorf("a NOT NULL column with a default added to mutes: found %v, want mutes.shop_flag", found)
	}
	// And a nullable column made NOT NULL.
	bad = migration{version: all[len(all)-1].version + 1, name: "set_not_null_on_mutes",
		sql: `ALTER TABLE mutes ALTER COLUMN entity_type SET NOT NULL;`}
	if found := laterNotNull(t, append(append([]migration{}, all...), bad)); len(found) != 1 || !strings.HasPrefix(found[0], "mutes.entity_type") {
		t.Errorf("mutes.entity_type made NOT NULL: found %v, want mutes.entity_type", found)
	}
}

// The engine behaviour laterNotNull relies on: a NOT NULL column with a
// default loads from files that all lack it, but not from a mix of files
// with and without it, read by name as Rebuild reads them.
func TestCompatNotNullDefaultFailsMixedRebuild(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	oldFile, newFile := sqlString(filepath.Join(dir, "old.parquet")), sqlString(filepath.Join(dir, "new.parquet"))
	for _, q := range []string{
		`COPY (SELECT 1 AS a) TO ` + oldFile + ` (FORMAT parquet)`,
		`COPY (SELECT 2 AS a, 7 AS b) TO ` + newFile + ` (FORMAT parquet)`,
		`CREATE TABLE dst (a INTEGER, b INTEGER NOT NULL DEFAULT 0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	load := func(files string) error {
		_, err := db.Exec(`INSERT INTO dst BY NAME SELECT * FROM read_parquet([` + files + `], union_by_name = true, hive_partitioning = false)`)
		return err
	}
	if err := load(oldFile); err != nil {
		t.Errorf("old file alone: %v, want the default to apply", err)
	}
	if err := load(oldFile + ", " + newFile); err == nil {
		t.Error("old and new files together loaded; laterNotNull's reason no longer holds")
	}
}
