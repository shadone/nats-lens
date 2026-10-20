package fake_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
)

func topology(t *testing.T, c natslensv1connect.LensServiceClient, system string, r *v1.TimeRange) *v1.GetTopologyResponse {
	t.Helper()
	return must(c.GetTopology(ctx, connect.NewRequest(&v1.GetTopologyRequest{System: system, Range: r})))
}

func optCount(p *int32) string {
	if p == nil {
		return "unset"
	}
	return fmt.Sprint(*p)
}

// example-global: three clusters, the ap-us gateway dropping from
// nats-ap-1, nats-eu-3's slow routes and the flapping pickup point first.
func TestGlobalTopology(t *testing.T) {
	c := demo(t)
	start := time.Now()
	res := topology(t, c, fake.GlobalSystem, nil)
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("GetTopology took %v", d)
	}
	var clusters []string
	for _, cl := range res.GetClusters() {
		var missing []string
		for _, s := range cl.GetServers() {
			if s.GetMissing() {
				missing = append(missing, s.GetName())
			}
		}
		clusters = append(clusters, fmt.Sprintf("%s %s %d %v %v %d/%d", cl.GetName(), cl.GetPlace(), len(cl.GetServers()), cl.GetMetaLeader(),
			missing, cl.GetLeafnodesConnected(), cl.GetLeafnodesNotConnected()))
	}
	if want := []string{"ap ap 6 false [] 0/0", "eu eu 9 true [] 17/1", "us us 9 false [nats-us-4] 12/0"}; !slices.Equal(clusters, want) {
		t.Errorf("clusters = %v, want %v", clusters, want)
	}
	var gws []string
	for _, g := range res.GetGateways() {
		gws = append(gws, fmt.Sprintf("%s-%s %d/%d %v %s %s %v", g.GetClusterA(), g.GetClusterB(), g.GetConnections(), g.GetExpected(),
			g.GetRttMedian().AsDuration(), optCount(g.Drops_1H), optCount(g.Drops_24H), g.GetDroppingServers()))
	}
	want := []string{"ap-us 14/14 212ms 3 7 [nats-ap-1]", "ap-eu 15/15 166ms 0 0 []", "eu-us 17/17 84ms 0 4 []"}
	if !slices.Equal(gws, want) {
		t.Errorf("gateways = %v, want %v", gws, want)
	}
	var slow []string
	for _, r := range res.GetRoutes() {
		if r.GetSlow() {
			slow = append(slow, r.GetServerA()+"-"+r.GetServerB())
		}
		if r.GetPool() != 3 || r.GetOneSided() || len(r.GetReportedBy()) != 2 {
			t.Errorf("route %v", r)
		}
	}
	if len(slow) != 8 || !res.GetRoutes()[7].GetSlow() || res.GetRoutes()[8].GetSlow() {
		t.Errorf("slow = %v", slow)
	}
	for _, s := range slow {
		if !slices.Contains([]string{s[:9], s[10:]}, "nats-eu-3") {
			t.Errorf("slow route %s without nats-eu-3", s)
		}
	}
	if res.GetLeafnodesTotal() != 30 || len(res.GetLeafnodes()) != 30 {
		t.Fatalf("%d leaf nodes of %d", len(res.GetLeafnodes()), res.GetLeafnodesTotal())
	}
	first := res.GetLeafnodes()[0]
	if first.GetName() != "pickup-0417" || first.GetConnected() || first.GetDrops_24H() != 17 || first.GetHubServer() != "nats-eu-1" ||
		!first.GetLastSeen().AsTime().Equal(time.Date(2026, 10, 17, 13, 51, 56, 0, time.UTC)) || first.Rtt != nil {
		t.Errorf("first leaf node = %v", first)
	}
	by := map[string]*v1.TopologyLeafnode{}
	for _, l := range res.GetLeafnodes() {
		by[l.GetName()] = l
	}
	if l := by["warehouse-eu-03"]; l.GetDrops_24H() != 2 || res.GetLeafnodes()[1] != l {
		t.Errorf("warehouse-eu-03 = %v, second %v", l, res.GetLeafnodes()[1].GetName())
	}
	if l := by["pickup-0420"]; l.Drops_24H != nil || !l.GetConnected() {
		t.Errorf("pickup-0420 = %v, want connected with drops unset", l)
	}
	// The sites of the us servers the upgrade stopped moved without a
	// drop.
	for _, l := range res.GetLeafnodes() {
		if l.GetHubCluster() == "us" && (l.GetDrops_24H() != 0 || slices.Contains([]string{"nats-us-1", "nats-us-2", "nats-us-3", "nats-us-4"}, l.GetHubServer())) {
			t.Errorf("us site %v", l)
		}
	}
	if len(res.GetUnobserved()) != 0 {
		t.Errorf("unobserved = %v", res.GetUnobserved())
	}
}

