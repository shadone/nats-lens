package storeprov_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// topoEpochs is the number of epochs of the topology fixture.
const topoEpochs = 12

// topoAt is when epoch e of the topology fixture started, the last at
// last: 30 seconds apart, with 10 minutes without collections between
// epochs 4 and 5.
func topoAt(last time.Time, e int64) time.Time {
	at := last.Add(-time.Duration(topoEpochs-e) * 30 * time.Second)
	if e <= 4 {
		at = at.Add(-10 * time.Minute)
	}
	return at
}

// topologyStore writes a system of two clusters whose newest epoch
// started at last:
//   - eu: nats-eu-0 to nats-eu-4, routes between all of them, one pooled
//     and, between nats-eu-0 and nats-eu-1, one for APP; nats-eu-4's at
//     4 ms, the others' at 0.4 ms.
//   - us: nats-us-0 and nats-us-1; nats-us-0 lists a route to nats-us-1,
//     which answers ROUTEZ and lists none. nats-us-1 restarted before
//     epoch 6; nats-us-2 is expected and missing in the newest epoch.
//   - Gateways: every server one outbound to the other cluster. nats-eu-0's
//     reconnected before epoch 11 (a drop in the last hour); nats-eu-1's
//     across the gap (no drop); nats-eu-2's GATEWAYZ failed in epoch 8,
//     its ID changing on both sides of it (no drop).
//   - Leaf nodes: warehouse-eu-01 on nats-eu-0 throughout; pickup-0417 on
//     nats-eu-1, reconnecting before epoch 7, gone in 10, back on
//     nats-eu-3 in 11 and gone again in 12 (3 drops); pickup-0420 on
//     nats-eu-2 in the newest epoch only (drops unset); pickup-0500 on
//     nats-eu-4 in epoch 11, whose LEAFZ failed in 12 (left out); and
//     nats-eu-0's own uplink to a hub, which is no site.
func topologyStore(t *testing.T, last time.Time) *store.Store {
	t.Helper()
	st := emptyStore(t)
	r := rows{t, st}
	type srv struct {
		id, name, cluster string
		from, to          int64
	}
	servers := []srv{{"E0", "nats-eu-0", "eu", 1, topoEpochs}, {"E1", "nats-eu-1", "eu", 1, topoEpochs}, {"E2", "nats-eu-2", "eu", 1, topoEpochs},
		{"E3", "nats-eu-3", "eu", 1, topoEpochs}, {"E4", "nats-eu-4", "eu", 1, topoEpochs}, {"U0", "nats-us-0", "us", 1, topoEpochs},
		{"U1OLD", "nats-us-1", "us", 1, 5}, {"U1", "nats-us-1", "us", 6, topoEpochs}, {"U2", "nats-us-2", "us", 1, topoEpochs - 1}}
	for _, s := range servers {
		r.add("servers", map[string]any{"server_id": s.id, "server_name": s.name, "cluster": s.cluster, "tags": []string{"region:" + s.cluster},
			"first_epoch": s.from, "last_epoch": s.to, "first_seen_at": topoAt(last, s.from), "last_seen_at": topoAt(last, s.to)})
	}
	up := func(e int64) []srv {
		var out []srv
		for _, s := range servers {
			if e >= s.from && e <= s.to {
				out = append(out, s)
			}
		}
		return out
	}
	for e := int64(1); e <= topoEpochs; e++ {
		var names, missing []string
		for _, s := range up(e) {
			names = append(names, s.name)
		}
		if e == topoEpochs {
			missing = []string{"nats-us-2"}
		}
		expected := append(slices.Clone(names), missing...)
		slices.Sort(expected)
		if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, ?, ?, ?, ?, ?, 0, ?, [], NULL)`, e, topoAt(last, e), topoAt(last, e),
			len(expected), len(names), expected, names, missing, len(missing) > 0); err != nil {
			t.Fatal(err)
		}
		for _, s := range up(e) {
			for _, ep := range []string{"ROUTEZ", "GATEWAYZ", "LEAFZ"} {
				state := "ok"
				if (s.id == "E2" && ep == "GATEWAYZ" && e == 8) || (s.id == "E4" && ep == "LEAFZ" && e == topoEpochs) {
					state = "missing"
				}
				r.add("epoch_endpoints", map[string]any{"epoch": e, "server_id": s.id, "endpoint": ep, "state": state})
			}
			r.add("server_stats", map[string]any{"epoch": e, "server_id": s.id, "meta_leader": "nats-eu-0", "js_streams_leader": 2})
			// One outbound gateway to the other cluster.
			if s.id == "E2" && e == 8 {
				continue
			}
			other, cid := map[string]string{"eu": "us", "us": "eu"}[s.cluster], int64(10)
			switch {
			case s.id == "E0" && e >= 11, s.id == "E1" && e >= 5, s.id == "E2" && e >= 8:
				cid = 11
			}
			if s.id == "E2" && e >= 9 {
				cid = 12
			}
			i := slices.IndexFunc(servers, func(x srv) bool { return x.id == s.id })
			r.add("gateways", map[string]any{"epoch": e, "server_id": s.id, "gateway": other, "direction": "outbound", "cid": cid,
				"rtt_ns": int64(80+i) * int64(time.Millisecond), "pending_bytes": 0})
		}
		leaf := func(id, name string, lid int64, spoke bool) {
			r.add("leafnodes", map[string]any{"epoch": e, "server_id": id, "lid": lid, "name": name, "account": "APP", "is_spoke": spoke,
				"rtt_ns": int64(20 * time.Millisecond), "subscriptions": 40, "compression": "s2_auto"})
		}
		leaf("E0", "warehouse-eu-01", 1, false)
		leaf("E0", "upstream-hub", 2, true)
		switch {
		case e <= 6:
			leaf("E1", "pickup-0417", 1, false)
		case e <= 9:
			leaf("E1", "pickup-0417", 2, false)
		case e == 11:
			leaf("E3", "pickup-0417", 1, false)
			leaf("E4", "pickup-0500", 1, false)
		}
		if e == topoEpochs {
			leaf("E2", "pickup-0420", 1, false)
		}
	}
	// An inbound gateway row: its pending bytes count.
	r.add("gateways", map[string]any{"epoch": topoEpochs, "server_id": "U0", "gateway": "eu", "direction": "inbound", "cid": 40,
		"rtt_ns": int64(81 * time.Millisecond), "pending_bytes": 100})
	route := func(from, to string, rtt time.Duration, account string) {
		r.add("routes", map[string]any{"epoch": topoEpochs, "server_id": from, "rid": 1, "remote_name": to, "rtt_ns": int64(rtt),
			"pending_bytes": 0, "account": account})
	}
	eu := []string{"E0", "E1", "E2", "E3", "E4"}
	for _, a := range eu {
		for _, b := range eu {
			if a == b {
				continue
			}
			rtt := 400 * time.Microsecond
			if a == "E4" || b == "E4" {
				rtt = 4 * time.Millisecond
			}
			route(a, "nats-eu-"+b[1:], rtt, "")
			if (a == "E0" && b == "E1") || (a == "E1" && b == "E0") {
				route(a, "nats-eu-"+b[1:], rtt, "APP")
			}
		}
	}
	route("U0", "nats-us-1", 600*time.Microsecond, "")
	return st
}

// topologySingle is one cluster of three servers with routes and nothing
// else, in one epoch that started at at.
func topologySingle(t *testing.T, at time.Time) *store.Store {
	t.Helper()
	st := emptyStore(t)
	r := rows{t, st}
	names := []string{"nats-0", "nats-1", "nats-2"}
	if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (1, ?, ?, 0, 3, 3, ?, ?, [], 0, false, [], NULL)`, at, at, names, names); err != nil {
		t.Fatal(err)
	}
	for i, n := range names {
		id := fmt.Sprintf("S%d", i)
		r.add("servers", map[string]any{"server_id": id, "server_name": n, "cluster": "example-dev", "first_epoch": 1, "last_epoch": 1,
			"first_seen_at": at, "last_seen_at": at})
		for _, ep := range []string{"ROUTEZ", "GATEWAYZ", "LEAFZ"} {
			r.add("epoch_endpoints", map[string]any{"epoch": 1, "server_id": id, "endpoint": ep, "state": "ok"})
		}
		for _, m := range names {
			if m != n {
				r.add("routes", map[string]any{"epoch": 1, "server_id": id, "rid": 1, "remote_name": m, "rtt_ns": 350_000, "pending_bytes": 0})
			}
		}
	}
	return st
}

