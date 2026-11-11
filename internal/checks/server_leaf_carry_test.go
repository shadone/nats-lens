package checks

import (
	"context"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func reviewLeafEngine(t *testing.T) *Engine {
	t.Helper()
	s, err := Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(s.Checks()); got != 92 {
		t.Fatalf("registered checks=%d want 92", got)
	}
	return &Engine{Set: s, Interval: 30 * time.Second, only: map[string]bool{LeafHealthCode: true}}
}
func reviewLeafRun(t *testing.T, f *fx, e *Engine, epoch int64) {
	t.Helper()
	if _, err := e.Run(context.Background(), f.st, epoch); err != nil {
		t.Fatal(err)
	}
}
func TestReviewLeafExecutionGap(t *testing.T) {
	for _, gap := range []string{"not-applied", "error", "budget-skipped", "query-error", "global-gap", "disabled"} {
		t.Run(gap, func(t *testing.T) {
			f := leafFixture(t, 5)
			site(f, 1, "A", "pickup", "APP", 1)
			f.exec(`DELETE FROM epoch_endpoints WHERE epoch>=4 AND server_id='B'`)
			e := reviewLeafEngine(t)
			reviewLeafRun(t, f, e, 3)
			switch gap {
			case "query-error":
				f.exec(`ALTER TABLE leafnodes RENAME TO leafnodes_unavailable`)
				reviewLeafRun(t, f, e, 4)
				f.exec(`ALTER TABLE leafnodes_unavailable RENAME TO leafnodes`)
				var status string
				if err := f.st.DB().QueryRow(`SELECT status FROM check_runs WHERE epoch=4 AND check_code=?`, LeafHealthCode).Scan(&status); err != nil || status != store.RunError {
					t.Fatalf("status=%s err=%v", status, err)
				}
			case "global-gap":
				f.exec(`DELETE FROM epochs WHERE epoch=4`)
			case "error", "disabled":
				status := store.RunError
				if gap == "disabled" {
					status = store.RunDisabled
				}
				if _, err := f.st.ApplyCheckRuns(context.Background(), 4, []store.CheckRun{{Code: LeafHealthCode, EntityType: "system", Status: status, Error: "review synthetic failure"}}); err != nil {
					t.Fatal(err)
				}
			case "budget-skipped":
				e.Budget = time.Nanosecond
				reviewLeafRun(t, f, e, 4)
				e.Budget = 0
			}
			reviewLeafRun(t, f, e, 5)
			var state, detail string
			if err := f.st.DB().QueryRow(`SELECT state,detail FROM findings WHERE check_code=?`, LeafHealthCode).Scan(&state, &detail); err != nil {
				t.Fatal(err)
			}
			want := "firing"
			if gap == "disabled" {
				want = "resolved"
			}
			if state != want {
				t.Fatalf("after %s epoch 4 and uncertain epoch 5: state=%s want=%s; evidence=%s", gap, state, want, detail)
			}
			if state == "firing" && !strings.Contains(detail, leafUnknownSuffix) {
				t.Fatal("retained without uncertainty evidence:", detail)
			}
		})
	}
}
func TestReviewLeafHistoricalUpdatedGuard(t *testing.T) {
	for _, update := range []int64{4, 5, 6} {
		t.Run(at(update).Format("150405"), func(t *testing.T) {
			f := leafFixture(t, 6)
			site(f, 1, "A", "pickup", "APP", 1)
			f.exec(`DELETE FROM epoch_endpoints WHERE epoch>=4 AND server_id='B'`)
			e := reviewLeafEngine(t)
			reviewLeafRun(t, f, e, 3)
			reviewLeafRun(t, f, e, 4)
			f.exec(`UPDATE findings SET updated_epoch=? WHERE check_code=?`, update, LeafHealthCode)
			runs, err := e.Evaluate(context.Background(), f.st.DB(), 5)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range runs {
				if r.Code == LeafHealthCode {
					if r.Status != store.RunOK {
						t.Fatal(r)
					}
					want := 0
					if update < 5 {
						want = 1
					}
					if len(r.Rows) != want {
						t.Fatalf("updated_epoch=%d, evaluating 5: got %d rows want %d; rows=%+v", update, len(r.Rows), want, r.Rows)
					}
				}
			}
		})
	}
}

func TestReviewLeafNaturalFutureUpdate(t *testing.T) {
	f := leafFixture(t, 6)
	site(f, 1, "A", "pickup", "APP", 1)
	f.exec(`DELETE FROM epoch_endpoints WHERE epoch>=4 AND server_id='B'`)
	e := reviewLeafEngine(t)
	reviewLeafRun(t, f, e, 3)
	reviewLeafRun(t, f, e, 4)
	f.exec(`UPDATE epochs SET started_at=started_at+INTERVAL 61 MINUTE WHERE epoch=6`)
	e.Budget = time.Nanosecond
	reviewLeafRun(t, f, e, 6)
	var seen, updated int64
	if err := f.st.DB().QueryRow(`SELECT last_seen_epoch,updated_epoch FROM findings WHERE check_code=?`, LeafHealthCode).Scan(&seen, &updated); err != nil {
		t.Fatal(err)
	}
	t.Logf("after real engine runs 3,4 and budget-skipped 6: last_seen_epoch=%d updated_epoch=%d", seen, updated)
	e.Budget = 0
	runs, err := e.Evaluate(context.Background(), f.st.DB(), 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range runs {
		if r.Code == LeafHealthCode && len(r.Rows) != 0 {
			t.Fatalf("future-mutated finding leaked into epoch 5: %+v", r)
		}
	}
}
func TestReviewLeafWindowExpiry(t *testing.T) {
	f := leafFixture(t, 5)
	site(f, 1, "A", "pickup", "APP", 1)
	f.exec(`DELETE FROM epoch_endpoints WHERE epoch>=4 AND server_id='B'`)
	f.exec(`UPDATE epochs SET started_at=started_at+INTERVAL 25 HOUR WHERE epoch>=4`)
	e := reviewLeafEngine(t)
	reviewLeafRun(t, f, e, 3)
	reviewLeafRun(t, f, e, 4)
	reviewLeafRun(t, f, e, 5)
	var state, detail string
	if err := f.st.DB().QueryRow(`SELECT state,detail FROM findings WHERE check_code=?`, LeafHealthCode).Scan(&state, &detail); err != nil {
		t.Fatal(err)
	}
	if state != "resolved" {
		t.Fatalf("sighting older than 24h still %s: %s", state, detail)
	}
}

func TestLeafCarryExpiryBoundaryAndMixedEvidence(t *testing.T) {
	for _, unknownInterval := range []bool{false, true} {
		t.Run(map[bool]string{false: "coverage", true: "interval"}[unknownInterval], func(t *testing.T) {
			f := leafFixture(t, 8)
			f.exec(`UPDATE epochs SET started_at=make_timestamp(?::BIGINT) WHERE epoch=1`, t0.Add(123456*time.Microsecond).UnixMicro())
			site(f, 1, "A", `old; "site"`, "APP", 1)
			// A new identity is observed after the old warning and then disconnects.
			site(f, 4, "A", "new", "OTHER", 2)
			for epoch, offset := range map[int64]time.Duration{4: 23 * time.Hour, 5: 23*time.Hour + 30*time.Second, 6: 24 * time.Hour, 7: 24*time.Hour + time.Microsecond, 8: 25 * time.Hour} {
				f.exec(`UPDATE epochs SET started_at=make_timestamp(?::BIGINT) WHERE epoch=?`, t0.Add(offset+123456*time.Microsecond).UnixMicro(), epoch)
			}
			e := reviewLeafEngine(t)
			reviewLeafRun(t, f, e, 3)
			if unknownInterval {
				e.Interval = 0
			} else {
				f.exec(`DELETE FROM epoch_endpoints WHERE epoch>=4 AND server_id='B'`)
			}
			for _, epoch := range []int64{4, 5, 6, 7, 8} {
				reviewLeafRun(t, f, e, epoch)
				var state, detail string
				if err := f.st.DB().QueryRow(`SELECT state,detail FROM findings WHERE check_code=?`, LeafHealthCode).Scan(&state, &detail); err != nil {
					t.Fatal(err)
				}
				want := "firing"
				if epoch >= 7 {
					want = "resolved"
				}
				if state != want {
					t.Fatalf("epoch %d state=%s want=%s: %s", epoch, state, want, detail)
				}
			}
		})
	}
}

func TestLeafCarryFreshEvidenceSurvivesOldExpiry(t *testing.T) {
	f := leafFixture(t, 9)
	site(f, 1, "A", "old", "APP", 1)
	site(f, 5, "A", "new", "OTHER", 2)
	// At 7, new absence is proven but old absence crossed a coverage gap.
	f.exec(`DELETE FROM epoch_endpoints WHERE epoch=4 AND server_id='B'`)
	for epoch := int64(4); epoch <= 9; epoch++ {
		offset := 23*time.Hour + time.Duration(epoch-4)*30*time.Second
		if epoch >= 8 {
			offset = 24*time.Hour + time.Duration(epoch-6)*30*time.Second
		}
		f.exec(`UPDATE epochs SET started_at=make_timestamp(?::BIGINT) WHERE epoch=?`, t0.Add(offset).UnixMicro(), epoch)
	}
	e := reviewLeafEngine(t)
	for _, epoch := range []int64{3, 4, 5, 6, 7, 8, 9} {
		reviewLeafRun(t, f, e, epoch)
		if epoch == 7 {
			var n float64
			if err := f.st.DB().QueryRow(`SELECT value FROM findings WHERE check_code=?`, LeafHealthCode).Scan(&n); err != nil || n != 2 {
				t.Fatalf("mixed warning count=%v err=%v", n, err)
			}
		}
	}
	var state, detail string
	var value float64
	if err := f.st.DB().QueryRow(`SELECT state,detail,value FROM findings WHERE check_code=?`, LeafHealthCode).Scan(&state, &detail, &value); err != nil {
		t.Fatal(err)
	}
	if state != "firing" || value != 1 || strings.Contains(detail, `"old"`) || !strings.Contains(detail, `"new"`) {
		t.Fatalf("%s value=%v: %s", state, value, detail)
	}
}

func TestLeafCarryRequiresSuccessfulPastExecution(t *testing.T) {
	for _, history := range []string{"none", "error", "disabled", "other", "current", "future"} {
		t.Run(history, func(t *testing.T) {
			f := leafFixture(t, 6)
			site(f, 1, "A", "pickup", "APP", 1)
			f.exec(`DELETE FROM epoch_endpoints WHERE epoch>=4 AND server_id='B'`)
			e := reviewLeafEngine(t)
			reviewLeafRun(t, f, e, 3)
			f.exec(`DELETE FROM check_runs`)
			run, code, status := int64(3), LeafHealthCode, "ok"
			switch history {
			case "error", "disabled":
				status = history
			case "other":
				code = "server-025"
			case "current":
				run = 5
			case "future":
				run = 6
			}
			if history != "none" {
				f.row("check_runs", m{"epoch": run, "check_code": code, "status": status, "rows": 1, "duration_ms": 0})
			}
			// The global predecessor is deliberately matched, but no successful check proves it.
			f.exec(`UPDATE findings SET last_seen_epoch=4,updated_epoch=4`)
			runs, err := e.Evaluate(context.Background(), f.st.DB(), 5)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range runs {
				if r.Code == LeafHealthCode && (r.Status != store.RunOK || len(r.Rows) != 0) {
					t.Fatal(r)
				}
			}
		})
	}
}

func TestLeafCarryReopenAndDisableReenable(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "reopen", true: "disable reenable"}[disabled], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "leaf.duckdb")
			f := leafFixturePath(t, 7, path)
			site(f, 1, "A", "pickup", "APP", 1)
			f.exec(`DELETE FROM epoch_endpoints WHERE epoch IN (4,5) AND server_id='B'`)
			e := reviewLeafEngine(t)
			reviewLeafRun(t, f, e, 3)
			if disabled {
				if _, err := f.st.ApplyCheckRuns(context.Background(), 4, []store.CheckRun{{Code: LeafHealthCode, EntityType: "system", Status: store.RunDisabled}}); err != nil {
					t.Fatal(err)
				}
			} else {
				reviewLeafRun(t, f, e, 4)
			}
			if err := f.st.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := store.OpenPath(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			f.st = reopened
			e = reviewLeafEngine(t)
			reviewLeafRun(t, f, e, 5)
			var state string
			if err := f.st.DB().QueryRow(`SELECT state FROM findings WHERE check_code=?`, LeafHealthCode).Scan(&state); err != nil {
				t.Fatal(err)
			}
			want := "firing"
			if disabled {
				want = "resolved"
			}
			if state != want {
				t.Fatalf("%s want %s", state, want)
			}
			// Reenable needs fresh proven evidence; a new pair of absent collections suffices.
			site(f, 5, "A", "pickup", "APP", 2)
			f.exec(`INSERT INTO epoch_endpoints(epoch,server_id,endpoint,state) VALUES (5,'B','LEAFZ','ok')`)
			reviewLeafRun(t, f, e, 6)
			reviewLeafRun(t, f, e, 7)
			if err := f.st.DB().QueryRow(`SELECT state FROM findings WHERE check_code=?`, LeafHealthCode).Scan(&state); err != nil || state != "firing" {
				t.Fatalf("fresh proof: %s %v", state, err)
			}
		})
	}
}

