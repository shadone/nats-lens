package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

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
	addEpoch(t, &Store{db: db}, 1, t0)
	if _, err := db.Exec(`INSERT INTO notify_outbox (id,channel,created_at,epoch,kind,dedup_key,message,state,delivered_at)
 VALUES (nextval('notify_outbox_seq'),'ops',?,1,'baseline','delivered','{}','delivered',?)`, t0, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CHECKPOINT`); err != nil {
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
	if err != nil || len(rows) != 2 || rows[0].State != OutboxPending {
		t.Fatalf("upgrade lost pending rows: %+v, %v", rows, err)
	}
	finished, err := s.LastDeliveryFinished(ctx, "ops")
	if err != nil || !finished.Equal(t0.Add(time.Minute)) {
		t.Fatalf("upgrade lost terminal rate: %s %v", finished, err)
	}
	sent, err := s.NotificationEpochSent(ctx, "ops")
	if err != nil || !sent {
		t.Fatalf("upgrade lost successful epoch: %t %v", sent, err)
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

func TestNotificationRateSurvivesReopenAndSweep(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "rate.duckdb")
	s, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	addEpoch(t, s, 1, t0)
	if err := s.ApplyNotify(ctx, NotifyChange{Outbox: []OutboxRow{{Channel: "ops", CreatedAt: t0, Epoch: 1, Kind: "baseline", DedupKey: "baseline", Message: []byte(`{}`)}}}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.OutboxRows(ctx, "ops")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishDelivery(ctx, "ops", rows[0].ID, rows[0].ID, OutboxDelivered, 204, t0, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SweepNotifications(ctx, t0.Add(8*24*time.Hour), 24*time.Hour, []string{"ops"}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM notify_outbox`); n != 0 {
		t.Fatal("outcome not swept", n)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	at, err := s.LastDeliveryFinished(ctx, "ops")
	if err != nil || !at.Equal(t0) {
		t.Fatalf("lost interval watermark: %s %v", at, err)
	}
	sent, err := s.NotificationEpochSent(ctx, "ops")
	if err != nil || !sent {
		t.Fatalf("lost epoch watermark: %t %v", sent, err)
	}
	addEpoch(t, s, 2, t0.Add(8*24*time.Hour))
	sent, err = s.NotificationEpochSent(ctx, "ops")
	if err != nil || sent {
		t.Fatalf("new epoch not released: %t %v", sent, err)
	}
	if _, err := s.SweepNotifications(ctx, t0.Add(9*24*time.Hour), 24*time.Hour, nil); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM notify_delivery_rate`); n != 0 {
		t.Fatal("removed channel kept watermark", n)
	}
}
