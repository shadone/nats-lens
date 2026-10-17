package storeprov_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/events"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// detailStore holds two epochs of three servers, nats-0 to nats-2, with
// the VARZ limits, routes, gateways and JetStream leaders. nats-2 runs
// with an 8 MiB max_payload, the others with 1 MiB. k8s adds the
// watcher's events for nats-0's pod.
func detailStore(t *testing.T, k8s bool) *store.Store {
	t.Helper()
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	loaded := t0.Add(-2 * time.Hour)
	for i := range 2 {
		start := t0.Add(time.Duration(i+1) * 30 * time.Second)
		snap := collect.NewSnapshot(start)
		for n, name := range []string{"nats-0", "nats-1", "nats-2"} {
			id := "NDETAIL" + name
			info := server.ServerInfo{Name: name, ID: id, Cluster: "example-dev", Version: "2.15.0", JetStream: true}
			if n == 0 {
				info.Tags = []string{"az:eu-1a", "tier:fast"}
			}
			env := func(data any) []byte {
				b, err := json.Marshal(map[string]any{"server": info, "data": data})
				if err != nil {
					t.Fatal(err)
				}
				return b
			}
			apply := func(e collect.Endpoint, b []byte) {
				if err := snap.Apply(e, b); err != nil {
					t.Fatal(err)
				}
			}
			payload := int32(1 << 20)
			if n == 2 {
				payload = 8 << 20
			}
			apply(collect.Varz, env(server.Varz{ID: id, Name: name, Version: "2.15.0", Start: t0.Add(-time.Hour),
				ConfigLoadTime: loaded, MaxConn: 64000, MaxPayload: int(payload), MaxPending: 64 << 20,
				WriteDeadline: 10 * time.Second, PingInterval: 2 * time.Minute, MaxPingsOut: 2, Leafs: 1,
				SlowConsumers: int64(3 * i * (1 - n%2)),
				JetStream:     server.JetStreamVarz{Config: &server.JetStreamConfig{MaxStore: 50 << 30, MaxMemory: 2 << 30}}}))
			apply(collect.Jsz, env(server.JSInfo{ID: id, Config: server.JetStreamConfig{MaxStore: 50 << 30, MaxMemory: 2 << 30},
				Streams: 4, StreamsLeader: 2 + n, Consumers: 9, ConsumersLeader: 5 + n}))
			apply(collect.Routez, env(server.Routez{ID: id, Name: name, Routes: []*server.RouteInfo{
				{Rid: 1, RemoteName: "nats-9", RTT: "2.8ms", Pending: 128}, {Rid: 2, RemoteName: "nats-8", RTT: "300µs"},
			}}))
			apply(collect.Gatewayz, env(server.Gatewayz{ID: id, Name: "example-dev",
				OutboundGateways: map[string]*server.RemoteGatewayz{"us": {IsConfigured: true}},
				InboundGateways:  map[string][]*server.RemoteGatewayz{"us": {{}, {}}, "ap": {{}}},
			}))
		}
		snap.Expected = []string{"nats-0", "nats-1", "nats-2"}
		snap.FinishedAt = start.Add(100 * time.Millisecond)
		if _, err := st.WriteSnapshot(ctx, snap); err != nil {
			t.Fatal(err)
		}
	}
	if !k8s {
		return st
	}
	pod := func(at time.Time, kind, node string) events.Event {
		raw, _ := json.Marshal(map[string]any{"namespace": "nats", "pod": "nats-0", "uid": "u1", "node": node, "phase": "Running"})
		return events.Event{Time: at, ReceivedAt: at, Source: events.SourceK8s, Kind: kind, ServerName: "nats-0", Summary: kind, Raw: raw}
	}
	if err := st.WriteEvents(ctx, []events.Event{
		pod(t0.Add(-3*time.Hour), "k8s.pod.scheduled", "node-old"),
		pod(t0.Add(-3*time.Hour), "k8s.pod.restarted", "node-old"),
		pod(t0.Add(-20*time.Minute), "k8s.pod.scheduled", "node-b"),
		pod(t0.Add(-10*time.Minute), "k8s.pod.restarted", "node-b"),
		pod(t0.Add(-9*time.Minute), "k8s.pod.ready", "node-b"),
		// A node event names no pod.
		{Time: t0.Add(-time.Minute), ReceivedAt: t0, Source: events.SourceK8s, Kind: "k8s.node.cordoned", ServerName: "nats-0",
			Summary: "cordoned", Raw: json.RawMessage(`{"node":"node-b"}`)},
	}); err != nil {
		t.Fatal(err)
	}
	return st
}

