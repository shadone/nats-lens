package checks

import (
	"fmt"
	"testing"
	"time"
)

func degradedFixture(t *testing.T, n int64) *fx {
	t.Helper()
	f := newFx(t)
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
