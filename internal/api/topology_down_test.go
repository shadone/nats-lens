package api_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/shadone/nats-lens/internal/api"
)

// downAfter runs the drop count over h and FinishTopology on topoFacts
// with no routes listed in the epoch, and reports whether e0-e1 is in the
// response and down.
func downAfter(t *testing.T, h []api.TopologyCollection) (listed, down bool) {
	t.Helper()
	last := h[len(h)-1]
	f := topoFacts()
	f.Epoch, f.EpochAt = last.Epoch, last.At
	f.Drops = api.CountTopologyDrops(h, f.Interval, last.Epoch, last.At)
	// Every server answered ROUTEZ in the collection before the epoch too.
	f.RoutezBefore = []string{"e0", "e1", "e2", "u0", "u1", "u2"}
	for _, r := range api.FinishTopology(f).GetRoutes() {
		if r.GetServerA() == "e0" && r.GetServerB() == "e1" {
			return true, r.GetDown()
		}
	}
	return false, false
}

// e1 restarts and never gets a route to e0 back, while both answer
// ROUTEZ for many collections: the pair is partitioned.
func TestReviewRestartNeverRejoinsIsDown(t *testing.T) {
	a := []api.TopologyRouteConn{rconn("e0", "e1", 1), rconn("e1", "e0", 1)}
	h := routeHistory(answering("e0#1", "e1#1"), a, nil, nil, nil)
	for i := 1; i < len(h); i++ {
		h[i].Routez = answering("e0#1", "e1#2")
	}
	if listed, down := downAfter(t, h); !listed || !down {
		t.Errorf("restarted e1 never rejoined e0: listed %v down %v, want a down pair", listed, down)
	}
}

// The routes go in the collection in which e1's ROUTEZ failed, and stay
// gone while both answer afterwards.
func TestReviewDropDuringFailedRoutezIsDown(t *testing.T) {
	a := []api.TopologyRouteConn{rconn("e0", "e1", 1), rconn("e1", "e0", 1)}
	h := routeHistory(answering("e0#1", "e1#1"), a, nil, nil, nil)
	h[1].Routez = answering("e0#1")
	if listed, down := downAfter(t, h); !listed || !down {
		t.Errorf("drop hidden by a failed ROUTEZ: listed %v down %v, want a down pair", listed, down)
	}
}

// A pair stays down for more than a day: no drop is left in the window,
// and the pair is still down.
func TestTopologyDownWithoutDropsInWindow(t *testing.T) {
	if listed, down := downAfter(t, routeHistory(answering("e0#1", "e1#1"), nil, nil)); !listed || !down {
		t.Errorf("pair down for over a day: listed %v down %v, want a down pair", listed, down)
	}
}

// A server that did not answer ROUTEZ in the collection before the epoch
// (just started, or its ROUTEZ failed then) makes no pair down; a pair
// one end lists is one-sided, not down.
func TestTopologyDownNeedsTwoCollections(t *testing.T) {
	f := topoFacts()
	f.Routes = []api.TopologyRouteRow{route("e0", "e1", 0.4, "")}
	f.RoutezBefore = []string{"e0", "e1"}
	var got []string
	for _, r := range api.FinishTopology(f).GetRoutes() {
		got = append(got, fmt.Sprintf("%s-%s down %v one-sided %v", r.GetServerA(), r.GetServerB(), r.GetDown(), r.GetOneSided()))
	}
	if want := []string{"e0-e1 down false one-sided true"}; !slices.Equal(got, want) {
		t.Errorf("routes = %v, want %v", got, want)
	}
}