func getServer(t *testing.T, st *store.Store, name string) *v1.GetServerResponse {
	t.Helper()
	_, c := serve(t, st)
	return must(c.GetServer(ctx, connect.NewRequest(&v1.GetServerRequest{Name: name, Range: hour()})))
}

func TestServerSummaryLeadersTagsSlow(t *testing.T) {
	_, c := serve(t, detailStore(t, false))
	sv := must(c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{Range: hour()})))
	if len(sv.GetServers()) != 3 {
		t.Fatalf("servers = %v", sv.GetServers())
	}
	n0, n1 := sv.GetServers()[0], sv.GetServers()[1]
	if n0.StreamLeaders == nil || n0.GetStreamLeaders() != 2 || n0.GetConsumerLeaders() != 5 || n1.GetStreamLeaders() != 3 {
		t.Errorf("leaders nats-0 %v/%v, nats-1 %v", n0.StreamLeaders, n0.ConsumerLeaders, n1.StreamLeaders)
	}
	if !slices.Equal(n0.GetTags(), []string{"az:eu-1a", "tier:fast"}) || len(n1.GetTags()) != 0 {
		t.Errorf("tags nats-0 %v, nats-1 %v", n0.GetTags(), n1.GetTags())
	}
	// nats-0 counts 0 then 3; nats-1 reports 0 twice: no slow consumers,
	// which is 0, not unset.
	if n0.SlowConsumersInRange == nil || n0.GetSlowConsumersInRange() != 3 || n1.SlowConsumersInRange == nil || n1.GetSlowConsumersInRange() != 0 {
		t.Errorf("slow in range nats-0 %v, nats-1 %v", n0.SlowConsumersInRange, n1.SlowConsumersInRange)
	}
}

func TestServerDetailLimitsRoutesGateways(t *testing.T) {
	st := detailStore(t, false)
	gs := getServer(t, st, "nats-0")
	l := gs.GetLimits()
	if l.GetMaxPayload() != 1<<20 || l.GetMaxPending() != 64<<20 || l.GetWriteDeadline().AsDuration() != 10*time.Second ||
		l.GetPingInterval().AsDuration() != 2*time.Minute || l.GetMaxPingsOut() != 2 || l.GetJsMaxStorage() != 50<<30 ||
		l.GetJsMaxMemory() != 2<<30 || l.GetMaxConnections() != 64000 {
		t.Errorf("limits = %v", l)
	}
	if len(gs.GetLimitsDiffer()) != 0 {
		t.Errorf("nats-0 has the majority's limits, differ = %v", gs.GetLimitsDiffer())
	}
	if len(gs.GetRouteViews()) != 2 {
		t.Fatalf("routes = %v", gs.GetRouteViews())
	}
	r := gs.GetRouteViews()[1]
	if r.GetRemoteName() != "nats-9" || r.GetRtt().AsDuration() != 2800*time.Microsecond || r.GetPendingBytes() != 128 {
		t.Errorf("route to nats-9 = %v", r)
	}
	if gs.Gateways == nil || gs.GetGateways() != 2 || gs.GetLeafnodes() != 1 {
		t.Errorf("gateways %v, leafnodes %v", gs.Gateways, gs.Leafnodes)
	}
	if !gs.GetConfigLoadedAt().AsTime().Equal(t0.Add(-2 * time.Hour)) {
		t.Errorf("config loaded %v", gs.GetConfigLoadedAt())
	}
	if d := getServer(t, st, "nats-2").GetLimitsDiffer(); !slices.Equal(d, []string{"max_payload"}) {
		t.Errorf("nats-2 differ = %v, want max_payload", d)
	}
}

func TestServerDetailKubernetes(t *testing.T) {
	st := detailStore(t, true)
	k := getServer(t, st, "nats-0").GetKubernetes()
	if k.GetPod() != "nats-0" || k.GetNamespace() != "nats" || k.GetNode() != "node-b" ||
		!k.GetScheduledAt().AsTime().Equal(t0.Add(-20*time.Minute)) || k.GetRestartsInRange() != 1 {
		t.Errorf("kubernetes = %v", k)
	}
	// No event names nats-1's pod.
	if k := getServer(t, st, "nats-1").Kubernetes; k != nil {
		t.Errorf("nats-1 kubernetes = %v, want unset", k)
	}
}

// Without the watcher no k8s event exists: the placement is unset, not a
// message of empty strings.
func TestServerDetailKubernetesOff(t *testing.T) {
	if k := getServer(t, detailStore(t, false), "nats-0").Kubernetes; k != nil {
		t.Fatalf("kubernetes = %v, want unset", k)
	}
}
