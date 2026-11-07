package storeprov_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// A stream no server reports as leader shows the same numbers on the
// Capacity page as on the Streams page (the largest report), and its
// account by its readable name. Written by the review of T2.
func TestCapacityLeaderlessAgreesWithStreams(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	st := emptyStore(t)
	r := rows{t, st}
	const acc = "ADSHOPKEYEXAMPLE"
	at := func(e int64) time.Time { return last.Add(-time.Duration(40-e) * 10 * time.Minute) }
	for i, n := range []string{"nats-0", "nats-1"} {
		r.add("servers", map[string]any{"server_id": []string{"S0", "S1"}[i], "server_name": n, "cluster": "c1", "jetstream": true,
			"first_epoch": 1, "last_epoch": 40, "first_seen_at": at(1), "last_seen_at": at(40)})
	}
	r.add("accounts", map[string]any{"account": acc, "account_name": "shop", "is_system": false, "first_epoch": 1, "last_epoch": 40})
	for e := int64(1); e <= 40; e++ {
		if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, 2, 2, ['nats-0', 'nats-1'], [], [], 0, false, [], NULL)`,
			e, at(e), at(e).Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		// No leader flag on either server; nats-1 reports more.
		r.add("stream_stats", map[string]any{"epoch": e, "server_id": "S0", "account": acc, "stream": "orders", "messages": 5 * e, "bytes": 500 * e,
			"first_ts": at(e).Add(-time.Hour)})
		r.add("stream_stats", map[string]any{"epoch": e, "server_id": "S1", "account": acc, "stream": "orders", "messages": 9 * e, "bytes": 900 * e,
			"first_ts": at(e).Add(-time.Hour)})
	}
	for _, s := range []string{"S0", "S1"} {
		r.add("stream_config", map[string]any{"server_id": s, "account": acc, "stream": "orders", "first_epoch": 1, "last_epoch": 40,
			"config_hash": "h", "retention": "limits", "storage": "file", "replicas": 2, "max_msgs": -1, "max_bytes": -1, "max_age_ns": 0,
			"max_msgs_per_subject": -1, "discard": "old"})
	}
	clock := last.Add(30 * time.Second)
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 10 * time.Minute, Live: true, Clock: func() time.Time { return clock }})
	rg := &v1.TimeRange{Start: timestamppb.New(last.Add(-24 * time.Hour)), End: timestamppb.New(clock)}
	res := must(c.GetCapacity(ctx, connect.NewRequest(&v1.GetCapacityRequest{Range: rg})))
	if len(res.GetStreams()) != 1 {
		t.Fatalf("streams = %v", res.GetStreams())
	}
	s := res.GetStreams()[0]
	if s.GetAccountName() != "shop" {
		t.Errorf("account_name = %q, want the readable name shop", s.GetAccountName())
	}
	ls := must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{Range: rg}))).GetStreams()
	t.Logf("capacity bytes %d /day %v; ListStreams bytes %d /day %v", s.GetBytes(), s.GetBytesPerDay(), ls[0].GetBytes(), ls[0].GetBytesPerDay())
	if s.GetBytes() != ls[0].GetBytes() {
		t.Errorf("bytes %d on Capacity, %d on Streams for the same epoch", s.GetBytes(), ls[0].GetBytes())
	}
}
