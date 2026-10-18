package apitest

import (
	"context"
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
)

// Env is one provider's systems served over HTTP, as the conformance
// table reads them. The store provider runs it over a store fixture, the
// fake over the demo's systems.
type Env struct {
	Client natslensv1connect.LensServiceClient
	// Systems are the system IDs a state may be found in.
	Systems []string
	// Now is the providers' present.
	Now time.Time
}

// State is one row of the conformance table: a state of the API's
// responses that the UI shows in its own way, and how to find it in one
// system. Every row must be found in at least one system of each
// provider.
type State struct {
	Name string
	// Find returns where the state was found in the system q reads, or
	// "" when it is not there.
	Find func(q Q) string
}

// Q reads one system of an Env; its methods fail the test on an error.
type Q struct {
	t      *testing.T
	env    Env
	System string
}

// Last is the range of the d before the providers' present.
func (q Q) Last(d time.Duration) *v1.TimeRange {
	return &v1.TimeRange{Start: timestamppb.New(q.env.Now.Add(-d)), End: timestamppb.New(q.env.Now)}
}

// day is the default range: the 24 hours before now.
func (q Q) day() *v1.TimeRange { return q.Last(24 * time.Hour) }

// result is a call's response or error; of fails the test on the error.
type result[T any] struct {
	res *connect.Response[T]
	err error
}

func must[T any](res *connect.Response[T], err error) result[T] { return result[T]{res, err} }

func (r result[T]) of(t *testing.T) *T {
	t.Helper()
	if r.err != nil {
		t.Fatal(r.err)
	}
	return r.res.Msg
}

var ctx = context.Background()

func (q Q) streams(r *v1.TimeRange) []*v1.StreamSummary {
	q.t.Helper()
	return must(q.env.Client.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{System: q.System, Range: r, IncludeKv: true}))).of(q.t).GetStreams()
}

func (q Q) stream(e *v1.EntityRef) *v1.GetStreamResponse {
	q.t.Helper()
	return must(q.env.Client.GetStream(ctx, connect.NewRequest(&v1.GetStreamRequest{System: q.System, Range: q.day(),
		Account: e.GetAccount(), Stream: e.GetStream()}))).of(q.t)
}

func (q Q) consumers() []*v1.ConsumerSummary {
	q.t.Helper()
	return must(q.env.Client.ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{System: q.System, Range: q.day()}))).of(q.t).GetConsumers()
}

func (q Q) consumer(e *v1.EntityRef) *v1.GetConsumerResponse {
	q.t.Helper()
	return must(q.env.Client.GetConsumer(ctx, connect.NewRequest(&v1.GetConsumerRequest{System: q.System, Range: q.day(),
		Account: e.GetAccount(), Stream: e.GetStream(), Consumer: e.GetConsumer()}))).of(q.t)
}

func (q Q) findings(states ...v1.FindingState) []*v1.Finding {
	q.t.Helper()
	return must(q.env.Client.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{System: q.System, Range: q.day(),
		States: states, IncludeMuted: true, PageSize: 1000}))).of(q.t).GetFindings()
}

func (q Q) freshness(r *v1.TimeRange) *v1.Freshness {
	q.t.Helper()
	return must(q.env.Client.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{System: q.System, Range: r}))).of(q.t).GetFreshness()
}

func (q Q) summary() *v1.SystemSummary {
	q.t.Helper()
	for _, s := range must(q.env.Client.ListSystems(ctx, connect.NewRequest(&v1.ListSystemsRequest{}))).of(q.t).GetSystems() {
		if s.GetSystem().GetId() == q.System {
			return s
		}
	}
	q.t.Fatalf("system %q not listed", q.System)
	return nil
}

// streamWith finds a stream whose placement has a replica in state st.
func streamWith(st v1.PlacementState) func(Q) string {
	return func(q Q) string {
		for _, s := range q.streams(q.day()) {
			for _, p := range s.GetPlacement() {
				if p.GetState() == st {
					return s.GetEntity().GetKey() + " on " + p.GetServer()
				}
			}
		}
		return ""
	}
}

