package fake_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
)

func demo(t *testing.T) natslensv1connect.LensServiceClient {
	t.Helper()
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: fake.Demo(false)})
	return c
}

func TestDemoListsThreeSystems(t *testing.T) {
	res := must(demo(t).ListSystems(ctx, connect.NewRequest(&v1.ListSystemsRequest{})))
	var ids []string
	for _, s := range res.GetSystems() {
		ids = append(ids, s.GetSystem().GetId())
		if s.GetError() != "" || len(s.GetStrip()) != 48 || s.GetTally() == nil || s.GetFreshness() == nil {
			t.Errorf("%s: error %q, %d strip buckets, tally %v", s.GetSystem().GetId(), s.GetError(), len(s.GetStrip()), s.GetTally())
		}
	}
	if strings.Join(ids, ",") != "example-prod,example-global,example-staging" || res.GetDefaultSystem() != "example-prod" {
		t.Fatalf("systems %v, default %q", ids, res.GetDefaultSystem())
	}
	prod, global, staging := res.GetSystems()[0], res.GetSystems()[1], res.GetSystems()[2]

	if prod.GetServers() != 3 || prod.GetServersAnswering() != 3 || prod.GetClusters() != 1 || prod.GetStreams() != 13 || prod.GetConnections() != 182 ||
		prod.GetCollection() != v1.CollectionState_COLLECTION_STATE_DEMO || prod.GetStatus() != v1.Status_STATUS_CRITICAL ||
		prod.GetWorstFinding().GetCheckCode() != "consumer-010" {
		t.Errorf("prod = %v", prod)
	}
	if global.GetServers() != 24 || global.GetServersAnswering() != 23 || global.GetClusters() != 3 ||
		global.GetCollection() != v1.CollectionState_COLLECTION_STATE_DEMO || global.GetStatus() != v1.Status_STATUS_WARNING ||
		global.GetWorstFinding().GetCheckCode() != "server-001" || global.GetTally().GetMuted() != 2 || !global.GetFreshness().GetPartial() {
		t.Errorf("global = %v", global)
	}
	if staging.GetCollection() != v1.CollectionState_COLLECTION_STATE_CONNECTION_CLOSED || !strings.Contains(staging.GetCollectionError(), "authorization") ||
		!staging.GetCollectionErrorSince().AsTime().Equal(fake.FixedNow.Add(-3*time.Hour)) {
		t.Errorf("staging collection %v %q since %v", staging.GetCollection(), staging.GetCollectionError(), staging.GetCollectionErrorSince())
	}
	// Its newest epoch is three hours old, and the strip has no data since.
	if age := staging.GetFreshness().GetAge().AsDuration(); age < 3*time.Hour || age > 3*time.Hour+time.Minute || staging.GetServers() != 3 {
		t.Errorf("staging freshness %v, servers %d", staging.GetFreshness(), staging.GetServers())
	}
	// Nothing writes to a system that is not collecting.
	if staging.GetFreshness().GetLive() || !prod.GetFreshness().GetLive() || !global.GetFreshness().GetLive() {
		t.Errorf("live: prod %v, global %v, staging %v", prod.GetFreshness().GetLive(), global.GetFreshness().GetLive(), staging.GetFreshness().GetLive())
	}
	strip := staging.GetStrip()
	if strip[len(strip)-6] != v1.Status_STATUS_NO_DATA || strip[len(strip)-1] != v1.Status_STATUS_NO_DATA || strip[0] == v1.Status_STATUS_NO_DATA {
		t.Errorf("staging strip = %v", strip)
	}
}

func TestDemoGlobalServers(t *testing.T) {
	c := demo(t)
	o := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{System: fake.GlobalSystem})))
	if o.GetCluster() == fake.Cluster || len(o.GetServers()) != 24 {
		t.Fatalf("global overview: cluster %q, %d servers", o.GetCluster(), len(o.GetServers()))
	}
	sv := must(c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{System: fake.GlobalSystem})))
	clusters := map[string]int{}
	versions := map[string]int{}
	var fullest float64
	var leaders []string
	for _, s := range sv.GetServers() {
		clusters[s.GetCluster()]++
		versions[s.GetVersion()]++
		if s.GetRole() == v1.ServerRole_SERVER_ROLE_META_LEADER {
			leaders = append(leaders, s.GetName())
		}
		if s.GetPresent() {
			fullest = max(fullest, float64(s.GetJsStorageBytes())/float64(s.GetJsMaxStorageBytes()))
		}
	}
	if len(clusters) != 3 || clusters["eu"] == 0 || clusters["us"] == 0 || clusters["ap"] == 0 {
		t.Errorf("clusters = %v", clusters)
	}
	if len(versions) != 2 || slices.Min([]int{versions["2.12.4"], versions["2.12.2"]}) != 2 {
		t.Errorf("versions = %v", versions)
	}
	if strings.Join(sv.GetMissing(), ",") != "nats-us-4" || len(sv.GetExpected()) != 24 || len(leaders) != 1 {
		t.Errorf("missing %v, %d expected, leaders %v", sv.GetMissing(), len(sv.GetExpected()), leaders)
	}
	if fullest < 0.705 || fullest > 0.715 {
		t.Errorf("fullest store at %.3f, want 71%%", fullest)
	}
	// The overview of the default system is still example-prod.
	if o := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{}))); o.GetCluster() != fake.Cluster {
		t.Errorf("default overview cluster %q", o.GetCluster())
	}
}