func topology(t *testing.T, st *store.Store, clock time.Time, r *v1.TimeRange) *v1.GetTopologyResponse {
	t.Helper()
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Clock: func() time.Time { return clock }})
	return must(c.GetTopology(ctx, connect.NewRequest(&v1.GetTopologyRequest{Range: r})))
}

func TestTopologyFromStore(t *testing.T) {
	last := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	res := topology(t, topologyStore(t, last), last.Add(30*time.Second), nil)
	if res.GetEpoch() != topoEpochs || !res.GetEpochAt().AsTime().Equal(last) || res.GetCollectInterval().AsDuration() != 30*time.Second {
		t.Fatalf("epoch %d at %v", res.GetEpoch(), res.GetEpochAt().AsTime())
	}
	if !res.GetDropsWindowStart().AsTime().Equal(topoAt(last, 1)) {
		t.Errorf("window start %v", res.GetDropsWindowStart().AsTime())
	}
	// Clusters.
	if len(res.GetClusters()) != 2 {
		t.Fatalf("clusters = %v", res.GetClusters())
	}
	eu, us := res.GetClusters()[0], res.GetClusters()[1]
	if eu.GetName() != "eu" || eu.GetPlace() != "eu" || !eu.GetMetaLeader() || eu.GetStreamsLed() != 10 || len(eu.GetServers()) != 5 ||
		!eu.GetServers()[0].GetMetaLeader() {
		t.Errorf("eu = %v", eu)
	}
	if us.GetName() != "us" || len(us.GetServers()) != 3 || !us.GetServers()[2].GetMissing() || us.GetServers()[2].GetName() != "nats-us-2" {
		t.Errorf("us = %v", us)
	}
	// Gateways: 7 of 7 outbound up; nats-eu-0's drop in the hour only.
	if len(res.GetGateways()) != 1 {
		t.Fatalf("gateways = %v", res.GetGateways())
	}
	g := res.GetGateways()[0]
	if g.GetClusterA() != "eu" || g.GetConnections() != 7 || g.GetExpected() != 7 || g.GetPendingBytes() != 100 ||
		g.GetRttMax().AsDuration() != 87*time.Millisecond || g.GetRttMedian().AsDuration() != 83*time.Millisecond {
		t.Errorf("gateway = %v", g)
	}
	if g.GetDrops_1H() != 1 || g.GetDrops_24H() != 1 || !slices.Equal(g.GetDroppingServers(), []string{"nats-eu-0"}) {
		t.Errorf("drops 1h %d, 24h %d, dropping %v; want 1, 1, nats-eu-0", g.GetDrops_1H(), g.GetDrops_24H(), g.GetDroppingServers())
	}
	// Routes: nats-eu-4's four slow, first; the APP route pinned.
	var slow []string
	var oneSided []string
	for _, r := range res.GetRoutes() {
		if r.GetSlow() {
			slow = append(slow, r.GetServerA()+"-"+r.GetServerB())
		}
		if r.GetOneSided() {
			oneSided = append(oneSided, r.GetServerA()+"-"+r.GetServerB()+" by "+r.GetReportedBy()[0])
		}
		if r.GetServerA() == "nats-eu-0" && r.GetServerB() == "nats-eu-1" && (r.GetPool() != 1 || !slices.Equal(r.GetPinnedAccounts(), []string{"APP"})) {
			t.Errorf("eu-0 to eu-1 = %v", r)
		}
	}
	if len(res.GetRoutes()) != 11 || !slices.Equal(slow, []string{"nats-eu-0-nats-eu-4", "nats-eu-1-nats-eu-4", "nats-eu-2-nats-eu-4", "nats-eu-3-nats-eu-4"}) ||
		!res.GetRoutes()[0].GetSlow() {
		t.Errorf("%d routes, slow %v", len(res.GetRoutes()), slow)
	}
	if !slices.Equal(oneSided, []string{"nats-us-0-nats-us-1 by nats-us-0"}) {
		t.Errorf("one-sided = %v", oneSided)
	}
	// Leaf nodes, worst first.
	var got []string
	for _, l := range res.GetLeafnodes() {
		got = append(got, fmt.Sprintf("%s %s %v %s", l.GetName(), l.GetHubServer(), l.GetConnected(), drops(l.Drops_24H)))
	}
	want := []string{"pickup-0417 nats-eu-3 false 3", "pickup-0420 nats-eu-2 true unset", "warehouse-eu-01 nats-eu-0 true 0"}
	if !slices.Equal(got, want) || res.GetLeafnodesTotal() != 3 {
		t.Errorf("leaf nodes = %v, want %v", got, want)
	}
	if l := res.GetLeafnodes()[0]; !l.GetLastSeen().AsTime().Equal(topoAt(last, 11)) || l.Rtt != nil || l.GetHubCluster() != "eu" {
		t.Errorf("pickup-0417 = %v", l)
	}
	if eu.GetLeafnodesConnected() != 2 || eu.GetLeafnodesNotConnected() != 1 || us.GetLeafnodesConnected() != 0 || us.LeafnodesConnected == nil {
		t.Errorf("leaf counts eu %d/%d us %v", eu.GetLeafnodesConnected(), eu.GetLeafnodesNotConnected(), us.LeafnodesConnected)
	}
	if u := res.GetUnobserved(); len(u) != 1 || u[0].GetEndpoint() != "LEAFZ" || !slices.Equal(u[0].GetServers(), []string{"nats-eu-4"}) {
		t.Errorf("unobserved = %v", u)
	}
}

