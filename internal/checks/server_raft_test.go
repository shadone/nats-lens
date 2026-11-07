package checks

import "testing"

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
