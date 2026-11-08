package storeprov_test

import (
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	"slices"
	"testing"
	"time"
)

func TestCapacityStorageHistory(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	r.server("S1", "nats-1")
	r.server("restart", "nats-0")
	r.add("streams", map[string]any{"account": "APP", "stream": "orders", "first_epoch": 1, "last_epoch": 5})
	start := t0.Truncate(time.Hour)
	for n := int64(1); n <= 5; n++ {
		ids := []string{"S0", "S1"}
		if n == 3 {
			ids = append(ids, "restart")
		}
		if n == 5 {
			ids = []string{"restart", "S1"}
		}
		r.epoch(n, start.Add(time.Duration(n)*time.Minute), ids...)
		for _, id := range ids {
			r.add("server_stats", map[string]any{"epoch": n, "server_id": id, "js_enabled": true, "js_storage": 100})
			r.add("stream_stats", map[string]any{"epoch": n, "server_id": id, "account": "APP", "stream": "orders", "bytes": 20, "is_leader": id == ids[0]})
		}
	}
	// Unknown JSZ report and incomplete expected membership must not become dips.
	if _, err := st.DB().Exec(`UPDATE server_stats SET js_storage = NULL WHERE epoch = 2 AND server_id = 'S1'; UPDATE epochs SET expected_servers = 3 WHERE epoch = 4`); err != nil {
		t.Fatal(err)
	}
	c := serveSeries(t, st, start.Add(time.Hour))
	rng := &v1.TimeRange{Start: timestamppb.New(start), End: timestamppb.New(start.Add(6 * time.Minute))}
	for _, tc := range []struct {
		metric string
		entity *v1.EntityRef
		want   []float64
	}{
		{"cluster.capacity_file_store_bytes", nil, []float64{200, 200}},
		{"server.capacity_file_store_bytes", serverRef("nats-0"), []float64{100, 100, 100, 100}},
		{"stream.observed_replica_bytes", &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "APP/orders", Account: "APP", Stream: "orders"}, []float64{40, 40, 60, 40, 40}},
	} {
		t.Run(tc.metric, func(t *testing.T) {
			got := values(seriesOf(t, c, tc.metric, tc.entity, rng))
			if !slices.Equal(got, tc.want) {
				t.Fatalf("values = %v, want %v", got, tc.want)
			}
		})
	}
}
