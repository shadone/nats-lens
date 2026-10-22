package store

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/micro"

	"github.com/shadone/nats-lens/internal/collect"
)

// One instance with two endpoints both named "get" (groups users and
// orders: the framework does not refuse a repeated name) is stored as two
// rows, paired by subject between epochs and summed into one hourly row
// per name: users.get does 1000 requests per epoch and orders.get 1, so
// two increases make 2002 requests.
func TestMicroEndpointHoursSameNamedEndpoints(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	h := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)
	old := h.Add(-24 * time.Hour)
	for i := range 3 {
		snap := fixtureSnapshot(t, h.Add(time.Duration(i)*30*time.Second))
		acc := collect.MicroAccount{Label: "APP", Account: "APP",
			Asked: []collect.MicroAsked{{Request: collect.MicroStats, State: collect.MicroOK, Replies: 1}}}
		acc.Stats = []micro.Stats{{ServiceIdentity: micro.ServiceIdentity{Name: "orders", ID: "A"},
			Type: micro.StatsResponseType, Started: old, Endpoints: []*micro.EndpointStats{
				{Name: "get", Subject: "users.get", NumRequests: 50000 + 1000*i, ProcessingTime: time.Duration(50000+1000*i) * time.Millisecond},
				{Name: "get", Subject: "orders.get", NumRequests: 10 + i, ProcessingTime: time.Duration(10+i) * time.Millisecond},
			}}}
		snap.Micro = []collect.MicroAccount{acc}
		if _, err := s.WriteSnapshot(ctx, snap); err != nil {
			t.Fatal(err)
		}
	}
	got := rowStrings(t, s.DB(), `SELECT requests, processing_time_ns, epochs FROM micro_endpoint_hours`)
	if n := count(t, s.DB(), `SELECT count(*) FROM micro_endpoints WHERE endpoint = 'get'`); n != 6 {
		t.Errorf("micro_endpoints holds %d get rows over 3 epochs, want 6", n)
	}
	if len(got) != 1 || got[0] != "2002|2002000000|3" {
		t.Errorf("hours %v, want 2002 requests over 3 epochs", got)
	}
}
