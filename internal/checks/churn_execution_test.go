package checks

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
)

var churnCodes = []string{"client-005", "server-011"}

// Both checks fire at 22; at 24 their counts are in the holding band.
// The other client/server have always been in the band and must never fire.
func churnHistory(t *testing.T, path string) (*fx, *Engine) {
	t.Helper()
	st, err := store.OpenPath(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	f := &fx{t: t, st: st}
	t.Cleanup(func() { _ = f.st.Close() })
	f.epochs(1, 50, "nats-0", "nats-1")
	for i, name := range []string{"nats-0", "nats-1"} {
		id := []string{"S0", "S1"}[i]
		f.server(id, name, 1, 50, t0, "2.15.0")
		for epoch := int64(1); epoch <= 50; epoch++ {
			total := 1000
			if i == 0 && epoch >= 4 {
				total = 1015
			}
			if epoch >= 21 {
				if i == 0 {
					total = 1060
				} else {
					total = 1045
				}
			}
			f.stats(epoch, id, m{"total_connections": total, "connections": 10})
		}
	}
	f.churnEvent(100001, at(3), "order-sync", "10.0.0.5", 4, map[string]int{"nats-0": 4})
	f.churnEvent(100002, at(20), "order-sync", "10.0.0.5", 16, map[string]int{"nats-0": 16})
	f.churnEvent(100003, at(20), "stock-poller", "10.0.0.6", 16, map[string]int{"nats-1": 16})
	set, err := Load(config.Checks{Timeout: config.Duration(10 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	return f, &Engine{Set: set, only: map[string]bool{"client-005": true, "server-011": true}}
}

func churnRun(t *testing.T, f *fx, e *Engine, epoch int64) {
	t.Helper()
	if _, err := e.Run(context.Background(), f.st, epoch); err != nil {
		t.Fatal(err)
	}
}

func churnStates(t *testing.T, f *fx, state string, last int64) {
	t.Helper()
	for _, code := range churnCodes {
		key := "order-sync"
		if code == "server-011" {
			key = "nats-0"
		}
		var got string
		var seen int64
		if err := f.st.DB().QueryRow(`SELECT state, last_seen_epoch FROM findings WHERE check_code = ? AND entity_key = ?`, code, key).Scan(&got, &seen); err != nil {
			t.Fatal(err)
		}
		if got != state || seen != last {
			t.Errorf("%s: state=%s last_seen=%d; want %s/%d", code, got, seen, state, last)
		}
		var others int
		if err := f.st.DB().QueryRow(`SELECT count(*) FROM findings WHERE check_code = ? AND entity_key <> ?`, code, key).Scan(&others); err != nil || others != 0 {
			t.Errorf("%s unrelated entities: %d, %v", code, others, err)
		}
	}
}

func TestChurnHysteresisExecutionGaps(t *testing.T) {
	for _, gap := range []string{"budget", "query failure", "independent checks", "missing run", "future success", "reopen", "disabled", "successful recovery"} {
		t.Run(gap, func(t *testing.T) {
			path := ""
			if gap == "reopen" {
				path = filepath.Join(t.TempDir(), "churn.duckdb")
			}
			f, e := churnHistory(t, path)
			churnRun(t, f, e, 22)
			churnStates(t, f, "firing", 22)
			switch gap {
			case "budget", "reopen":
				e.Budget = time.Nanosecond
				churnRun(t, f, e, 23)
				churnRun(t, f, e, 24)
				e.Budget = 0
			case "query failure":
				for _, code := range churnCodes {
					c, _ := e.Set.Lookup(code)
					c.SQL = `SELECT * FROM missing_churn_test_table`
				}
				churnRun(t, f, e, 23)
				// Reload restores queries, like a new engine after a corrected check.
				set, err := Load(config.Checks{})
				if err != nil {
					t.Fatal(err)
				}
				e.Set = set
				e.Budget = time.Nanosecond
				churnRun(t, f, e, 24)
				e.Budget = 0
			case "independent checks":
				c, _ := e.Set.Lookup("client-005")
				original := c.SQL
				c.SQL = `SELECT * FROM missing_churn_test_table`
				churnRun(t, f, e, 24)
				c.SQL = original
			case "future success":
				for _, code := range churnCodes {
					f.row("check_runs", m{"epoch": 49, "check_code": code, "status": "ok", "rows": 0, "duration_ms": 0})
				}
			case "missing run":
				// Other successful checks must not advance these checks' history.
				f.row("check_runs", m{"epoch": 24, "check_code": "server-001", "status": "ok", "rows": 0, "duration_ms": 0})
			case "disabled":
				e.Set.disabled = map[string]bool{"client-005": true, "server-011": true}
				churnRun(t, f, e, 23)
				e.Set.disabled = nil
			case "successful recovery":
				f.exec(`DELETE FROM events WHERE client_name = 'order-sync'`)
				f.exec(`UPDATE server_stats SET total_connections = 1015 WHERE server_id = 'S0' AND epoch IN (22, 23)`)
				churnRun(t, f, e, 23)
				f.churnEvent(100004, at(24), "order-sync", "10.0.0.5", 16, map[string]int{"nats-0": 16})
			}
			if gap == "budget" || gap == "query failure" || gap == "reopen" {
				churnStates(t, f, "firing", 22)
				for _, code := range churnCodes {
					var failed int
					if err := f.st.DB().QueryRow(`SELECT count(*) FROM check_runs WHERE check_code = ? AND epoch IN (23, 24) AND status = 'error'`, code).Scan(&failed); err != nil || failed != 2 {
						t.Fatalf("%s: failed/skipped executions = %d, %v", code, failed, err)
					}
				}
			}
			if gap == "reopen" {
				if err := f.st.Close(); err != nil {
					t.Fatal(err)
				}
				st, err := store.OpenPath(context.Background(), path)
				if err != nil {
					t.Fatal(err)
				}
				f.st = st
				set, err := Load(config.Checks{})
				if err != nil {
					t.Fatal(err)
				}
				e = &Engine{Set: set, only: map[string]bool{"client-005": true, "server-011": true}}
			}
			churnRun(t, f, e, 25)
			if gap == "disabled" || gap == "successful recovery" {
				churnStates(t, f, "resolved", 22)
				// A resolved/disabled episode needs full firing thresholds again.
				f.churnEvent(100005, at(26), "order-sync", "10.0.0.5", 20, map[string]int{"nats-0": 20})
				f.exec(`UPDATE server_stats SET total_connections = 1100 WHERE server_id = 'S0' AND epoch IN (26, 27)`)
				churnRun(t, f, e, 27)
				churnStates(t, f, "firing", 27)
			} else {
				churnStates(t, f, "firing", 25)
				for _, code := range churnCodes {
					var transitions int
					if err := f.st.DB().QueryRow(`SELECT count(*) FROM finding_transitions WHERE check_code = ?`, code).Scan(&transitions); err != nil || transitions != 1 {
						t.Errorf("%s: gap caused extra transitions: %d, %v", code, transitions, err)
					}
				}
				// Expired windows cannot retain a finding just because it fired before.
				churnRun(t, f, e, 50)
				churnStates(t, f, "resolved", 25)
			}
		})
	}
}

// A fabricated/stale finding alone is not proof of a successful evaluation.
// Current/future executions and mutable future finding state must not leak back.
func TestChurnHysteresisHistoricalGuards(t *testing.T) {
	for _, history := range []string{"none", "error", "disabled", "other check", "future run", "current run", "future finding", "current finding"} {
		t.Run(history, func(t *testing.T) {
			f, e := churnHistory(t, "")
			churnRun(t, f, e, 22)
			f.exec(`DELETE FROM check_runs`)
			for _, code := range churnCodes {
				runEpoch, status := int64(22), "ok"
				switch history {
				case "none":
					continue
				case "error":
					status = "error"
				case "disabled":
					status = "disabled"
				case "other check":
					continue
				case "future run":
					runEpoch = 26
				case "current run":
					runEpoch = 25
				}
				f.row("check_runs", m{"epoch": runEpoch, "check_code": code, "status": status, "rows": 0, "duration_ms": 0})
			}
			if history != "future finding" && history != "current finding" {
				// Match the previous global epoch to reject unsupported state
				// even when the old global-epoch guard would accept it.
				f.exec(`UPDATE findings SET last_seen_epoch = 24, updated_epoch = 24`)
			}
			if history == "other check" {
				f.row("check_runs", m{"epoch": 22, "check_code": "server-001", "status": "ok", "rows": 0, "duration_ms": 0})
			}
			if history == "future finding" {
				f.exec(`UPDATE findings SET updated_epoch = 26`)
			}
			if history == "current finding" {
				f.exec(`UPDATE findings SET updated_epoch = 25`)
			}
			for _, code := range churnCodes {
				f.fires(code, 25)
			}
		})
	}
}
