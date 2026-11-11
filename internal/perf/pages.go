package perf

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
)

// call is one API request a page makes; it returns the procedure's name.
type call struct {
	proc string
	do   func(ctx context.Context, c natslensv1connect.LensServiceClient) error
}

// page is the set of calls one UI page starts at once when it loads.
type page struct {
	name  string
	calls []call
}

// liveRange is a live UI range of span: a start and no end.
func liveRange(now time.Time, span time.Duration) *v1.TimeRange {
	return &v1.TimeRange{Start: timestamppb.New(now.Add(-span))}
}

func errOnly[T any](_ *connect.Response[T], err error) error { return err }

// trendQueries are the Overview's trend series (web/src/components/Trends.tsx).
var trendQueries = []string{"cluster.servers_up", "cluster.connections", "cluster.slow_consumers", "cluster.in_msgs_rate",
	"cluster.out_msgs_rate", "cluster.in_bytes_rate", "cluster.out_bytes_rate", "cluster.cpu_percent", "cluster.mem_bytes",
	"cluster.assets_replicated", "cluster.assets_single"}

// overviewPage is what web/src/pages/Overview.tsx and the app shell ask
// for on load, all at once as React Query starts them: the lens info, the
// systems, the overview with its tiles, the current findings, the status
// timeline, the data history, the trend batch, the busiest consumers and
// the stream placement.
func overviewPage(now func() time.Time, span time.Duration) page {
	r := func() *v1.TimeRange { return liveRange(now(), span) }
	var trends []*v1.SeriesQuery
	for _, m := range trendQueries {
		trends = append(trends, &v1.SeriesQuery{Metric: m, MaxPoints: 96})
	}
	return page{name: "overview", calls: []call{
		{"GetLensInfo", func(ctx context.Context, c natslensv1connect.LensServiceClient) error {
			return errOnly(c.GetLensInfo(ctx, connect.NewRequest(&v1.GetLensInfoRequest{})))
		}},
		{"ListSystems", func(ctx context.Context, c natslensv1connect.LensServiceClient) error {
			return errOnly(c.ListSystems(ctx, connect.NewRequest(&v1.ListSystemsRequest{})))
		}},
		{"GetOverview", func(ctx context.Context, c natslensv1connect.LensServiceClient) error {
			return errOnly(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{Range: r()})))
		}},
		{"ListFindings", func(ctx context.Context, c natslensv1connect.LensServiceClient) error {
			return errOnly(c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{Range: r(), IncludeMuted: true, PageSize: 50})))
		}},
		{"GetStatusTimeline", func(ctx context.Context, c natslensv1connect.LensServiceClient) error {
			return errOnly(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{Range: r()})))
		}},
		{"GetDataHistory", func(ctx context.Context, c natslensv1connect.LensServiceClient) error {
			return errOnly(c.GetDataHistory(ctx, connect.NewRequest(&v1.GetDataHistoryRequest{Range: r()})))
		}},
		{"GetTimeSeriesBatch", func(ctx context.Context, c natslensv1connect.LensServiceClient) error {
			return errOnly(c.GetTimeSeriesBatch(ctx, connect.NewRequest(&v1.GetTimeSeriesBatchRequest{Range: r(), Queries: trends, MaxPoints: 96})))
		}},
		{"ListConsumers", func(ctx context.Context, c natslensv1connect.LensServiceClient) error {
			return errOnly(c.ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{Range: r(), Sort: v1.ConsumerSort_CONSUMER_SORT_DELIVERED_RATE, Limit: 6})))
		}},
		{"ListStreams", func(ctx context.Context, c natslensv1connect.LensServiceClient) error {
			return errOnly(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{Range: r(), Limit: 12})))
		}},
	}}
}