func TestLeafCarrySeparateAbsenceAndReconnectAges(t *testing.T) {
	f := leafFixture(t, 6)
	site(f, 1, "A", "absent", "APP", 100)
	for epoch := int64(1); epoch <= 4; epoch++ {
		site(f, epoch, "A", "reconnecting", "OTHER", epoch)
	}
	f.exec(`DELETE FROM epoch_endpoints WHERE epoch>=5 AND server_id='B'`)
	f.exec(`UPDATE epochs SET started_at=make_timestamp(?::BIGINT) WHERE epoch=5`, t0.Add(24*time.Hour+time.Second).UnixMicro())
	f.exec(`UPDATE epochs SET started_at=make_timestamp(?::BIGINT) WHERE epoch=6`, at(4).Add(24*time.Hour+time.Second).UnixMicro())
	e := reviewLeafEngine(t)
	reviewLeafRun(t, f, e, 3)
	reviewLeafRun(t, f, e, 4)
	reviewLeafRun(t, f, e, 5)
	var detail, state string
	var value float64
	if err := f.st.DB().QueryRow(`SELECT state,detail,value FROM findings WHERE check_code=?`, LeafHealthCode).Scan(&state, &detail, &value); err != nil {
		t.Fatal(err)
	}
	if state != "firing" || value != 1 || strings.Contains(detail, `"absent"`) || !strings.Contains(detail, `"reconnecting"`) {
		t.Fatalf("%s %v %s", state, value, detail)
	}
	reviewLeafRun(t, f, e, 6)
	if err := f.st.DB().QueryRow(`SELECT state FROM findings WHERE check_code=?`, LeafHealthCode).Scan(&state); err != nil || state != "resolved" {
		t.Fatalf("%s %v", state, err)
	}
}

func TestLeafCarryMalformedProofFailsCheck(t *testing.T) {
	f := leafFixture(t, 4)
	site(f, 1, "A", "pickup", "APP", 1)
	f.exec(`DELETE FROM epoch_endpoints WHERE epoch=4 AND server_id='B'`)
	e := reviewLeafEngine(t)
	reviewLeafRun(t, f, e, 3)
	f.exec(`UPDATE findings SET detail='unrecognized evidence' WHERE check_code=?`, LeafHealthCode)
	reviewLeafRun(t, f, e, 4)
	var state, status string
	if err := f.st.DB().QueryRow(`SELECT state FROM findings WHERE check_code=?`, LeafHealthCode).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := f.st.DB().QueryRow(`SELECT status FROM check_runs WHERE check_code=? AND epoch=4`, LeafHealthCode).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if state != "firing" || status != store.RunError {
		t.Fatalf("state=%s status=%s", state, status)
	}
}
