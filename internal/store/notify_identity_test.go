package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

// Existing T2 databases get an identity exactly once, without losing pending
// messages. The identity contains only random bits, never deployment inputs.
func TestNotificationIdentityUpgrade(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "upgrade.duckdb")
	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatal(err)
	}
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range all {
		if m.version >= 36 {
			break
		}
		if m.version == 1 {
			if _, err := db.Exec(`CREATE TABLE migrations (version INTEGER PRIMARY KEY, name VARCHAR NOT NULL, applied_at TIMESTAMP DEFAULT current_timestamp)`); err != nil {
				t.Fatal(err)
			}
		}
		if err := applyMigration(ctx, db, m); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO notify_outbox (id,channel,created_at,epoch,kind,dedup_key,message) VALUES (nextval('notify_outbox_seq'),'ops',TIMESTAMP '2026-10-17 14:30:00',1,'baseline','baseline','{}'); CHECKPOINT`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.NotificationIncarnation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(id); err != nil {
		t.Fatal(err)
	}
	rows, err := s.OutboxRows(ctx, "ops")
	if err != nil || len(rows) != 1 || rows[0].State != OutboxPending {
		t.Fatalf("upgrade lost pending rows: %+v, %v", rows, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	again, err := s.NotificationIncarnation(ctx)
	if err != nil || id != again {
		t.Fatalf("identity changed on reopen: %q %q, %v", id, again, err)
	}
}
