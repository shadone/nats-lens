package checks

import (
	"fmt"
	"math"
	"testing"
	"time"
)

// expiryStream stores the stream events, max_age 1h, on S0 for collections
// 1..n: 10 messages a collection, so sequence s is first reported in
// collection ceil(s/10), and messages older than 120 collections removed.
func (f *fx) expiryStream(n int64) {
	f.streamConfig("S0", "events", 1, n, m{"max_age_ns": int64(time.Hour)})
	f.exec(`UPDATE stream_config SET subjects = ['events.>'] WHERE stream = 'events'`)
	for e := int64(1); e <= n; e++ {
		first := max(int64(1), 10*(e-120)+1)
		f.streamStats(e, "S0", "events", m{"is_leader": true, "last_seq": 10 * e, "first_seq": first,
			"first_ts": at((first + 9) / 10), "created": t0.Add(-48 * time.Hour)})
	}
}

// lagging stores a consumer of events whose oldest unacknowledged message
// is lag(e) collections old in collection e, for collections from..to.
func (f *fx) lagging(name string, from, to int64, lag func(e int64) int64, cols m) {
	for e := from; e <= to; e++ {
		d := 10 * (e - lag(e))
		f.consumerStats(e, "S0", "events", name, merge(m{"is_leader": true, "delivered_stream_seq": d,
			"ack_floor_stream_seq": d - 5, "num_ack_pending": 5, "num_pending": 10*e - d}, cols))
	}
}

func steady(n int64) func(int64) int64 { return func(int64) int64 { return n } }

// consumer-014 over 200 collections 30 seconds apart: max_age 1h is 120
// collections, the lookback 10m is 20.
func TestOldestUnreadAboutToExpire(t *testing.T) {
	f := newFx(t)
	f.cluster(200)
	f.expiryStream(200)
	durables := []string{"behind", "catching", "critical", "fresh", "boundary", "below", "filtered-sparse",
		"filtered-stalled", "same-filter", "lost-floor", "ephemeral", "leaderless", "short", "paused"}
	for _, name := range durables {
		cols := m{}
		if name == "ephemeral" {
			cols["durable_name"] = ""
		}
		f.consumerConfig("S0", "events", name, 1, 200, cols)
	}
	f.exec(`UPDATE consumer_config SET filter_subjects = ['events.eu'] WHERE consumer LIKE 'filtered-%'`)
	f.exec(`UPDATE consumer_config SET filter_subjects = ['events.>'] WHERE consumer = 'same-filter'`)

	// behind: 100 collections (50 minutes, 83% of max_age) behind, steadily.
	f.lagging("behind", 1, 200, steady(100), nil)
	// catching: 110 collections behind at 180, 100 at 200.
	f.lagging("catching", 1, 200, func(e int64) int64 {
		if e < 180 {
			return 110
		}
		return 110 - (e-180)/2
	}, nil)
	f.lagging("critical", 1, 200, steady(116), nil)
	f.lagging("fresh", 1, 200, steady(2), nil)
	// boundary: exactly 0.8 of max_age; below: just under.
	f.lagging("boundary", 1, 200, steady(96), nil)
	f.lagging("below", 1, 200, steady(95), nil)
	f.lagging("same-filter", 1, 200, steady(100), nil)
	f.lagging("ephemeral", 1, 200, steady(100), nil)
	f.lagging("leaderless", 1, 199, steady(100), nil)
	f.consumerStats(200, "S1", "events", "leaderless", m{"is_leader": false, "leader": ""})
	f.lagging("short", 195, 200, steady(100), nil)
	f.lagging("paused", 1, 200, steady(100), m{"paused": true})
	for e := int64(1); e <= 200; e++ {
		// filtered-sparse: its ack floor sits below messages that do not
		// match its filter; the one unacknowledged message was just delivered.
		f.consumerStats(e, "S0", "events", "filtered-sparse", m{"is_leader": true, "delivered_stream_seq": 10 * e,
			"ack_floor_stream_seq": 900, "num_ack_pending": 1, "num_pending": 0})
		// filtered-stalled: delivered everything up to 910, then nothing; the
		// backlog waits from collection 92 (54 minutes at 200).
		d, p := 10*e, int64(0)
		if e >= 91 {
			d, p = 910, 10*e-910
		}
		f.consumerStats(e, "S0", "events", "filtered-stalled", m{"is_leader": true, "delivered_stream_seq": d,
			"ack_floor_stream_seq": d, "num_ack_pending": 0, "num_pending": p})
		// lost-floor: stopped at 50 long ago; its next message is the
		// stream's first.
		f.consumerStats(e, "S0", "events", "lost-floor", m{"is_leader": true, "delivered_stream_seq": 50,
			"ack_floor_stream_seq": 50, "num_ack_pending": 0, "num_pending": 10*e - 50})
	}

	r := f.fires("consumer-014", 200, "APP/events/behind", "APP/events/critical", "APP/events/boundary",
		"APP/events/filtered-stalled", "APP/events/same-filter", "APP/events/lost-floor", "APP/events/paused")
	want(t, r["APP/events/behind"], "warning", "The oldest unread message is about 50 minutes old, 83% of max_age 1h",
		"it leaves the stream by 2026-10-17 10:49 UTC, in 10 minutes", "10 minutes earlier it was 50 minutes old",
		"1000 waiting, 5 unacknowledged.")
	if v := r["APP/events/behind"].Value; v == nil || *v != 0.8333 {
		t.Errorf("value = %v, want 0.8333", v)
	}
	if r["APP/events/behind"].RelatedConfig["max_age"] != "1h" {
		t.Errorf("related = %v", r["APP/events/behind"].RelatedConfig)
	}
	want(t, r["APP/events/critical"], "critical", "about 58 minutes old, 97% of max_age 1h")
	want(t, r["APP/events/boundary"], "warning", "80% of max_age")
	want(t, r["APP/events/filtered-stalled"], "warning", "at least 54 minutes old, 90%")
	want(t, r["APP/events/lost-floor"], "critical", "about 1 hour old, 99% of max_age 1h", "in 1 minute")
	want(t, r["APP/events/paused"], "warning", "; the consumer is paused.")
}

