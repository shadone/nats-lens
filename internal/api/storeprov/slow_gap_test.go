package storeprov_test

import (
	"testing"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/collect"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// A row without the slow consumer counter does not hide the increase
// across it.
func TestSlowConsumersAcrossAMissingCount(t *testing.T) {
	_, c := serve(t, detailStoreWith(t, false, detailOpts{epochs: 3, skip: skipIn(1, "nats-0", collect.Varz)}))
	sv := must(c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{Range: hour()})))
	if n0 := sv.GetServers()[0]; n0.GetSlowConsumersInRange() != 6 {
		t.Fatalf("nats-0 slow consumers in range = %d (set %v), want 6 (0, none, 6)", n0.GetSlowConsumersInRange(), n0.SlowConsumersInRange != nil)
	}
}
