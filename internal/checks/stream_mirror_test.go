package checks

import (
	"testing"
	"time"
)

// streamMirrorAt stores the leader's report of a mirror stream in epoch e.
func (f *fx) streamMirrorAt(e int64, stream string, lag int64, active time.Duration, cols m) {
	f.streamStats(e, "S0", stream, merge(m{"leader": "nats-0", "is_leader": true, "mirror_lag": lag,
		"mirror_active_ns": active.Nanoseconds(), "created": t0.Add(-24 * time.Hour)}, cols))
}

// streamFiringAt stores a finding of code for key that fired and was last
// returned in epoch.
func (f *fx) streamFiringAt(code, key string, epoch int64) {
	f.row("findings", m{"id": code + ":" + key, "check_code": code, "entity_type": "stream", "entity_key": key,
		"severity": "warning", "state": "firing", "last_transition": "firing", "since": at(1), "since_epoch": 1,
		"transitioned_at": at(1), "last_seen_epoch": epoch, "updated_epoch": epoch, "detail": "x"})
}

// The lookback of 5m at 30-second epochs covers epochs 1 to 11 at epoch 11.
func TestMirrorLagging(t *testing.T) {
	f := newFx(t)
	f.cluster(11)
	for _, s := range []string{"order-archive", "catching-up", "fine", "at-limit", "unheard", "never", "short", "KV_stock-copy"} {
		f.streamConfig("S0", s, 1, 11, m{"mirror": "order-events"})
	}
	f.streamConfig("S0", "order-jobs", 1, 11, nil)
	ok := 500 * time.Millisecond
	for e := int64(1); e <= 11; e++ {
		// Behind and growing: fires.
		f.streamMirrorAt(e, "order-archive", 40000+e*1000, ok, nil)
		// A KV bucket mirror is a stream like any other.
		f.streamMirrorAt(e, "KV_stock-copy", 20000, ok, nil)
		// Behind but falling: a mirror catching up is not reported.
		f.streamMirrorAt(e, "catching-up", 200000-e*10000, ok, nil)
		f.streamMirrorAt(e, "fine", 3, ok, nil)
		// Exactly max_lag is not above it.
		f.streamMirrorAt(e, "at-limit", 10000, ok, nil)
		// Seen in the last minute only: too little of the lookback.
		if e >= 9 {
			f.streamMirrorAt(e, "short", 90000, ok, nil)
		}
		// A follower reports no mirror state worth reading.
		f.streamStats(e, "S1", "fine", m{"leader": "nats-0", "is_leader": false, "mirror_lag": 99999})
		// A plain stream has no mirror lag.
		f.streamStats(e, "S0", "order-jobs", m{"leader": "nats-0", "is_leader": true})
	}
	f.streamMirrorAt(11, "unheard", 0, 3*time.Minute, nil)
	// Never heard in the two newest reports, under a leader elected a day ago.
	f.streamMirrorAt(10, "never", 0, -1, m{"leader_since": t0.Add(-24 * time.Hour)})
	f.streamMirrorAt(11, "never", 0, -1, m{"leader_since": t0.Add(-24 * time.Hour)})
	r := f.fires("stream-015", 11, "APP/order-archive", "APP/KV_stock-copy", "APP/unheard", "APP/never")
	want(t, r["APP/order-archive"], "warning", "Mirror of order-events on leader nats-0: 51000 messages behind",
		"not catching up over the last 5m (41000 at its start)")
	if r["APP/order-archive"].Value == nil || *r["APP/order-archive"].Value != 51000 {
		t.Errorf("value = %v", r["APP/order-archive"].Value)
	}
	want(t, r["APP/unheard"], "warning", "nothing received from its origin for 3 minutes")
	want(t, r["APP/never"], "warning", "has never heard from its origin since 08:00:00 UTC, when its leader was elected or its server started.")
	// A leader elected within max_inactive has not had time to hear, even
	// in two reports.
	g := newFx(t)
	g.cluster(2)
	for e := int64(1); e <= 2; e++ {
		g.streamMirrorAt(e, "new", 0, -1, m{"leader_since": at(2).Add(-time.Minute)})
	}
	g.fires("stream-015", 2)
	// A lower max_lag catches the steady one.
	if r := f.eval("stream-015", 11, map[string]string{"max_lag": "9999"}); len(r) != 5 {
		t.Fatalf("max_lag 9999: %s", rowsText(r))
	}
}

// A firing finding stays down to resolve_ratio (0.8) of max_lag and
// through a collection without a leader report; a new one needs the full
// threshold.
func TestMirrorLaggingResolve(t *testing.T) {
	f := newFx(t)
	f.cluster(12)
	for _, s := range []string{"keeps", "resolves", "new", "away", "quiet"} {
		f.streamConfig("S0", s, 1, 12, m{"mirror": "order-events"})
	}
	ok := 500 * time.Millisecond
	for e := int64(1); e <= 12; e++ {
		f.streamMirrorAt(e, "keeps", 9000, ok, nil)
		f.streamMirrorAt(e, "resolves", 7000, ok, nil)
		f.streamMirrorAt(e, "new", 9000, ok, nil)
		if e < 12 {
			f.streamMirrorAt(e, "away", 50000, ok, nil)
		}
		f.streamMirrorAt(e, "quiet", 0, 100*time.Second, nil)
	}
	for _, s := range []string{"keeps", "resolves", "away", "quiet"} {
		f.streamFiringAt("stream-015", "APP/"+s, 11)
	}
	r := f.fires("stream-015", 12, "APP/keeps", "APP/away", "APP/quiet")
	want(t, r["APP/keeps"], "warning", "9000 messages behind.")
	want(t, r["APP/away"], "warning", "50000 messages behind (no leader report in this collection)")
	want(t, r["APP/quiet"], "warning", "nothing received from its origin for 2 minutes")
}

// A new mirror leader reports that it never heard from the origin until
// its copy of the origin starts: one such report after a leader change is
// not a finding.
func TestMirrorLeaderChangeIsNotNeverHeard(t *testing.T) {
	f := newFx(t)
	f.cluster(11)
	f.streamConfig("S0", "order-archive", 1, 11, m{"mirror": "order-events"})
	for e := int64(1); e <= 10; e++ {
		f.streamMirrorAt(e, "order-archive", 0, 500*time.Millisecond, nil)
	}
	f.streamMirrorAt(11, "order-archive", 0, -1, m{"leader_since": at(11).Add(-2 * time.Second)})
	f.fires("stream-015", 11)
	// Nor one under a leader elected long ago: it needs two reports.
	g := newFx(t)
	g.cluster(11)
	g.streamConfig("S0", "order-archive", 1, 11, m{"mirror": "order-events"})
	for e := int64(1); e <= 10; e++ {
		g.streamMirrorAt(e, "order-archive", 0, 500*time.Millisecond, nil)
	}
	g.streamMirrorAt(11, "order-archive", 0, -1, nil)
	g.fires("stream-015", 11)
}

// An R1 mirror whose server restarted reports that it never heard from its
// origin until its copy starts again; the server's start time counts like
// a leader election.
func TestMirrorServerRestartIsNotNeverHeard(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 11, "nats-0")
	f.server("S0", "nats-0", 1, 11, at(10).Add(-30*time.Second), "2.15.0")
	f.streamConfig("S0", "order-archive", 1, 11, m{"mirror": "order-events", "replicas": 1})
	for e := int64(10); e <= 11; e++ {
		f.streamMirrorAt(e, "order-archive", 0, -1, m{"leader_since": t0.Add(-24 * time.Hour)})
	}
	f.fires("stream-015", 11)
}