func drops(p *int32) string {
	if p == nil {
		return "unset"
	}
	return fmt.Sprint(*p)
}

// A past range reads its own newest epoch; a range without one gives the
// empty response.
func TestTopologyFromStorePastRange(t *testing.T) {
	last := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	st := topologyStore(t, last)
	clock := last.Add(30 * time.Second)
	past := topology(t, st, clock, &v1.TimeRange{Start: timestamppb.New(last.Add(-time.Hour)), End: timestamppb.New(topoAt(last, 9).Add(time.Second))})
	if past.GetEpoch() != 9 || len(past.GetLeafnodes()) != 2 {
		t.Errorf("past epoch %d, leaf nodes %v", past.GetEpoch(), past.GetLeafnodes())
	}
	// In epoch 9 nats-us-2 still answered.
	for _, s := range past.GetClusters()[1].GetServers() {
		if s.GetMissing() {
			t.Errorf("%s missing in epoch 9", s.GetName())
		}
	}
	none := topology(t, st, clock, &v1.TimeRange{Start: timestamppb.New(last.Add(-48 * time.Hour)), End: timestamppb.New(last.Add(-47 * time.Hour))})
	if none.GetEpoch() != 0 || len(none.GetClusters()) != 0 || none.GetEpochAt() != nil {
		t.Errorf("empty range gave %v", none)
	}
}

func TestTopologySingleClusterFromStore(t *testing.T) {
	at := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	res := topology(t, topologySingle(t, at), at.Add(30*time.Second), nil)
	if len(res.GetClusters()) != 1 || len(res.GetRoutes()) != 3 || len(res.GetGateways()) != 0 || len(res.GetLeafnodes()) != 0 ||
		res.GetClusters()[0].GetLeafnodesConnected() != 0 || res.DropsWindowStart == nil {
		t.Errorf("single = %v", res)
	}
}
