package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	collection "github.com/shadone/nats-lens/internal/collect"
)

// A source-shaped RAFTZ group: the peer map excludes the reporting node.
func (f *fx) raftDiagnostic(e int64, id, group string, over m) {
	f.row("raft_stats", merge(m{"epoch": e, "server_id": id, "group_id": group, "raft_account": "SYS", "state": "FOLLOWER", "size": 3, "quorum_needed": 2, "observer": false, "paused": false, "overrun": false, "overrun_count": 0, "catching_up": false, "leader": "peer1", "ever_had_leader": true, "term": 4, "pindex": 2000, "committed": 2000, "applied": 10, "peers_total": 2, "peers_known": 2, "peers": `{"peer1":{"name":"nats-1","known":true},"peer2":{"name":"nats-2","known":true}}`}, over))
}
func raftDiagnosticFixture(t *testing.T, candidate bool) *fx {
	f := newFx(t)
	f.cluster(7)
	for e := int64(1); e <= 7; e++ {
		for _, id := range []string{"S0", "S1"} {
			f.row("epoch_endpoints", m{"epoch": e, "server_id": id, "endpoint": "RAFTZ", "state": "ok"})
			row := m{}
			if candidate {
				row = m{"state": "CANDIDATE", "leader": "", "term": e + 4}
			}
			if id == "S1" {
				row = m{"applied": 2000}
			}
			f.raftDiagnostic(e, id, "_meta_", row)
		}
	}
	return f
}
func TestRaftDiagnosticsSustained(t *testing.T) {
	for _, tc := range []struct {
		code      string
		candidate bool
	}{{"server-027", false}, {"server-028", true}} {
		t.Run(tc.code, func(t *testing.T) {
			f := raftDiagnosticFixture(t, tc.candidate)
			f.fires(tc.code, 4)
			r := f.fires(tc.code, 5, "nats-0")["nats-0"]
			want(t, r, "warning", "_meta_")
			f.fires(tc.code, 7, "nats-0")
		})
	}
}
func TestRaftDiagnosticsBoundaries(t *testing.T) {
	for _, tc := range []struct {
		code      string
		candidate bool
	}{{"server-027", false}, {"server-028", true}} {
		t.Run(tc.code, func(t *testing.T) {
			for _, q := range []string{
				`DELETE FROM raft_stats WHERE epoch=4`,
				`UPDATE epoch_endpoints SET state='missing' WHERE epoch=4 AND endpoint='RAFTZ'`,
				`UPDATE raft_stats SET paused=true WHERE epoch=4`,
				`UPDATE raft_stats SET catching_up=true WHERE epoch=4`,
				`UPDATE raft_stats SET observer=true WHERE epoch=4`,
				`UPDATE raft_stats SET overrun_count=1 WHERE epoch>=4`,
				`UPDATE raft_stats SET term=0 WHERE epoch=4`,
				`UPDATE raft_stats SET peers=NULL WHERE epoch=4`,
				`UPDATE raft_stats SET peers_known=1 WHERE epoch=4`,
				`UPDATE raft_stats SET raft_account='OTHER' WHERE epoch>=4`,
				`UPDATE raft_stats SET server_id='NEW' WHERE epoch>=4`,
				`UPDATE epochs SET started_at=started_at+INTERVAL 1 HOUR WHERE epoch>=4`,
			} {
				t.Run(q, func(t *testing.T) { f := raftDiagnosticFixture(t, tc.candidate); f.exec(q); f.fires(tc.code, 7) })
			}
		})
	}
}
func TestRaftApplyProgressAndUnknown(t *testing.T) {
	for _, q := range []string{
		`UPDATE raft_stats SET applied=20 WHERE epoch>=4`,
		`UPDATE raft_stats SET committed=100 WHERE epoch=4`,
		`UPDATE raft_stats SET applied=NULL WHERE epoch=4`,
		`UPDATE raft_stats SET committed=applied+999`,
		`UPDATE raft_stats SET wal_error='{}' WHERE epoch=7`,
	} {
		t.Run(q, func(t *testing.T) { f := raftDiagnosticFixture(t, false); f.exec(q); f.fires("server-027", 7) })
	}
}
func TestRaftCandidateRecovery(t *testing.T) {
	for _, q := range []string{`UPDATE raft_stats SET state='FOLLOWER' WHERE epoch=7`, `UPDATE raft_stats SET ever_had_leader=false`, `UPDATE raft_stats SET leader='peer1' WHERE epoch=7`} {
		t.Run(q, func(t *testing.T) { f := raftDiagnosticFixture(t, true); f.exec(q); f.fires("server-028", 7) })
	}
}

