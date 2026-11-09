package api

import (
	"context"
	"fmt"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
	"math"
	"math/big"
	"slices"
	"strings"
	"time"
)

// NormalizeMetricContributors is shared by handlers and direct provider callers.
// Metric/sort identifiers are allowlisted; no caller-provided SQL is accepted.
func NormalizeMetricContributors(r *v1.ListMetricContributorsRequest, now time.Time) error {
	m, ok := Metrics[r.Metric]
	if !ok || (m.Entity != v1.EntityType_ENTITY_TYPE_SERVER && m.Entity != v1.EntityType_ENTITY_TYPE_STREAM && m.Entity != v1.EntityType_ENTITY_TYPE_CONSUMER) {
		return invalid("metric needs a server, stream or consumer scope")
	}
	if r.Ranking == v1.MetricRanking_METRIC_RANKING_UNSPECIFIED {
		r.Ranking = v1.MetricRanking_METRIC_RANKING_NAME
	}
	if r.Ranking < v1.MetricRanking_METRIC_RANKING_NAME || r.Ranking > v1.MetricRanking_METRIC_RANKING_COUNTER_INCREASE {
		return invalid("unknown metric ranking")
	}
	summed := r.Metric == "server.slow_consumers"
	counter := slices.Contains([]string{"server.in_msgs_rate", "server.out_msgs_rate", "server.in_bytes_rate", "server.out_bytes_rate"}, r.Metric)
	if r.Ranking == v1.MetricRanking_METRIC_RANKING_OBSERVED_TOTAL && !summed {
		return invalid("metric has no observed event total")
	}
	if r.Ranking == v1.MetricRanking_METRIC_RANKING_COUNTER_INCREASE && !counter {
		return invalid("metric has no comparable counter increase")
	}
	if m.Entity == v1.EntityType_ENTITY_TYPE_SERVER && (r.Account != "" || r.Stream != "") {
		return invalid("server measurements are not account or stream filtered")
	}
	if m.Entity != v1.EntityType_ENTITY_TYPE_SERVER && r.Server != "" {
		return invalid("resource measurements are not server filtered")
	}
	for _, s := range []string{r.Account, r.Stream, r.Server, r.Query} {
		if len(s) > 512 {
			return invalid("metric filter is too long")
		}
	}
	var err error
	if _, err = EntityLimit(r.Limit); err != nil {
		return err
	}
	r.Range, err = EntityListRange(r.Range, r.PageToken, now)
	if err != nil {
		return err
	}
	if r.Range.End.AsTime().Sub(r.Range.Start.AsTime()) > MaxObservationRange {
		return invalid("metric ranking range exceeds 30 days")
	}
	return nil
}

// SortMetricContributors ranks the entire bounded capture before page selection.
// Missing/nonfinite values sort last in either direction; exact totals never
// pass through floating-point conversion. Entity keys break ties deterministically.
func SortMetricContributors(rows []*v1.MetricContributor, r *v1.ListMetricContributorsRequest) {
	number := func(o *v1.ObservationSummary) *float64 {
		if o == nil {
			return nil
		}
		var n *float64
		switch r.Ranking {
		case v1.MetricRanking_METRIC_RANKING_LATEST:
			n = o.Latest
		case v1.MetricRanking_METRIC_RANKING_MEAN:
			n = o.Mean
		case v1.MetricRanking_METRIC_RANKING_MAXIMUM:
			n = o.Maximum
		case v1.MetricRanking_METRIC_RANKING_MINIMUM:
			n = o.Minimum
		}
		if n != nil && (math.IsNaN(*n) || math.IsInf(*n, 0)) {
			return nil
		}
		return n
	}
	integer := func(o *v1.ObservationSummary) *big.Int {
		var s *string
		if o != nil {
			if r.Ranking == v1.MetricRanking_METRIC_RANKING_COUNTER_INCREASE {
				if c := o.CounterComparisons; c != nil {
					s = c.ObservedIncrease
				}
			} else {
				s = o.ObservedTotal
			}
		}
		if s == nil {
			return nil
		}
		n, ok := new(big.Int).SetString(*s, 10)
		if !ok || n.Sign() < 0 {
			return nil
		}
		return n
	}
	slices.SortFunc(rows, func(a, b *v1.MetricContributor) int {
		c := 0
		if r.Ranking == v1.MetricRanking_METRIC_RANKING_NAME {
			c = strings.Compare(a.GetEntity().GetKey(), b.GetEntity().GetKey())
		} else if r.Ranking == v1.MetricRanking_METRIC_RANKING_OBSERVED_TOTAL || r.Ranking == v1.MetricRanking_METRIC_RANKING_COUNTER_INCREASE {
			x, y := integer(a.Observations), integer(b.Observations)
			if x == nil || y == nil {
				if x != nil {
					return -1
				}
				if y != nil {
					return 1
				}
			} else {
				c = x.Cmp(y)
			}
		} else {
			x, y := number(a.Observations), number(b.Observations)
			if x == nil || y == nil {
				if x != nil {
					return -1
				}
				if y != nil {
					return 1
				}
			} else {
				if *x < *y {
					c = -1
				} else if *x > *y {
					c = 1
				}
			}
		}
		if r.Descending {
			c = -c
		}
		if c != 0 {
			return c
		}
		return strings.Compare(a.GetEntity().GetKey(), b.GetEntity().GetKey())
	})
}

func MetricContributorPage(ctx context.Context, pages *EntityPages, r *v1.ListMetricContributorsRequest, load func() (*EntitySnapshot, error)) (*v1.ListMetricContributorsResponse, error) {
	scope := proto.Clone(r).(*v1.ListMetricContributorsRequest)
	scope.Limit = 0
	scope.PageToken = ""
	snap, offset, err := pages.Begin(ctx, r.PageToken, EntityScope(scope), load)
	if err != nil {
		return nil, err
	}
	limit, err := EntityLimit(r.Limit)
	if err != nil {
		return nil, err
	}
	out := &v1.ListMetricContributorsResponse{Metric: r.Metric, Unit: Metrics[r.Metric].Unit, Ranking: r.Ranking, Descending: r.Descending, Range: snap.Range, ReadAt: snap.ReadAt, TotalCount: uint32(len(snap.MetricRows))}
	for offset < len(snap.MetricRows) && len(out.Contributors) < limit {
		out.Contributors = append(out.Contributors, snap.MetricRows[offset])
		if !FitsEntityPage(out) {
			out.Contributors = out.Contributors[:len(out.Contributors)-1]
			if len(out.Contributors) == 0 {
				return nil, fmt.Errorf("%w: metric entity exceeds page budget", ErrResourceExhausted)
			}
			break
		}
		offset++
	}
	out.NextPageToken = snap.Next(offset)
	return out, nil
}
