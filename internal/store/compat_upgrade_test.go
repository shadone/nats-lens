package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// compatMatrixEnv selects the whole upgrade and archive matrix: every
// schema version as a starting point. Without it the tests run the seed
// versions in compatSeeds only.
const compatMatrixEnv = "NATSLENS_COMPAT_MATRIX"

// compatSeeds are the starting versions the normal test tier runs, with
// the newest version: the first schema, whose upgrade applies every later
// migration (over rows only in the three tables 0001 creates), and the
// last before the notification backfills of 0036 and 0038, which run over
// rows in the tables they read. compatVersions adds the newest.
var compatSeeds = []int{1, 35, 37}

// compatVersions lists the schema versions a matrix test starts from:
// every version with compatMatrixEnv set, else seeds and the newest.
func compatVersions(t *testing.T, seeds ...int) []int {
	t.Helper()
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv(compatMatrixEnv) != "" {
		var out []int
		for _, m := range all {
			out = append(out, m.version)
		}
		return out
	}
	return append(append([]int{}, seeds...), all[len(all)-1].version)
}

// compatNow is a time after every fixture row and before any mute's expiry.
var compatNow = time.Date(2026, 10, 17, 16, 0, 0, 0, time.UTC)

// An older database opened by the current binary is upgraded in place:
// the schema is then exactly a new database's, every row keeps the values
// of the columns it had, each added column holds its default (or the
// migration's documented backfill), sequences continue where they were,
// the delivery identity is kept, and the store reads and writes the
// upgraded rows. The fixture itself is only read: every run upgrades a
// copy, and the fixture's hash is checked afterwards.
func TestCompatUpgradeFromOlderSchemas(t *testing.T) {
	ctx := context.Background()
	fresh := compatFreshSchema(t)
	for _, v := range compatVersions(t, compatSeeds...) {
		t.Run(fmt.Sprintf("from_%04d", v), func(t *testing.T) {
			dir := t.TempDir()
			fixture := filepath.Join(dir, "fixture.duckdb")
			buildCompatFixture(t, fixture, v, compatOpts{hour: 14})
			sum := fileSum(t, fixture)
			work := filepath.Join(dir, "work.duckdb")
			copyFile(t, fixture, work)

			var identity string
			if v >= 36 {
				identity = compatIdentity(t, fixture)
			}
			s, err := OpenPath(ctx, work)
			if err != nil {
				t.Fatalf("upgrade from %04d: %v", v, err)
			}
			compatStoreReads(t, s, v)
			s.Close()
			switch got := compatIdentity(t, work); {
			case got == "":
				t.Error("no delivery identity after the upgrade")
			case identity != "" && got != identity:
				t.Errorf("delivery identity changed by the upgrade: %s, was %s", got, identity)
			default:
				identity = got
			}

			compatCheckUpgraded(t, work, fixture, v, fresh)

			s, err = OpenPath(ctx, work)
			if err != nil {
				t.Fatalf("reopen after the upgrade: %v", err)
			}
			var reopened string
			if err := s.DB().QueryRow(`SELECT incarnation::VARCHAR FROM notify_identity`).Scan(&reopened); err != nil || reopened != identity {
				t.Errorf("delivery identity on reopen = %q, %v; want %s", reopened, err, identity)
			}
			compatStoreWrites(t, s, v)
			s.Close()
			if after := fileSum(t, fixture); after != sum {
				t.Error("the fixture file changed")
			}
		})
	}
}