// Every entity the non-default systems list answers its own detail call
// and its series, so that the later pages have data to show.
func TestDemoSystemsAnswerEveryCall(t *testing.T) {
	c := demo(t)
	for _, sys := range []string{fake.GlobalSystem, fake.StagingSystem} {
		t.Run(sys, func(t *testing.T) {
			series := func(metric string, e *v1.EntityRef) {
				t.Helper()
				s := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{System: sys, Metric: metric, Entity: e})))
				if len(s.GetSeries().GetPoints()) == 0 {
					t.Errorf("%s %s has no points", metric, e.GetKey())
				}
			}
			for _, m := range api.MetricList() {
				if m.GetEntityType() == v1.EntityType_ENTITY_TYPE_SYSTEM && m.GetId() != "cluster.slow_consumers" {
					series(m.GetId(), nil)
				}
			}
			for _, s := range must(c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{System: sys}))).GetServers() {
				must(c.GetServer(ctx, connect.NewRequest(&v1.GetServerRequest{System: sys, Name: s.GetName()})))
				if s.GetPresent() {
					series("server.cpu_percent", &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: s.GetName()})
				}
			}
			streams := must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{System: sys, IncludeKv: true}))).GetStreams()
			if len(streams) < 3 {
				t.Errorf("%d streams", len(streams))
			}
			for _, s := range streams {
				must(c.GetStream(ctx, connect.NewRequest(&v1.GetStreamRequest{System: sys, Account: s.GetEntity().GetAccount(), Stream: s.GetEntity().GetStream()})))
				series("stream.bytes", s.GetEntity())
			}
			for _, cs := range must(c.ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{System: sys}))).GetConsumers() {
				e := cs.GetEntity()
				must(c.GetConsumer(ctx, connect.NewRequest(&v1.GetConsumerRequest{System: sys, Account: e.GetAccount(), Stream: e.GetStream(), Consumer: e.GetConsumer()})))
				series("consumer.num_pending", e)
			}
			fs := must(c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{System: sys, IncludeMuted: true,
				States: []v1.FindingState{v1.FindingState_FINDING_STATE_FIRING, v1.FindingState_FINDING_STATE_FLAPPING, v1.FindingState_FINDING_STATE_RESOLVED}})))
			if len(fs.GetFindings()) == 0 {
				t.Error("no findings")
			}
			for _, f := range fs.GetFindings() {
				must(c.GetFinding(ctx, connect.NewRequest(&v1.GetFindingRequest{System: sys, Id: f.GetId()})))
			}
			evs := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{System: sys, PageSize: 500})))
			if len(evs.GetEvents()) < 10 {
				t.Errorf("%d events", len(evs.GetEvents()))
			}
			for _, e := range evs.GetEvents()[:10] {
				must(c.GetEvent(ctx, connect.NewRequest(&v1.GetEventRequest{System: sys, Id: e.GetId()})))
			}
			tl := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{System: sys})))
			if len(tl.GetRows()) == 0 {
				t.Error("no timeline rows")
			}
			if n := must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{System: sys}))).GetTotalCount(); n == 0 {
				t.Error("no connections")
			}
		})
	}
}

// Live mode moves every demo system with the clock.
func TestDemoLiveSystems(t *testing.T) {
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: fake.Demo(true)})
	res := must(c.ListSystems(ctx, connect.NewRequest(&v1.ListSystemsRequest{})))
	for _, s := range res.GetSystems() {
		if age := time.Since(s.GetFreshness().GetEpochAt().AsTime()); s.GetFreshness().GetEpochAt() == nil || age > 3*time.Hour+time.Minute {
			t.Errorf("%s: newest epoch %v", s.GetSystem().GetId(), s.GetFreshness().GetEpochAt())
		}
	}
	if s := res.GetSystems()[2]; time.Since(s.GetCollectionErrorSince().AsTime()) < 3*time.Hour-time.Minute {
		t.Errorf("staging error since %v", s.GetCollectionErrorSince())
	}
}

// example-staging's freshness says it does not collect, since its
// connection closed; the others collect.
func TestDemoFreshnessSaysWhetherItCollects(t *testing.T) {
	c := demo(t)
	stop := fake.FixedNow.Add(-3 * time.Hour)
	for _, sys := range []string{fake.Cluster, fake.GlobalSystem, fake.StagingSystem} {
		f := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{System: sys}))).GetFreshness()
		if sys == fake.StagingSystem {
			if f.GetCollecting() || !f.GetNotCollectingSince().AsTime().Equal(stop) {
				t.Errorf("%s = %v", sys, f)
			}
			continue
		}
		if !f.GetCollecting() || f.GetNotCollectingSince() != nil {
			t.Errorf("%s = %v", sys, f)
		}
	}
	res := must(c.ListSystems(ctx, connect.NewRequest(&v1.ListSystemsRequest{})))
	for _, s := range res.GetSystems() {
		if f := s.GetFreshness(); f.GetCollecting() != (s.GetSystem().GetId() != fake.StagingSystem) {
			t.Errorf("%s summary = %v", s.GetSystem().GetId(), f)
		}
	}
}