// tabPages are the other pages a user opens in tabs, each with its primary
// call or calls.
func tabPages(now func() time.Time, span time.Duration) []page {
	r := func() *v1.TimeRange { return liveRange(now(), span) }
	one := func(name, proc string, do func(context.Context, natslensv1connect.LensServiceClient) error) page {
		return page{name: name, calls: []call{{proc, do}}}
	}
	return []page{
		one("streams", "ListStreams", func(ctx context.Context, c natslensv1connect.LensServiceClient) error {
			return errOnly(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{Range: r(), Limit: 100})))
		}),
		one("consumers", "ListConsumers", func(ctx context.Context, c natslensv1connect.LensServiceClient) error {
			return errOnly(c.ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{Range: r(), Limit: 100})))
		}),
		one("servers", "ListServers", func(ctx context.Context, c natslensv1connect.LensServiceClient) error {
			return errOnly(c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{Range: r()})))
		}),
		one("connections", "ListConnections", func(ctx context.Context, c natslensv1connect.LensServiceClient) error {
			return errOnly(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{Range: r(), PageSize: 100})))
		}),
		one("subjects", "GetSubjects", func(ctx context.Context, c natslensv1connect.LensServiceClient) error {
			return errOnly(c.GetSubjects(ctx, connect.NewRequest(&v1.GetSubjectsRequest{Range: r()})))
		}),
		{name: "timeline", calls: []call{
			{"GetStatusTimeline", func(ctx context.Context, c natslensv1connect.LensServiceClient) error {
				return errOnly(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{Range: r(), IncludeOk: true})))
			}},
			{"ListEvents", func(ctx context.Context, c natslensv1connect.LensServiceClient) error {
				return errOnly(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Range: r(), PageSize: 100})))
			}},
		}},
		one("findings", "ListFindings", func(ctx context.Context, c natslensv1connect.LensServiceClient) error {
			return errOnly(c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{Range: r(), PageSize: 100})))
		}),
		one("capacity", "GetCapacity", func(ctx context.Context, c natslensv1connect.LensServiceClient) error {
			return errOnly(c.GetCapacity(ctx, connect.NewRequest(&v1.GetCapacityRequest{Range: r()})))
		}),
		one("topology", "GetTopology", func(ctx context.Context, c natslensv1connect.LensServiceClient) error {
			return errOnly(c.GetTopology(ctx, connect.NewRequest(&v1.GetTopologyRequest{Range: r()})))
		}),
	}
}

// agentCalls are an agent's batch reads: 32 per-stream rate series (the
// most a batch takes), and the streams ranked by mean rate.
func agentCalls(now func() time.Time, span time.Duration, streams []*v1.EntityRef, k int) []call {
	r := func() *v1.TimeRange { return liveRange(now(), span) }
	var qs []*v1.SeriesQuery
	for i := range 32 {
		if len(streams) == 0 {
			break
		}
		qs = append(qs, &v1.SeriesQuery{Metric: "stream.msgs_rate", Entity: streams[(k*32+i)%len(streams)], MaxPoints: 120})
	}
	return []call{
		{"GetTimeSeriesBatch", func(ctx context.Context, c natslensv1connect.LensServiceClient) error {
			return errOnly(c.GetTimeSeriesBatch(ctx, connect.NewRequest(&v1.GetTimeSeriesBatchRequest{Range: r(), Queries: qs, MaxPoints: 120})))
		}},
		{"ListMetricContributors", func(ctx context.Context, c natslensv1connect.LensServiceClient) error {
			return errOnly(c.ListMetricContributors(ctx, connect.NewRequest(&v1.ListMetricContributorsRequest{Metric: "stream.msgs_rate",
				Range: r(), Ranking: v1.MetricRanking_METRIC_RANKING_MEAN, Descending: true, Limit: 50})))
		}},
	}
}

// serialSeries asks for the same 32 series as one agent batch, one
// GetTimeSeries call after another: what a batch costs without its
// per-request parallelism.
func serialSeries(now func() time.Time, span time.Duration, streams []*v1.EntityRef) call {
	return call{"GetTimeSeries x32", func(ctx context.Context, c natslensv1connect.LensServiceClient) error {
		for i := range min(32, len(streams)) {
			if _, err := c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "stream.msgs_rate",
				Entity: streams[i], Range: liveRange(now(), span), MaxPoints: 120})); err != nil {
				return fmt.Errorf("series %d: %w", i, err)
			}
		}
		return nil
	}}
}
