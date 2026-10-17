package storeprov_test

import (
	"slices"
	"testing"

	"github.com/shadone/nats-lens/internal/collect"
)

// skipIn leaves out endpoints of server in epoch index epoch.
func skipIn(epoch int, server string, eps ...collect.Endpoint) func(int, string, collect.Endpoint) bool {
	return func(i int, name string, e collect.Endpoint) bool {
		return i == epoch && name == server && slices.Contains(eps, e)
	}
}

// A server that misses VARZ (and JSZ) in the newest epoch keeps the
// limits it reported before; it is still marked as differing.
func TestServerLimitsSurviveAMissedReply(t *testing.T) {
	st := detailStoreWith(t, false, detailOpts{skip: func(i int, name string, e collect.Endpoint) bool {
		return skipIn(1, "nats-2", collect.Varz)(i, name, e) || skipIn(1, "nats-1", collect.Varz, collect.Jsz)(i, name, e)
	}})
	n2 := getServer(t, st, "nats-2")
	if n2.GetLimits().GetMaxPayload() != 8<<20 || n2.GetLimits().GetMaxPingsOut() != 2 || !slices.Equal(n2.GetLimitsDiffer(), []string{"max_payload"}) {
		t.Errorf("nats-2 limits %v, differ %v", n2.GetLimits(), n2.GetLimitsDiffer())
	}
	n1 := getServer(t, st, "nats-1")
	if n1.GetLimits().GetJsMaxStorage() != 50<<30 || n1.GetLimits().GetJsMaxMemory() != 2<<30 || n1.GetLimits().GetMaxPayload() != 1<<20 {
		t.Errorf("nats-1 limits %v", n1.GetLimits())
	}
}