// A crash part-way through an upgrade, after some of the pending
// migrations committed, leaves a database that the next start finishes
// upgrading. The crash is a copy of the database file and its log taken
// while the upgrading process still holds them; the original is left
// alone.
func TestCompatUpgradeInterruptedThenRestarted(t *testing.T) {
	ctx := context.Background()
	fresh := compatFreshSchema(t)
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range compatVersions(t, 1) {
		if v >= all[len(all)-1].version {
			continue
		}
		t.Run(fmt.Sprintf("from_%04d", v), func(t *testing.T) {
			dir := t.TempDir()
			fixture := filepath.Join(dir, "fixture.duckdb")
			buildCompatFixture(t, fixture, v, compatOpts{hour: 14})
			work := filepath.Join(dir, "work.duckdb")
			copyFile(t, fixture, work)
			db, err := sql.Open("duckdb", work)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			// Apply about half of the pending migrations, then copy the
			// files without a checkpoint or close.
			stop := v + (all[len(all)-1].version-v+1)/2
			var part []migration
			for _, m := range all {
				if m.version <= stop {
					part = append(part, m)
				}
			}
			if err := migrateWith(ctx, db, part, v); err != nil {
				t.Fatal(err)
			}
			crash := filepath.Join(t.TempDir(), "crash.duckdb")
			copyFile(t, work, crash)
			if _, err := os.Stat(work + ".wal"); err == nil {
				copyFile(t, work+".wal", crash+".wal")
			}
			s, err := OpenPath(ctx, crash)
			if err != nil {
				t.Fatalf("restart after a crash at %04d of %04d..%04d: %v", stop, v, all[len(all)-1].version, err)
			}
			compatStoreReads(t, s, v)
			s.Close()
			compatCheckUpgraded(t, crash, fixture, v, fresh)
		})
	}
}

// compatFreshSchema describes the schema of a new database.
func compatFreshSchema(t *testing.T) []string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fresh.duckdb")
	s, err := OpenPath(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	db, err := openReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	return compatSchema(t, db, compatCatalog(t, db))
}

// compatSchema describes a database's catalog: every column with its
// type, default, nullability and comment, the constraints, indexes,
// sequences, views and table comments. Two databases with the same
// description have the same schema. A sequence's start is left out: after
// a reopen DuckDB reports the next value there.
func compatSchema(t *testing.T, db *sql.DB, catalog string) []string {
	t.Helper()
	var out []string
	for _, q := range []string{
		`SELECT 'column ' || table_name || '.' || column_name || ' #' || column_index || ' ' || data_type ||
    ' default=' || coalesce(column_default, '-') || ' nullable=' || is_nullable || ' comment=' || coalesce(comment, '-')
FROM duckdb_columns() WHERE database_name = ? AND schema_name = 'main'`,
		`SELECT 'constraint ' || table_name || ' ' || constraint_type || ' ' || constraint_text
FROM duckdb_constraints() WHERE database_name = ? AND schema_name = 'main'`,
		`SELECT 'index ' || table_name || ' ' || index_name || ' unique=' || is_unique || ' ' || coalesce(sql, '-')
FROM duckdb_indexes() WHERE database_name = ? AND schema_name = 'main'`,
		`SELECT 'sequence ' || sequence_name || ' inc=' || increment_by || ' min=' || min_value ||
    ' max=' || max_value || ' cycle=' || cycle || ' comment=' || coalesce(comment, '-')
FROM duckdb_sequences() WHERE database_name = ? AND schema_name = 'main'`,
		`SELECT 'view ' || view_name || ' ' || coalesce(sql, '-') || ' comment=' || coalesce(comment, '-')
FROM duckdb_views() WHERE database_name = ? AND schema_name = 'main' AND NOT internal`,
		`SELECT 'table ' || table_name || ' comment=' || coalesce(comment, '-')
FROM duckdb_tables() WHERE database_name = ? AND schema_name = 'main'`,
	} {
		out = append(out, compatStrings(t, db, q, catalog)...)
	}
	slices.Sort(out)
	return out
}

