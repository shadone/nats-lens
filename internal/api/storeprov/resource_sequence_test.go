package storeprov_test

import (
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	"math"
	"testing"
	"time"
)

func TestResourceSequenceComparisonsDoNotRejoinOldRuns(t *testing.T) {
	for _, consumer := range []bool{false, true} {
		for _, change := range []string{"incarnation ABA", "unknown creation", "leader ABA", "no leader", "conflicting creation", "two leaders"} {
			if !consumer && (change == "leader ABA" || change == "no leader" || change == "two leaders") {
				continue
			}
			t.Run(map[bool]string{false: "stream", true: "consumer"}[consumer]+"/"+change, func(t *testing.T) {
				st := emptyStore(t)
				r := rows{t, st}
				r.server("S0", "nats-0")
				r.server("S1", "nats-1")
				start := t0.Truncate(time.Hour)
				created := start.Add(-time.Hour)
				r.add("streams", map[string]any{"account": "APP", "stream": "orders", "first_epoch": 1, "last_epoch": 4})
				r.add("consumers", map[string]any{"account": "APP", "stream": "orders", "consumer": "worker", "first_epoch": 1, "last_epoch": 4})
				for i, n := range []int64{100, 200, 300, 330} {
					epoch := int64(i + 1)
					r.epoch(epoch, start.Add(time.Duration(i)*30*time.Second), "S0", "S1")
					var birth any = created
					id := "S0"
					leader := true
					if i == 1 {
						switch change {
						case "incarnation ABA":
							birth = created.Add(time.Minute)
						case "unknown creation":
							birth = nil
						case "leader ABA":
							id = "S1"
						case "no leader":
							leader = false
						}
					}
					table, col := "stream_stats", "last_seq"
					data := map[string]any{"epoch": epoch, "server_id": id, "account": "APP", "stream": "orders", "created": birth, "is_leader": leader}
					if consumer {
						table, col = "consumer_stats", "delivered_consumer_seq"
						data["consumer"] = "worker"
					}
					data[col] = n
					r.add(table, data)
					if i == 1 && (change == "conflicting creation" || change == "two leaders") {
						other := map[string]any{"epoch": epoch, "server_id": "S1", "account": "APP", "stream": "orders", "created": created.Add(time.Minute), "is_leader": false, col: n + 1000}
						if consumer {
							other["consumer"] = "worker"
						}
						if change == "two leaders" {
							other["created"] = created
							other["is_leader"] = true
						}
						r.add(table, other)
					}
				}
				metric := "stream.msgs_rate"
				entity := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "APP/orders"}
				if consumer {
					metric = "consumer.delivered_rate"
					entity = &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: "APP/orders/worker"}
				}
				p := &storeprov.Provider{St: st, Interval: 30 * time.Second}
				rng := &v1.TimeRange{Start: timestamppb.New(start), End: timestamppb.New(start.Add(2 * time.Minute))}
				got, err := p.GetTimeSeries(ctx, &v1.GetTimeSeriesRequest{Metric: metric, Entity: entity, Range: rng, MaxPoints: 100, IncludeObservations: true})
				if err != nil {
					t.Fatal(err)
				}
				if len(got.Series.Points) != 1 || math.Abs(got.Series.Points[0].Value-1) > 1e-9 {
					t.Errorf("rejoined old/unknown comparison: %v", got.Series)
				}
				if consumer {
					list, err := p.ListConsumers(ctx, &v1.ListConsumersRequest{Range: rng, Account: "APP", Stream: "orders", IncludeEphemeral: true})
					if err != nil {
						t.Fatal(err)
					}
					if len(list.Consumers) != 1 || list.Consumers[0].DeliveredPerSecond == nil || math.Abs(list.Consumers[0].GetDeliveredPerSecond()-1) > 1e-9 {
						t.Errorf("summary rejoined old/unknown run: %v", list)
					}
				}
			})
		}
	}
}
func TestResourceSequenceComparisonsPreserveTimestampAdjacency(t *testing.T) {
	for _, consumer := range []bool{false, true} {
		t.Run(map[bool]string{false: "stream", true: "consumer"}[consumer], func(t *testing.T) {
			st := emptyStore(t)
			r := rows{t, st}
			r.server("S0", "nats-0")
			start := t0.Truncate(time.Hour)
			r.add("streams", map[string]any{"account": "APP", "stream": "orders", "first_epoch": 1, "last_epoch": 3})
			r.add("consumers", map[string]any{"account": "APP", "stream": "orders", "consumer": "worker", "first_epoch": 1, "last_epoch": 3})
			for i, seconds := range []int64{0, 70, 30} {
				ep := int64(i + 1)
				r.epoch(ep, start.Add(time.Duration(seconds)*time.Second), "S0")
				table, col := "stream_stats", "last_seq"
				data := map[string]any{"epoch": ep, "server_id": "S0", "account": "APP", "stream": "orders", "created": start.Add(-time.Hour), "is_leader": true}
				if consumer {
					table, col = "consumer_stats", "delivered_consumer_seq"
					data["consumer"] = "worker"
				}
				data[col] = int64(i * 100)
				r.add(table, data)
			}
			metric := "stream.msgs_rate"
			entity := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "APP/orders"}
			if consumer {
				metric = "consumer.delivered_rate"
				entity = &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: "APP/orders/worker"}
			}
			p := &storeprov.Provider{St: st, Interval: 30 * time.Second}
			rng := &v1.TimeRange{Start: timestamppb.New(start), End: timestamppb.New(start.Add(time.Minute))}
			out, err := p.GetTimeSeries(ctx, &v1.GetTimeSeriesRequest{Metric: metric, Entity: entity, Range: rng, MaxPoints: 100})
			if err != nil {
				t.Fatal(err)
			}
			if len(out.Series.Points) != 0 {
				t.Fatalf("filtered actual predecessor: %v", out.Series.Points)
			}
		})
	}
}
