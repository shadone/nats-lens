package storeprov_test

import (
	"math"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The Capacity page and the Streams page read bytes growth with two
// queries (streamPoints and streamGrowth). This pins them to one number
// where they could drift: a stream whose replicas report different bytes
// and no leader for most of the range, a leader for part of it, and
// epochs where one server reports no bytes. Written by the review of T2
// fix round 1.
func TestCapacityGrowthEqualsStreamsGrowth(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	st := emptyStore(t)
	r := rows{t, st}
	const acc = "APP"
	const n = 80 // 80 epochs of 10 minutes: over 13 hours
	at := func(e int64) time.Time { return last.Add(-time.Duration(n-e) * 10 * time.Minute) }
	for i, nm := range []string{"nats-0", "nats-1"} {
		r.add("servers", map[string]any{"server_id": []string{"S0", "S1"}[i], "server_name": nm, "cluster": "c1", "jetstream": true,
			"first_epoch": 1, "last_epoch": n, "first_seen_at": at(1), "last_seen_at": at(n)})
	}
	for e := int64(1); e <= n; e++ {
		if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, 2, 2, ['nats-0', 'nats-1'], [], [], 0, false, [], NULL)`,
			e, at(e), at(e).Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		lead := e > 30 && e < 45 // S0 leads in a stretch only
		// S0 grows fast, S1 slowly; S1 misses its bytes every 7th epoch.
		r.add("stream_stats", map[string]any{"epoch": e, "server_id": "S0", "account": acc, "stream": "orders", "messages": 5 * e,
			"bytes": 100_000 + 900*e*e, "is_leader": lead, "first_ts": at(e).Add(-time.Hour)})
		row := map[string]any{"epoch": e, "server_id": "S1", "account": acc, "stream": "orders", "messages": 9 * e,
			"bytes": 400_000 + 300*e, "is_leader": false, "first_ts": at(e).Add(-time.Hour)}
		if e%7 == 0 {
			row["bytes"] = nil
		}
		r.add("stream_stats", row)
	}
	for _, s := range []string{"S0", "S1"} {
		r.add("stream_config", map[string]any{"server_id": s, "account": acc, "stream": "orders", "first_epoch": 1, "last_epoch": n,
			"config_hash": "h", "retention": "limits", "storage": "file", "replicas": 2, "max_msgs": -1, "max_bytes": -1, "max_age_ns": 0,
			"max_msgs_per_subject": -1, "discard": "old"})
	}
	clock := last.Add(30 * time.Second)
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 10 * time.Minute, Live: true, Clock: func() time.Time { return clock }})
	for _, h := range []time.Duration{24, 12, 7} {
		rg := &v1.TimeRange{Start: timestamppb.New(clock.Add(-h * time.Hour)), End: timestamppb.New(clock)}
		cs := must(c.GetCapacity(ctx, connect.NewRequest(&v1.GetCapacityRequest{Range: rg}))).GetStreams()
		ls := must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{Range: rg}))).GetStreams()
		if len(cs) != 1 || len(ls) != 1 {
			t.Fatalf("%dh: %d capacity rows, %d streams", h, len(cs), len(ls))
		}
		a, b := cs[0].BytesPerDay, ls[0].BytesPerDay
		if a == nil || b == nil || math.Abs(*a-*b) > 1e-6*math.Abs(*b) {
			t.Errorf("%dh: growth %v on Capacity, %v on Streams", h, cs[0].GetBytesPerDay(), ls[0].GetBytesPerDay())
		}
	}
}
