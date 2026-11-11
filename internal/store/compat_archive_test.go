package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// compatHour is the hour of 2026-10-17, UTC, that a fixture with hour h
// writes its rows in.
func compatHour(h int) time.Time { return time.Date(2026, 10, 17, h, 0, 0, 0, time.UTC) }

// compatExport writes the archived tables of the database at path as an
// export of hour does: each table's rows for the hour, SELECT *, to one
// Parquet file. A fixture at an older version has older tables, so the
// files have the columns that version's export wrote. It returns the file
// of each table that has rows.
func compatExport(t *testing.T, path, dir string, hour time.Time) map[string]string {
	t.Helper()
	db, err := openReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tables := compatTables(t, db)
	start, end := tsLiteral(hour), tsLiteral(hour.Add(time.Hour))
	files := map[string]string{}
	for _, at := range archiveTables() {
		if !slices.Contains(tables, at.name) {
			continue
		}
		where := "true"
		if at.where != "" {
			where = fmt.Sprintf(at.where, start, end)
		}
		var n int
		if err := db.QueryRow("SELECT count(*) FROM " + at.name + " WHERE " + where).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", at.name, err)
		}
		if n == 0 {
			continue
		}
		p := filepath.Join(dir, fmt.Sprintf("%s.%s.parquet", at.name, hour.Format("2006010215")))
		q := fmt.Sprintf("COPY (SELECT * FROM %s WHERE %s) TO %s (FORMAT parquet, COMPRESSION zstd)", at.name, where, sqlString(p))
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("export %s: %v", at.name, err)
		}
		files[at.name] = p
	}
	return files
}

// Archive hours written at an older schema rebuild into a database of the
// current schema, alone or together with hours of the current schema:
// every archived row comes back with the values of the columns its file
// has, the columns its file lacks are NULL or their default, and the
// sequences continue after the highest loaded numbers.
func TestCompatRebuildFromOlderArchives(t *testing.T) {
	ctx := context.Background()
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	newest := all[len(all)-1].version
	currentDir := t.TempDir()
	current := filepath.Join(currentDir, "current.duckdb")
	buildCompatFixture(t, current, newest, compatOpts{base: 100, hour: 15})
	currentFiles := compatExport(t, current, currentDir, compatHour(15))
	for _, v := range compatVersions(t, compatSeeds...) {
		for _, mixed := range []bool{false, true} {
			t.Run(fmt.Sprintf("from_%04d/%s", v, map[bool]string{false: "alone", true: "mixed"}[mixed]), func(t *testing.T) {
				dir := t.TempDir()
				fixture := filepath.Join(dir, "fixture.duckdb")
				buildCompatFixture(t, fixture, v, compatOpts{hour: 14})
				sum := fileSum(t, fixture)
				hours := map[time.Time]RebuildHour{compatHour(14): {Files: compatExport(t, fixture, dir, compatHour(14)), Final: true}}
				if mixed {
					hours[compatHour(15)] = RebuildHour{Files: currentFiles, Final: true}
				}
				rb, err := OpenPath(ctx, "")
				if err != nil {
					t.Fatal(err)
				}
				defer rb.Close()
				if err := rb.Rebuild(ctx, hours, MaxIDs{}, compatNow); err != nil {
					t.Fatalf("rebuild from %04d archives (mixed %v): %v", v, mixed, err)
				}
				compatCheckRebuilt(t, rb.DB(), hours)
				compatStoreReads(t, rb, v)
				if after := fileSum(t, fixture); after != sum {
					t.Error("the fixture file changed")
				}
			})
		}
	}
}

// compatCheckRebuilt compares a rebuilt database with the files it was
// rebuilt from.
func compatCheckRebuilt(t *testing.T, db *sql.DB, hours map[time.Time]RebuildHour) {
	t.Helper()
	perTable := map[string][]string{}
	for _, h := range hours {
		for table, p := range h.Files {
			perTable[table] = append(perTable[table], p)
		}
	}
	self := compatCatalog(t, db)
	for table, paths := range perTable {
		total := 0
		fileCols := map[string][]string{}
		for _, p := range paths {
			cols := compatTopColumns(t, db, p)
			fileCols[p] = cols
			var n, missing int
			q := fmt.Sprintf(`SELECT (SELECT count(*) FROM read_parquet(%[2]s, hive_partitioning = false)),
    (SELECT count(*) FROM (SELECT %[1]s FROM read_parquet(%[2]s, hive_partitioning = false) EXCEPT ALL SELECT %[1]s FROM %[3]s))`,
				strings.Join(cols, ", "), sqlString(p), table)
			if err := db.QueryRow(q).Scan(&n, &missing); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
			if missing != 0 {
				t.Errorf("%s: %d of %d archived rows of %s are not rebuilt as archived", table, missing, n, filepath.Base(p))
			}
			total += n
		}
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != total {
			t.Errorf("%s: %d rows rebuilt from %d archived", table, n, total)
		}
		// A column some file lacks is NULL or its default in that file's
		// rows: every rebuilt row with another value comes from a file
		// that has the column (whose rows were compared above).
		for _, c := range compatColumns(t, db, self, table) {
			col := quoteIdent(c.name)
			var with []string
			for _, p := range paths {
				if slices.Contains(fileCols[p], col) {
					with = append(with, p)
				}
			}
			if len(with) == len(paths) {
				continue
			}
			var def sql.NullString
			if err := db.QueryRow(`SELECT column_default FROM duckdb_columns() WHERE database_name = ? AND table_name = ? AND column_name = ?`,
				self, table, c.name).Scan(&def); err != nil {
				t.Fatal(err)
			}
			cond := col + " IS NOT NULL"
			if def.Valid {
				cond += fmt.Sprintf(" AND %s IS DISTINCT FROM (%s)", col, def.String)
			}
			var actual, expected int
			if err := db.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s WHERE %s`, table, cond)).Scan(&actual); err != nil {
				t.Fatal(err)
			}
			for _, p := range with {
				var k int
				if err := db.QueryRow(fmt.Sprintf(`SELECT count(*) FROM read_parquet(%s, hive_partitioning = false) WHERE %s`, sqlString(p), cond)).Scan(&k); err != nil {
					t.Fatal(err)
				}
				expected += k
			}
			if actual != expected {
				t.Errorf("%s.%s: %d rebuilt rows from files without the column hold a value other than NULL or the default", table, c.name, actual-expected)
			}
		}
	}
	for _, s := range []struct{ seq, table, col string }{
		{"epoch_seq", "epochs", "epoch"}, {"event_seq", "events", "id"}, {"mute_seq", "mutes", "id"}, {"ack_seq", "acknowledgements", "id"},
	} {
		var next, max int64
		if err := db.QueryRow(fmt.Sprintf(`SELECT nextval('%s'), (SELECT coalesce(max(%s), 0) FROM %s)`, s.seq, s.col, s.table)).Scan(&next, &max); err != nil {
			t.Fatal(err)
		}
		if next != max+1 {
			t.Errorf("%s after the rebuild: next %d, want %d", s.seq, next, max+1)
		}
	}
}

// compatTopColumns lists the top-level columns of a Parquet file, quoted.
func compatTopColumns(t *testing.T, db *sql.DB, path string) []string {
	t.Helper()
	var out []string
	for _, c := range compatStrings(t, db, `SELECT column_name FROM (DESCRIBE SELECT * FROM read_parquet(`+sqlString(path)+`, hive_partitioning = false))`) {
		out = append(out, quoteIdent(c))
	}
	return out
}
