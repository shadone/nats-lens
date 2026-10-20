package api

import (
	"fmt"
	"math/rand"
	"slices"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// slowCluster is the pairs of servers s0..s(n-1) with the round trip rtt
// gives; 0 leaves it unreported.
func slowCluster(n int, rtt func(a, b int) time.Duration) []*v1.TopologyRoute {
	var out []*v1.TopologyRoute
	for a := range n {
		for b := a + 1; b < n; b++ {
			r := &v1.TopologyRoute{Cluster: "c", ServerA: fmt.Sprintf("s%d", a), ServerB: fmt.Sprintf("s%d", b)}
			if d := rtt(a, b); d > 0 {
				r.Rtt = durationpb.New(d)
			}
			out = append(out, r)
		}
	}
	return out
}

func slowOf(rs []*v1.TopologyRoute) []string {
	markSlowRoutes(rs)
	var out []string
	for _, r := range rs {
		if r.GetSlow() {
			out = append(out, r.GetServerA()+"-"+r.GetServerB())
		}
	}
	slices.Sort(out)
	return out
}

// touching are the pairs of s0..s(n-1) with an end among servers.
func touching(n int, servers ...int) []string {
	var out []string
	for a := range n {
		for b := a + 1; b < n; b++ {
			if slices.Contains(servers, a) || slices.Contains(servers, b) {
				out = append(out, fmt.Sprintf("s%d-s%d", a, b))
			}
		}
	}
	slices.Sort(out)
	return out
}

const base = 400 * time.Microsecond

func slowServers(at time.Duration, slow ...int) func(a, b int) time.Duration {
	return func(a, b int) time.Duration {
		if slices.Contains(slow, a) || slices.Contains(slow, b) {
			return at
		}
		return base
	}
}

// The slow rule's matrix: slow servers and links found, normal layouts
// quiet.
func TestSlowRouteMatrix(t *testing.T) {
	check := func(name string, rs []*v1.TopologyRoute, want []string) {
		t.Helper()
		if got := slowOf(rs); !slices.Equal(got, want) {
			t.Errorf("%s: slow %v, want %v", name, got, want)
		}
	}
	for n := 2; n <= 6; n++ {
		var one, two []string
		if n >= 3 {
			one = touching(n, 0)
		}
		if n >= 5 {
			two = touching(n, 0, 1)
		}
		check(fmt.Sprintf("n=%d one slow server", n), slowCluster(n, slowServers(4*time.Millisecond, 0)), one)
		// Two slow servers of 3 or 4 leave a core of half or less: not
		// judged.
		check(fmt.Sprintf("n=%d two slow servers", n), slowCluster(n, slowServers(4*time.Millisecond, 0, 1)), two)
		var link []string
		if n >= 5 {
			link = []string{"s0-s1"}
		}
		// A slow link needs 2 pairs that touch neither end: 5 servers.
		check(fmt.Sprintf("n=%d one slow link", n), slowCluster(n, func(a, b int) time.Duration {
			if a == 0 && b == 1 {
				return 4 * time.Millisecond
			}
			return base
		}), link)
		check(fmt.Sprintf("n=%d all at 5 ms", n), slowCluster(n, func(int, int) time.Duration { return 5 * time.Millisecond }), nil)
		check(fmt.Sprintf("n=%d one slow server at 1.9 ms", n), slowCluster(n, slowServers(1900*time.Microsecond, 0)), nil)
		jitter := func(lo, hi time.Duration) func(a, b int) time.Duration {
			return func(a, b int) time.Duration { return lo + time.Duration((a*7+b*13)%10)*(hi-lo)/9 }
		}
		check(fmt.Sprintf("n=%d jitter 0.3-0.9 ms", n), slowCluster(n, jitter(300*time.Microsecond, 900*time.Microsecond)), nil)
		check(fmt.Sprintf("n=%d jitter 0.4-1.6 ms", n), slowCluster(n, jitter(400*time.Microsecond, 1600*time.Microsecond)), nil)
	}
	check("slow server at 2 ms against 0.6 ms", slowCluster(4, func(a, b int) time.Duration {
		if a == 0 {
			return 2 * time.Millisecond
		}
		return 600 * time.Microsecond
	}), touching(4, 0))
	// Pairs without a round trip are left out. s3 is measured only against
	// s0, at 4 ms: half the servers' nearest pair is 4 ms, so nothing
	// tells which of them is far, and nothing is slow.
	check("unreported pairs", slowCluster(4, func(a, b int) time.Duration {
		if a == 0 {
			return 4 * time.Millisecond
		}
		if a == 1 && b == 2 {
			return base
		}
		return 0
	}), nil)
	// s2 measured only against s0, far from s0, s1 and s3: slow.
	check("one slow server, partly measured", slowCluster(4, func(a, b int) time.Duration {
		if (a == 1 && b == 2) || (a == 2 && b == 3) {
			return 0
		}
		if a == 2 || b == 2 {
			return 10 * time.Millisecond
		}
		return base
	}), []string{"s0-s2"})
	// Zones: 0.15 ms inside a zone, 1.1 to 1.4 ms across.
	for _, z := range [][]int{{0, 0, 1}, {0, 0, 1, 2}, {0, 0, 1, 1}, {0, 0, 1, 1, 2}, {0, 0, 1, 1, 2, 2}, {0, 1, 2}, {0, 0, 0, 1}} {
		check(fmt.Sprintf("zones %v", z), slowCluster(len(z), func(a, b int) time.Duration {
			if z[a] == z[b] {
				return 150 * time.Microsecond
			}
			return 1100*time.Microsecond + time.Duration((a+b)%4)*100*time.Microsecond
		}), nil)
	}
}

// zoned is a cluster of zones of the given sizes, 0.15 ms inside a zone
// and cross across, every round trip within a sixth either way.
func zoned(rng *rand.Rand, sizes []int, cross time.Duration) []*v1.TopologyRoute {
	var zone []int
	for z, n := range sizes {
		for range n {
			zone = append(zone, z)
		}
	}
	jitter := func(d time.Duration) time.Duration { return d + time.Duration(rng.Int63n(int64(d)/3+1)) - d/6 }
	return slowCluster(len(zone), func(a, b int) time.Duration {
		if zone[a] == zone[b] {
			return jitter(150 * time.Microsecond)
		}
		return jitter(cross)
	})
}

// A cluster split into groups of similar size is not judged, on every
// refresh; a group of fewer than half is; a co-located pair does not make
// the rest slow; one slow server is found whatever the jitter.
func TestSlowRouteSplitsAndJitter(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for _, sizes := range [][]int{{3, 3}, {4, 4}, {5, 5}, {3, 3, 3}, {4, 3, 3}} {
		for _, cross := range []time.Duration{2500 * time.Microsecond, 3 * time.Millisecond, 5 * time.Millisecond} {
			for refresh := range 20 {
				if got := slowOf(zoned(rng, sizes, cross)); len(got) > 0 {
					t.Errorf("zones %v at %v, refresh %d: slow %v", sizes, cross, refresh, got)
					break
				}
			}
		}
	}
	// Two of seven far from the other five: the two are slow.
	if got := slowOf(zoned(rng, []int{5, 2}, 5*time.Millisecond)); !slices.Equal(got, touching(7, 5, 6)) {
		t.Errorf("zones 5+2 at 5 ms: slow %v, want the routes of s5 and s6", got)
	}
	for n := 3; n <= 8; n++ {
		for _, other := range []time.Duration{2500 * time.Microsecond, 4 * time.Millisecond} {
			got := slowOf(slowCluster(n, func(a, b int) time.Duration {
				if a == 0 && b == 1 {
					return 50 * time.Microsecond
				}
				return other
			}))
			// With 3, the co-located pair is the majority and the third
			// server is far from it: slow, as one slow server of 3 is.
			var want []string
			if n == 3 {
				want = touching(3, 2)
			}
			if !slices.Equal(got, want) {
				t.Errorf("n=%d, s0 and s1 0.05 ms apart, the rest %v: slow %v, want %v", n, other, got, want)
			}
		}
	}
	jitter := func(d time.Duration) time.Duration { return d + time.Duration(rng.Int63n(int64(d)/3+1)) - d/6 }
	for n := 3; n <= 10; n++ {
		for _, k := range []int{1, 2} {
			if k == 2 && n < 5 {
				continue
			}
			for _, at := range []time.Duration{2600 * time.Microsecond, 4 * time.Millisecond, 20 * time.Millisecond} {
				for refresh := range 20 {
					slow := rng.Perm(n)[:k]
					got := slowOf(slowCluster(n, func(a, b int) time.Duration {
						if slices.Contains(slow, a) || slices.Contains(slow, b) {
							return jitter(at)
						}
						return jitter(base)
					}))
					if want := touching(n, slow...); !slices.Equal(got, want) {
						t.Errorf("n=%d, %v slow at %v, refresh %d: slow %v, want %v", n, slow, at, refresh, got, want)
						break
					}
				}
			}
		}
	}
}

// zonedFrom is zoned with each zone's servers listed in zones (server i in
// zone zones[i]), drop leaving pairs unreported, at 0.4 ms inside a zone
// and 10 ms across, each with up to 0.1 ms (1 ms across) of jitter.
func zonedFrom(rng *rand.Rand, zones []int, drop func(a, b int) bool) []*v1.TopologyRoute {
	return slowCluster(len(zones), func(a, b int) time.Duration {
		if drop != nil && drop(a, b) {
			return 0
		}
		j := time.Duration(rng.Intn(100)) * time.Microsecond
		if zones[a] == zones[b] {
			return 400*time.Microsecond + j
		}
		return 10*time.Millisecond + 10*j
	})
}

// Uneven and even splits, and pairs left unreported, give the same slow
// routes for every seed of the jitter: the result never depends on which
// group holds the fastest pair. Cases of the fourth review of the API.
func TestSlowRouteSameOnEveryRefresh(t *testing.T) {
	for _, c := range []struct {
		name  string
		zones []int
		drop  func(a, b int) bool
		want  []string
	}{
		{"3+2", []int{0, 0, 0, 1, 1}, nil, touching(5, 3, 4)},
		{"4+3", []int{0, 0, 0, 0, 1, 1, 1}, nil, touching(7, 4, 5, 6)},
		{"5+2", []int{0, 0, 0, 0, 0, 1, 1}, nil, touching(7, 5, 6)},
		{"6+2", []int{0, 0, 0, 0, 0, 0, 1, 1}, nil, touching(8, 6, 7)},
		{"4+2", []int{0, 0, 0, 0, 1, 1}, nil, touching(6, 4, 5)},
		{"3+1+1", []int{0, 0, 0, 1, 2}, nil, touching(5, 3, 4)},
		{"1+3", []int{0, 1, 1, 1}, nil, touching(4, 0)},
		{"2+1", []int{0, 0, 1}, nil, touching(3, 2)},
		{"2+2", []int{0, 0, 1, 1}, nil, nil},
		{"3+3", []int{0, 0, 0, 1, 1, 1}, nil, nil},
		{"4+4", []int{0, 0, 0, 0, 1, 1, 1, 1}, nil, nil},
		{"2+2+1", []int{0, 0, 1, 1, 2}, nil, nil},
		{"3+2+2", []int{0, 0, 0, 1, 1, 2, 2}, nil, nil},
		{"2+2+2", []int{0, 0, 1, 1, 2, 2}, nil, nil},
		{"2+1+1", []int{0, 0, 1, 2}, nil, nil},
		{"1+1+1", []int{0, 1, 2}, nil, nil},
		// Unreported pairs cannot make one half the reference.
		{"2+2, s3 unreported to s0 and s1", []int{0, 0, 1, 1}, func(a, b int) bool { return b == 3 && a < 2 }, nil},
		{"3+3, only s3 measured to s0..s2", []int{0, 0, 0, 1, 1, 1}, func(a, b int) bool { return a < 3 && b > 3 }, nil},
		// s4 is measured only against the slow s3: not judged, but its
		// pair with s3 is a route of a slow server.
		{"3+2, s4 measured only to s3", []int{0, 0, 0, 1, 1}, func(a, b int) bool { return b == 4 && a < 3 }, touching(5, 3)},
	} {
		for seed := range 200 {
			got := slowOf(zonedFrom(rand.New(rand.NewSource(int64(seed))), c.zones, c.drop))
			if !slices.Equal(got, c.want) {
				t.Errorf("%s, seed %d: slow %v, want %v", c.name, seed, got, c.want)
				break
			}
		}
	}
}

// A cluster of 30 servers, every pair reported, is judged quickly.
func TestSlowRouteThirtyServers(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	zones := make([]int, 30)
	for i := range zones {
		zones[i] = i % 3
	}
	zones[29] = 3
	rs := zonedFrom(rng, zones, nil)
	start := time.Now()
	slowOf(rs)
	took := time.Since(start)
	t.Logf("30 servers, %d pairs: %v", len(rs), took)
	if took > 100*time.Millisecond {
		t.Errorf("30 servers took %v", took)
	}
}
