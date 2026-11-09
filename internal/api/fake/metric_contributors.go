package fake

import (
	"context"
	"fmt"
	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
	"strings"
	"time"
)

func (p *Provider) ListMetricContributors(ctx context.Context, in *v1.ListMetricContributorsRequest) (*v1.ListMetricContributorsResponse, error) {
	r := proto.Clone(in).(*v1.ListMetricContributorsRequest)
	if err := api.NormalizeMetricContributors(r, p.Now()); err != nil {
		return nil, err
	}
	return api.MetricContributorPage(ctx, &p.listPages, r, func() (*api.EntitySnapshot, error) {
		s := p.scen()
		kind := api.Metrics[r.Metric].Entity
		var entities []*v1.EntityRef
		if kind == v1.EntityType_ENTITY_TYPE_SERVER {
			for _, sp := range p.sc.servers {
				if r.Server == "" || r.Server == sp.name {
					entities = append(entities, serverRef(sp.name))
				}
			}
		} else if r.Account == "" || r.Account == Account {
			for _, sp := range p.sc.streams {
				if r.Stream != "" && r.Stream != sp.name {
					continue
				}
				if kind == v1.EntityType_ENTITY_TYPE_STREAM {
					entities = append(entities, streamRef(sp.name))
				} else {
					seen := map[string]bool{}
					for _, c := range append(s.consumers(sp, nil, true, r.Range.Start.AsTime()), s.consumers(sp, nil, true, r.Range.End.AsTime())...) {
						if !seen[c.Entity.Key] {
							entities = append(entities, c.Entity)
							seen[c.Entity.Key] = true
						}
					}
				}
			}
		}
		var rows []*v1.MetricContributor
		size := 0
		for _, e := range entities {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !strings.Contains(strings.ToLower(e.Key), strings.ToLower(r.Query)) {
				continue
			}
			if len(rows) >= api.MaxListRankingRows {
				return nil, fmt.Errorf("%w: demo metric capture exceeds identity limit", api.ErrResourceExhausted)
			}
			o, err := s.observationSummary(ctx, r.Metric, e, r.Range)
			if err != nil {
				return nil, err
			}
			if o.SampleCount == 0 {
				continue
			}
			c := &v1.MetricContributor{Entity: e, Observations: o}
			if kind == v1.EntityType_ENTITY_TYPE_SERVER {
				sp, _ := p.sc.server(e.Key)
				processStart := s.started(sp)
				for _, process := range []struct {
					id                 string
					start, first, last time.Time
				}{
					{sp.oldID, s.on(2, 8, 12), s.on(3, 0, 0), processStart.Add(-2 * time.Minute)},
					{sp.id, processStart, processStart.Add(30 * time.Second), s.dataEnd()},
				} {
					if process.id == "" {
						continue
					}
					first, last := latest(r.Range.Start.AsTime(), process.first), earliest(r.Range.End.AsTime(), process.last.Add(time.Nanosecond))
					for at := api.BucketStart(first.Add(sampleEvery-1), sampleEvery); at.Before(last); at = at.Add(sampleEvery) {
						if err := ctx.Err(); err != nil {
							return nil, err
						}
						if s.sc.hasCollection(s.clock(at)) && !s.downAt(sp, at) {
							c.ObservedServerIds++
							if !process.start.Before(r.Range.Start.AsTime()) && process.start.Before(r.Range.End.AsTime()) {
								c.ProcessStarts++
							}
							break
						}
					}
				}
			}
			size += proto.Size(c)
			if size > api.MaxListContextBytes {
				return nil, fmt.Errorf("%w: demo metric capture exceeds byte limit", api.ErrResourceExhausted)
			}
			rows = append(rows, c)
		}
		api.SortMetricContributors(rows, r)
		return &api.EntitySnapshot{MetricRows: rows, Range: r.Range, ReadAt: ts(s.now)}, nil
	})
}