// Before the upgrade nats-us-4 answered and the gateways to us had not
// moved; a range before the history gives no epoch.
func TestGlobalTopologyPast(t *testing.T) {
	c := demo(t)
	at := func(h, m int) *timestamppb.Timestamp {
		return timestamppb.New(time.Date(2026, 10, 17, h, m, 0, 0, time.UTC))
	}
	res := topology(t, c, fake.GlobalSystem, &v1.TimeRange{Start: at(7, 0), End: at(8, 0)})
	for _, cl := range res.GetClusters() {
		for _, s := range cl.GetServers() {
			if s.GetMissing() {
				t.Errorf("%s missing at 08:00", s.GetName())
			}
		}
	}
	for _, g := range res.GetGateways() {
		if g.GetConnections() != g.GetExpected() || g.GetDrops_24H() != 0 {
			t.Errorf("gateway at 08:00 = %v", g)
		}
	}
	// pickup-0420 connects at 14:29:40.
	if len(res.GetLeafnodes()) != 29 || res.GetLeafnodes()[0].GetName() != "pickup-0417" || !res.GetLeafnodes()[0].GetConnected() {
		t.Errorf("leaf nodes at 08:00: %d, first %v", len(res.GetLeafnodes()), res.GetLeafnodes()[0])
	}
	old := topology(t, c, fake.GlobalSystem, &v1.TimeRange{Start: timestamppb.New(fake.FixedNow.AddDate(0, -3, 0)),
		End: timestamppb.New(fake.FixedNow.AddDate(0, -3, 0).Add(time.Hour))})
	if old.GetEpoch() != 0 {
		t.Errorf("a range before the history gave epoch %d", old.GetEpoch())
	}
}

// example-prod and example-staging are one cluster each: routes only.
func TestSingleClusterTopology(t *testing.T) {
	c := demo(t)
	for _, sys := range []string{fake.Cluster, fake.StagingSystem} {
		res := topology(t, c, sys, nil)
		if len(res.GetClusters()) != 1 || len(res.GetRoutes()) != 3 || len(res.GetGateways()) != 0 || len(res.GetLeafnodes()) != 0 {
			t.Errorf("%s = %v", sys, res)
		}
		for _, r := range res.GetRoutes() {
			if r.GetPool() != 3 || r.GetSlow() {
				t.Errorf("%s route %v", sys, r)
			}
			if pinned := r.GetPinnedAccounts(); (sys == fake.Cluster) != slices.Equal(pinned, []string{fake.Account}) {
				t.Errorf("%s pinned %v", sys, pinned)
			}
		}
	}
	// example-staging's data ended three hours ago: its newest epoch is
	// then.
	st := topology(t, c, fake.StagingSystem, nil)
	if age := fake.FixedNow.Sub(st.GetEpochAt().AsTime()); age < 3*time.Hour || age > 3*time.Hour+time.Minute {
		t.Errorf("staging epoch %v old", age)
	}
}

// The Server page agrees with the Topology page: each server's leaf node
// count is the sites on it, and its routes have the same round trips.
func TestGlobalTopologyAgreesWithServers(t *testing.T) {
	c := demo(t)
	res := topology(t, c, fake.GlobalSystem, nil)
	sites := map[string]int32{}
	for _, l := range res.GetLeafnodes() {
		if l.GetConnected() {
			sites[l.GetHubServer()]++
		}
	}
	rtt := map[[2]string]time.Duration{}
	for _, r := range res.GetRoutes() {
		rtt[[2]string{r.GetServerA(), r.GetServerB()}] = r.GetRtt().AsDuration()
	}
	for _, cl := range res.GetClusters() {
		for _, s := range cl.GetServers() {
			d := detail(t, c, fake.GlobalSystem, s.GetName())
			if s.GetMissing() {
				continue
			}
			if d.GetLeafnodes() != sites[s.GetName()] {
				t.Errorf("%s: %d leaf nodes on the Server page, %d sites", s.GetName(), d.GetLeafnodes(), sites[s.GetName()])
			}
			for _, v := range d.GetRouteViews() {
				k := [2]string{min(s.GetName(), v.GetRemoteName()), max(s.GetName(), v.GetRemoteName())}
				if v.GetRtt().AsDuration() > rtt[k] {
					t.Errorf("%s to %s: %v on the Server page, the pair's highest %v", s.GetName(), v.GetRemoteName(), v.GetRtt().AsDuration(), rtt[k])
				}
			}
		}
	}
}

// In a past range the Server page lists the routes of that range's epoch,
// as the Topology page pairs them: before nats-us-4 stopped, nats-us-0
// has routes to it.
func TestPastServerRoutesAgreeWithTopology(t *testing.T) {
	c := demo(t)
	r := &v1.TimeRange{Start: timestamppb.New(time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)),
		End: timestamppb.New(time.Date(2026, 10, 17, 8, 30, 0, 0, time.UTC))}
	d := must(c.GetServer(ctx, connect.NewRequest(&v1.GetServerRequest{System: fake.GlobalSystem, Name: "nats-us-0", Range: r})))
	remotes := map[string]bool{}
	for _, v := range d.GetRouteViews() {
		remotes[v.GetRemoteName()] = true
	}
	pairs := map[string]bool{}
	for _, rt := range topology(t, c, fake.GlobalSystem, r).GetRoutes() {
		if rt.GetServerA() == "nats-us-0" {
			pairs[rt.GetServerB()] = true
		}
	}
	if len(d.GetRouteViews()) != 24 || !remotes["nats-us-4"] || len(remotes) != len(pairs) {
		t.Errorf("nats-us-0 at 08:30: %d routes to %v; topology pairs %v", len(d.GetRouteViews()), remotes, pairs)
	}
	for n := range pairs {
		if !remotes[n] {
			t.Errorf("topology pairs nats-us-0 with %s, the Server page lists no route", n)
		}
	}
}
