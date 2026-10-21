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

// A leader that reports its stream empty (no first message) beside a
// follower that has not yet applied the purge: the row's messages and
// bytes are the leader's, so its first message should be the leader's
// (none), not the follower's. Written by the review of T2 fix round 1.
func TestCapacityEmptyLeaderHasNoFirstMessage(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	st := emptyStore(t)
	r := rows{t, st}
	for i, nm := range []string{"nats-0", "nats-1"} {
		r.add("servers", map[string]any{"server_id": []string{"S0", "S1"}[i], "server_name": nm, "cluster": "c1", "jetstream": true,
			"first_epoch": 1, "last_epoch": 1, "first_seen_at": last, "last_seen_at": last})
	}
	if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (1, ?, ?, 0, 2, 2, ['nats-0', 'nats-1'], [], [], 0, false, [], NULL)`,
		last, last.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	r.add("stream_stats", map[string]any{"epoch": int64(1), "server_id": "S0", "account": "APP", "stream": "orders", "messages": 0,
		"bytes": 0, "is_leader": true, "leader": "nats-0"})
	r.add("stream_stats", map[string]any{"epoch": int64(1), "server_id": "S1", "account": "APP", "stream": "orders", "messages": 40,
		"bytes": 4000, "is_leader": false, "leader": "nats-0", "first_ts": last.Add(-30 * time.Hour)})
	clock := last.Add(30 * time.Second)
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 10 * time.Minute, Live: true, Clock: func() time.Time { return clock }})
	rg := &v1.TimeRange{Start: timestamppb.New(last.Add(-time.Hour)), End: timestamppb.New(clock)}
	s := must(c.GetCapacity(ctx, connect.NewRequest(&v1.GetCapacityRequest{Range: rg}))).GetStreams()[0]
	if s.GetMessages() != 0 || s.FirstTs != nil {
		t.Errorf("messages %d (the leader's) with first_ts %v (the follower's)", s.GetMessages(), s.GetFirstTs().AsTime())
	}
}
