package api_test

import (
	"slices"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
)

// withEarlier gives f an earlier collection back before the epoch whose
// routes are those of earlier.
func withEarlier(f *api.TopologyFacts, back time.Duration, earlier *api.TopologyFacts) *api.TopologyFacts {
	f.Earlier = &api.TopologyEarlier{Epoch: f.Epoch - 1, At: f.EpochAt.Add(-back), Routes: earlier.Routes}
	return f
}

func slowOnce(f *api.TopologyFacts) []string {
	var out []string
	for _, r := range api.FinishTopology(f).GetRoutes() {
		if r.GetSlow() {
			out = append(out, r.GetServerA()+"-"+r.GetServerB())
		}
	}
	slices.Sort(out)
	return out
}

// A route is slow only when the epoch and the collection before both
// measure it slow.
func TestTopologySlowNeedsTwoCollections(t *testing.T) {
	slow := []string{"s0-s4", "s1-s4", "s2-s4", "s3-s4"}
	interval := 30 * time.Second
	cases := []struct {
		name    string
		facts   func() *api.TopologyFacts
		wantAll bool
	}{
		{"slow in both", func() *api.TopologyFacts { return withEarlier(cluster(5, 4), interval, cluster(5, 4)) }, true},
		{"a spike in the epoch only", func() *api.TopologyFacts { return withEarlier(cluster(5, 4), interval, cluster(5, 0.4)) }, false},
		{"slow in the earlier collection only", func() *api.TopologyFacts { return withEarlier(cluster(5, 0.4), interval, cluster(5, 4)) }, false},
		{"no earlier collection", func() *api.TopologyFacts { return cluster(5, 4) }, false},
		{"the earlier collection 3 intervals before", func() *api.TopologyFacts {
			return withEarlier(cluster(5, 4), 3*interval, cluster(5, 4))
		}, true},
		{"the earlier collection more than 3 intervals before", func() *api.TopologyFacts {
			return withEarlier(cluster(5, 4), 3*interval+time.Second, cluster(5, 4))
		}, false},
		{"the earlier collection missed the slow server", func() *api.TopologyFacts {
			e := cluster(5, 4)
			e.Routes = slices.DeleteFunc(e.Routes, func(r api.TopologyRouteRow) bool { return r.Server == "s4" || r.Remote == "s4" })
			return withEarlier(cluster(5, 4), interval, e)
		}, false},
		{"the earlier collection reported no round trip", func() *api.TopologyFacts {
			e := cluster(5, 4)
			for i := range e.Routes {
				e.Routes[i].RTT = nil
			}
			return withEarlier(cluster(5, 4), interval, e)
		}, false},
	}
	for _, c := range cases {
		var want []string
		if c.wantAll {
			want = slow
		}
		if got := slowOnce(c.facts()); !slices.Equal(got, want) {
			t.Errorf("%s: slow %v, want %v", c.name, got, want)
		}
	}
}

// One spike on one route of a cluster of 5 is a slow link in that
// collection only, and so not slow; the same link slow twice is.
func TestTopologySlowLinkSpike(t *testing.T) {
	spiked := func() *api.TopologyFacts {
		f := cluster(5, 0.4)
		for i := range f.Routes {
			if r := &f.Routes[i]; (r.Server == "s0" && r.Remote == "s1") || (r.Server == "s1" && r.Remote == "s0") {
				r.RTT = ms(4)
			}
		}
		return f
	}
	if got := slowOnce(withEarlier(spiked(), 30*time.Second, cluster(5, 0.4))); len(got) != 0 {
		t.Errorf("one spike: slow %v, want none", got)
	}
	if got := slowOnce(withEarlier(spiked(), 30*time.Second, spiked())); !slices.Equal(got, []string{"s0-s1"}) {
		t.Errorf("slow twice: slow %v, want s0-s1", got)
	}
}