// A firing finding stays until the oldest unread message has used less
// than resolve_ratio (0.7) of max_age; one that was not firing needs
// warn_ratio.
func TestOldestUnreadHysteresis(t *testing.T) {
	f := newFx(t)
	f.cluster(200)
	f.expiryStream(200)
	for name, lag := range map[string]int64{"holding": 90, "dropped": 83, "new": 90} {
		f.consumerConfig("S0", "events", name, 1, 200, nil)
		f.lagging(name, 1, 200, steady(lag), nil)
	}
	for _, key := range []string{"APP/events/holding", "APP/events/dropped"} {
		f.row("findings", m{"id": "consumer-014:" + key, "check_code": "consumer-014", "entity_type": "consumer", "entity_key": key,
			"severity": "warning", "state": "firing", "last_transition": "firing", "since": at(150), "since_epoch": 150,
			"transitioned_at": at(150), "last_seen_epoch": 199, "updated_epoch": 199, "detail": "x"})
	}
	r := f.fires("consumer-014", 200, "APP/events/holding")
	want(t, r["APP/events/holding"], "warning", "about 45 minutes old, 75% of max_age 1h")
}

// A stream without max_age, and a consumer recreated within the lookback,
// are not judged.
func TestOldestUnreadNotJudged(t *testing.T) {
	f := newFx(t)
	f.cluster(200)
	f.expiryStream(200)
	f.streamConfig("S0", "keep", 1, 200, nil)
	for e := int64(1); e <= 200; e++ {
		f.streamStats(e, "S0", "keep", m{"is_leader": true, "last_seq": 10 * e, "first_seq": 1, "first_ts": at(1)})
	}
	f.consumerConfig("S0", "keep", "archiver", 1, 200, nil)
	for e := int64(1); e <= 200; e++ {
		d := 10 * (e - 100)
		f.consumerStats(e, "S0", "keep", "archiver", m{"is_leader": true, "delivered_stream_seq": d,
			"ack_floor_stream_seq": d - 5, "num_ack_pending": 5, "num_pending": 10*e - d})
	}
	f.consumerConfig("S0", "events", "renewed", 1, 200, nil)
	f.lagging("renewed", 1, 200, steady(100), nil)
	f.exec(`UPDATE consumer_stats SET created = ? WHERE consumer = 'renewed' AND epoch >= 195`, at(195))
	f.fires("consumer-014", 200)
}

