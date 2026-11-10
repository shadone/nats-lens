package storeprov_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/store"
)

// X and Y in cluster eu, epochs 1..4 at 30 s, both answer ROUTEZ in each.
// The X-Y route is replaced between epochs 2 and 3 (one drop). Y started
// yStartBefore2 before epoch 2. With gone, no route is listed from epoch
// 3 on; with yRestartAt4, Y restarts (new ID) at epoch 4.
func restartStore(t *testing.T, last time.Time, yStartBefore2 time.Duration, gone bool, yRestartAt4 bool) *store.Store {
	st := emptyStore(t)
	r := rows{t, st}
	at := func(e int64) time.Time { return last.Add(-time.Duration(4-e) * 30 * time.Second) }
	r.add("servers", map[string]any{"server_id": "X", "server_name": "x", "cluster": "eu", "first_epoch": 1, "last_epoch": 4,
		"first_seen_at": at(1), "last_seen_at": at(4), "start_time": at(1).Add(-48 * time.Hour)})
	yTo := int64(4)
	if yRestartAt4 {
		yTo = 3
		r.add("servers", map[string]any{"server_id": "Y2", "server_name": "y", "cluster": "eu", "first_epoch": 4, "last_epoch": 4,
			"first_seen_at": at(4), "last_seen_at": at(4), "start_time": at(4).Add(-5 * time.Second)})
	}
	r.add("servers", map[string]any{"server_id": "Y", "server_name": "y", "cluster": "eu", "first_epoch": 1, "last_epoch": yTo,
		"first_seen_at": at(1), "last_seen_at": at(yTo), "start_time": at(2).Add(-yStartBefore2)})
	yid := func(e int64) string {
		if yRestartAt4 && e == 4 {
			return "Y2"
		}
		return "Y"
	}
	names := []string{"x", "y"}
	for e := int64(1); e <= 4; e++ {
		if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, 2, 2, ?, ?, [], 0, false, [], NULL)`, e, at(e), at(e), names, names); err != nil {
			t.Fatal(err)
		}
		r.add("epoch_endpoints", map[string]any{"epoch": e, "server_id": "X", "endpoint": "ROUTEZ", "state": "ok"})
		r.add("epoch_endpoints", map[string]any{"epoch": e, "server_id": yid(e), "endpoint": "ROUTEZ", "state": "ok"})
		if e >= 3 && gone {
			continue
		}
		rid := int64(1)
		if e >= 3 {
			rid = 2
		}
		r.add("routes", map[string]any{"epoch": e, "server_id": "X", "rid": rid, "remote_id": yid(e), "remote_name": "y", "rtt_ns": int64(400 * time.Microsecond)})
		r.add("routes", map[string]any{"epoch": e, "server_id": yid(e), "rid": rid + 10, "remote_id": "X", "remote_name": "x", "rtt_ns": int64(400 * time.Microsecond)})
	}
	return st
}

func restartRoutes(t *testing.T, st *store.Store, last time.Time, tz string) []string {
	if _, err := st.DB().Exec(`SET GLOBAL TimeZone = '` + tz + `'`); err != nil {
		t.Fatal(err)
	}
	res := topology(t, st, last.Add(10*time.Second), nil)
	var out []string
	for _, rt := range res.GetRoutes() {
		out = append(out, fmt.Sprintf("%s-%s %s/%s down %v", rt.GetServerA(), rt.GetServerB(), drops(rt.Drops_1H), drops(rt.Drops_24H), rt.GetDown()))
	}
	return out
}

// A route replaced less than the restart window after Y started counts no
// drop, one replaced after it counts one, in four session time zones.
func TestTopologyRouteRestartWindowTimeZones(t *testing.T) {
	last := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	for _, tz := range []string{"UTC", "Pacific/Honolulu", "Asia/Kolkata", "Pacific/Kiritimati"} {
		for _, c := range []struct {
			before time.Duration
			want   string
		}{{9*time.Minute + 50*time.Second, "x-y 0/0 down false"}, {10*time.Minute + 10*time.Second, "x-y 1/1 down false"}} {
			got := restartRoutes(t, restartStore(t, last, c.before, false, false), last, tz)
			if !slices.Equal(got, []string{c.want}) {
				t.Errorf("tz %s start %v before: %v, want %v", tz, c.before, got, c.want)
			}
		}
	}
}

// No route listed in epochs 3 and 4, both answering ROUTEZ in each: the
// pair is down, with the drop between 2 and 3. Y restarted at epoch 4 (a
// new ID): not down yet.
func TestTopologyDownFromStore(t *testing.T) {
	last := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	for _, tz := range []string{"UTC", "Pacific/Honolulu", "Asia/Kolkata"} {
		got := restartRoutes(t, restartStore(t, last, time.Hour, true, false), last, tz)
		if !slices.Equal(got, []string{"x-y 1/1 down true"}) {
			t.Errorf("tz %s: %v, want down", tz, got)
		}
		// y restarted at epoch 4 (new ID): no down yet.
		got = restartRoutes(t, restartStore(t, last, time.Hour, true, true), last, tz)
		for _, g := range got {
			if g[len(g)-4:] == "true" {
				t.Errorf("tz %s: restarted y read down: %v", tz, got)
			}
		}
	}
}
