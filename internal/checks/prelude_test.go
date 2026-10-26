package checks

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// macroRows runs q with the engine's prelude and variables for epoch and
// returns each row's columns joined by "|", sorted.
func (f *fx) macroRows(epoch int64, q string) []string {
	f.t.Helper()
	ctx := context.Background()
	conn, err := f.st.DB().Conn(ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, Prelude); err != nil {
		f.t.Fatal(err)
	}
	if err := setVars(ctx, conn, map[string]string{
		"epoch": strconv.FormatInt(epoch, 10) + "::BIGINT", "epoch_time": tsLit(at(epoch)),
		"restart_window": Value{Unit: UnitDuration, Duration: DefaultRestartWindow}.literal(),
	}); err != nil {
		f.t.Fatal(err)
	}
	rows, err := conn.QueryContext(ctx, q)
	if err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out []string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			f.t.Fatal(err)
		}
		parts := make([]string, len(vals))
		for i, v := range vals {
			parts[i] = text(v)
		}
		out = append(out, strings.Join(parts, "|"))
	}
	sort.Strings(out)
	return out
}

func TestRestartingMacro(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 40, three...)
	// nats-0 restarted at epoch 30 (an earlier process exists); nats-1 is
	// a first process started just now (a new cluster, not a restart);
	// nats-2 reports a JetStream error and a raft group catching up.
	f.server("A0", "nats-0", 1, 29, t0.Add(-time.Hour), "2.15.0")
	f.server("B0", "nats-0", 30, 40, at(30), "2.15.0")
	f.server("S1", "nats-1", 1, 40, at(1), "2.15.0")
	f.server("S2", "nats-2", 1, 40, t0.Add(-time.Hour), "2.15.0")
	for e := int64(1); e <= 40; e++ {
		if e < 30 {
			f.stats(e, "A0", nil)
		} else {
			f.stats(e, "B0", m{"start_time": at(30)})
		}
		f.stats(e, "S1", m{"start_time": at(1)})
		f.stats(e, "S2", m{"start_time": t0.Add(-time.Hour)})
	}
	f.row("health", m{"epoch": 40, "server_id": "S2", "status": "unavailable", "error_count": 1,
		"errors": `[{"type":"JETSTREAM","error":"JetStream is still recovering meta layer"}]`})
	f.row("raft_stats", m{"epoch": 40, "server_id": "S2", "group_id": "G", "catching_up": true})
	got := f.macroRows(40, `SELECT server_name, array_to_string(reasons, ', '), restarted FROM restarting(40, 40)`)
	// A JetStream error or a raft group catching up does not make nats-2
	// restarting: a server in trouble shows the same. It is not ready to
	// hold assets, though, and not on purpose.
	want := []string{"nats-0|restarted 09:14:30|true"}
	if strings.Join(got, ";") != strings.Join(want, ";") {
		t.Fatalf("restarting = %v, want %v", got, want)
	}
	if got := f.macroRows(40, `SELECT server_name, why, planned FROM not_ready(40)`); strings.Join(got, ";") !=
		"nats-0|is restarting (restarted 09:14:30)|true;nats-2|reports JetStream not ready|false" {
		t.Fatalf("not_ready = %v", got)
	}
	// Past restart_window the restart no longer counts.
	if got := f.macroRows(40, `SELECT server_name FROM restarting(29, 29)`); len(got) != 0 {
		t.Fatalf("before the restart: %v", got)
	}
}

func TestPlannedDownAndNotReady(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 9, three...)
	f.epoch(10, three, "nats-0", "nats-1")
	f.server("S0", "nats-0", 1, 9, t0, "2.15.0")
	f.server("S1", "nats-1", 1, 9, t0, "2.15.0")
	f.server("S2", "nats-2", 1, 10, t0, "2.15.0")
	f.row("epoch_endpoints", m{"epoch": 10, "server_id": "S2", "endpoint": "JSZ", "state": "ok"})
	f.stats(10, "S2", m{"js_enabled": false})
	// nats-0 announced lame duck after its last answer; nats-1 vanished.
	f.event(1, at(9).Add(5*time.Second), "server.lameduck", m{"source": "sys", "server_name": "nats-0"})
	f.event(2, at(1), "server.shutdown", m{"source": "sys", "server_name": "nats-1"})
	if got := f.macroRows(10, `SELECT server_name, kind FROM planned_down(10)`); strings.Join(got, ";") != "nats-0|server.lameduck" {
		t.Fatalf("planned_down = %v", got)
	}
	got := f.macroRows(10, `SELECT server_name, planned FROM not_ready(10)`)
	if strings.Join(got, ";") != "nats-0|true;nats-1|false;nats-2|false" {
		t.Fatalf("not_ready = %v", got)
	}
}

// A lame duck stamped by a server whose clock runs behind, but received
// after the server's last answer, still counts as announced.
func TestPlannedDownUsesReceivedAt(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 9, three...)
	f.epoch(10, three, "nats-0")
	f.server("S0", "nats-0", 1, 9, t0, "2.15.0")
	f.event(1, at(9).Add(-2*time.Minute), "server.lameduck", m{"source": "sys", "server_name": "nats-0",
		"received_at": at(9).Add(5 * time.Second)})
	if got := f.macroRows(10, `SELECT server_name FROM planned_down(10)`); strings.Join(got, ";") != "nats-0" {
		t.Fatalf("planned_down = %v", got)
	}
}

// span_text picks its unit after rounding, writes whole numbers without
// ".0" and 1 in the singular.
func TestSpanText(t *testing.T) {
	f := newFx(t)
	for _, c := range []struct {
		secs int64
		want string
	}{
		{29, "less than a minute"},
		{60, "1 minute"},
		{90, "2 minutes"},
		{59*60 + 50, "1 hour"},
		{150 * 60, "2.5 hours"},
		{23*3600 + 58*60, "1 day"},
		{36 * 3600, "1.5 days"},
		{12 * 86400, "12 days"},
	} {
		got := f.macroRows(1, "SELECT span_text(to_seconds("+strconv.FormatInt(c.secs, 10)+"))")
		if len(got) != 1 || got[0] != c.want {
			t.Errorf("span_text(%ds) = %v, want %q", c.secs, got, c.want)
		}
	}
}
