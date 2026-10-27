package store

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

// Cases for the set-based configuration versions, first written in the
// review of the batched write.

// randomJSView builds one server's JSZ from a random draw: streams s0..s3,
// each maybe absent, maybe without configuration, with one of two
// configurations and one of three creation times; each with consumers
// c0..c1 drawn the same way; sometimes a stream listed twice.
func randomJSView(r *rand.Rand, id string, created []time.Time) *collect.ServerView {
	app := &server.AccountDetail{Name: "APP", Id: "AAPP"}
	other := &server.AccountDetail{Name: "OTHER", Id: "AOTHER"}
	for si := range 4 {
		if r.IntN(5) == 0 {
			continue
		}
		name := fmt.Sprintf("s%d", si)
		s := server.StreamDetail{Name: name, Created: created[r.IntN(len(created))]}
		if r.IntN(6) != 0 {
			s.Config = &server.StreamConfig{Storage: server.FileStorage, Name: name, Description: fmt.Sprintf("d%d", r.IntN(2)), Replicas: 3}
		}
		for ci := range 2 {
			if r.IntN(5) == 0 {
				continue
			}
			c := &server.ConsumerInfo{Stream: name, Name: fmt.Sprintf("c%d", ci), Created: created[r.IntN(len(created))]}
			if r.IntN(6) != 0 {
				c.Config = &server.ConsumerConfig{Durable: c.Name, MaxDeliver: r.IntN(2)}
			}
			s.Consumer = append(s.Consumer, c)
		}
		app.Streams = append(app.Streams, s)
		if r.IntN(8) == 0 {
			dup := s
			dup.Config = &server.StreamConfig{Storage: server.FileStorage, Name: name, Description: "dup", Replicas: 3}
			app.Streams = append(app.Streams, dup)
		}
		// The same names in a second account.
		if r.IntN(2) == 0 {
			other.Streams = append(other.Streams, s)
		}
	}
	return &collect.ServerView{Info: server.ServerInfo{ID: id, Name: "n-" + id},
		Jsz: &server.JSInfo{AccountDetails: []*server.AccountDetail{app, other}}}
}

type scheduledEpoch struct {
	epoch, prev int64
	views       []*collect.ServerView
}

func randomSchedule(seed uint64) []scheduledEpoch {
	r := rand.New(rand.NewPCG(seed, 7))
	ids := []string{"NA", "NB'x", "NC"}
	created := []time.Time{t0, t0.Add(1500 * time.Nanosecond), {}}
	var out []scheduledEpoch
	e, prev := int64(0), int64(0)
	for range 25 {
		e += 1 + int64(r.IntN(3)) // gaps
		var views []*collect.ServerView
		for _, id := range ids {
			if r.IntN(4) != 0 {
				views = append(views, randomJSView(r, id, created))
			}
		}
		// Servers in a random order within the epoch.
		r.Shuffle(len(views), func(i, j int) { views[i], views[j] = views[j], views[i] })
		// Two servers reporting one key with different configurations
		// arise from the draws; so does a server reporting nothing.
		out = append(out, scheduledEpoch{epoch: e, prev: prev, views: views})
		prev = e
	}
	return out
}

func writeSchedule(t *testing.T, path string, sched []scheduledEpoch, write func(*writer, *collect.ServerView)) {
	t.Helper()
	ctx := context.Background()
	s, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, ep := range sched {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		w := newWriter(ctx, tx, ep.epoch, collect.NewSnapshot(t0))
		w.prevEpoch = ep.prev
		w.warned = map[assetKey]bool{}
		w.log = slog.New(slog.DiscardHandler)
		for _, v := range ep.views {
			write(w, v)
		}
		w.closeStatements()
		if w.err != nil {
			_ = tx.Rollback()
			t.Fatalf("epoch %d: %v", ep.epoch, w.err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
}

func compareStores(t *testing.T, ref, got string, tables ...string) {
	t.Helper()
	s, err := OpenPath(context.Background(), got)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	db := s.DB()
	if _, err := db.Exec(`ATTACH ` + sqlString(ref) + ` AS ref (READ_ONLY)`); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		if n := count(t, db, `SELECT count(*) FROM `+table); n == 0 {
			t.Errorf("%s empty", table)
		}
		for _, q := range []string{
			`SELECT * FROM ref.` + table + ` EXCEPT ALL SELECT * FROM main.` + table,
			`SELECT * FROM main.` + table + ` EXCEPT ALL SELECT * FROM ref.` + table,
		} {
			if d := count(t, db, `SELECT count(*) FROM (`+q+`)`); d != 0 {
				t.Errorf("%s: %d rows differ: %s", table, d, q)
			}
		}
	}
}

// TestConfigRunsRandomMatchByRow writes random schedules both ways and
// compares the rows. It takes about half a minute, so -short skips it.
func TestConfigRunsRandomMatchByRow(t *testing.T) {
	if testing.Short() {
		t.Skip("long")
	}
	for seed := range uint64(12) {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			dir := t.TempDir()
			sched := randomSchedule(seed)
			ref, got := filepath.Join(dir, "ref.duckdb"), filepath.Join(dir, "got.duckdb")
			writeSchedule(t, ref, sched, (*writer).writeJetStreamByRow)
			writeSchedule(t, got, sched, (*writer).writeJetStream)
			compareStores(t, ref, got, "stream_config", "consumer_config", "stream_stats", "consumer_stats")
		})
	}
}

