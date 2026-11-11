package store

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"
)

// compatUnarchived lists the tables the hourly export leaves out, each on
// purpose: the schema record, the archive's own bookkeeping, the inventory
// file's copy, and the notification channels' local state, which a
// rebuild starts afresh.
var compatUnarchived = []string{
	"migrations", "archive_hours", "inventory",
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
// columns of archived tables that a migration after the table's own adds
// as NOT NULL. A rebuild reads the hours of a table with
// union_by_name, so in rows from files written before such a column
// existed the column is NULL whenever another file has it: a default does
// not apply, and the load fails the constraint.
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
	tableSeen := map[string]bool{}
	var found []string
	for _, m := range ms {
		before := map[string]bool{}
		for _, col := range compatStrings(t, db, `SELECT table_name || '.' || column_name FROM duckdb_columns()
WHERE database_name = 'memory' AND schema_name = 'main'`) {
			before[col] = true
		}
		if err := applyMigration(ctx, db, m); err != nil {
			t.Fatal(err)
		}
		rows := compatStrings(t, db, `SELECT table_name || '.' || column_name FROM duckdb_columns()
WHERE database_name = 'memory' AND schema_name = 'main' AND NOT is_nullable ORDER BY 1`)
		for _, table := range compatTables(t, db) {
			existed := tableSeen[table]
			tableSeen[table] = true
			if !existed || !slices.Contains(archived, table) {
				continue
			}
			for _, col := range rows {
				if strings.HasPrefix(col, table+".") && !before[col] {
					found = append(found, col+" (migration "+m.name+")")
				}
			}
		}
	}
	return found
}

// A column added to an archived table after the table was created is
// nullable. This is stricter than a default: see laterNotNull.
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
}
