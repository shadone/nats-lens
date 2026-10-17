package store

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

func streamRows(t *testing.T, s *Store, stream string) (stats, config int) {
	t.Helper()
	stats = count(t, s.DB(), `SELECT count(*) FROM stream_stats WHERE server_id = 'NFAKESERVER1ID' AND stream = ?`, stream)
	config = count(t, s.DB(), `SELECT count(*) FROM stream_config WHERE server_id = 'NFAKESERVER1ID' AND stream = ?`, stream)
	return stats, config
}

// One server listing the same stream twice in one epoch must not fail the
// epoch, and the stream is stored once.
func TestDuplicateStreamSameEpoch(t *testing.T) {
	s := openMem(t)
	snap := fixtureSnapshot(t, t0)
	var name string
	for _, a := range snap.Servers["NFAKESERVER1ID"].Jsz.AccountDetails {
		if len(a.Streams) > 0 {
			a.Streams = append(a.Streams, a.Streams[0])
			name = a.Streams[0].Name
			break
		}
	}
	if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatalf("write: %v", err)
	}
	if st, cf := streamRows(t, s, name); st != 1 || cf != 1 {
		t.Fatalf("%s rows: stats %d, config %d; want 1 and 1", name, st, cf)
	}
}

// One server listing the same account twice repeats every stream and consumer.
func TestDuplicateAccountSameEpoch(t *testing.T) {
	s := openMem(t)
	snap := fixtureSnapshot(t, t0)
	j := snap.Servers["NFAKESERVER1ID"].Jsz
	j.AccountDetails = append(j.AccountDetails, j.AccountDetails...)
	if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatalf("write: %v", err)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM consumer_stats WHERE server_id = 'NFAKESERVER1ID' AND consumer = 'resize-worker'`); n != 1 {
		t.Fatalf("resize-worker rows = %d", n)
	}
	if n := count(t, s.DB(), `SELECT len(errors) FROM epochs`); n != 0 {
		t.Fatalf("an identical repeat is not an error, got %d", n)
	}
}

// A repeat with different configuration keeps the first and is noted once.
func TestConflictingDuplicateKeepsFirst(t *testing.T) {
	s := openMem(t)
	snap := fixtureSnapshot(t, t0)
	for _, a := range snap.Servers["NFAKESERVER1ID"].Jsz.AccountDetails {
		for i := range a.Streams {
			if a.Streams[i].Name != "catalog-changes" {
				continue
			}
			dup := a.Streams[i]
			cfg := *dup.Config
			cfg.MaxBytes = 1 << 30
			dup.Config = &cfg
			a.Streams = append(a.Streams, dup, dup)
			break
		}
	}
	if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatalf("write: %v", err)
	}
	var maxBytes int64
	if err := s.DB().QueryRow(`SELECT max_bytes FROM stream_config WHERE server_id = 'NFAKESERVER1ID' AND stream = 'catalog-changes'`).Scan(&maxBytes); err != nil {
		t.Fatal(err)
	}
	if maxBytes == 1<<30 {
		t.Fatal("the later, conflicting report replaced the first")
	}
	if n := count(t, s.DB(), `SELECT len(errors) FROM epochs`); n != 1 {
		t.Fatalf("epoch errors = %d, want one note", n)
	}
}

// A database at version 9, before checksums, upgrades and gets every
// checksum filled.
func TestUpgradeFromVersion9(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.duckdb")
	s, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
	// Undo migrations 10 and later by hand.
	for _, q := range []string{
		`DROP VIEW consumer_leader_stats`,
		`DROP INDEX epoch_endpoints_key`, `DROP INDEX server_stats_key`, `DROP INDEX health_key`,
		`DROP INDEX stream_config_key`, `DROP INDEX consumer_config_key`,
		`ALTER TABLE archive_hours DROP COLUMN epochs`, `ALTER TABLE archive_hours DROP COLUMN events`,
		`ALTER TABLE archive_hours DROP COLUMN final`, `ALTER TABLE archive_hours DROP COLUMN table_rows`,
		`DROP TABLE findings`, `DROP TABLE finding_transitions`, `DROP TABLE check_runs`, `DROP TABLE inventory`,
		`DROP TABLE mutes`, `DROP SEQUENCE mute_seq`, `ALTER TABLE archive_hours DROP COLUMN mute_changes`,
		`ALTER TABLE epochs DROP COLUMN loopback_tooling`,
		`ALTER TABLE server_stats DROP COLUMN max_payload`, `ALTER TABLE server_stats DROP COLUMN max_pending`,
		`ALTER TABLE server_stats DROP COLUMN write_deadline_ns`, `ALTER TABLE server_stats DROP COLUMN ping_interval_ns`,
		`ALTER TABLE server_stats DROP COLUMN max_pings_out`,
		`DELETE FROM migrations WHERE version >= 10`,
		`CREATE TABLE m2 AS SELECT version, name, applied_at FROM migrations`,
		`DROP TABLE migrations`,
		`CREATE TABLE migrations (version INTEGER PRIMARY KEY, name VARCHAR NOT NULL, applied_at TIMESTAMP NOT NULL DEFAULT current_timestamp)`,
		`INSERT INTO migrations SELECT * FROM m2`,
		`DROP TABLE m2`,
	} {
		if _, err := s.DB().Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	s.Close()
	s, err = OpenPath(ctx, path)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	defer s.Close()
	if n := count(t, s.DB(), `SELECT count(*) FROM migrations WHERE sha256 IS NULL`); n != 0 {
		t.Fatalf("%d migrations without checksum", n)
	}
}

// Assets whose names only look alike when joined into a string are distinct.
func TestDistinctAssetsWithSeparatorInNames(t *testing.T) {
	s := openMem(t)
	snap := fixtureSnapshot(t, t0)
	j := snap.Servers["NFAKESERVER1ID"].Jsz
	a := *j.AccountDetails[0]
	a.Id, a.Name = "A", "A"
	a.Streams = []server.StreamDetail{{Name: "s > x", Created: t0}}
	b := *j.AccountDetails[0]
	b.Id, b.Name = "A > s", "A > s"
	b.Streams = []server.StreamDetail{{Name: "x", Created: t0}}
	j.AccountDetails = append(j.AccountDetails, &a, &b)
	if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM stream_stats WHERE account IN ('A', 'A > s')`); n != 2 {
		t.Fatalf("stream rows = %d, want 2", n)
	}
}

// A conflicting duplicate that recurs every epoch is logged once per
// process, and still noted in each epoch's errors.
func TestConflictingDuplicateLoggedOnce(t *testing.T) {
	s := openMem(t)
	var buf bytes.Buffer
	s.SetLogger(slog.New(slog.NewTextHandler(&buf, nil)))
	for i := range 3 {
		snap := fixtureSnapshot(t, t0.Add(time.Duration(i)*time.Minute))
		for _, a := range snap.Servers["NFAKESERVER1ID"].Jsz.AccountDetails {
			for k := range a.Streams {
				if a.Streams[k].Name != "catalog-changes" {
					continue
				}
				dup := a.Streams[k]
				cfg := *dup.Config
				cfg.MaxBytes = 1 << 30
				dup.Config = &cfg
				a.Streams = append(a.Streams, dup)
				break
			}
		}
		if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
			t.Fatal(err)
		}
	}
	if n := strings.Count(buf.String(), "conflicting duplicate"); n != 1 {
		t.Fatalf("warnings = %d, want 1:\n%s", n, buf.String())
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM epochs WHERE len(errors) = 1`); n != 3 {
		t.Fatalf("epochs with the note = %d, want 3", n)
	}
}
