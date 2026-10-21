package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// Migrations 0022 and 0023 apply to a database written by 0001 to 0021
// that holds a finding; the new column is NULL for it.
func TestMigrationsFrom21OnAPopulatedDatabase(t *testing.T) {
	ctx := context.Background()
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("duckdb", filepath.Join(t.TempDir(), "db.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE migrations (version INTEGER PRIMARY KEY, name VARCHAR NOT NULL, applied_at TIMESTAMP NOT NULL DEFAULT current_timestamp)`); err != nil {
		t.Fatal(err)
	}
	if err := migrateWith(ctx, db, all[:21], 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO findings (id, check_code, entity_type, entity_key, severity, state, last_transition, since, since_epoch, transitioned_at, last_seen_epoch, updated_epoch, detail, transitions_last_hour, flapping)
VALUES ('a','c','e','k','warning','firing','firing', now()::TIMESTAMP, 1, now()::TIMESTAMP, 1, 1, 'd', 0, false)`); err != nil {
		t.Fatal(err)
	}
	if err := migrateWith(ctx, db, all, 21); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM findings WHERE condition_since IS NULL`).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}

// Migrations 0027 and 0028 apply to a database written by 0001 to 0026
// that holds server_stats and connections rows; the new columns are NULL
// for them and account_details starts empty.
func TestMigrationsFrom26OnAPopulatedDatabase(t *testing.T) {
	ctx := context.Background()
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("duckdb", filepath.Join(t.TempDir(), "db.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE migrations (version INTEGER PRIMARY KEY, name VARCHAR NOT NULL, applied_at TIMESTAMP NOT NULL DEFAULT current_timestamp)`); err != nil {
		t.Fatal(err)
	}
	if err := migrateWith(ctx, db, all[:26], 0); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO server_stats (epoch, server_id, max_payload) VALUES (1, 'NSERVER0', 1048576)`,
		`INSERT INTO connections (epoch, server_id, cid, account) VALUES (1, 'NSERVER0', 7, 'APP')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := migrateWith(ctx, db, all, 26); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM server_stats WHERE max_payload = 1048576 AND tls_cert_not_after IS NULL
  AND mqtt_tls_cert_not_after IS NULL`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("server_stats rows kept with NULL certificates = %d, %v", n, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM connections WHERE cid = 7 AND user_jwt IS NULL AND jwt_expires IS NULL`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("connections rows kept with NULL user JWT = %d, %v", n, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM account_details`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("account_details = %d, %v", n, err)
	}
}

// Migration 0031 applies to a database written by 0001 to 0030 that holds
// mutes, one of them lifted; they stay plain mutes, read as such.
func TestMigrationsFrom30OnMutes(t *testing.T) {
	ctx := context.Background()
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("duckdb", filepath.Join(t.TempDir(), "db.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE migrations (version INTEGER PRIMARY KEY, name VARCHAR NOT NULL, applied_at TIMESTAMP NOT NULL DEFAULT current_timestamp)`); err != nil {
		t.Fatal(err)
	}
	if err := migrateWith(ctx, db, all[:30], 0); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO mutes VALUES (1, 'stream-025', NULL, NULL, 'known', 'op', TIMESTAMP '2026-10-16 22:00:00', TIMESTAMP '2026-10-18 22:00:00', NULL, NULL)`,
		`INSERT INTO mutes VALUES (2, 'stream-005', 'stream', 'SHOP/order-archive', 'in review', 'op', TIMESTAMP '2026-10-16 22:00:00', TIMESTAMP '2026-10-18 22:00:00', TIMESTAMP '2026-10-17 01:00:00', 'lead')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := migrateWith(ctx, db, all, 30); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT ` + muteCols + ` FROM mutes ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ms []Mute
	for rows.Next() {
		m, err := scanMute(rows)
		if err != nil {
			t.Fatal(err)
		}
		ms = append(ms, m)
	}
	if len(ms) != 2 || ms[1].UnmutedBy != "lead" || ms[1].EntityKey != "SHOP/order-archive" {
		t.Fatalf("mutes after 0031 = %+v", ms)
	}
	for _, m := range ms {
		if m.Accepted || m.GrowthPerDay != nil || m.EndReason != "" {
			t.Errorf("mute %d after 0031 = %+v, want a plain mute", m.ID, m)
		}
	}
}