func consumerWhere(ok func(*v1.ConsumerSummary) bool) func(Q) string {
	return func(q Q) string {
		for _, c := range q.consumers() {
			if ok(c) {
				return c.GetEntity().GetKey()
			}
		}
		return ""
	}
}

func findingIn(st v1.FindingState) func(Q) string {
	return func(q Q) string {
		for _, f := range q.findings(st) {
			if f.GetState() == st {
				return f.GetId()
			}
		}
		return ""
	}
}

// growthAgrees reports whether a stream's growth per day is the Theil-Sen
// slope of its own stream.bytes series over the same range, within 2%
// and 1 KiB a day: the series' buckets and the growth's medians may fall
// a little apart.
func growthAgrees(q Q, s *v1.StreamSummary, r *v1.TimeRange) (string, bool) {
	q.t.Helper()
	ser := must(q.env.Client.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{System: q.System, Range: r,
		Metric: "stream.bytes", Entity: s.GetEntity(), MaxPoints: api.GrowthBuckets}))).of(q.t).GetSeries()
	var pts []api.Sample
	for _, p := range ser.GetPoints() {
		pts = append(pts, api.Sample{T: p.GetTime().AsTime(), V: p.GetValue()})
	}
	want := api.RobustPerDay(pts)
	if want == nil {
		return fmt.Sprintf("%s: growth %.0f/day, but its series gives none", s.GetEntity().GetKey(), s.GetBytesPerDay()), false
	}
	if math.Abs(s.GetBytesPerDay()-*want) > max(0.02*math.Abs(*want), 1024) {
		return fmt.Sprintf("%s: growth %.0f/day, its series gives %.0f/day", s.GetEntity().GetKey(), s.GetBytesPerDay(), *want), false
	}
	return "", true
}

