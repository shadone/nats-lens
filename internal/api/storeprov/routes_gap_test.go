package storeprov_test

import (
	"testing"

	"github.com/shadone/nats-lens/internal/collect"
)

// Route views come from the newest epoch in which ROUTEZ answered.
func TestRouteViewsFromTheLastRoutez(t *testing.T) {
	st := detailStoreWith(t, false, detailOpts{skip: skipIn(1, "nats-0", collect.Routez)})
	if rv := getServer(t, st, "nats-0").GetRouteViews(); len(rv) != 2 {
		t.Fatalf("route views %v, want the 2 of the epoch before", rv)
	}
}
