package store

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/micro"

	"github.com/shadone/nats-lens/internal/collect"
)

// hourInstance is one instance's STATS for one endpoint get of orders.
type hourInstance struct {
	id               string
	started          time.Time
	requests, errors int
	processing       time.Duration
}

func hourSnapshot(t *testing.T, at time.Time, instances ...hourInstance) *collect.Snapshot {
	t.Helper()
	snap := fixtureSnapshot(t, at)
	acc := collect.MicroAccount{Label: "APP", Account: "APP",
		Asked: []collect.MicroAsked{{Request: collect.MicroStats, State: collect.MicroOK, Replies: len(instances)}}}
	for _, in := range instances {
		acc.Stats = append(acc.Stats, micro.Stats{ServiceIdentity: micro.ServiceIdentity{Name: "orders", ID: in.id},
			Type: micro.StatsResponseType, Started: in.started, Endpoints: []*micro.EndpointStats{{Name: "get",
				NumRequests: in.requests, NumErrors: in.errors, ProcessingTime: in.processing}}})
	}
	snap.Micro = []collect.MicroAccount{acc}
	return snap
}

// The hourly sums add each epoch's increase over the instance's previous
// row to the epoch's hour: a restart (a new start time) or a counter that
// fell counts the new value whole, an instance first seen after starting
// since the previous epoch counts its value, one first seen that started
// long before counts nothing.
func TestMicroEndpointHours(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	h := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)
	old := h.Add(-24 * time.Hour)
	ms := time.Millisecond
	epochs := []*collect.Snapshot{
		// 22:59:00 A's baseline, C first seen with an old start: nothing.
		hourSnapshot(t, h.Add(59*time.Minute), hourInstance{"A", old, 100, 5, 100 * ms}),
		// 22:59:30 A +10 requests, +1 error, +20 ms; B started 22:59:10: 4.
		hourSnapshot(t, h.Add(59*time.Minute+30*time.Second), hourInstance{"A", old, 110, 6, 120 * ms},
			hourInstance{"B", h.Add(59*time.Minute + 10*time.Second), 4, 0, 8 * ms},
			hourInstance{"C", old, 1000, 0, time.Second}),
		// 23:00:00 A restarted: 3; B +6; C +10.
		hourSnapshot(t, h.Add(time.Hour), hourInstance{"A", h.Add(59*time.Minute + 50*time.Second), 3, 0, 3 * ms},
			hourInstance{"B", h.Add(59*time.Minute + 10*time.Second), 10, 1, 20 * ms},
			hourInstance{"C", old, 1010, 0, time.Second + 10*ms}),
		// 23:00:30 B's counter fell without a new start: 2 counted whole.
		hourSnapshot(t, h.Add(time.Hour+30*time.Second), hourInstance{"B", h.Add(59*time.Minute + 10*time.Second), 2, 0, 2 * ms}),
	}
	for _, snap := range epochs {
		if _, err := s.WriteSnapshot(ctx, snap); err != nil {
			t.Fatal(err)
		}
	}
	wantRows(t, "micro_endpoint_hours", rowStrings(t, s.DB(), `SELECT strftime(hour, '%H'), account, service, endpoint,
  requests, errors, processing_time_ns, epochs FROM micro_endpoint_hours ORDER BY hour`), []string{
		"22|APP|orders|get|14|1|28000000|2",
		"23|APP|orders|get|21|1|27000000|2",
	})
}

// The hourly sums are swept with the hours wholly before the retention
// window.
func TestMicroEndpointHoursSwept(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	h := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)
	for i := range 3 {
		at := h.Add(time.Duration(i) * time.Hour)
		if _, err := s.WriteSnapshot(ctx, hourSnapshot(t, at, hourInstance{"A", h.Add(-time.Hour), 10 * (i + 1), 0, time.Millisecond})); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Sweep(ctx, h.Add(2*time.Hour+30*time.Minute), time.Hour); err != nil {
		t.Fatal(err)
	}
	wantRows(t, "hours kept", rowStrings(t, s.DB(), `SELECT strftime(hour, '%H') FROM micro_endpoint_hours ORDER BY hour`),
		[]string{"23", "00"})
}
