package api

import (
	"cmp"
	"slices"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// SortConsumers orders a consumer list as the request asks and applies
// its limit. DELIVERED_RATE puts the highest rate first and consumers
// without one last, each group by key; the default keeps the order given.
func SortConsumers(cs []*v1.ConsumerSummary, sort v1.ConsumerSort, limit int32) []*v1.ConsumerSummary {
	if sort == v1.ConsumerSort_CONSUMER_SORT_DELIVERED_RATE {
		slices.SortStableFunc(cs, func(a, b *v1.ConsumerSummary) int {
			if (a.DeliveredPerSecond == nil) != (b.DeliveredPerSecond == nil) {
				if a.DeliveredPerSecond == nil {
					return 1
				}
				return -1
			}
			return cmp.Or(cmp.Compare(b.GetDeliveredPerSecond(), a.GetDeliveredPerSecond()),
				cmp.Compare(a.GetEntity().GetKey(), b.GetEntity().GetKey()))
		})
	}
	if limit > 0 && int(limit) < len(cs) {
		cs = cs[:limit]
	}
	return cs
}
