package fake

import (
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// withRates sets each present consumer's delivery rate: the mean of
// consumer.delivered_rate over the 15 minutes before the range's end.
func (s scen) withRates(cs []*v1.ConsumerSummary, r *v1.TimeRange) []*v1.ConsumerSummary {
	end := r.GetEnd().AsTime()
	span := &v1.TimeRange{Start: ts(end.Add(-15 * time.Minute)), End: r.GetEnd()}
	for _, c := range cs {
		if !c.GetPresent() {
			continue
		}
		pts := s.series("consumer.delivered_rate", c.GetEntity(), span, 1).GetPoints()
		if len(pts) == 0 {
			continue
		}
		var t float64
		for _, p := range pts {
			t += p.GetValue()
		}
		c.DeliveredPerSecond = ptr(t / float64(len(pts)))
	}
	return cs
}

// sortedConsumers orders and limits a consumer list as the request asks.
func sortedConsumers(cs []*v1.ConsumerSummary, req *v1.ListConsumersRequest) []*v1.ConsumerSummary {
	return api.SortConsumers(cs, req.GetSort(), req.GetLimit())
}
