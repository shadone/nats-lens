package storeprov_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// A count series takes the last value of each bucket, so it stays whole,
// and a tile shows its series' last point: the value lies within the
// series' range and matches the chart.
func TestCountTilesAreWholeAndMatchTheirChart(t *testing.T) {
	st := fixtureStore(t)
	if _, err := st.DB().Exec(`UPDATE epochs SET observed_servers = 1 WHERE epoch = 3`); err != nil {
		t.Fatal(err)
	}
	_, c := serve(t, st)
	s := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "cluster.servers_up", Range: hour(), MaxPoints: 1,
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SYSTEM}}))).GetSeries()
	if len(s.GetPoints()) != 1 || s.GetPoints()[0].GetValue() != 1 {
		t.Fatalf("servers up in one bucket = %v, want the last value 1", s.GetPoints())
	}
	o := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{Range: hour(), MaxPoints: 1})))
	for _, tile := range o.GetTiles() {
		if tile.Value == nil || len(tile.GetSeries().GetPoints()) == 0 {
			continue
		}
		v, ser := tile.GetValue(), tile.GetSeries()
		if v < ser.GetMin() || v > ser.GetMax() {
			t.Errorf("tile %s: value %v outside its series' range %v..%v", tile.GetId(), v, ser.GetMin(), ser.GetMax())
		}
		if tile.GetId() == "servers" || tile.GetId() == "connections" || tile.GetId() == "msgs_rate" {
			if last := ser.GetPoints()[len(ser.GetPoints())-1].GetValue(); last != v {
				t.Errorf("tile %s: value %v, chart ends at %v", tile.GetId(), v, last)
			}
		}
	}
}

// After a gap in collection (a collector restart) the first epoch has no
// rate: the increase over the gap is not drawn as one sample.
func TestRateAfterCollectorGapDropped(t *testing.T) {
	st := fixtureStore(t)
	if _, err := st.DB().Exec(`UPDATE epochs SET started_at = started_at + INTERVAL 1 HOUR WHERE epoch = 3`); err != nil {
		t.Fatal(err)
	}
	_, c := serve(t, st)
	r := &v1.TimeRange{Start: timestamppb.New(t0.Add(-time.Hour)), End: timestamppb.New(t0.Add(3 * time.Hour))}
	s := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "server.in_msgs_rate", Range: r, MaxPoints: 1000,
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: "nats-0"}}))).GetSeries()
	if len(s.GetPoints()) != 1 {
		t.Fatalf("points = %v, want only the rate before the gap", s.GetPoints())
	}
}

// A stream's message rate is not computed across a collection gap either.
func TestStreamRateAfterCollectorGapDropped(t *testing.T) {
	st := fixtureStore(t)
	if _, err := st.DB().Exec(`UPDATE epochs SET started_at = started_at + INTERVAL 1 HOUR WHERE epoch = 3`); err != nil {
		t.Fatal(err)
	}
	_, c := serve(t, st)
	r := &v1.TimeRange{Start: timestamppb.New(t0.Add(-time.Hour)), End: timestamppb.New(t0.Add(3 * time.Hour))}
	s := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "stream.msgs_rate", Range: r, MaxPoints: 1000,
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: account + "/order-jobs"}}))).GetSeries()
	if len(s.GetPoints()) != 1 {
		t.Fatalf("points = %v, want only the rate before the gap", s.GetPoints())
	}
}
