package fake_test

import (
	"connectrpc.com/connect"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"testing"
)

func TestCapacityHistoryMetrics(t *testing.T) {
	c := client(t)
	for _, sys := range []string{"", fake.GlobalSystem, fake.StagingSystem} {
		for _, q := range []struct {
			metric string
			entity *v1.EntityRef
		}{
			{"cluster.capacity_file_store_bytes", nil},
			{"server.capacity_file_store_bytes", &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: "nats-0"}},
			{"stream.observed_replica_bytes", &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "APP/order-events", Account: "APP", Stream: "order-events"}},
		} {
			if sys != "" && q.entity != nil {
				continue
			}
			s := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{System: sys, Metric: q.metric, Entity: q.entity}))).GetSeries()
			if len(s.GetPoints()) == 0 || s.GetMax() <= 0 {
				t.Fatalf("%s %s: no storage samples", sys, q.metric)
			}
		}
	}
}
