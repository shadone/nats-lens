package storeprov_test

import (
	"cmp"
	"fmt"
	"math/rand"
	"slices"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/checks"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The SQL drop count and api.CountTopologyDrops agree on random
// histories with restarts, failed and missing replies, rows without a
// reply, gaps of every length, reconnects, moves, sites on two hubs,
// sites keyed by IP and spoke rows, and, from their own random source,
// pooled routes replaced, gone, listed by one end, without a remote ID or
// twice. Seeds are fixed, so a run is repeatable.

type rndEpoch struct {
	n  int64
	at time.Time
	ok map[string]map[string]bool // endpoint -> server id -> ok
	gw []api.TopologyGatewayRow   // outbound rows (cid may be nil)
	lf []api.TopologyLeafRow      // site rows (not spoke)
	rt []api.TopologyRouteConn    // route rows with a remote ID
}

func rndSteps(rng *rand.Rand, edgeSubMs bool) time.Duration {
	x := rng.Intn(100)
	switch {
	case x < 60:
		return 30 * time.Second
	case x < 70:
		return 60 * time.Second
	case x < 76:
		return 90 * time.Second
	case x < 80:
		if edgeSubMs {
			return 90*time.Second + 400*time.Microsecond
		}
		return 90*time.Second + 2*time.Millisecond
	case x < 86:
		return 120 * time.Second
	case x < 90:
		return 30*time.Minute + time.Duration(rng.Intn(60))*time.Second
	case x < 93:
		return 6 * time.Hour
	case x < 95:
		return 20 * time.Hour
	default:
		return 30*time.Second + time.Duration(rng.Intn(1000))*time.Microsecond
	}
}

func rndBuild(t *testing.T, seed int64, edgeSubMs bool) (*storeprov.Provider, []rndEpoch, map[string]time.Time) {
	rng := rand.New(rand.NewSource(seed))
	st := emptyStore(t)
	r := rows{t, st}
	names := []string{"s0", "s1", "s2", "s3", "s4", "s5", "s6"}
	clusterOf := map[string]any{"s0": "eu", "s1": "eu", "s2": "eu", "s3": "us", "s4": "us", "s5": "us", "s6": nil}
	gen := map[string]int{}
	idOf := func(n string) string { return fmt.Sprintf("%s#%d", n, gen[n]) }
	type srv struct {
		name        string
		first, last int64
		start       time.Time
	}
	servers := map[string]*srv{}
	cids := map[string]int64{} // id|gateway -> cid
	nextCID := int64(100)
	type site struct {
		name, ip, account string
		hubs              map[string]int64 // server name -> lid
	}
	var sites []*site
	for i := 0; i < 14; i++ {
		s := &site{name: fmt.Sprintf("site-%d", i), ip: fmt.Sprintf("10.0.0.%d", i), account: "A", hubs: map[string]int64{}}
		if i%5 == 4 {
			s.name = "" // keyed by IP
		}
		if i == 13 {
			s.name, s.account = "site-0", "B" // same name, other account
		}
		if i == 12 {
			s.name, s.ip = "", "" // neither
		}
		sites = append(sites, s)
	}
	nextLID := int64(1)
	// Routes come from their own source, so the gateway and leaf node
	// histories stay those of the seed.
	rrng := rand.New(rand.NewSource(seed + 1000))
	rids := map[string][]int64{} // reporter id|remote name -> pool
	nextRID := int64(1)
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	var hist []rndEpoch
	K := int64(80)
	for e := int64(1); e <= K; e++ {
		if e > 1 {
			at = at.Add(rndSteps(rng, edgeSubMs))
		}
		ep := rndEpoch{n: e, at: at, ok: map[string]map[string]bool{"GATEWAYZ": {}, "LEAFZ": {}, "ROUTEZ": {}}}
		// restarts
		for _, n := range names {
			if rng.Intn(25) == 0 {
				gen[n]++
			}
			id := idOf(n)
			if servers[id] == nil {
				// Started up to 20 minutes before its first collection, so
				// some route pairs fall in the restart window.
				servers[id] = &srv{name: n, first: e, start: at.Add(-time.Duration(rrng.Intn(1200)) * time.Second)}
			}
			servers[id].last = e
		}
		var all []string
		for _, n := range names {
			all = append(all, idOf(n))
		}
		if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, 7, 7, ?, ?, [], 0, false, [], NULL)`, e, at, at, names, names); err != nil {
			t.Fatal(err)
		}
		for _, id := range all {
			for _, endp := range []string{"GATEWAYZ", "LEAFZ"} {
				x := rng.Intn(100)
				switch {
				case x < 82:
					r.add("epoch_endpoints", map[string]any{"epoch": e, "server_id": id, "endpoint": endp, "state": "ok"})
					ep.ok[endp][id] = true
				case x < 92:
					r.add("epoch_endpoints", map[string]any{"epoch": e, "server_id": id, "endpoint": endp, "state": "error"})
				case x < 96:
					r.add("epoch_endpoints", map[string]any{"epoch": e, "server_id": id, "endpoint": endp, "state": "missing"})
				}
			}
		}
		for _, id := range all {
			switch x := rrng.Intn(100); {
			case x < 85:
				r.add("epoch_endpoints", map[string]any{"epoch": e, "server_id": id, "endpoint": "ROUTEZ", "state": "ok"})
				ep.ok["ROUTEZ"][id] = true
			case x < 95:
				r.add("epoch_endpoints", map[string]any{"epoch": e, "server_id": id, "endpoint": "ROUTEZ", "state": "error"})
			}
		}
		// routes: a pool of two from each server to each other of s0 to s3
		for _, n := range names[:4] {
			id := idOf(n)
			for _, x := range names[:4] {
				if x == n {
					continue
				}
				k := id + "|" + x
				pool := rids[k]
				if pool == nil || rrng.Intn(30) == 0 {
					pool = []int64{nextRID, nextRID + 1} // whole pool replaced
					nextRID += 2
				} else if rrng.Intn(12) == 0 {
					pool = []int64{pool[0], nextRID} // one connection replaced
					nextRID++
				}
				rids[k] = pool
				if rrng.Intn(15) == 0 {
					continue // the pair is gone in this collection
				}
				if !ep.ok["ROUTEZ"][id] && rrng.Intn(2) == 0 {
					continue
				}
				for i, rid := range pool {
					if i == 1 && rrng.Intn(10) == 0 {
						continue // one connection not listed
					}
					var remoteID any = idOf(x)
					if rrng.Intn(40) == 0 {
						remoteID = nil
					}
					times := 1
					if rrng.Intn(60) == 0 {
						times = 2 // listed twice
					}
					for range times {
						r.add("routes", map[string]any{"epoch": e, "server_id": id, "rid": rid, "remote_id": remoteID, "remote_name": x})
						if remoteID != nil {
							ep.rt = append(ep.rt, api.TopologyRouteConn{ServerID: id, Server: n, RID: rid, RemoteID: idOf(x), Remote: x})
						}
					}
				}
			}
		}
		// gateways
		for _, n := range names {
			id := idOf(n)
			remote := "us"
			if clusterOf[n] == "us" {
				remote = "eu"
			}
			remotes := []string{remote}
			if n == "s0" {
				remotes = append(remotes, "ap") // second remote
			}
			for _, g := range remotes {
				k := id + "|" + g
				if _, ok := cids[k]; !ok || rng.Intn(8) == 0 {
					nextCID++
					cids[k] = nextCID
				}
				x := rng.Intn(100)
				if x < 8 {
					continue // no row
				}
				// adversarial: rows even when GATEWAYZ failed
				if !ep.ok["GATEWAYZ"][id] && rng.Intn(2) == 0 {
					continue
				}
				var cid any = cids[k]
				var cidp *int64
				if x < 13 {
					cid = nil
				} else {
					c := cids[k]
					cidp = &c
				}
				r.add("gateways", map[string]any{"epoch": e, "server_id": id, "gateway": g, "direction": "outbound", "cid": cid})
				cl, _ := clusterOf[n].(string)
				ep.gw = append(ep.gw, api.TopologyGatewayRow{ServerID: id, Server: n, Cluster: cl, Gateway: g, Outbound: true, CID: cidp})
				if rng.Intn(2) == 0 {
					r.add("gateways", map[string]any{"epoch": e, "server_id": id, "gateway": g, "direction": "inbound", "cid": cids[k] + 5000})
				}
			}
		}
		// leaf sites
		for _, s := range sites {
			x := rng.Intn(100)
			switch {
			case x < 5:
				s.hubs = map[string]int64{} // disappears
			case x < 15 || len(s.hubs) == 0 && x < 60:
				// move to (or appear on) a random hub
				s.hubs = map[string]int64{names[rng.Intn(len(names))]: nextLID}
				nextLID++
			case x < 20:
				// also on a second hub
				s.hubs[names[rng.Intn(len(names))]] = nextLID
				nextLID++
			case x < 27:
				for h := range s.hubs {
					s.hubs[h] = nextLID // reconnect, new lid
					nextLID++
				}
			}
			hubNames := make([]string, 0, len(s.hubs))
			for h := range s.hubs {
				hubNames = append(hubNames, h)
			}
			slices.Sort(hubNames)
			for _, h := range hubNames {
				id := idOf(h)
				if !ep.ok["LEAFZ"][id] && rng.Intn(2) == 0 {
					continue
				}
				var name, ip any = s.name, s.ip
				if s.name == "" && rng.Intn(2) == 0 {
					name = nil
				}
				if s.ip == "" {
					ip = nil
				}
				var acct any = s.account
				r.add("leafnodes", map[string]any{"epoch": e, "server_id": id, "lid": s.hubs[h], "name": name, "ip": ip,
					"account": acct, "is_spoke": false})
				cl, _ := clusterOf[h].(string)
				ep.lf = append(ep.lf, api.TopologyLeafRow{ServerID: id, Server: h, Cluster: cl, LID: s.hubs[h], Name: s.name, Account: s.account, IP: s.ip})
			}
		}
		// spoke rows (uplinks) never count
		r.add("leafnodes", map[string]any{"epoch": e, "server_id": idOf("s1"), "lid": 900000 + e, "name": "hub-x", "account": "A", "is_spoke": true})
		hist = append(hist, ep)
	}
	started := map[string]time.Time{}
	for id, s := range servers {
		r.add("servers", map[string]any{"server_id": id, "server_name": s.name, "cluster": clusterOf[s.name], "first_epoch": s.first,
			"last_epoch": s.last, "first_seen_at": at, "last_seen_at": at, "start_time": s.start})
		started[id] = s.start
	}
	return &storeprov.Provider{St: st, Interval: 30 * time.Second}, hist, started
}

func rndNorm(d api.TopologyDrops) string {
	slices.SortFunc(d.GatewayPairs, func(a, b [2]string) int { return cmp.Or(cmp.Compare(a[0], b[0]), cmp.Compare(a[1], b[1])) })
	slices.SortFunc(d.Gateways, func(a, b api.TopologyGatewayDrops) int {
		return cmp.Or(cmp.Compare(a.Server, b.Server), cmp.Compare(a.Cluster, b.Cluster), cmp.Compare(a.Gateway, b.Gateway))
	})
	slices.SortFunc(d.Leafs, func(a, b api.TopologyLeafDrops) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Account, b.Account))
	})
	slices.SortFunc(d.Routes, func(a, b api.TopologyRouteDrops) int {
		return cmp.Or(cmp.Compare(a.A, b.A), cmp.Compare(a.B, b.B))
	})
	var seen []string
	for _, s := range d.LastSeen {
		seen = append(seen, fmt.Sprintf("%s/%s@%s on %s lid %d", s.Row.SiteName(), s.Row.Account, s.At.UTC().Format(time.RFC3339Nano), s.Row.Server, s.Row.LID))
	}
	slices.Sort(seen)
	return fmt.Sprintf("start=%s\npairs=%v\ngw=%+v\nleafs=%+v\nseen=%v\nroutes=%+v", d.WindowStart.UTC().Format(time.RFC3339Nano), d.GatewayPairs, d.Gateways, d.Leafs, seen, d.Routes) + fmt.Sprintf("\nroutePairs=%v", d.RoutePairs)
}

func rndCompare(t *testing.T, seed int64, edgeSubMs bool) int {
	p, hist, started := rndBuild(t, seed, edgeSubMs)
	mismatches := 0
	for i := range hist {
		E := hist[i]
		f, err := p.Topology(ctx, &v1.GetTopologyRequest{Range: &v1.TimeRange{Start: timestamppb.New(E.at), End: timestamppb.New(E.at.Add(time.Microsecond))}})
		if err != nil {
			t.Fatal(err)
		}
		if f.Epoch != E.n {
			t.Fatalf("epoch %d read as %d", E.n, f.Epoch)
		}
		var h []api.TopologyCollection
		for _, c := range hist[:i+1] {
			if c.at.Before(E.at.Add(-api.TopologyDropWindow)) {
				continue
			}
			h = append(h, api.TopologyCollection{Epoch: c.n, At: c.at, Gatewayz: c.ok["GATEWAYZ"], Leafz: c.ok["LEAFZ"], Gateways: c.gw, Leafnodes: c.lf,
				Routez: c.ok["ROUTEZ"], Routes: c.rt})
		}
		goD := api.CountTopologyDropsWith(h, 30*time.Second, E.n, E.at, api.TopologyDropRules{RestartWindow: checks.DefaultRestartWindow, Started: started})
		a, b := rndNorm(f.Drops), rndNorm(goD)
		if a != b {
			mismatches++
			if mismatches <= 2 {
				t.Errorf("seed %d epoch %d (at %s): SQL and Go differ\nSQL:\n%s\nGo:\n%s", seed, E.n, E.at.Format(time.RFC3339Nano), a, b)
			}
		}
	}
	return mismatches
}

func TestTopologyDropsSQLMatchesGo(t *testing.T) {
	for seed := int64(1); seed <= 3; seed++ {
		if n := rndCompare(t, seed, false); n > 0 {
			t.Errorf("seed %d: %d epochs differ", seed, n)
		}
	}
}

// Gaps of 90 s and under a millisecond more are no longer consecutive in
// SQL either: the gap is compared in microseconds.
func TestTopologyDropsSubMillisecondGap(t *testing.T) {
	total := 0
	for seed := int64(5); seed <= 6; seed++ {
		total += rndCompare(t, seed, true)
	}
	if total > 0 {
		t.Errorf("%d epochs differ with gaps of 90 s and under a millisecond", total)
	}
}