func TestRaftAssetIdentityAndAccountCoverage(t *testing.T) {
	for _, kind := range []string{"stream", "consumer"} {
		t.Run(kind, func(t *testing.T) {
			f := raftDiagnosticFixture(t, false)
			f.exec(`UPDATE raft_stats SET group_id='S-orders'`)
			for e := int64(1); e <= 7; e++ {
				if kind == "stream" {
					f.streamStats(e, "S0", "orders", m{"raft_group": "S-orders", "created": t0})
				} else {
					f.consumerStats(e, "S0", "orders", "worker", m{"raft_group": "S-orders", "created": t0})
				}
			}
			f.fires("server-027", 7, "nats-0")
			f.exec(`UPDATE ` + kind + `_stats SET created=created+INTERVAL 1 SECOND WHERE epoch>=4`)
			f.fires("server-027", 7)
		})
	}
	for _, q := range []string{
		`UPDATE raft_stats SET group_id='unmapped'`,
		`UPDATE servers SET start_time=(SELECT started_at FROM epochs WHERE epoch=1)`,
		`UPDATE raft_stats SET peers='{"new1":{"known":true},"new2":{"known":true}}' WHERE epoch>=4`,
		`INSERT INTO epoch_endpoints (epoch,server_id,endpoint,state) SELECT epoch,server_id,'RAFTZ:SYS','missing' FROM raft_stats WHERE epoch=4`,
		`INSERT INTO raft_stats SELECT * FROM raft_stats WHERE epoch=4`,
	} {
		t.Run(q, func(t *testing.T) { f := raftDiagnosticFixture(t, false); f.exec(q); f.fires("server-027", 7) })
	}
}

func TestRaftDiagnosticsScale(t *testing.T) {
	f := raftDiagnosticFixture(t, false)
	f.exec(`INSERT INTO raft_stats SELECT r.* REPLACE ('S-' || i::VARCHAR AS group_id) FROM raft_stats r CROSS JOIN range(1000) x(i) WHERE server_id='S0'`)
	f.exec(`INSERT INTO stream_stats (epoch,server_id,account,stream,created,raft_group)
 SELECT epoch,server_id,'APP','orders-' || i::VARCHAR, make_timestamp(?::BIGINT),'S-' || i::VARCHAR
 FROM raft_stats CROSS JOIN range(1000) x(i) WHERE group_id='_meta_' AND server_id='S0'`, t0.UnixMicro())
	f.fires("server-027", 7, "nats-0")
}

