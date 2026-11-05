package checks

import (
	"testing"
	"time"
)

// mirrorAt stores the leader's report of a mirror stream in epoch e.
func (f *fx) mirrorAt(e int64, stream string, lag int64, active time.Duration, cols m) {
	f.streamStats(e, "S0", stream, merge(m{"leader": "nats-0", "is_leader": true, "mirror_lag": lag,
		"mirror_active_ns": active.Nanoseconds(), "created": t0.Add(-24 * time.Hour)}, cols))
}

// firingAt stores a finding of code for key that fired and was last
// returned in epoch.
func (f *fx) firingAt(code, key string, epoch int64) {
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
		f.mirrorAt(e, "order-archive", 40000+e*1000, ok, nil)
		// A KV bucket mirror is a stream like any other.
		f.mirrorAt(e, "KV_stock-copy", 20000, ok, nil)
		// Behind but falling: a mirror catching up is not reported.
		f.mirrorAt(e, "catching-up", 200000-e*10000, ok, nil)
		f.mirrorAt(e, "fine", 3, ok, nil)
		// Exactly max_lag is not above it.
		f.mirrorAt(e, "at-limit", 10000, ok, nil)
		// Seen in the last minute only: too little of the lookback.
		if e >= 9 {
			f.mirrorAt(e, "short", 90000, ok, nil)
		}
		// A follower reports no mirror state worth reading.
		f.streamStats(e, "S1", "fine", m{"leader": "nats-0", "is_leader": false, "mirror_lag": 99999})
		// A plain stream has no mirror lag.
		f.streamStats(e, "S0", "order-jobs", m{"leader": "nats-0", "is_leader": true})
	}
	f.mirrorAt(11, "unheard", 0, 3*time.Minute, nil)
	f.mirrorAt(11, "never", 0, -1, nil)
	r := f.fires("stream-015", 11, "APP/order-archive", "APP/KV_stock-copy", "APP/unheard", "APP/never")
	want(t, r["APP/order-archive"], "warning", "Mirror of order-events on leader nats-0: 51000 messages behind",
		"not catching up over the last 5m (41000 at its start)")
	if r["APP/order-archive"].Value == nil || *r["APP/order-archive"].Value != 51000 {
		t.Errorf("value = %v", r["APP/order-archive"].Value)
	}
	want(t, r["APP/unheard"], "warning", "nothing received from its origin for 3 minutes")
	want(t, r["APP/never"], "warning", "has never heard from its origin")
	// A mirror created within max_inactive has not had time to hear.
	g := newFx(t)
	g.cluster(1)
	g.mirrorAt(1, "new", 0, -1, m{"created": at(1).Add(-time.Minute)})
	g.fires("stream-015", 1)
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
		f.mirrorAt(e, "keeps", 9000, ok, nil)
		f.mirrorAt(e, "resolves", 7000, ok, nil)
		f.mirrorAt(e, "new", 9000, ok, nil)
		if e < 12 {
			f.mirrorAt(e, "away", 50000, ok, nil)
		}
		f.mirrorAt(e, "quiet", 0, 100*time.Second, nil)
	}
	for _, s := range []string{"keeps", "resolves", "away", "quiet"} {
		f.firingAt("stream-015", "APP/"+s, 11)
	}
	r := f.fires("stream-015", 12, "APP/keeps", "APP/away", "APP/quiet")
	want(t, r["APP/keeps"], "warning", "9000 messages behind.")
	want(t, r["APP/away"], "warning", "50000 messages behind (no leader report in this collection)")
	want(t, r["APP/quiet"], "warning", "nothing received from its origin for 2 minutes")
}
