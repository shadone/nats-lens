package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Compatibility fixtures are generated, not stored: a database at schema
// version v is built by applying the embedded migrations 1..v, which never
// change (TestMigrationsMatchGolden), and filling every table with
// deterministic generic rows. The same version and options always give the
// same rows, so a fixture is reproducible from the repository alone.
//
// Rows: right after the migration that creates a table, when the
// migration left the table empty, rows 1 and 2 are written with the
// columns the table has then; once the fixture reaches v, row 3 is written
// to every table with all its columns at v. Every sequence is advanced by
// compatSequenceSteps. Values depend on the row number n (opts.base+i) and
// the column's type; a few columns take values their CHECK constraints
// allow. All times fall in one hour, opts.hour, so one archive hour holds
// every row.

// compatOpts selects the fixture's values; fixtures with different bases
// and hours can be archived together without key conflicts.
type compatOpts struct {
	base int // added to the row number
	hour int // the hour of 2026-10-17, UTC, every time falls in
}

// compatSequenceSteps is how often a fixture calls nextval on each
// sequence, so that its next value is compatSequenceSteps+1.
const compatSequenceSteps = 7

// compatSkip lists the tables the generator writes no rows to: the
// migrations table records the schema itself, and notify_identity holds
// the one row migration 0036 writes.
var compatSkip = []string{"migrations", "notify_identity"}

// buildCompatFixture writes a database at schema version v to path and
// closes it after a checkpoint, so the file holds everything without a
// write-ahead log.
func buildCompatFixture(t *testing.T, path string, v int, opts compatOpts) {
	t.Helper()
	ctx := context.Background()
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// The migrations table as migrate creates it before 0010 adds the
	// checksum column.
	if _, err := db.Exec(`CREATE TABLE migrations (version INTEGER PRIMARY KEY, name VARCHAR NOT NULL,
    applied_at TIMESTAMP NOT NULL DEFAULT current_timestamp)`); err != nil {
		t.Fatal(err)
	}
	for _, m := range all {
		if m.version > v {
			break
		}
		before := compatTables(t, db)
		if err := applyMigration(ctx, db, m); err != nil {
			t.Fatal(err)
		}
		for _, table := range compatTables(t, db) {
			if slices.Contains(before, table) || slices.Contains(compatSkip, table) {
				continue
			}
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				compatInsert(t, db, table, opts, 1, 2)
			}
		}
	}
	for _, table := range compatTables(t, db) {
		if !slices.Contains(compatSkip, table) {
			compatInsert(t, db, table, opts, 3)
		}
	}
	for _, seq := range compatSequences(t, db) {
		for range compatSequenceSteps {
			if _, err := db.Exec(`SELECT nextval('` + seq + `')`); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := db.Exec(`CHECKPOINT`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".wal"); err == nil {
		t.Fatalf("fixture %s left a write-ahead log after its checkpoint", filepath.Base(path))
	}
}

// compatTables lists the base tables of the main schema, sorted.
func compatTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	return compatStrings(t, db, `SELECT table_name FROM duckdb_tables() WHERE schema_name = 'main' ORDER BY 1`)
}

// compatSequences lists the sequences of the main schema, sorted.
func compatSequences(t *testing.T, db *sql.DB) []string {
	t.Helper()
	return compatStrings(t, db, `SELECT sequence_name FROM duckdb_sequences() WHERE schema_name = 'main' ORDER BY 1`)
}

func compatStrings(t *testing.T, db *sql.DB, q string, args ...any) []string {
	t.Helper()
	rows, err := db.Query(q, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

type compatColumn struct{ name, dataType string }

// compatColumns lists a table's columns in order, in the database of db
// (an attached database is named by catalog).
func compatColumns(t *testing.T, db *sql.DB, catalog, table string) []compatColumn {
	t.Helper()
	rows, err := db.Query(`SELECT column_name, data_type FROM duckdb_columns()
WHERE database_name = ? AND schema_name = 'main' AND table_name = ? ORDER BY column_index`, catalog, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []compatColumn
	for rows.Next() {
		var c compatColumn
		if err := rows.Scan(&c.name, &c.dataType); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// compatInsert writes the generator's rows i to table.
func compatInsert(t *testing.T, db *sql.DB, table string, opts compatOpts, is ...int) {
	t.Helper()
	cols := compatColumns(t, db, compatCatalog(t, db), table)
	names := make([]string, len(cols))
	for k, c := range cols {
		names[k] = quoteIdent(c.name)
	}
	for _, i := range is {
		values := make([]string, len(cols))
		for k, c := range cols {
			values[k] = compatValue(t, table, c, opts, i)
		}
		q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(names, ", "), strings.Join(values, ", "))
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("fixture row %d of %s: %v\n%s", i, table, err, q)
		}
	}
}

// compatCatalog is the name of db's own database.
func compatCatalog(t *testing.T, db *sql.DB) string {
	t.Helper()
	var name string
	if err := db.QueryRow(`SELECT current_database()`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	return name
}

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

// compatValue is the SQL literal of row i's value in column c of table.
func compatValue(t *testing.T, table string, c compatColumn, opts compatOpts, i int) string {
	t.Helper()
	n := opts.base + i
	at := func(day int) string {
		return fmt.Sprintf("2026-10-%02d %02d:%02d:00", 17+day, opts.hour, i)
	}
	odd := i%2 == 1
	switch table + "." + c.name {
	case "acknowledgements.ended_at", "mutes.unmuted_at":
		if odd {
			return "NULL"
		}
	case "acknowledgements.ended_by", "mutes.unmuted_by":
		if odd {
			return "NULL"
		}
	case "acknowledgements.end_reason":
		if odd {
			return "NULL"
		}
		return "'released'"
	case "check_runs.status":
		return "'ok'"
	case "finding_transitions.kind", "findings.state", "findings.last_transition", "notify_state.state":
		return "'firing'"
	case "findings.severity", "notify_state.severity":
		return "'warning'"
	case "inventory.kind":
		return "'stream'"
	case "inventory.consumer":
		return "NULL"
	case "mutes.expires_at":
		return "TIMESTAMP '" + at(1) + "'"
	case "notify_outbox.state":
		return []string{"'delivered'", "'refused'", "'pending'"}[(i-1)%3]
	}
	switch c.dataType {
	case "BIGINT", "INTEGER", "UBIGINT", "SMALLINT", "HUGEINT", "UINTEGER":
		return strconv.Itoa(n)
	case "DOUBLE", "FLOAT":
		return strconv.Itoa(n) + ".5"
	case "BOOLEAN":
		return strconv.FormatBool(odd)
	case "VARCHAR":
		return fmt.Sprintf("'shop-%s-%d'", c.name, n)
	case "JSON":
		return fmt.Sprintf(`'{"shop": "value-%d"}'::JSON`, n)
	case "TIMESTAMP":
		return "TIMESTAMP '" + at(0) + "'"
	case "TIMESTAMP WITH TIME ZONE":
		return "TIMESTAMPTZ '" + at(0) + "+00'"
	case "UUID":
		return fmt.Sprintf("'00000000-0000-4000-8000-%012d'::UUID", n)
	case "VARCHAR[]":
		return fmt.Sprintf("['shop-%s-%d']", c.name, n)
	case "BIGINT[]":
		return fmt.Sprintf("[%d]::BIGINT[]", n)
	case "MAP(VARCHAR, VARCHAR)":
		return fmt.Sprintf("MAP {'shop': 'value-%d'}", n)
	}
	t.Fatalf("fixture generator has no value for %s.%s of type %s: add the type to compatValue", table, c.name, c.dataType)
	return ""
}
