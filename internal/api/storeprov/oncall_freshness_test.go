package storeprov_test

import (
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestReviewOnCallStaleDelivery(t *testing.T) {
	st, set := conformanceStore(t)
	if _, err := st.DB().Exec(`UPDATE stream_stats SET last_seq = epoch * 30 WHERE stream = 'orders' AND is_leader`); err != nil {
		t.Fatal(err)
	}
	for i := int64(1); i <= 18; i++ {
		addEpochAt(t, st, int64(confEpochs)+i, confAt(confEpochs).Add(time.Duration(i)*30*time.Second))
	}
	if _, err := st.ApplyCheckRuns(ctx, int64(confEpochs)+18, []store.CheckRun{{Code: "review-gap", EntityType: "consumer", Status: store.RunOK, Rows: []store.FindingRow{{EntityKey: "APP/orders/fast", Severity: "critical", Detail: "missing now"}}}}); err != nil {
		t.Fatal(err)
	}
	// Historical reports in conformanceStore are five minutes apart. Newer
	// empty collections are deliberately closer together, leaving those old
	// reports inside the fifteen-minute lookback: current presence must still
	// be required, independently of whether a historical rate is comparable.
	p := &storeprov.Provider{St: st, Checks: set, Interval: confStep, Clock: func() time.Time { return confAt(confEpochs).Add(9*time.Minute + time.Second) }}
	f, err := p.OnCall(ctx, &v1.GetOnCallRequest{FindingId: "review-gap:APP/orders/fast"})
	if err != nil {
		t.Fatal(err)
	}
	if f.Now.DeliveredPerSecond != nil {
		t.Fatalf("absent consumers still claim present delivery: rate=%v epoch=%v", *f.Now.DeliveredPerSecond, f.Now.EpochAt)
	}
	if f.Now.StreamMsgsPerSecond != nil {
		t.Fatalf("absent stream still has rate: %v", *f.Now.StreamMsgsPerSecond)
	}
	// GetOnCall has no historical range. The historical consumer API keeps
	// its observed rate at the requested endpoint after current reports stop.
	past, err := p.GetConsumer(ctx, &v1.GetConsumerRequest{Account: "APP", Stream: "orders", Consumer: "fast", Range: &v1.TimeRange{Start: timestamppb.New(confStart), End: timestamppb.New(confAt(confEpochs).Add(time.Microsecond))}})
	if err != nil {
		t.Fatal(err)
	}
	if past.Consumer.DeliveredPerSecond == nil || *past.Consumer.DeliveredPerSecond != 1 {
		t.Fatalf("historical rate lost: %v", past.Consumer)
	}
}

// Reports are JSZ facts: no VARZ rows are needed to establish a current rate.
func TestOnCallCurrentRateReports(t *testing.T) {
	for _, tc := range []struct {
		name     string
		values   []int
		leaders  []string
		created  []int
		want     *float64
		follower bool
	}{
		{name: "current", values: []int{0, 30, 60, 90}, want: ratePtr(1)},
		{name: "zero", values: []int{0, 0, 0, 0}, want: ratePtr(0)},
		{name: "stopped", values: []int{0, 30, -1, -1}},
		{name: "no-history", values: []int{-1, -1, -1, 90}},
		{name: "follower-only", values: []int{0, 30, 60, 90}, follower: true},
		{name: "leader-change", values: []int{0, 30, 60, 90}, leaders: []string{"S0", "S0", "S1", "S1"}, want: ratePtr(1)},
		{name: "leader-return", values: []int{0, 30, 60, 90}, leaders: []string{"S0", "S1", "S1", "S0"}},
		{name: "restart", values: []int{0, 30, 0, 30}, created: []int{0, 0, 1, 1}, want: ratePtr(1)},
		{name: "reset", values: []int{100, 130, 0, 30}, want: ratePtr(1)},
		{name: "last-reset", values: []int{0, 30, 60, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := emptyStore(t)
			r := rows{t, st}
			for i, v := range tc.values {
				e := int64(i + 1)
				addEpochAt(t, st, e, t0.Add(time.Duration(i)*30*time.Second))
				if v < 0 {
					continue
				}
				leader := "S0"
				if len(tc.leaders) > 0 {
					leader = tc.leaders[i]
				}
				created := t0
				if len(tc.created) > 0 {
					created = created.Add(time.Duration(tc.created[i]) * time.Second)
				}
				lead := !(tc.follower && i == 3)
				r.add("consumer_stats", map[string]any{"epoch": e, "server_id": leader, "account": "APP", "stream": "orders", "consumer": "fast", "is_leader": lead, "created": created, "delivered_consumer_seq": v})
				r.add("stream_stats", map[string]any{"epoch": e, "server_id": leader, "account": "APP", "stream": "orders", "is_leader": lead, "created": created, "last_seq": v})
				// A sibling once delivered faster but has stopped reporting. It must
				// not inflate the sum of the consumers still reporting.
				if i < 2 {
					r.add("consumer_stats", map[string]any{"epoch": e, "server_id": "S0", "account": "APP", "stream": "orders", "consumer": "stale", "is_leader": true, "delivered_consumer_seq": 100 * v})
				}
			}
			if _, err := st.ApplyCheckRuns(ctx, 4, []store.CheckRun{
				{Code: "review-gap-consumer", EntityType: "consumer", Status: store.RunOK, Rows: []store.FindingRow{{EntityKey: "APP/orders/missing", Severity: "critical", Detail: "missing now"}}},
				{Code: "review-gap-stream", EntityType: "stream", Status: store.RunOK, Rows: []store.FindingRow{{EntityKey: "APP/orders", Severity: "critical", Detail: "missing now"}}},
			}); err != nil {
				t.Fatal(err)
			}
			for _, entityType := range []string{"consumer", "stream"} {
				key := "APP/orders"
				if entityType == "consumer" {
					key += "/missing"
				}
				p := &storeprov.Provider{St: st, Interval: 30 * time.Second, Clock: func() time.Time { return t0.Add(91 * time.Second) }}
				f, err := p.OnCall(ctx, &v1.GetOnCallRequest{FindingId: "review-gap-" + entityType + ":" + key})
				if err != nil {
					t.Fatal(err)
				}
				for name, got := range map[string]*float64{"delivery": f.Now.DeliveredPerSecond, "stream": f.Now.StreamMsgsPerSecond} {
					if tc.want == nil {
						if got != nil {
							t.Errorf("%s: want unset, got %v", name, *got)
						}
					} else if got == nil || *got != *tc.want {
						if got == nil {
							t.Errorf("%s %s: want %v, got unset", entityType, name, *tc.want)
						} else {
							t.Errorf("%s %s: want %v, got %v", entityType, name, *tc.want, *got)
						}
					}
				}
			}
		})
	}
}
func ratePtr(v float64) *float64 { return &v }
