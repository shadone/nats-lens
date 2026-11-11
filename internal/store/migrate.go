package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"slices"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// migration is one numbered schema change, applied once.
type migration struct {
	version int
	name    string
	sql     string
	sha256  string
}

// loadMigrations reads the embedded files named NNNN_name.sql, in version order.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		base, ok := strings.CutSuffix(e.Name(), ".sql")
		if !ok {
			continue
		}
		num, name, ok := strings.Cut(base, "_")
		if !ok {
			return nil, fmt.Errorf("migration %s: name must be NNNN_name.sql", e.Name())
		}
		v, err := strconv.Atoi(num)
		if err != nil {
			return nil, fmt.Errorf("migration %s: %w", e.Name(), err)
		}
		b, err := migrationFiles.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		out = append(out, migration{version: v, name: name, sql: string(b), sha256: hex.EncodeToString(sum[:])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i := 1; i < len(out); i++ {
		if out[i].version == out[i-1].version {
			return nil, fmt.Errorf("duplicate migration version %d", out[i].version)
		}
	}
	return out, nil
}

// migrate applies every migration newer than the database's version, each
// in its own transaction together with its row in the migrations table.
func migrate(ctx context.Context, db *sql.DB) error {
	all, err := loadMigrations()
	if err != nil {
		return err
	}
	// A database a later binary wrote, or one with an edited migration, is
	// refused before the statements below rewrite its migrations table's
	// catalog.
	if err := refuseNewer(ctx, db, all); err != nil {
		return err
	}
	if _, err := checksumFixes(ctx, db, all); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS migrations (
    version     INTEGER PRIMARY KEY,
    name        VARCHAR NOT NULL,
    applied_at  TIMESTAMP NOT NULL DEFAULT current_timestamp
);
COMMENT ON TABLE migrations IS 'Schema versions applied to this database.';
COMMENT ON COLUMN migrations.version IS 'Migration number.';
COMMENT ON COLUMN migrations.name IS 'Migration file name without number and extension.';
COMMENT ON COLUMN migrations.applied_at IS 'UTC time the migration was applied.';`); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}
	var current int
	if err := db.QueryRowContext(ctx, `SELECT coalesce(max(version), 0) FROM migrations`).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	return migrateWith(ctx, db, all, current)
}

// refuseNewer returns an error when the database records a migration
// version above the newest of all. It only reads, and does nothing on a
// database without a migrations table.
func refuseNewer(ctx context.Context, db *sql.DB, all []migration) error {
	var tables int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM duckdb_tables()
WHERE schema_name = 'main' AND table_name = 'migrations'`).Scan(&tables); err != nil {
		return fmt.Errorf("inspect migrations table: %w", err)
	}
	if tables == 0 || len(all) == 0 {
		return nil
	}
	var current int
	if err := db.QueryRowContext(ctx, `SELECT coalesce(max(version), 0) FROM migrations`).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	return newerError(current, all[len(all)-1].version)
}

// newerError says that a database at version current was written by a
// later binary than one whose newest migration is newest, or returns nil.
func newerError(current, newest int) error {
	if current <= newest {
		return nil
	}
	return fmt.Errorf("database schema version %d is newer than this binary (%d): it was written by a later natslens; "+
		"run that release or a later one, or restore a backup taken before the upgrade", current, newest)
}

// migrateWith brings a database at version current up to the last of all.
func migrateWith(ctx context.Context, db *sql.DB, all []migration, current int) error {
	if n := len(all); n > 0 {
		if err := newerError(current, all[n-1].version); err != nil {
			return err
		}
	}
	// Check what is applied before applying more, so that nothing runs on
	// top of a schema that differs from its files.
	if err := verifyChecksums(ctx, db, all); err != nil {
		return err
	}
	for _, m := range all {
		if m.version <= current {
			continue
		}
		if err := applyMigration(ctx, db, m); err != nil {
			return err
		}
	}
	return verifyChecksums(ctx, db, all)
}

// acceptedEarlierChecksums lists, per migration version, checksums that a
// file had before an edit made ahead of the first public release; only its
// comments changed. A database that recorded one of them gets the current
// checksum instead of refusing to start. Never add to this list after the
// first release: change the schema with a new migration.
var acceptedEarlierChecksums = map[int][]string{
	// 0018_findings: a column comment's example names changed.
	18: {"8c3fdcfe0e5ae37dca68b8fc991976a3e77f8ef1407bb547a029b4d123faec53"},
}