// Conformance is the table of states both providers must show.
func Conformance() []State {
	return []State{
		// Placement: one state per square the UI draws.
		{"a stream with a leader and a replica in sync", func(q Q) string {
			for _, s := range q.streams(q.day()) {
				var states []v1.PlacementState
				for _, p := range s.GetPlacement() {
					states = append(states, p.GetState())
				}
				if s.GetLeader() != "" && slices.Contains(states, v1.PlacementState_PLACEMENT_STATE_LEADER) &&
					slices.Contains(states, v1.PlacementState_PLACEMENT_STATE_IN_SYNC) {
					return s.GetEntity().GetKey()
				}
			}
			return ""
		}},
		{"a stream replica behind its leader", streamWith(v1.PlacementState_PLACEMENT_STATE_BEHIND)},
		{"a stream replica whose server is offline", streamWith(v1.PlacementState_PLACEMENT_STATE_OFFLINE)},
		{"a stream replica that disagrees with its leader", streamWith(v1.PlacementState_PLACEMENT_STATE_DISAGREES)},
		// A MISSING square with a server: one without a server is a
		// configured replica no server names, a different state.
		{"a stream replica the leader lists that no server reports", func(q Q) string {
			for _, s := range q.streams(q.day()) {
				for _, p := range s.GetPlacement() {
					if p.GetState() == v1.PlacementState_PLACEMENT_STATE_MISSING && p.GetServer() != "" {
						return s.GetEntity().GetKey() + " on " + p.GetServer()
					}
				}
			}
			return ""
		}},
		// A server that does not answer reports no view: the details list
		// the servers that report the stream or consumer, no empty rows.
		{"an offline replica's server has no empty view in the stream detail", func(q Q) string {
			for _, s := range q.streams(q.day()) {
				for _, p := range s.GetPlacement() {
					if p.GetState() != v1.PlacementState_PLACEMENT_STATE_OFFLINE {
						continue
					}
					if slices.ContainsFunc(q.stream(s.GetEntity()).GetServers(), func(v *v1.StreamServerView) bool {
						return v.GetServer() == p.GetServer() && v.Messages == nil
					}) {
						q.t.Errorf("system %q: %s has an empty view for %s", q.System, s.GetEntity().GetKey(), p.GetServer())
						return ""
					}
					return s.GetEntity().GetKey() + " on " + p.GetServer()
				}
			}
			return ""
		}},
		{"an offline replica's server has no empty view in its consumers' details", func(q Q) string {
			for _, s := range q.streams(q.day()) {
				for _, p := range s.GetPlacement() {
					if p.GetState() != v1.PlacementState_PLACEMENT_STATE_OFFLINE {
						continue
					}
					for _, c := range q.stream(s.GetEntity()).GetConsumers() {
						if !c.GetPresent() {
							continue
						}
						if slices.ContainsFunc(q.consumer(c.GetEntity()).GetServers(), func(v *v1.ConsumerServerView) bool {
							return v.GetServer() == p.GetServer() && v.DeliveredStreamSeq == nil
						}) {
							q.t.Errorf("system %q: %s has an empty view for %s", q.System, c.GetEntity().GetKey(), p.GetServer())
							return ""
						}
						return c.GetEntity().GetKey() + " beside " + p.GetServer()
					}
				}
			}
			return ""
		}},
		{"a stream without a leader, its replicas' states unknown", func(q Q) string {
			for _, s := range q.streams(q.day()) {
				ps := s.GetPlacement()
				if s.GetLeader() == "" && len(ps) > 0 && !slices.ContainsFunc(ps, func(p *v1.ReplicaPlacement) bool {
					return p.GetState() != v1.PlacementState_PLACEMENT_STATE_UNSPECIFIED &&
						p.GetState() != v1.PlacementState_PLACEMENT_STATE_MISSING
				}) && slices.ContainsFunc(ps, func(p *v1.ReplicaPlacement) bool {
					return p.GetState() == v1.PlacementState_PLACEMENT_STATE_UNSPECIFIED
				}) {
					return s.GetEntity().GetKey()
				}
			}
			return ""
		}},
		{"a stream whose servers disagree, with the setting in its detail", func(q Q) string {
			for _, s := range q.streams(q.day()) {
				if s.GetServersDisagree() && len(q.stream(s.GetEntity()).GetDisagreements()) > 0 {
					return s.GetEntity().GetKey()
				}
			}
			return ""
		}},

		// Growth, computed the same way by both providers.
		{"stream growth per day from six hours or more, the Theil-Sen slope of its series", func(q Q) string {
			r, found := q.day(), ""
			for _, s := range q.streams(r) {
				if s.BytesPerDay == nil {
					continue
				}
				if why, ok := growthAgrees(q, s, r); !ok {
					q.t.Errorf("system %q: %s", q.System, why)
					return ""
				}
				found = s.GetEntity().GetKey()
			}
			return found
		}},
		{"no stream growth over a range shorter than six hours", func(q Q) string {
			ss := q.streams(q.Last(time.Hour))
			if len(ss) == 0 {
				return ""
			}
			for _, s := range ss {
				if s.BytesPerDay != nil {
					q.t.Errorf("system %q: %s has growth %v over one hour", q.System, s.GetEntity().GetKey(), s.GetBytesPerDay())
					return ""
				}
			}
			return fmt.Sprintf("%d streams", len(ss))
		}},
		{"a stream or bucket that shrinks", func(q Q) string {
			for _, s := range q.streams(q.day()) {
				if s.BytesPerDay != nil && s.GetBytesPerDay() < 0 {
					return s.GetEntity().GetKey()
				}
			}
			return ""
		}},

		// Consumers.
		{"a consumer delivering, its rate set", consumerWhere(func(c *v1.ConsumerSummary) bool {
			return c.GetPresent() && c.DeliveredPerSecond != nil && c.GetDeliveredPerSecond() > 0
		})},
		{"a consumer that delivered nothing, its rate zero", consumerWhere(func(c *v1.ConsumerSummary) bool {
			return c.GetPresent() && c.DeliveredPerSecond != nil && c.GetDeliveredPerSecond() == 0
		})},
		// In a system that collects: one that stopped has no rate for any
		// consumer.
		{"a present consumer without a rate yet, in a system that collects", func(q Q) string {
			if !q.freshness(q.day()).GetCollecting() {
				return ""
			}
			return consumerWhere(func(c *v1.ConsumerSummary) bool { return c.GetPresent() && c.DeliveredPerSecond == nil })(q)
		}},
		{"a paused consumer", consumerWhere(func(c *v1.ConsumerSummary) bool { return c.GetPresent() && c.GetPaused() })},
		{"an expected consumer that is missing, without numbers", consumerWhere(func(c *v1.ConsumerSummary) bool {
			return !c.GetPresent() && c.NumPending == nil && c.NumAckPending == nil && c.DeliveredPerSecond == nil
		})},
		{"a consumer whose servers disagree, with the setting in its detail", func(q Q) string {
			for _, c := range q.consumers() {
				if c.GetServersDisagree() && len(q.consumer(c.GetEntity()).GetDisagreements()) > 0 {
					return c.GetEntity().GetKey()
				}
			}
			return ""
		}},

		// Buckets.
		{"a bucket the inventory lists that no server reports", func(q Q) string {
			res := must(q.env.Client.ListKeyValueStores(ctx, connect.NewRequest(&v1.ListKeyValueStoresRequest{System: q.System, Range: q.day()}))).of(q.t)
			for _, b := range res.GetKeyValueStores() {
				if !b.GetPresent() && b.GetExpected() && len(b.GetPlacement()) == 0 && b.Bytes == nil {
					return b.GetEntity().GetKey()
				}
			}
			return ""
		}},

		// Findings: each state the UI labels.
		{"a firing finding", findingIn(v1.FindingState_FINDING_STATE_FIRING)},
		{"a finding whose severity changed while firing", func(q Q) string {
			for _, f := range q.findings(v1.FindingState_FINDING_STATE_CHANGED) {
				tr := must(q.env.Client.GetFinding(ctx, connect.NewRequest(&v1.GetFindingRequest{System: q.System, Id: f.GetId()}))).of(q.t).GetTransitions()
				if len(tr) > 1 && tr[0].GetState() == v1.FindingState_FINDING_STATE_CHANGED && tr[0].GetSeverity() == f.GetSeverity() &&
					tr[len(tr)-1].GetSeverity() != f.GetSeverity() {
					return f.GetId()
				}
			}
			return ""
		}},
		{"a flapping finding", findingIn(v1.FindingState_FINDING_STATE_FLAPPING)},
		{"a finding resolved in the range", findingIn(v1.FindingState_FINDING_STATE_RESOLVED)},
		{"a muted firing finding", func(q Q) string {
			for _, f := range q.findings() {
				if f.GetMute() != nil {
					return f.GetId()
				}
			}
			return ""
		}},

		// Freshness and collection.
		{"a newest epoch that missed a server", func(q Q) string {
			if q.freshness(q.day()).GetPartial() {
				return "partial"
			}
			return ""
		}},
		{"a past range that holds epochs reports its newest epoch", func(q Q) string {
			end := q.env.Now.Add(-time.Hour)
			f := q.freshness(&v1.TimeRange{Start: timestamppb.New(q.env.Now.Add(-3 * time.Hour)), End: timestamppb.New(end)})
			if at := f.GetEpochAt().AsTime(); f.GetEpoch() != 0 && f.GetEpochAt() != nil && at.Before(end) && end.Sub(at) <= 10*time.Minute {
				return at.String()
			}
			return ""
		}},
		{"a system that collects", func(q Q) string {
			s := q.summary()
			if s.GetFreshness().GetCollecting() && s.GetFreshness().GetLive() && s.GetCollectionError() == "" {
				return s.GetCollection().String()
			}
			return ""
		}},
		{"a system whose collector lost its connection", func(q Q) string {
			s := q.summary()
			switch s.GetCollection() {
			case v1.CollectionState_COLLECTION_STATE_NOT_CONNECTED, v1.CollectionState_COLLECTION_STATE_CONNECTION_CLOSED:
				if s.GetCollectionError() != "" && s.GetCollectionErrorSince() != nil {
					return s.GetCollection().String()
				}
			}
			return ""
		}},
	}
}

// RunConformance checks every row of the table against env: each must be
// found in one of its systems.
func RunConformance(t *testing.T, env Env) {
	t.Helper()
	for _, row := range Conformance() {
		t.Run(row.Name, func(t *testing.T) {
			for _, sys := range env.Systems {
				if where := row.Find(Q{t: t, env: env, System: sys}); where != "" {
					t.Logf("%s: %s", sys, where)
					return
				}
			}
			t.Errorf("not found in %v", env.Systems)
		})
	}
}
