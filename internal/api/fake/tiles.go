package fake

import (
	"fmt"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// last is the series' newest value; nil without points.
func last(s *v1.Series) *float64 {
	if n := len(s.GetPoints()); n > 0 {
		return ptr(s.GetPoints()[n-1].GetValue())
	}
	return nil
}

func sum(s *v1.Series) float64 {
	var t float64
	for _, p := range s.GetPoints() {
		t += p.GetValue()
	}
	return t
}

// spanLabel names a range length for a tile, for example "in 24h".
func spanLabel(r *v1.TimeRange) string {
	d := r.GetEnd().AsTime().Sub(r.GetStart().AsTime())
	if d%time.Hour == 0 {
		return fmt.Sprintf("in %dh", int(d.Hours()))
	}
	return "in range"
}

// prodTiles are example-prod's six numbers of the overview.
func prodTiles(s scen, r *v1.TimeRange, maxPoints int32) []*v1.Tile {
	sys := s.sc.systemRef()
	ser := func(metric string) *v1.Series { return s.series(metric, sys, r, maxPoints) }
	up, conns, slow := ser("cluster.servers_up"), ser("cluster.connections"), ser("cluster.slow_consumers")
	storage, in, out, durable := ser("cluster.js_storage_bytes"), ser("cluster.in_msgs_rate"), ser("cluster.out_msgs_rate"), ser("cluster.durable_consumers")

	recentSlow := s.series("cluster.slow_consumers", sys, &v1.TimeRange{Start: ts(r.GetEnd().AsTime().Add(-15 * time.Minute)), End: r.GetEnd()}, 1)
	limit := 150.0 * gib
	cur := last(storage)
	used := 0.0
	if cur != nil {
		used = *cur / limit * 100
	}
	return []*v1.Tile{
		{Id: "servers", Label: "Servers", Value: last(up), Unit: v1.Unit_UNIT_COUNT, ValueSuffix: "/ 3",
			Subtitle: "v" + Version + " on all", Status: v1.Status_STATUS_OK, Series: up},
		{Id: "connections", Label: "Connections", Value: last(conns), Unit: v1.Unit_UNIT_COUNT,
			Subtitle: "41 ephemeral consumers", Status: v1.Status_STATUS_OK, Series: conns},
		{Id: "slow_consumers", Label: "Slow consumer events", Value: ptr(sum(slow)), Unit: v1.Unit_UNIT_COUNT, ValueSuffix: spanLabel(r),
			Subtitle: fmt.Sprintf("%d in last 15m", int(sum(recentSlow))), Status: v1.Status_STATUS_WARNING, Series: slow},
		{Id: "js_storage", Label: "JetStream storage", Value: cur, Unit: v1.Unit_UNIT_BYTES, ValueSuffix: "of 150 GiB",
			Subtitle: fmt.Sprintf("%.1f%% used, +0.19 GiB/day", used), Status: v1.Status_STATUS_OK, Series: storage, Limit: ptr(limit)},
		{Id: "msgs_rate", Label: "Messages / s", Value: last(in), Unit: v1.Unit_UNIT_PER_SECOND, ValueSuffix: "in",
			Value2: last(out), Value2Suffix: "out", Subtitle: "out = fan-out to 13 durables", Status: v1.Status_STATUS_OK,
			Series: in, Series2: out},
		{Id: "consumers", Label: "Consumers", Value: last(durable), Unit: v1.Unit_UNIT_COUNT, ValueSuffix: "durable",
			Value2: ptr(41.0), Value2Suffix: "ephemeral", Subtitle: "1 expected durable missing", Status: v1.Status_STATUS_CRITICAL,
			Series: durable},
	}
}