// Marshal the pinned server API types, decode through the real collector and
// persist through WriteSnapshot: checks must not depend on fixture-only columns.
func TestRaftDiagnosticsSourceWireToStore(t *testing.T) {
	for _, candidate := range []bool{false, true} {
		t.Run(map[bool]string{false: "apply", true: "candidate"}[candidate], func(t *testing.T) {
			f := newFx(t)
			for e := int64(1); e <= 5; e++ {
				g := server.RaftzGroup{ID: "local-peer", State: "FOLLOWER", Size: 3, QuorumNeeded: 2, Committed: 2000, Applied: 10, Leader: "peer1", EverHadLeader: true, Term: 4, PIndex: 2000, Peers: map[string]server.RaftzGroupPeer{"peer1": {Name: "nats-1", Known: true}, "peer2": {Name: "nats-2", Known: true}}}
				if candidate {
					g.State = "CANDIDATE"
					g.Leader = ""
					g.Term = uint64(e + 4)
				}
				data := server.RaftzStatus{"SYS": {"_meta_": g}}
				b, err := json.Marshal(server.ServerAPIRaftzResponse{Server: &server.ServerInfo{ID: "S0", Name: "nats-0"}, Data: &data})
				if err != nil {
					t.Fatal(err)
				}
				snap := collection.NewSnapshot(at(e))
				snap.FinishedAt = at(e).Add(time.Millisecond)
				snap.Expected = []string{"nats-0"}
				if err := snap.Apply(collection.Raftz, b); err != nil {
					t.Fatal(err)
				}
				snap.Servers["S0"].Varz = &server.Varz{Start: t0.Add(-time.Hour)}
				if _, err := f.st.WriteSnapshot(context.Background(), snap); err != nil {
					t.Fatal(err)
				}
			}
			code := "server-027"
			if candidate {
				code = "server-028"
			}
			f.fires(code, 5, "nats-0")
		})
	}
}

func TestRaftDiagnosticsTemporalHysteresis(t *testing.T) {
	for _, candidate := range []bool{false, true} {
		code := "server-027"
		if candidate {
			code = "server-028"
		}
		t.Run(code, func(t *testing.T) {
			f := raftDiagnosticFixture(t, candidate)
			f.fires(code, 5, "nats-0")
			// One observed recovery clears the symptom. Two bad samples after
			// recovery must not inherit the old run's duration.
			if candidate {
				f.exec(`UPDATE raft_stats SET state='FOLLOWER', leader='peer1' WHERE epoch=6`)
			} else {
				f.exec(`UPDATE raft_stats SET applied=2000 WHERE epoch>=6`)
				f.exec(`UPDATE raft_stats SET committed=4000,pindex=4000 WHERE epoch=7`)
			}
			f.fires(code, 6)
			f.fires(code, 7)
		})
	}
}

// An unknown observation breaks the run even when later samples are eligible.
func TestRaftDiagnosticsUnknownEligibility(t *testing.T) {
	for _, candidate := range []bool{false, true} {
		code := "server-027"
		if candidate {
			code = "server-028"
		}
		fields := []string{"state", "size", "observer", "paused", "overrun", "overrun_count", "catching_up", "term", "pindex", "committed", "applied", "peers_total", "peers_known", "peers", "raft_account"}
		if candidate {
			fields = append(fields, "ever_had_leader")
		}
		for _, field := range fields {
			for _, epoch := range []int64{4, 7} {
				t.Run(fmt.Sprintf("%s/%s/epoch%d", code, field, epoch), func(t *testing.T) {
					f := raftDiagnosticFixture(t, candidate)
					f.exec("UPDATE raft_stats SET "+field+"=NULL WHERE epoch=?", epoch)
					f.fires(code, 7)
				})
			}
		}
	}
}

func TestRaftDiagnosticsLeaderEvidence(t *testing.T) {
	for _, candidate := range []bool{false, true} {
		code := "server-027"
		if candidate {
			code = "server-028"
		}
		for _, epoch := range []int64{4, 7} {
			for _, leader := range []any{nil, "", "peer1"} {
				t.Run(fmt.Sprintf("%s/epoch%d/leader%v", code, epoch, leader), func(t *testing.T) {
					f := raftDiagnosticFixture(t, candidate)
					f.exec("UPDATE raft_stats SET leader=? WHERE epoch=?", leader, epoch)
					if (candidate && leader == "") || (!candidate && leader == "peer1") {
						f.fires(code, 7, "nats-0")
					} else {
						f.fires(code, 7)
					}
				})
			}
		}
	}
}
