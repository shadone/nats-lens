package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

type storedSubscription struct {
	server, account, subject string
	queue                    sql.NullString
	cid                      int64
	subs                     int
	msgs                     int64
}

func storedSubscriptions(t *testing.T, s *Store, epoch int64) []storedSubscription {
	t.Helper()
	rows, err := s.DB().Query(`SELECT s.server_name, account, subject, queue, cid, subs, msgs
FROM subscriptions JOIN servers s USING (server_id) WHERE epoch = ?
ORDER BY s.server_name, account, subject, queue NULLS FIRST, cid`, epoch)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []storedSubscription
	for rows.Next() {
		var r storedSubscription
		if err := rows.Scan(&r.server, &r.account, &r.subject, &r.queue, &r.cid, &r.subs, &r.msgs); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// A server's subscriptions are stored one row per (account, subject,
// queue, cid): duplicates of one connection fold into one row with their
// count and summed messages, a queue group is kept, no queue is NULL.
// Each server that answered has its list row with total and listed.
func TestSubscriptionsWritten(t *testing.T) {
	s := openMem(t)
	epoch, err := s.WriteSnapshot(context.Background(), fixtureSnapshot(t, t0))
	if err != nil {
		t.Fatal(err)
	}
	q := func(v string) sql.NullString { return sql.NullString{String: v, Valid: true} }
	want := []storedSubscription{
		{"nats-0", "APP", "$JS.API.>", sql.NullString{}, 2, 1, 0},
		{"nats-0", "APP", "_INBOX.k3J9.*", sql.NullString{}, 7, 1, 0},
		{"nats-0", "APP", "media.resize.>", q("media-worker"), 7, 2, 7},
		{"nats-0", "APP", "media.status", sql.NullString{}, 7, 1, 1},
		{"nats-1", "APP", "media.status", sql.NullString{}, 7, 1, 0},
	}
	got := storedSubscriptions(t, s, epoch)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("subscriptions\n got %v\nwant %v", got, want)
	}
	rows, err := s.DB().Query(`SELECT s.server_name, total, listed FROM subscription_lists JOIN servers s USING (server_id)
WHERE epoch = ? ORDER BY 1`, epoch)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lists []string
	for rows.Next() {
		var name string
		var total, listed int64
		if err := rows.Scan(&name, &total, &listed); err != nil {
			t.Fatal(err)
		}
		lists = append(lists, fmt.Sprintf("%s %d/%d", name, listed, total))
	}
	if fmt.Sprint(lists) != "[nats-0 5/7 nats-1 1/1]" {
		t.Errorf("subscription_lists = %v", lists)
	}
	// The cid joins the CONNZ row of the same epoch and server.
	if n := count(t, s.DB(), `SELECT count(*) FROM subscriptions s JOIN connections c USING (epoch, server_id, cid)
WHERE s.epoch = ? AND c.name = 'media-worker'`, epoch); n != 4 {
		t.Errorf("subscriptions joined to their connection = %d, want 4", n)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM epoch_endpoints WHERE epoch = ? AND endpoint = 'SUBSZ' AND state = 'ok'`, epoch); n != 2 {
		t.Errorf("SUBSZ endpoint rows = %d, want 2", n)
	}
}

// An epoch that did not collect subscription lists has no rows in either
// table, and an answered empty list has its list row only.
func TestSubscriptionsAbsentAndEmpty(t *testing.T) {
	s := openMem(t)
	snap := fixtureSnapshot(t, t0)
	for _, v := range snap.Servers {
		v.Subsz = nil
		delete(v.Status, "SUBSZ")
	}
	epoch, err := s.WriteSnapshot(context.Background(), snap)
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, s.DB(), `SELECT (SELECT count(*) FROM subscriptions) + (SELECT count(*) FROM subscription_lists)`); n != 0 {
		t.Errorf("epoch without SUBSZ stored %d subscription rows", n)
	}
	snap = fixtureSnapshot(t, t0.Add(time.Minute))
	for _, v := range snap.Servers {
		v.Subsz = &server.Subsz{ID: v.Info.ID, SublistStats: &server.SublistStats{}}
	}
	if epoch, err = s.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM subscription_lists WHERE epoch = ? AND total = 0 AND listed = 0`, epoch); n != 2 {
		t.Errorf("empty lists = %d rows, want 2", n)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM subscriptions WHERE epoch = ?`, epoch); n != 0 {
		t.Errorf("empty lists stored %d subscriptions", n)
	}
}

// A list of 10,000 subscriptions per server is written in batches.
func TestSubscriptionsLargeList(t *testing.T) {
	s := openMem(t)
	snap := fixtureSnapshot(t, t0)
	for _, v := range snap.Servers {
		subs := make([]server.SubDetail, 10000)
		for i := range subs {
			subs[i] = server.SubDetail{Account: "APP", Subject: fmt.Sprintf("orders.%05d.status", i), Queue: "order-service",
				Cid: uint64(100 + i%50), Msgs: int64(i)}
		}
		v.Subsz = &server.Subsz{ID: v.Info.ID, SublistStats: &server.SublistStats{}, Total: 12000, Subs: subs}
	}
	start := time.Now()
	epoch, err := s.WriteSnapshot(context.Background(), snap)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("epoch with 2 x 10,000 subscriptions written in %s", time.Since(start))
	if n := count(t, s.DB(), `SELECT count(*) FROM subscriptions WHERE epoch = ? AND queue = 'order-service'`, epoch); n != 20000 {
		t.Errorf("rows = %d, want 20000", n)
	}
	if n := count(t, s.DB(), `SELECT sum(msgs) FROM subscriptions WHERE epoch = ?`, epoch); n != 2*(9999*10000/2) {
		t.Errorf("msgs summed to %d", n)
	}
}

// Subscriptions and their list rows go with their epoch in a sweep.
func TestSubscriptionsSwept(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	for i := range 2 {
		if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0.Add(time.Duration(i)*24*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Sweep(ctx, t0.Add(24*time.Hour+time.Minute), time.Hour); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"subscriptions", "subscription_lists"} {
		if n := count(t, s.DB(), `SELECT count(*) FROM `+table+` WHERE epoch = 1`); n != 0 {
			t.Errorf("swept epoch keeps %d %s rows", n, table)
		}
		if n := count(t, s.DB(), `SELECT count(*) FROM `+table+` WHERE epoch = 2`); n == 0 {
			t.Errorf("kept epoch lost its %s rows", table)
		}
	}
}

// Migration 0032 applies to a database written by 0001 to 0031 that holds
// server_stats and connections rows; both tables start empty and take rows.
func TestMigrationsFrom31OnAPopulatedDatabase(t *testing.T) {
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
	if err := migrateWith(ctx, db, all[:31], 0); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO server_stats (epoch, server_id, subscriptions) VALUES (1, 'NSERVER0', 40)`,
		`INSERT INTO connections (epoch, server_id, cid, account) VALUES (1, 'NSERVER0', 7, 'APP')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := migrateWith(ctx, db, all, 31); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT (SELECT count(*) FROM subscriptions) + (SELECT count(*) FROM subscription_lists)`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("new tables hold %d rows, %v", n, err)
	}
	for _, q := range []string{
		`INSERT INTO subscriptions VALUES (1, 'NSERVER0', 'APP', 'orders.created', NULL, 7, 1, 0)`,
		`INSERT INTO subscription_lists VALUES (1, 'NSERVER0', 1, 1)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO subscription_lists VALUES (1, 'NSERVER0', 1, 1)`); err == nil {
		t.Error("a second list row for one server and epoch was accepted")
	}
}
