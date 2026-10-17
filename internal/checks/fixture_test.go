package checks

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
)

// t0 is the start of the fixtures' first epoch.
var t0 = time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)

// fx builds a store by inserting rows directly, with only the columns a
// check reads; every other column is null.
type fx struct {
	t  *testing.T
	st *store.Store
}

func newFx(t *testing.T) *fx {
	t.Helper()
	st, err := store.OpenPath(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &fx{t: t, st: st}
}

type m map[string]any

// row inserts one row into table.
func (f *fx) row(table string, cols m) {
	f.t.Helper()
	names := make([]string, 0, len(cols))
	for k := range cols {
		names = append(names, k)
	}
	sort.Strings(names)
	vals := make([]any, len(names))
	for i, n := range names {
		vals[i] = cols[n]
	}
	q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(names, ", "),
		strings.TrimSuffix(strings.Repeat("?, ", len(names)), ", "))
	if _, err := f.st.DB().Exec(q, vals...); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
}

// exec runs a statement, for list and JSON values the driver cannot bind.
func (f *fx) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.st.DB().Exec(q, args...); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
}

// at is the start of epoch n: one epoch per 30 seconds from t0.
func at(n int64) time.Time { return t0.Add(time.Duration(n-1) * 30 * time.Second) }

// epoch stores epoch n with the given servers expected and those of them
// missing.
func (f *fx) epoch(n int64, expected []string, missing ...string) {
	f.t.Helper()
	f.epochAt(n, at(n), expected, missing...)
}

// epochAt stores epoch n starting at start.
func (f *fx) epochAt(n int64, start time.Time, expected []string, missing ...string) {
	f.t.Helper()
	observed := []string{}
	for _, e := range expected {
		if !contains(missing, e) {
			observed = append(observed, e)
		}
	}
	f.exec(`INSERT INTO epochs VALUES (?, ?, ?, 100, ?, ?, ?::VARCHAR[], ?::VARCHAR[], ?::VARCHAR[], 0, ?, [], NULL)`,
		n, start, start.Add(100*time.Millisecond), len(expected), len(observed),
		listLit(expected), listLit(observed), listLit(missing), len(missing) > 0)
}

// epochs stores epochs from..to, all servers answering.
func (f *fx) epochs(from, to int64, expected ...string) {
	for n := from; n <= to; n++ {
		f.epoch(n, expected)
	}
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// listLit formats a string list as a DuckDB list literal for a ::VARCHAR[]
// cast.
func listLit(xs []string) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = "'" + strings.ReplaceAll(x, "'", "''") + "'"
	}
	return "[" + strings.Join(q, ", ") + "]"
}

// server stores a server process: id, name, the epochs it answered in and
// its start time.
func (f *fx) server(id, name string, first, last int64, start time.Time, version string) {
	f.row("servers", m{"server_id": id, "server_name": name, "version": version, "jetstream": true,
		"start_time": start, "first_epoch": first, "last_epoch": last, "first_seen_at": at(first), "last_seen_at": at(last)})
}

// stats stores a server_stats row; cols add to and override the defaults.
func (f *fx) stats(epoch int64, id string, cols m) {
	row := m{"epoch": epoch, "server_id": id, "version": "2.15.0", "js_enabled": true}
	for k, v := range cols {
		row[k] = v
	}
	f.row("server_stats", row)
}

// cluster stores three servers nats-0..2 (ids S0..S2) answering every
// epoch from 1 to n with JSZ ok.
func (f *fx) cluster(n int64) {
	names := []string{"nats-0", "nats-1", "nats-2"}
	f.epochs(1, n, names...)
	for i, name := range names {
		id := fmt.Sprintf("S%d", i)
		f.server(id, name, 1, n, t0.Add(-time.Hour), "2.15.0")
		for e := int64(1); e <= n; e++ {
			f.row("epoch_endpoints", m{"epoch": e, "server_id": id, "endpoint": "JSZ", "state": "ok"})
		}
	}
}

// eval runs one check with the default thresholds, or those in over, for
// the epoch and returns its rows by entity key. It fails the test when the
// check did not run.
func (f *fx) eval(code string, epoch int64, over ...map[string]string) map[string]store.FindingRow {
	f.t.Helper()
	cfg := config.Checks{Timeout: config.Duration(10 * time.Second)}
	if len(over) > 0 {
		cfg.Thresholds = map[string]map[string]string{code: over[0]}
	}
	set, err := Load(cfg)
	if err != nil {
		f.t.Fatal(err)
	}
	e := &Engine{Set: set, only: map[string]bool{code: true}}
	runs, err := e.Evaluate(context.Background(), f.st.DB(), epoch)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, r := range runs {
		if r.Code != code {
			continue
		}
		if r.Status != store.RunOK {
			f.t.Fatalf("%s: %s %s", code, r.Status, r.Error)
		}
		out := map[string]store.FindingRow{}
		for _, row := range r.Rows {
			out[row.EntityKey] = row
		}
		return out
	}
	f.t.Fatalf("%s did not run", code)
	return nil
}

