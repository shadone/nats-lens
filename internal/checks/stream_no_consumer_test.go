package checks

import (
	"testing"
	"time"
)

// streamLeaderAt stores the leader's report of a stream in epoch e.
func (f *fx) streamLeaderAt(e int64, stream string, cols m) {
	f.streamStats(e, "S0", stream, merge(m{"leader": "nats-0", "is_leader": true, "consumer_count": 0, "messages": 0,
		"last_seq": 100, "created": t0.Add(-24 * time.Hour)}, cols))
}

// The lookback of 10m at 30-second epochs covers epochs 1 to 21 at epoch
// 21.
func TestStreamWithoutConsumer(t *testing.T) {
	f := newFx(t)
	f.cluster(21)
	wq, interest := m{"retention": "workqueue"}, m{"retention": "interest"}
	for s, cfg := range map[string]m{"order-callbacks": wq, "media-windows": wq, "account-changes": interest,
		"idle-interest": interest, "order-jobs": nil, "had-one": wq, "young": wq, "short": wq, "named": wq} {
		f.streamConfig("S0", s, 1, 21, cfg)
	}
	for e := int64(1); e <= 21; e++ {
		f.streamLeaderAt(e, "order-callbacks", m{"messages": 50})
		f.streamLeaderAt(e, "media-windows", nil)
		f.streamLeaderAt(e, "account-changes", m{"last_seq": 100 + e*12})
		f.streamLeaderAt(e, "idle-interest", nil)
		f.streamLeaderAt(e, "order-jobs", m{"messages": 50})
		f.streamLeaderAt(e, "named", m{"messages": 50})
		count := 0
		if e == 10 {
			count = 1
		}
		f.streamLeaderAt(e, "had-one", m{"consumer_count": count, "messages": 50})
		f.streamLeaderAt(e, "young", m{"messages": 50, "created": at(15)})
		if e >= 18 {
			f.streamLeaderAt(e, "short", m{"messages": 50})
		}
	}
	f.row("findings", m{"id": "consumer-010:APP/named/callback-delivery", "check_code": "consumer-010", "entity_type": "consumer",
		"entity_key": "APP/named/callback-delivery", "severity": "critical", "state": "firing", "last_transition": "firing",
		"since": at(1), "since_epoch": 1, "transitioned_at": at(1), "last_seen_epoch": 20, "updated_epoch": 20, "detail": "x"})
	r := f.fires("stream-020", 21, "APP/order-callbacks", "APP/media-windows", "APP/account-changes", "APP/idle-interest")
	want(t, r["APP/order-callbacks"], "warning",
		"Work-queue stream with no consumer for at least 10m (leader nats-0): 50 messages wait, and nothing removes them until a consumer takes them.")
	want(t, r["APP/media-windows"], "info", "it is empty")
	want(t, r["APP/account-changes"], "warning", "Interest stream", "the server drops every message published to it, 240 within the last 10m.")
	want(t, r["APP/idle-interest"], "info", "nothing was published to it within the last 10m")
	// With less than half the lookback observed, 4.5 minutes, nothing is
	// reported.
	f.fires("stream-020", 10)
}

// A firing finding stays until a leader report counts a consumer, also
// through a collection without a leader report.
func TestStreamWithoutConsumerResolve(t *testing.T) {
	f := newFx(t)
	f.cluster(21)
	for _, s := range []string{"keeps", "back", "away"} {
		f.streamConfig("S0", s, 1, 21, m{"retention": "workqueue"})
	}
	for e := int64(1); e <= 21; e++ {
		count := 0
		if e == 5 {
			count = 1
		}
		f.streamLeaderAt(e, "keeps", m{"consumer_count": count, "messages": 5})
		if e == 21 {
			count = 1
		}
		f.streamLeaderAt(e, "back", m{"consumer_count": count, "messages": 5})
		if e < 21 {
			f.streamLeaderAt(e, "away", m{"messages": 5})
		}
	}
	for _, s := range []string{"keeps", "back", "away"} {
		f.streamFiringAt("stream-020", "APP/"+s, 20)
	}
	r := f.fires("stream-020", 21, "APP/keeps", "APP/away")
	want(t, r["APP/away"], "warning", "5 messages wait", "(no leader report in this collection)")
}