// TestFailedEpochDoesNotPoisonTheNext fails one WriteSnapshot in the
// configuration staging step, then writes the next epoch on the same
// store and requires its configuration rows.
func TestFailedEpochDoesNotPoisonTheNext(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	snap := func(maxCons int) *collect.Snapshot {
		sn := collect.NewSnapshot(t0)
		for _, id := range []string{"N1", "N2"} {
			sn.Servers[id] = &collect.ServerView{Info: server.ServerInfo{ID: id, Name: "n-" + id},
				Jsz: &server.JSInfo{AccountDetails: []*server.AccountDetail{{Id: "A", Streams: []server.StreamDetail{{
					Name: "s", Created: t0, Config: &server.StreamConfig{Storage: server.FileStorage, Name: "s", MaxConsumers: maxCons},
					Consumer: []*server.ConsumerInfo{{Stream: "s", Name: "c", Created: t0,
						Config: &server.ConsumerConfig{Durable: "c"}}},
				}}}}}}
		}
		sn.FinishedAt = t0.Add(time.Second)
		return sn
	}
	e1, err := s.WriteSnapshot(ctx, snap(1))
	if err != nil {
		t.Fatal(err)
	}
	// N1 goes in first (sorted) and fails inside its staging insert, with
	// the staging table created and not dropped.
	if _, err := s.WriteSnapshot(ctx, snap(math.MaxInt32+1)); err == nil || !strings.Contains(err.Error(), "stage_stream_config") {
		t.Fatalf("err = %v, want a staging error", err)
	}
	e3, err := s.WriteSnapshot(ctx, snap(1))
	if err != nil {
		t.Fatalf("epoch after the failed one: %v", err)
	}
	db := s.DB()
	if n := count(t, db, `SELECT count(*) FROM stream_config WHERE first_epoch = ? AND last_epoch = ?`, e1, e3); n != 2 {
		t.Errorf("stream runs %d-%d = %d, want 2 (extended over the failed epoch number)", e1, e3, n)
	}
	if n := count(t, db, `SELECT count(*) FROM consumer_config WHERE first_epoch = ? AND last_epoch = ?`, e1, e3); n != 2 {
		t.Errorf("consumer runs %d-%d = %d, want 2", e1, e3, n)
	}
	if n := count(t, db, `SELECT count(*) FROM duckdb_tables() WHERE table_name LIKE 'stage_%'`); n != 0 {
		t.Errorf("%d staging tables left visible", n)
	}
	// Every pooled connection: no staging table left in any of them.
	for range 4 {
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var n int
		if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM duckdb_tables() WHERE table_name LIKE 'stage_%'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
		if n != 0 {
			t.Errorf("connection holds %d staging tables", n)
		}
	}
}

// TestStagingNameClash: a real table named like the staging table
// in main would be shadowed by the temp one and left alone.
func TestStagingNameClash(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	if _, err := s.DB().Exec(`CREATE TABLE stage_stream_config (x INT); INSERT INTO stage_stream_config VALUES (42)`); err != nil {
		t.Fatal(err)
	}
	sn := collect.NewSnapshot(t0)
	sn.Servers["N1"] = &collect.ServerView{Info: server.ServerInfo{ID: "N1", Name: "n1"},
		Jsz: &server.JSInfo{AccountDetails: []*server.AccountDetail{{Id: "A", Streams: []server.StreamDetail{{
			Name: "s", Created: t0, Config: &server.StreamConfig{Name: "s"}}}}}}}
	sn.FinishedAt = t0.Add(time.Second)
	if _, err := s.WriteSnapshot(ctx, sn); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM main.stage_stream_config WHERE x = 42`); n != 1 {
		t.Errorf("main.stage_stream_config rows = %d, want 1", n)
	}
}

// TestOddServerIDsMatchByRow writes server IDs with a backslash, unicode and a
// NUL both ways.
func TestOddServerIDsMatchByRow(t *testing.T) {
	cfg := &server.StreamConfig{Storage: server.FileStorage, Name: "s"}
	var sched []scheduledEpoch
	for e := int64(1); e <= 3; e++ {
		var views []*collect.ServerView
		for _, id := range []string{`N\x'`, "Nü€", "N';DROP TABLE stream_config;--"} {
			views = append(views, &collect.ServerView{Info: server.ServerInfo{ID: id, Name: "n"},
				Jsz: &server.JSInfo{AccountDetails: []*server.AccountDetail{{Id: "A", Streams: []server.StreamDetail{{Name: "s", Created: t0, Config: cfg}}}}}})
		}
		sched = append(sched, scheduledEpoch{e, e - 1, views})
	}
	dir := t.TempDir()
	ref, got := filepath.Join(dir, "ref.duckdb"), filepath.Join(dir, "got.duckdb")
	writeSchedule(t, ref, sched, (*writer).writeJetStreamByRow)
	writeSchedule(t, got, sched, (*writer).writeJetStream)
	compareStores(t, ref, got, "stream_config", "stream_stats")
}

// TestNULStreamNameMatchByRow: a NUL inside a name goes through the JSON batch.
func TestNULStreamNameMatchByRow(t *testing.T) {
	cfg := &server.StreamConfig{Storage: server.FileStorage, Name: "s\x00t"}
	v := &collect.ServerView{Info: server.ServerInfo{ID: "N1", Name: "n"},
		Jsz: &server.JSInfo{AccountDetails: []*server.AccountDetail{{Id: "A", Streams: []server.StreamDetail{{Name: "s\x00t", Created: t0, Config: cfg}}}}}}
	sched := []scheduledEpoch{{1, 0, []*collect.ServerView{v}}, {2, 1, []*collect.ServerView{v}}}
	dir := t.TempDir()
	ref, got := filepath.Join(dir, "ref.duckdb"), filepath.Join(dir, "got.duckdb")
	writeSchedule(t, ref, sched, (*writer).writeJetStreamByRow)
	writeSchedule(t, got, sched, (*writer).writeJetStream)
	compareStores(t, ref, got, "stream_config", "stream_stats")
}