// verifyChecksums fills missing checksums from the embedded files and fails
// when a recorded checksum differs from its file: an applied migration was
// edited, and the database may not have the schema the code expects. It
// writes nothing when it fails, and does nothing while the migrations table
// has no checksum column yet.
func verifyChecksums(ctx context.Context, db *sql.DB, all []migration) error {
	fixes, err := checksumFixes(ctx, db, all)
	if err != nil {
		return err
	}
	for _, f := range fixes {
		if _, err := db.ExecContext(ctx, `UPDATE migrations SET sha256 = ? WHERE version = ?`, f.m.sha256, f.m.version); err != nil {
			return fmt.Errorf("record checksum of migration %04d: %w", f.m.version, err)
		}
		if f.recorded != "" {
			slog.Info("migration checksum updated after an accepted edit", "migration", fmt.Sprintf("%04d_%s", f.m.version, f.m.name),
				"recorded", f.recorded, "file", f.m.sha256)
		}
	}
	return nil
}

// checksumFix is a recorded checksum to replace with its file's: a missing
// one (recorded empty) or one of acceptedEarlierChecksums.
type checksumFix struct {
	m        migration
	recorded string
}

// checksumFixes reads the recorded checksums and returns the ones to
// replace, or an error when a recorded migration has no file or was edited.
// It only reads.
func checksumFixes(ctx context.Context, db *sql.DB, all []migration) ([]checksumFix, error) {
	var hasColumn int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM duckdb_columns()
WHERE table_name = 'migrations' AND column_name = 'sha256'`).Scan(&hasColumn); err != nil {
		return nil, fmt.Errorf("inspect migrations table: %w", err)
	}
	if hasColumn == 0 {
		return nil, nil
	}
	byVersion := map[int]migration{}
	for _, m := range all {
		byVersion[m.version] = m
	}
	rows, err := db.QueryContext(ctx, `SELECT version, sha256 FROM migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("read checksums: %w", err)
	}
	type rec struct {
		version int
		sum     sql.NullString
	}
	var recs []rec
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.version, &r.sum); err != nil {
			rows.Close()
			return nil, err
		}
		recs = append(recs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var fixes []checksumFix
	for _, r := range recs {
		m, ok := byVersion[r.version]
		switch {
		case !ok:
			return nil, fmt.Errorf("migration %04d is applied but has no file in this binary", r.version)
		case !r.sum.Valid:
			fixes = append(fixes, checksumFix{m: m})
		case r.sum.String == m.sha256:
		case slices.Contains(acceptedEarlierChecksums[r.version], r.sum.String):
			fixes = append(fixes, checksumFix{m: m, recorded: r.sum.String})
		default:
			return nil, fmt.Errorf("migration %04d_%s was edited after it was applied (recorded sha256 %s, file %s): "+
				"never edit an applied migration, add a new one", m.version, m.name, r.sum.String, m.sha256)
		}
	}
	return fixes, nil
}

func applyMigration(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("migration %04d_%s: %w", m.version, m.name, err)
	}
	// The checksum goes in with the migration once the column exists (from
	// 0010 on, including 0010 itself), so that no applied row is left
	// without one.
	var hasColumn int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM duckdb_columns()
WHERE table_name = 'migrations' AND column_name = 'sha256'`).Scan(&hasColumn); err != nil {
		return fmt.Errorf("inspect migrations table: %w", err)
	}
	if hasColumn > 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO migrations (version, name, sha256) VALUES (?, ?, ?)`, m.version, m.name, m.sha256)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO migrations (version, name) VALUES (?, ?)`, m.version, m.name)
	}
	if err != nil {
		return fmt.Errorf("record migration %d: %w", m.version, err)
	}
	return tx.Commit()
}

// CheckSchema returns an error unless the database at db has exactly the
// migrations this binary has: for tools that open a database read-only
// and so cannot migrate it.
func CheckSchema(ctx context.Context, db *sql.DB) error {
	all, err := loadMigrations()
	if err != nil {
		return err
	}
	var current int
	if err := db.QueryRowContext(ctx, `SELECT coalesce(max(version), 0) FROM migrations`).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if err := newerError(current, all[len(all)-1].version); err != nil {
		return err
	}
	if want := all[len(all)-1].version; current != want {
		return fmt.Errorf("database schema version %d, this binary needs %d: run natslens on it once to migrate, or use a matching binary", current, want)
	}
	return nil
}
