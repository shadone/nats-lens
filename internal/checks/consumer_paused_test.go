package checks

import (
	"testing"
	"time"
)

// consumer-017 with max_paused 10m over collections 30 seconds apart.
func TestConsumerPausedLong(t *testing.T) {
	f := newFx(t)
	f.cluster(41)
	created := t0.Add(-24 * time.Hour)
	stat := func(e int64, name string, paused bool, pending int64, cols m) {
		f.consumerStats(e, "S0", "jobs", name, merge(m{"is_leader": true, "paused": paused, "num_pending": pending}, cols))
		f.consumerStats(e, "S1", "jobs", name, merge(m{"is_leader": false, "paused": paused}, cols))
	}
	for e := int64(1); e <= 41; e++ {
		// long: paused since collection 5 with messages waiting.
		stat(e, "long", e >= 5, 10, nil)
		// idle: paused as long, nothing waiting.
		stat(e, "idle", e >= 5, 0, nil)
		// short: paused for 5 minutes.
		stat(e, "short", e >= 31, 10, nil)
		// boundary: running until 19, paused from 20: 10 minutes at 40.
		stat(e, "boundary", e >= 20, 10, nil)
		// recreated: paused all along, created again at collection 35.
		c := created
		if e >= 35 {
			c = at(35)
		}
		stat(e, "recreated", true, 10, m{"created": c})
		// late: first reported at 30, already paused; the pause event is older.
		if e >= 30 {
			stat(e, "late", true, 10, nil)
		}
		// extended: paused at 5, the pause extended at 30.
		stat(e, "extended", e >= 5, 10, nil)
	}
	// leaderless: paused since 5; in 41 no server is its leader.
	for e := int64(1); e <= 40; e++ {
		stat(e, "leaderless", e >= 5, 10, nil)
	}
	f.consumerStats(41, "S1", "jobs", "leaderless", m{"is_leader": false, "paused": true, "leader": ""})

	acct := m{"account": "APP", "stream": "jobs"}
	// boundary: an older pause and its resume, before it was last seen running.
	f.event(1, at(2), "consumer.paused", merge(acct, m{"consumer": "boundary"}))
	f.event(2, at(3), "consumer.resumed", merge(acct, m{"consumer": "boundary"}))
	f.event(3, at(10), "consumer.paused", merge(acct, m{"consumer": "late"}))
	f.event(4, at(5), "consumer.paused", merge(acct, m{"consumer": "extended"}))
	f.event(5, at(30), "consumer.paused", merge(acct, m{"consumer": "extended"}))
	f.consumerConfig("S0", "jobs", "long", 1, 41, m{"config": `{"pause_until":"2026-10-17T12:00:00Z"}`})

	over := map[string]string{"max_paused": "10m"}
	got := f.eval("consumer-017", 40, over)
	if _, ok := got["APP/jobs/boundary"]; ok {
		t.Errorf("boundary fired at exactly max_paused: %s", rowsText(got))
	}
	got = f.eval("consumer-017", 41, over)
	keys := []string{"APP/jobs/long", "APP/jobs/idle", "APP/jobs/boundary", "APP/jobs/late", "APP/jobs/extended", "APP/jobs/leaderless"}
	if len(got) != len(keys) {
		t.Fatalf("got %s, want %v", rowsText(got), keys)
	}
	want(t, got["APP/jobs/long"], "warning", "Paused since 2026-10-17 09:02:00 UTC, for 18 minutes",
		"the pause ends 2026-10-17 12:00 UTC, in 2.7 hours", "10 messages waiting")
	want(t, got["APP/jobs/idle"], "info", "0 messages waiting")
	want(t, got["APP/jobs/boundary"], "warning", "Paused since 2026-10-17 09:09:30 UTC, for 11 minutes")
	want(t, got["APP/jobs/late"], "warning", "Paused since 2026-10-17 09:04:30 UTC")
	if got["APP/jobs/late"].CauseEventID != 3 {
		t.Errorf("late cause = %d, want 3", got["APP/jobs/late"].CauseEventID)
	}
	want(t, got["APP/jobs/extended"], "warning", "Paused since 2026-10-17 09:02:00 UTC")
	if got["APP/jobs/extended"].CauseEventID != 4 {
		t.Errorf("extended cause = %d, want 4, the pause that began it", got["APP/jobs/extended"].CauseEventID)
	}
	want(t, got["APP/jobs/leaderless"], "info", "no leader report of the messages waiting")
	if cs := got["APP/jobs/long"].ConditionSince; cs == nil || !cs.Equal(at(5)) {
		t.Errorf("condition since = %v, want %v", cs, at(5))
	}
}

// A resume ends the pause: a consumer paused again later counts from the
// new pause, not from the first.
func TestConsumerPausedAfterResume(t *testing.T) {
	f := newFx(t)
	f.cluster(41)
	for e := int64(1); e <= 41; e++ {
		// The collections between the resume and the new pause missed the
		// running state: paused in every one of them.
		f.consumerStats(e, "S0", "jobs", "worker", m{"is_leader": true, "paused": true, "num_pending": 1})
	}
	acct := m{"account": "APP", "stream": "jobs", "consumer": "worker"}
	f.event(1, at(1).Add(-time.Hour), "consumer.paused", acct)
	f.event(2, at(30), "consumer.resumed", acct)
	f.event(3, at(30).Add(10*time.Second), "consumer.paused", acct)
	r := f.eval("consumer-017", 41, map[string]string{"max_paused": "5m"})
	want(t, r[jobsKey], "warning", "Paused since 2026-10-17 09:14:40 UTC, for 5 minutes")
	if r[jobsKey].CauseEventID != 3 {
		t.Errorf("cause = %d, want 3, the pause after the resume", r[jobsKey].CauseEventID)
	}
	f.fires("consumer-017", 41)
}

// pause_until is read with its offset and fraction, and shown in UTC.
func TestConsumerPausedUntilOffset(t *testing.T) {
	f := newFx(t)
	f.cluster(41)
	for name, until := range map[string]string{"z": "2026-10-17T12:00:00Z", "frac": "2026-10-17T12:00:00.123456789Z",
		"off": "2026-10-17T14:00:00+02:00"} {
		for e := int64(1); e <= 41; e++ {
			f.consumerStats(e, "S0", "jobs", name, m{"is_leader": true, "paused": true, "num_pending": 3})
		}
		f.consumerConfig("S0", "jobs", name, 1, 41, m{"config": `{"pause_until":"` + until + `"}`})
	}
	r := f.eval("consumer-017", 41, map[string]string{"max_paused": "10m"})
	for _, k := range []string{"APP/jobs/z", "APP/jobs/frac", "APP/jobs/off"} {
		want(t, r[k], "warning", "the pause ends 2026-10-17 12:00 UTC, in 2.7 hours")
	}
}
