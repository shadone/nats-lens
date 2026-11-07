package checks

import "testing"

// The proposed contract is sampled evidence over retained epochs, not proof
// of continuous capture. Unlike the old R2 negative probe, a fully deleted
// epoch within max_gap is explicitly expected to bridge.
func TestRaftRetainedEpochContract(t *testing.T) {
	for _, candidate := range []bool{false, true} {
		code := "server-027"
		if candidate {
			code = "server-028"
		}
		cases := []struct {
			name  string
			edits []string
			epoch int64
			want  bool
		}{
			{"sequence_hole_only", []string{`UPDATE epochs SET epoch=epoch+100 WHERE epoch>=4`, `UPDATE raft_stats SET epoch=epoch+100 WHERE epoch>=4`, `UPDATE epoch_endpoints SET epoch=epoch+100 WHERE epoch>=4`, `UPDATE servers SET last_epoch=107`}, 107, true},
			{"absent_epoch_and_all_local_evidence_60s", []string{`DELETE FROM epochs WHERE epoch=4`, `DELETE FROM raft_stats WHERE epoch=4`, `DELETE FROM epoch_endpoints WHERE epoch=4`}, 7, true},
			{"retained_epoch_missing_group", []string{`DELETE FROM raft_stats WHERE epoch=4`}, 7, false},
			{"retained_epoch_missing_capture", []string{`DELETE FROM epoch_endpoints WHERE epoch=4 AND endpoint='RAFTZ'`}, 7, false},
			{"retained_epoch_failed_capture", []string{`UPDATE epoch_endpoints SET state='missing' WHERE epoch=4 AND endpoint='RAFTZ'`}, 7, false},
			{"absent_epoch_elapsed_91s", []string{`DELETE FROM epochs WHERE epoch=4`, `DELETE FROM raft_stats WHERE epoch=4`, `DELETE FROM epoch_endpoints WHERE epoch=4`, `UPDATE epochs SET started_at=started_at+INTERVAL 31 SECOND WHERE epoch>=5`}, 7, false},
			{"bounded_retained_history", []string{`DELETE FROM epochs WHERE epoch<4`, `DELETE FROM raft_stats WHERE epoch<4`, `DELETE FROM epoch_endpoints WHERE epoch<4`}, 7, false},
			{"minimum_retained_history_qualifies", []string{`DELETE FROM epochs WHERE epoch<3`, `DELETE FROM raft_stats WHERE epoch<3`, `DELETE FROM epoch_endpoints WHERE epoch<3`}, 7, true},
		}
		for _, tc := range cases {
			t.Run(code+"/"+tc.name, func(t *testing.T) {
				f := raftDiagnosticFixture(t, candidate)
				for _, q := range tc.edits {
					f.exec(q)
				}
				if tc.want {
					f.fires(code, tc.epoch, "nats-0")
				} else {
					f.fires(code, tc.epoch)
				}
			})
		}
	}
}

func TestRaftRetainedSampleMinimum(t *testing.T) {
	for _, candidate := range []bool{false, true} {
		code := "server-027"
		if candidate {
			code = "server-028"
		}
		t.Run(code, func(t *testing.T) {
			f := raftDiagnosticFixture(t, candidate)
			// A long enough span cannot replace the required number of retained samples.
			f.exec(`DELETE FROM epochs WHERE epoch NOT IN (1,4,7)`)
			f.exec(`DELETE FROM raft_stats WHERE epoch NOT IN (1,4,7)`)
			f.exec(`DELETE FROM epoch_endpoints WHERE epoch NOT IN (1,4,7)`)
			f.fires(code, 7, "nats-0") // exactly three samples; both gaps are exactly 90s
			if got := len(f.eval(code, 7, map[string]string{"min_epochs": "4"})); got != 0 {
				t.Fatalf("three retained samples met min_epochs=4: %d", got)
			}
		})
	}
}
