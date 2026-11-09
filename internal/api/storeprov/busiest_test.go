package storeprov_test

import (
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// busyStore writes epochs 30 seconds apart over 20 minutes. Consumer
// c<i> is durable and its leader delivers i messages per second;
// follower-only has rows from a follower only, and quiet stopped
// delivering; slowed delivered 20 per second for 5 minutes, then 1.
func busyStore(t *testing.T) func(*v1.ListConsumersRequest) []*v1.ConsumerSummary {
	t.Helper()
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	r.server("S1", "nats-1")
	const n = 41
	names := []string{"follower-only", "quiet", "slowed"}
	for i := 1; i <= 8; i++ {
		names = append(names, fmt.Sprintf("c%d", i))
	}
	for _, name := range names {
		r.add("consumers", map[string]any{"account": "APP", "stream": "order-jobs", "consumer": name, "first_epoch": 1, "last_epoch": n})
		r.add("consumer_config", map[string]any{"server_id": "S0", "account": "APP", "stream": "order-jobs", "consumer": name,
			"first_epoch": 1, "last_epoch": n, "config_hash": "h", "durable_name": name})
		r.add("consumer_config", map[string]any{"server_id": "S1", "account": "APP", "stream": "order-jobs", "consumer": name,
			"first_epoch": 1, "last_epoch": n, "config_hash": "h", "durable_name": name})
	}
	for e := int64(1); e <= n; e++ {
		at := t0.Add(time.Duration(e-1) * 30 * time.Second)
		r.epoch(e, at, "S0", "S1")
		secs := int64(at.Sub(t0).Seconds())
		for i := 1; i <= 8; i++ {
			r.add("consumer_stats", map[string]any{"epoch": e, "server_id": "S0", "account": "APP", "stream": "order-jobs",
				"consumer": fmt.Sprintf("c%d", i), "is_leader": true, "created": t0.Add(-time.Hour), "delivered_consumer_seq": 100 + int64(i)*secs})
		}
		r.add("consumer_stats", map[string]any{"epoch": e, "server_id": "S0", "account": "APP", "stream": "order-jobs",
			"consumer": "quiet", "is_leader": true, "created": t0.Add(-time.Hour), "delivered_consumer_seq": 500})
		slowed := 20 * secs
		if secs > 300 {
			slowed = 6000 + secs - 300
		}
		r.add("consumer_stats", map[string]any{"epoch": e, "server_id": "S0", "account": "APP", "stream": "order-jobs",
			"consumer": "slowed", "is_leader": true, "created": t0.Add(-time.Hour), "delivered_consumer_seq": slowed})
		r.add("consumer_stats", map[string]any{"epoch": e, "server_id": "S1", "account": "APP", "stream": "order-jobs",
			"consumer": "follower-only", "is_leader": false, "created": t0.Add(-time.Hour), "delivered_consumer_seq": 900 + 50*secs})
	}
	end := t0.Add(n * 30 * time.Second)
	cl := serveSeries(t, st, end)
	return func(req *v1.ListConsumersRequest) []*v1.ConsumerSummary {
		req.Range = &v1.TimeRange{Start: timestamppb.New(t0.Add(-time.Minute)), End: timestamppb.New(end)}
		return must(cl.ListConsumers(ctx, connect.NewRequest(req))).GetConsumers()
	}
}

// DELIVERED_RATE with limit 6 returns the six fastest consumers, fastest
// first, with the rate over the last 15 minutes.
func TestBusiestConsumers(t *testing.T) {
	list := busyStore(t)
	cs := list(&v1.ListConsumersRequest{Sort: v1.ConsumerSort_CONSUMER_SORT_DELIVERED_RATE, Limit: 6})
	if len(cs) != 6 {
		t.Fatalf("%d consumers, want 6", len(cs))
	}
	for i, c := range cs {
		want := fmt.Sprintf("c%d", 8-i)
		if c.GetEntity().GetName() != want || c.DeliveredPerSecond == nil || c.GetDeliveredPerSecond() != float64(8-i) {
			t.Errorf("row %d = %s at %v/s, want %s at %d/s", i, c.GetEntity().GetName(), c.GetDeliveredPerSecond(), want, 8-i)
		}
	}
}

// A consumer without a leader row has no rate: unset, not 0, and it sorts
// after the consumers with one. A consumer that delivered nothing has 0.
func TestDeliveredRateUnsetWithoutLeader(t *testing.T) {
	list := busyStore(t)
	cs := list(&v1.ListConsumersRequest{Sort: v1.ConsumerSort_CONSUMER_SORT_DELIVERED_RATE})
	if len(cs) != 11 {
		t.Fatalf("%d consumers, want 11", len(cs))
	}
	last, quiet := cs[len(cs)-1], cs[len(cs)-2]
	if last.GetEntity().GetName() != "follower-only" || last.DeliveredPerSecond != nil {
		t.Errorf("last = %s with rate set %v, want follower-only with no rate", last.GetEntity().GetName(), last.DeliveredPerSecond != nil)
	}
	if quiet.GetEntity().GetName() != "quiet" || quiet.DeliveredPerSecond == nil || quiet.GetDeliveredPerSecond() != 0 {
		t.Errorf("before last = %s at %v (set %v), want quiet at 0", quiet.GetEntity().GetName(), quiet.GetDeliveredPerSecond(), quiet.DeliveredPerSecond != nil)
	}
}

// The default order is unchanged by the rate, and limit applies to it too.
func TestConsumerLimitWithDefaultOrder(t *testing.T) {
	list := busyStore(t)
	all := list(&v1.ListConsumersRequest{})
	two := list(&v1.ListConsumersRequest{Limit: 2})
	if len(two) != 2 || two[0].GetEntity().GetKey() != all[0].GetEntity().GetKey() || two[1].GetEntity().GetKey() != all[1].GetEntity().GetKey() {
		t.Fatalf("limit 2 = %v, want the first two of %d", two, len(all))
	}
}

// The rate covers only the 15 minutes before the range's end: a consumer
// that slowed down 15.5 minutes before it shows its new rate.
func TestDeliveredRateReadsTheLastFifteenMinutes(t *testing.T) {
	list := busyStore(t)
	for _, c := range list(&v1.ListConsumersRequest{}) {
		if c.GetEntity().GetName() == "slowed" {
			if c.DeliveredPerSecond == nil || c.GetDeliveredPerSecond() != 1 {
				t.Fatalf("slowed = %v/s (set %v), want 1", c.GetDeliveredPerSecond(), c.DeliveredPerSecond != nil)
			}
			return
		}
	}
	t.Fatal("no consumer slowed")
}