// fires asserts that the check returns exactly the given entities.
func (f *fx) fires(code string, epoch int64, keys ...string) map[string]store.FindingRow {
	f.t.Helper()
	got := f.eval(code, epoch)
	if len(got) != len(keys) {
		f.t.Fatalf("%s at epoch %d returned %v, want %v", code, epoch, rowsText(got), keys)
	}
	for _, k := range keys {
		if _, ok := got[k]; !ok {
			f.t.Fatalf("%s at epoch %d returned %v, want %v", code, epoch, rowsText(got), keys)
		}
	}
	return got
}

func rowsText(rows map[string]store.FindingRow) string {
	var out []string
	for k, r := range rows {
		out = append(out, fmt.Sprintf("%q %s: %s", k, r.Severity, r.Detail))
	}
	sort.Strings(out)
	return "[" + strings.Join(out, "; ") + "]"
}

// want checks a row's severity and that its detail holds every fragment.
func want(t *testing.T, r store.FindingRow, severity string, fragments ...string) {
	t.Helper()
	if r.Severity != severity {
		t.Errorf("%s: severity %s, want %s (detail %q)", r.EntityKey, r.Severity, severity, r.Detail)
	}
	for _, s := range fragments {
		if !strings.Contains(r.Detail, s) {
			t.Errorf("%s: detail %q lacks %q", r.EntityKey, r.Detail, s)
		}
	}
}

func merge(base, over m) m {
	out := m{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

// streamStats stores one server's stream_stats row for APP/stream.
func (f *fx) streamStats(epoch int64, id, stream string, cols m) {
	f.row("stream_stats", merge(m{"epoch": epoch, "server_id": id, "account": "APP", "stream": stream,
		"cluster": "lens", "is_leader": false}, cols))
}

// streamConfig stores one server's configuration run of APP/stream.
func (f *fx) streamConfig(id, stream string, first, last int64, cols m) {
	f.row("stream_config", merge(m{"server_id": id, "account": "APP", "stream": stream, "first_epoch": first,
		"last_epoch": last, "config_hash": "h", "replicas": 3, "retention": "limits", "storage": "file", "discard": "old",
		"max_msgs": -1, "max_bytes": -1, "max_age_ns": 0, "max_msgs_per_subject": -1, "max_consumers": -1,
		"max_msg_size": -1, "duplicate_window_ns": 120e9, "compression": "none", "sealed": false, "deny_delete": false,
		"deny_purge": false, "allow_direct": true, "allow_msg_ttl": false}, cols))
}

// streamReplica stores a peer row as server id reports it.
func (f *fx) streamReplica(epoch int64, id, stream, peer string, cols m) {
	f.row("stream_replicas", merge(m{"epoch": epoch, "server_id": id, "account": "APP", "stream": stream,
		"peer_name": peer, "current": true, "offline": false, "lag": 0, "active_ns": 1e8}, cols))
}

// consumerStats stores one server's consumer_stats row.
func (f *fx) consumerStats(epoch int64, id, stream, consumer string, cols m) {
	f.row("consumer_stats", merge(m{"epoch": epoch, "server_id": id, "account": "APP", "stream": stream,
		"consumer": consumer, "cluster": "lens", "is_leader": false, "created": t0.Add(-24 * time.Hour)}, cols))
}

// consumerConfig stores one server's configuration run of a consumer.
func (f *fx) consumerConfig(id, stream, consumer string, first, last int64, cols m) {
	f.row("consumer_config", merge(m{"server_id": id, "account": "APP", "stream": stream, "consumer": consumer,
		"first_epoch": first, "last_epoch": last, "config_hash": "h", "durable_name": consumer, "deliver_policy": "all",
		"ack_policy": "explicit", "ack_wait_ns": 30e9, "max_deliver": 3, "replay_policy": "instant", "max_waiting": 512,
		"max_ack_pending": 1000, "max_batch": 0, "idle_heartbeat_ns": 0, "flow_control": false, "headers_only": false,
		"inactive_threshold_ns": 0, "replicas": 0, "mem_storage": false, "created": t0.Add(-24 * time.Hour)}, cols))
}

// consumerReplica stores a consumer peer row as server id reports it.
func (f *fx) consumerReplica(epoch int64, id, stream, consumer, peer string, cols m) {
	f.row("consumer_replicas", merge(m{"epoch": epoch, "server_id": id, "account": "APP", "stream": stream,
		"consumer": consumer, "peer_name": peer, "current": true, "offline": false, "lag": 0}, cols))
}

// event stores one event.
func (f *fx) event(id int64, when time.Time, kind string, cols m) {
	f.row("events", merge(m{"id": id, "time": when, "received_at": when, "source": "jetstream", "kind": kind,
		"summary": kind}, cols))
}