// A filtered consumer's backlog older than its stream's window (max_age
// plus the lookback) is dated at the window's first collection: its
// history before that is not read, even beside a stream with a long
// max_age.
func TestOldestUnreadWindowBound(t *testing.T) {
	f := newFx(t)
	f.cluster(200)
	f.expiryStream(200)
	f.streamConfig("S0", "archive", 1, 200, m{"max_age_ns": int64(30 * 24 * time.Hour)})
	f.consumerConfig("S0", "archive", "reader", 1, 200, nil)
	f.consumerConfig("S0", "events", "stalled", 1, 200, nil)
	f.exec(`UPDATE consumer_config SET filter_subjects = ['events.eu'] WHERE consumer = 'stalled'`)
	for e := int64(1); e <= 200; e++ {
		f.streamStats(e, "S0", "archive", m{"is_leader": true, "last_seq": 10 * e, "first_seq": 1, "first_ts": at(1)})
		f.consumerStats(e, "S0", "archive", "reader", m{"is_leader": true, "delivered_stream_seq": 10 * e,
			"ack_floor_stream_seq": 10 * e, "num_ack_pending": 0, "num_pending": 0})
		f.consumerStats(e, "S0", "events", "stalled", m{"is_leader": true, "delivered_stream_seq": 5,
			"ack_floor_stream_seq": 5, "num_ack_pending": 0, "num_pending": 10*e - 5})
	}
	r := f.fires("consumer-014", 200, "APP/events/stalled")
	// The window starts 1h10m before collection 200, at collection 61.
	want(t, r["APP/events/stalled"], "critical", "at least 1.2 hours old, 117% of max_age 1h")
}

// Deleted sequences after the consumer's position (a KV overwrite, a purge
// by subject, a work queue) can make the sequence after it much older than
// its next message. With num_deleted above 0 the check takes the bounds that
// hold anyway: here the backlog's start, 10.5 minutes ago, not the deleted
// sequence 501 (gone, so the stream's first message stands in, about
// an hour old).
func TestOldestUnreadDeletedSequences(t *testing.T) {
	f := newFx(t)
	f.cluster(200)
	f.expiryStream(200)
	f.exec(`UPDATE stream_stats SET num_deleted = 900 WHERE stream = 'events'`)
	f.consumerConfig("S0", "events", "stalled", 1, 200, nil)
	for e := int64(1); e <= 200; e++ {
		p := int64(0)
		if e > 178 {
			p = 3
		}
		f.consumerStats(e, "S0", "events", "stalled", m{"is_leader": true, "delivered_stream_seq": 500,
			"ack_floor_stream_seq": 500, "num_ack_pending": 0, "num_pending": p})
	}
	f.fires("consumer-014", 200)
}

// When the message after the ack floor has left the stream, the newest
// delivered message bounds the age, which then reads "at least".
func TestOldestUnreadFloorGone(t *testing.T) {
	f := newFx(t)
	f.cluster(200)
	f.expiryStream(200)
	f.consumerConfig("S0", "events", "worker", 1, 200, nil)
	for e := int64(1); e <= 200; e++ {
		d := 10 * (e - 100)
		f.consumerStats(e, "S0", "events", "worker", m{"is_leader": true, "delivered_stream_seq": d,
			"ack_floor_stream_seq": 50, "num_ack_pending": 5, "num_pending": 10*e - d})
	}
	r := f.fires("consumer-014", 200, "APP/events/worker")
	want(t, r["APP/events/worker"], "warning", "The oldest unread message is at least 50 minutes old")
}

// A filtered consumer stalled at the same delivered sequence whose backlog
// emptied (its messages aged out) and filled again: the backlog starts at
// the refill, collection 180, not at its first backlog in collection 1.
func TestOldestUnreadRefilledBacklog(t *testing.T) {
	f := newFx(t)
	f.cluster(200)
	f.expiryStream(200)
	f.consumerConfig("S0", "events", "refill", 1, 200, nil)
	f.exec(`UPDATE consumer_config SET filter_subjects = ['events.eu'] WHERE consumer = 'refill'`)
	for e := int64(1); e <= 200; e++ {
		p := int64(3)
		switch {
		case e > 100 && e < 180:
			p = 0
		case e >= 180:
			p = e - 179
		}
		f.consumerStats(e, "S0", "events", "refill", m{"is_leader": true, "delivered_stream_seq": 500,
			"ack_floor_stream_seq": 500, "num_ack_pending": 0, "num_pending": p})
	}
	f.fires("consumer-014", 200)
	r := f.eval("consumer-014", 200, map[string]string{"warn_ratio": "0.1", "resolve_ratio": "0.05"})
	want(t, r["APP/events/refill"], "warning", "at least 10 minutes old, 17% of max_age 1h")
}

