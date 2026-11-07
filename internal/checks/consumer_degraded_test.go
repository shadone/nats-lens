package checks

import (
	"context"
	"fmt"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
	"path/filepath"
	"testing"
	"time"
)

func degradedFixture(t *testing.T, n int64, path ...string) *fx {
	t.Helper()
	var f *fx
	if len(path) == 0 {
		f = newFx(t)
	} else {
		st, err := store.OpenPath(context.Background(), path[0])
		if err != nil {
			t.Fatal(err)
		}
		f = &fx{t: t, st: st}
		t.Cleanup(func() { _ = f.st.Close() })
	}
	f.cluster(n)
	f.streamConfig("S0", "jobs", 1, n, nil)
	for _, c := range []string{"bad", "good"} {
		f.consumerConfig("S0", "jobs", c, 1, n, nil)
		for e := int64(1); e <= n; e++ {
			for i := 0; i < 3; i++ {
				f.consumerStats(e, fmt.Sprint("S", i), "jobs", c, m{"leader": "nats-0", "is_leader": i == 0, "leader_since": t0.Add(-time.Hour), "replicas_reported": 2, "replicas_offline": 0})
			}
			f.consumerReplica(e, "S0", "jobs", c, "nats-1", m{"pending": false})
			f.consumerReplica(e, "S0", "jobs", c, "nats-2", m{"current": c == "good", "lag": map[bool]int{true: 0, false: 10}[c == "good"], "pending": false})
		}
	}
	return f
}

func TestConsumerReplicaDegraded(t *testing.T) {
	f := degradedFixture(t, 4)
	f.fires("consumer-018", 2)
	r := f.fires("consumer-018", 3, "APP/jobs/bad")
	want(t, r["APP/jobs/bad"], "warning", "nats-2", "10 Raft operations", "3 collections", "quorum")
	f.exec(`UPDATE consumer_replicas SET current=true, lag=0 WHERE epoch=4`)
	f.fires("consumer-018", 4)
}

func TestConsumerReplicaDegradedBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, sql string
		fire      bool
	}{
		{"offline", `UPDATE consumer_replicas SET offline=true WHERE consumer='bad' AND peer_name='nats-2'`, true},
		{"noncurrent", `UPDATE consumer_replicas SET lag=0 WHERE consumer='bad'`, true},
		{"behind", `UPDATE consumer_replicas SET current=true WHERE consumer='bad'`, true},
		{"quorum lost", `UPDATE consumer_replicas SET offline=true WHERE consumer='bad'`, false},
		{"unknown", `UPDATE consumer_replicas SET current=NULL WHERE consumer='bad'`, false},
		{"missing peers", `DELETE FROM consumer_replicas WHERE consumer='bad' AND peer_name='nats-2'`, false},
		{"missing leader", `DELETE FROM consumer_stats WHERE consumer='bad' AND is_leader`, false},
		{"election", `UPDATE consumer_stats SET leader='nats-1' WHERE consumer='bad' AND epoch=2`, false},
		{"recent election", `UPDATE consumer_stats SET leader_since=TIMESTAMP '2026-10-17 09:00:00' WHERE consumer='bad'`, false},
		{"incarnation", `UPDATE consumer_stats SET created=TIMESTAMP '2026-10-17 09:00:00' WHERE consumer='bad' AND epoch=2`, false},
		{"epoch gap", `DELETE FROM epochs WHERE epoch=2`, false},
		{"capture gap", `UPDATE epochs SET started_at=started_at+INTERVAL 10 MINUTE WHERE epoch=3`, false},
		{"paused", `UPDATE consumer_stats SET paused=true WHERE consumer='bad'`, true},
		{"pending", `UPDATE consumer_replicas SET pending=true WHERE consumer='bad'`, false},
		{"single", `UPDATE consumer_config SET replicas=1 WHERE consumer='bad'`, false},
		{"no quorum reports", `DELETE FROM consumer_stats WHERE consumer='bad' AND server_id<>'S0'`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := degradedFixture(t, 3)
			f.exec(tc.sql)
			if tc.fire {
				f.fires("consumer-018", 3, "APP/jobs/bad")
			} else {
				f.fires("consumer-018", 3)
			}
		})
	}
}

func TestConsumerReplicaDegradedHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "replicas.duckdb")
	f := degradedFixture(t, 9, path)
	set, err := Load(config.Checks{Timeout: config.Duration(10 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	eng := &Engine{Set: set, only: map[string]bool{"consumer-018": true}, Interval: 30 * time.Second}
	run := func(epoch int64) {
		t.Helper()
		if _, err := eng.Run(context.Background(), f.st, epoch); err != nil {
			t.Fatal(err)
		}
	}
	// No earlier check runs: persisted observations, not evaluations, establish the warning.
	run(3)
	f.exec(`DELETE FROM consumer_stats WHERE epoch IN (4,5) AND consumer='bad'`)
	eng.Budget = time.Nanosecond
	run(4)
	eng.Budget = 0
	// A failing/skipped run leaves findings untouched. An unknown successful run holds.
	run(5)
	if err := f.st.Close(); err != nil {
		t.Fatal(err)
	}
	f.st, err = store.OpenPath(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	r := f.fires("consumer-018", 6, "APP/jobs/bad")["APP/jobs/bad"]
	want(t, r, "warning", "health is unknown")
	if r.Value != nil {
		t.Fatal("unknown health has a numeric value")
	}
	// Recovery needs one complete healthy observation, even after skipped checks.
	f.exec(`UPDATE consumer_replicas SET current=true,lag=0 WHERE epoch=7`)
	run(7)
	f.fires("consumer-018", 8)
	f.fires("consumer-018", 9)
	// A historical read does not borrow future finding state.
	f.fires("consumer-018", 2)
}

func TestConsumerReplicaDegradedQuorumSizes(t *testing.T) {
	for _, r := range []int{2, 3, 5} {
		for off := 0; off < r; off++ {
			t.Run(fmt.Sprintf("R%d-offline%d", r, off), func(t *testing.T) {
				f := degradedFixture(t, 3)
				f.exec(`DELETE FROM consumer_replicas WHERE consumer='bad'`)
				f.exec(`DELETE FROM consumer_stats WHERE consumer='bad' AND server_id<>'S0'`)
				f.exec(`UPDATE consumer_config SET replicas=? WHERE consumer='bad'`, r)
				for e := int64(1); e <= 3; e++ {
					for i := 1; i < r; i++ {
						id, name := fmt.Sprint("S", i), fmt.Sprint("nats-", i)
						if i >= 3 && e == 1 {
							f.server(id, name, 1, 3, t0.Add(-time.Hour), "2.15.0")
						}
						f.consumerStats(e, id, "jobs", "bad", m{"leader": "nats-0"})
						f.consumerReplica(e, "S0", "jobs", "bad", name, m{"offline": i <= off, "current": i > off, "pending": false})
					}
				}
				if off > 0 && r-off >= r/2+1 {
					f.fires("consumer-018", 3, "APP/jobs/bad")
				} else {
					f.fires("consumer-018", 3)
				}
			})
		}
	}
}

func TestConsumerReplicaDegradedRestartAndConfig(t *testing.T) {
	for _, q := range []string{
		`UPDATE servers SET start_time=TIMESTAMP '2026-10-17 09:00:00' WHERE server_id='S2'`,
		`UPDATE consumer_config SET replicas=3 WHERE consumer='bad'; INSERT INTO consumer_config SELECT * REPLACE ('S1' AS server_id, 1 AS replicas) FROM consumer_config WHERE consumer='bad'`,
		`UPDATE consumer_stats SET created=NULL WHERE consumer='bad'`,
		`UPDATE consumer_stats SET leader_since=NULL WHERE consumer='bad'`,
	} {
		f := degradedFixture(t, 3)
		f.exec(q)
		f.fires("consumer-018", 3)
	}
}

func TestConsumerReplicaDegradedScale(t *testing.T) {
	f := degradedFixture(t, 3)
	// 10,000 consumers, 30,000 observations and 60,000 peer facts in the bounded window;
	// 100 old epochs add 1,000,000 unrelated observations that must not widen the window.
	f.exec(`INSERT INTO consumer_config SELECT consumer_config.* REPLACE ('c'||i AS consumer) FROM consumer_config, range(10000) t(i) WHERE consumer='bad'`)
	f.exec(`INSERT INTO consumer_stats SELECT consumer_stats.* REPLACE ('c'||i AS consumer) FROM consumer_stats, range(10000) t(i) WHERE consumer='bad' AND is_leader`)
	f.exec(`INSERT INTO consumer_stats SELECT consumer_stats.* REPLACE (epoch-100 AS epoch, 'old'||i AS consumer) FROM consumer_stats, range(100) t(i) WHERE consumer LIKE 'c%'`)
	f.exec(`INSERT INTO consumer_replicas SELECT consumer_replicas.* REPLACE ('c'||i AS consumer) FROM consumer_replicas, range(10000) t(i) WHERE consumer='bad'`)
	start := time.Now()
	got := f.eval("consumer-018", 3)
	// Only the original bad consumer has corroborating reports; bulk leaders cannot self-certify quorum.
	if len(got) != 1 {
		t.Fatalf("got %d findings", len(got))
	}
	elapsed := time.Since(start)
	t.Logf("10,000 consumers plus 3 million old reports: %s", elapsed)
	if elapsed > 4*time.Second {
		t.Fatalf("batch query took %s; budget 4s", elapsed)
	}
}