// compatIdentity reads the delivery identity of the database at path, or
// "" when it has none (before 0036).
func compatIdentity(t *testing.T, path string) string {
	t.Helper()
	db, err := openReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM duckdb_tables() WHERE table_name = 'notify_identity'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		return ""
	}
	var id string
	if err := db.QueryRow(`SELECT incarnation::VARCHAR FROM notify_identity`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// compatCheckUpgraded compares the upgraded database at path with the
// fixture it was copied from (version v) and with a new database's schema.
func compatCheckUpgraded(t *testing.T, path, fixture string, v int, fresh []string) {
	t.Helper()
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	self := compatCatalog(t, db)
	if got := compatSchema(t, db, self); !slices.Equal(got, fresh) {
		t.Errorf("schema after the upgrade from %04d differs from a new database's:\n%s", v, compatDiff(got, fresh))
	}
	var applied, checked int
	if err := db.QueryRow(`SELECT count(*), count(sha256) FROM migrations`).Scan(&applied, &checked); err != nil {
		t.Fatal(err)
	}
	if applied != len(all) || checked != len(all) {
		t.Errorf("migrations recorded %d, with checksum %d; want %d", applied, checked, len(all))
	}
	if _, err := db.Exec(`ATTACH '` + fixture + `' AS old (READ_ONLY)`); err != nil {
		t.Fatal(err)
	}
	for _, table := range compatStrings(t, db, `SELECT table_name FROM duckdb_tables() WHERE database_name = 'old' AND schema_name = 'main' AND table_name <> 'migrations' ORDER BY 1`) {
		oldCols := compatColumns(t, db, "old", table)
		names := make([]string, len(oldCols))
		for k, c := range oldCols {
			names[k] = quoteIdent(c.name)
		}
		cols := strings.Join(names, ", ")
		for _, dir := range [][2]string{{"old.main." + table, table}, {table, "old.main." + table}} {
			var n int
			q := fmt.Sprintf(`SELECT count(*) FROM (SELECT %[1]s FROM %[2]s EXCEPT ALL SELECT %[1]s FROM %[3]s)`, cols, dir[0], dir[1])
			if err := db.QueryRow(q).Scan(&n); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
			if n != 0 {
				t.Errorf("%s: %d rows of %s are not in %s", table, n, dir[0], dir[1])
			}
		}
		// Columns the upgrade added hold their default on the old rows.
		for _, c := range compatColumns(t, db, self, table) {
			if slices.ContainsFunc(oldCols, func(o compatColumn) bool { return o.name == c.name }) {
				continue
			}
			if compatBackfilled(table, c.name) {
				continue
			}
			var def sql.NullString
			if err := db.QueryRow(`SELECT column_default FROM duckdb_columns() WHERE database_name = ? AND table_name = ? AND column_name = ?`,
				self, table, c.name).Scan(&def); err != nil {
				t.Fatal(err)
			}
			expr := "NULL"
			if def.Valid {
				expr = def.String
			}
			var n int
			q := fmt.Sprintf(`SELECT count(*) FROM %s WHERE %s IS DISTINCT FROM (%s)`, table, quoteIdent(c.name), expr)
			if err := db.QueryRow(q).Scan(&n); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
			if n != 0 {
				t.Errorf("%s.%s, added by the upgrade from %04d: %d rows differ from its default %s", table, c.name, v, n, expr)
			}
		}
	}
	compatCheckBackfills(t, db, v)
	// Sequences that existed continue after the fixture's last value; the
	// others start where a new database's do.
	oldSeqs := compatStrings(t, db, `SELECT sequence_name FROM duckdb_sequences() WHERE database_name = 'old'`)
	for _, seq := range compatStrings(t, db, `SELECT sequence_name FROM duckdb_sequences() WHERE database_name = ? ORDER BY 1`, self) {
		var next, start int64
		if err := db.QueryRow(`SELECT nextval('`+seq+`'), (SELECT start_value FROM duckdb_sequences() WHERE database_name = ? AND sequence_name = ?)`,
			self, seq).Scan(&next, &start); err != nil {
			t.Fatal(err)
		}
		want := start
		if slices.Contains(oldSeqs, seq) {
			want = compatSequenceSteps + 1
		}
		if next != want {
			t.Errorf("sequence %s after the upgrade from %04d: next %d, want %d", seq, v, next, want)
		}
	}
}

// compatBackfilled reports the added columns a migration fills from the
// existing rows rather than leaving at their default.
func compatBackfilled(table, column string) bool {
	switch table + "." + column {
	case "notify_outbox.finished_at", "notify_state.acknowledged":
		return true
	}
	return false
}

// compatCheckBackfills checks the notification outcomes migrations 0036
// and 0038 derive from older rows.
func compatCheckBackfills(t *testing.T, db *sql.DB, v int) {
	t.Helper()
	checks := []struct {
		name string
		from bool // applies to upgrades from this version
		q    string
	}{
		// 0036: a finished message's outcome time is its delivery time,
		// else its queue time; a pending one has none.
		{"outbox finished_at", v < 36, `SELECT count(*) FROM notify_outbox o JOIN old.main.notify_outbox p USING (id)
WHERE o.finished_at IS DISTINCT FROM CASE WHEN p.state <> 'pending' THEN coalesce(p.delivered_at, p.created_at) END`},
		// 0036: a channel's delivery watermark is its newest outcome.
		{"delivery watermark", v < 36, `SELECT count(*) FROM (
    SELECT channel, max(finished_at) AS last_at FROM notify_outbox WHERE state <> 'pending' GROUP BY channel) w
FULL JOIN notify_delivery_rate r USING (channel) WHERE w.last_at IS DISTINCT FROM r.last_finished_at`},
		// 0038: an episode is acknowledged on its channel only with a
		// matching acknowledgement in force at the baseline; the fixture's
		// odd rows have one when acknowledgements existed before the
		// upgrade (0037).
		{"channel acknowledgement", v < 38, fmt.Sprintf(`SELECT count(*) FROM notify_state
WHERE acknowledged IS DISTINCT FROM (%v AND channel IN ('shop-channel-1', 'shop-channel-3'))`, v == 37)},
	}
	for _, c := range checks {
		if !c.from || v < 35 {
			continue
		}
		var n int
		if err := db.QueryRow(c.q).Scan(&n); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if n != 0 {
			t.Errorf("%s after the upgrade from %04d: %d rows differ", c.name, v, n)
		}
	}
}

// compatStoreReads reads the fixture's findings, mutes, acceptances and
// acknowledgements through the store and compares the counts with the
// table rows.
func compatStoreReads(t *testing.T, s *Store, v int) {
	t.Helper()
	ctx := context.Background()
	rows := func(q string) int {
		var n int
		if err := s.DB().QueryRow(q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	mutes, total, err := s.ListMutes(ctx, compatNow, true, 100, 0)
	if err != nil || len(mutes) != rows(`SELECT count(*) FROM mutes`) || total != len(mutes) {
		t.Errorf("mutes after the upgrade from %04d: %d of %d, %v", v, len(mutes), total, err)
	}
	accepted, total, _, err := s.ListAcceptances(ctx, AcceptanceQuery{Now: compatNow, Ended: true, Limit: 100})
	if want := rows(`SELECT count(*) FROM mutes WHERE accepted`); err != nil || total != want || len(accepted) != want {
		t.Errorf("acceptances after the upgrade from %04d: %d of %d, want %d, %v", v, len(accepted), total, want, err)
	}
	acks, err := s.Acks(ctx)
	if err != nil || len(acks) != rows(`SELECT count(*) FROM acknowledgements`) {
		t.Errorf("acknowledgements after the upgrade from %04d: %d, %v", v, len(acks), err)
	}
	findings, err := s.Findings(ctx, compatNow, FindingQuery{})
	if err != nil || len(findings) != rows(`SELECT count(*) FROM findings`) {
		t.Errorf("findings after the upgrade from %04d: %d, %v", v, len(findings), err)
	}
	for _, f := range findings {
		if _, err := s.Finding(ctx, f.ID, compatNow); err != nil {
			t.Errorf("finding %s after the upgrade from %04d: %v", f.ID, v, err)
		}
	}
	if v >= 18 && len(findings) == 0 {
		t.Errorf("no findings read after the upgrade from %04d", v)
	}
}

// compatStoreWrites writes a collection and a mute to an upgraded store
// from version v: the epoch and mute numbers come after the fixture's.
func compatStoreWrites(t *testing.T, s *Store, v int) {
	t.Helper()
	ctx := context.Background()
	epoch, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, compatNow))
	if err != nil {
		t.Fatalf("write a collection after the upgrade: %v", err)
	}
	if epoch <= 3 {
		t.Errorf("new epoch %d reuses a fixture epoch", epoch)
	}
	m, err := s.CreateMute(ctx, Mute{CheckCode: "stream-005", EntityType: "stream", EntityKey: "SHOP/orders", Reason: "upgrade check",
		MutedBy: "ops@example.com", CreatedAt: compatNow, ExpiresAt: compatNow.Add(24 * time.Hour)})
	if err != nil {
		t.Fatalf("create a mute after the upgrade: %v", err)
	}
	if v >= 16 && m.ID <= 3 {
		t.Errorf("new mute %d reuses a fixture mute id", m.ID)
	}
}

// compatDiff lists the lines only one of two sorted descriptions has.
func compatDiff(got, want []string) string {
	var b strings.Builder
	for _, l := range got {
		if !slices.Contains(want, l) {
			b.WriteString("+ " + l + "\n")
		}
	}
	for _, l := range want {
		if !slices.Contains(got, l) {
			b.WriteString("- " + l + "\n")
		}
	}
	return b.String()
}