// stalledFiltered stores a filtered consumer of events stalled at
// delivered sequence 500, whose messages waiting in collection e are
// pending(e); a negative value leaves the collection without a leader row.
func (f *fx) stalledFiltered(name string, n int64, pending func(e int64) int64) {
	f.consumerConfig("S0", "events", name, 1, n, nil)
	f.exec(`UPDATE consumer_config SET filter_subjects = ['events.eu'] WHERE consumer = ?`, name)
	for e := int64(1); e <= n; e++ {
		if p := pending(e); p >= 0 {
			f.consumerStats(e, "S0", "events", name, m{"is_leader": true, "delivered_stream_seq": 500,
				"ack_floor_stream_seq": 500, "num_ack_pending": 0, "num_pending": p})
		}
	}
}

// One collection with no message waiting, as a new leader may report, does
// not restart the age of a stalled backlog; two in a row do.
func TestOldestUnreadSingleEmptyCollection(t *testing.T) {
	f := newFx(t)
	f.cluster(400)
	f.expiryStream(400)
	f.stalledFiltered("glitch", 400, func(e int64) int64 {
		if e == 300 {
			return 0
		}
		return 3
	})
	f.stalledFiltered("emptied", 400, func(e int64) int64 {
		if e == 300 || e == 301 {
			return 0
		}
		return 3
	})
	r := f.fires("consumer-014", 400, "APP/events/glitch", "APP/events/emptied")
	// The window starts 1h10m before collection 400.
	want(t, r["APP/events/glitch"], "critical", "at least 1.2 hours old, 117% of max_age 1h")
	// The backlog starts at 302, after the two empty collections.
	want(t, r["APP/events/emptied"], "warning", "at least 49 minutes old, 82% of max_age 1h")
}

// A leader that did not report for longer than the three blocks of 64
// collections after the backlog emptied: the refill is still found, on a
// stream whose max_age keeps the empty collections in the window.
func TestOldestUnreadRefillAfterLongGap(t *testing.T) {
	f := newFx(t)
	f.cluster(400)
	f.expiryStream(400)
	f.exec(`UPDATE stream_config SET max_age_ns = ? WHERE stream = 'events'`, int64(30*24*time.Hour))
	f.stalledFiltered("gap", 400, func(e int64) int64 {
		switch {
		case e == 99 || e == 100:
			return 0
		case e > 100 && e < 350:
			return -1
		}
		return 3
	})
	r := f.eval("consumer-014", 400, map[string]string{"warn_ratio": "0.0001", "resolve_ratio": "0.00005"})
	want(t, r["APP/events/gap"], "warning", "at least 25 minutes old")
}

// Refinement includes the coarse epoch and the preceding 63 epochs,
// including when the earliest lookback point has no later coarse probe.
func TestOldestUnreadBacklogProbeBoundaries(t *testing.T) {
	f := newFx(t)
	f.cluster(400)
	f.expiryStream(400)
	starts := []int64{263, 319, 320, 321, 379}
	for _, start := range starts {
		f.stalledFiltered(fmt.Sprintf("start-%d", start), 400, func(e int64) int64 {
			if e < start {
				return -1
			}
			return 3
		})
	}
	got := f.eval("consumer-014", 400, map[string]string{"warn_ratio": "0.01", "resolve_ratio": "0.005"})
	if len(got) != len(starts) {
		t.Fatalf("findings = %s", rowsText(got))
	}
	for _, start := range starts {
		key := fmt.Sprintf("APP/events/start-%d", start)
		row, ok := got[key]
		if !ok {
			t.Errorf("missing %s", key)
			continue
		}
		ratio := math.Round(float64(400-start)/120*1e4) / 1e4
		if row.Value == nil || *row.Value != ratio {
			t.Errorf("%s value = %v, want %g", key, row.Value, ratio)
		}
	}
}
