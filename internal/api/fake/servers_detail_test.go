package fake_test

import (
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
)

func detail(t *testing.T, c natslensv1connect.LensServiceClient, system, name string) *v1.GetServerResponse {
	t.Helper()
	return must(c.GetServer(ctx, connect.NewRequest(&v1.GetServerRequest{System: system, Name: name})))
}

// Every example-global server has its limits, routes, gateways and pod;
// one runs with a larger max_payload than the others.
func TestGlobalServerDetail(t *testing.T) {
	c := demo(t)
	sv := must(c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{System: fake.GlobalSystem})))
	var differ []string
	var leaders, slow int64
	for _, s := range sv.GetServers() {
		d := detail(t, c, fake.GlobalSystem, s.GetName())
		if len(d.GetLimitsDiffer()) > 0 {
			differ = append(differ, s.GetName()+":"+strings.Join(d.GetLimitsDiffer(), ","))
		}
		if d.GetLimits() == nil || d.GetLimits().MaxPayload == nil || d.GetLimits().GetWriteDeadline() == nil || d.GetLimits().GetJsMaxStorage() != s.GetJsMaxStorageBytes() {
			t.Errorf("%s limits %v", s.GetName(), d.GetLimits())
		}
		// Routes to the other servers of the cluster; none to nats-us-4,
		// which stopped answering.
		routes := map[string]int{"eu": 8, "us": 7, "ap": 5}[s.GetCluster()]
		if s.GetName() == "nats-us-4" {
			routes = 8
		}
		if len(d.GetRouteViews()) != routes || int32(routes) != d.GetRoutes() || d.GetGateways() != 2 || d.Leafnodes == nil {
			t.Errorf("%s routes %d (%d), gateways %d", s.GetName(), len(d.GetRouteViews()), d.GetRoutes(), d.GetGateways())
		}
		k := d.GetKubernetes()
		if k.GetPod() != s.GetName() || k.GetNamespace() == "" || (k.GetNode() == "") != (s.GetName() == "nats-us-4") {
			t.Errorf("%s kubernetes %v", s.GetName(), k)
		}
		if len(s.GetTags()) == 0 || !strings.HasPrefix(s.GetTags()[0], "az:"+s.GetCluster()) {
			t.Errorf("%s tags %v", s.GetName(), s.GetTags())
		}
		if s.StreamLeaders == nil || s.ConsumerLeaders == nil || s.SlowConsumersInRange == nil || d.GetConfigLoadedAt() == nil {
			t.Errorf("%s leaders %v/%v, slow %v", s.GetName(), s.StreamLeaders, s.ConsumerLeaders, s.SlowConsumersInRange)
		}
		leaders += int64(s.GetStreamLeaders())
		slow += s.GetSlowConsumersInRange()
	}
	if !slices.Equal(differ, []string{"nats-ap-3:max_payload"}) {
		t.Errorf("servers with differing limits = %v", differ)
	}
	// 13 streams, 2 KV stores and an object store, each led by one server
	// except basket-events, which has no leader in the newest epoch.
	if leaders != 15 {
		t.Errorf("stream leaders sum to %d, want 15", leaders)
	}
	// The servers' slow consumers add up to the system's count in the range.
	series := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{System: fake.GlobalSystem, Metric: "cluster.slow_consumers"})))
	var total float64
	for _, p := range series.GetSeries().GetPoints() {
		total += p.GetValue()
	}
	if slow != int64(total) || slow == 0 {
		t.Errorf("servers' slow consumers in range = %d, system %v", slow, total)
	}
	if n3 := detail(t, c, fake.GlobalSystem, "nats-us-3"); n3.GetSlowConsumers() == 0 {
		t.Errorf("nats-us-3 drops label-printer, slow consumers = %v", n3.SlowConsumers)
	}
}

// example-prod runs on Kubernetes in one cluster; example-staging has no
// watcher, so its servers have no placement.
func TestProdAndStagingServerDetail(t *testing.T) {
	c := demo(t)
	p := detail(t, c, "", "nats-1")
	if p.GetKubernetes().GetNode() == "" || len(p.GetRouteViews()) != 2 || p.GetGateways() != 0 || p.Gateways == nil || len(p.GetLimitsDiffer()) != 0 {
		t.Errorf("prod nats-1 = kubernetes %v, routes %v, gateways %v, differ %v", p.GetKubernetes(), p.GetRouteViews(), p.Gateways, p.GetLimitsDiffer())
	}
	s := detail(t, c, fake.StagingSystem, "nats-1")
	if s.Kubernetes != nil || s.GetLimits() == nil {
		t.Errorf("staging nats-1 kubernetes %v, limits %v", s.Kubernetes, s.GetLimits())
	}
}
