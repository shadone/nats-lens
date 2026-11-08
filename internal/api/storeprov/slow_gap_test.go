package storeprov_test

import (
	"database/sql"
	"testing"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/collect"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// A row without the slow consumer counter does not hide the increase
// across it.
func TestSlowConsumersAcrossAMissingCount(t *testing.T) {
	st := detailStoreWith(t, false, detailOpts{epochs: 3, skip: skipIn(1, "nats-0", collect.Varz)})
	var counter sql.NullInt64
	if err := st.DB().QueryRow(`SELECT slow_consumers FROM server_stats JOIN servers USING(server_id) WHERE epoch=2 AND server_name='nats-0'`).Scan(&counter); err != nil {
		t.Fatal(err)
	}
	if counter.Valid {
		t.Fatalf("missing VARZ counter = %v, want stored NULL", counter)
	}
	t.Log("missing VARZ stores a server_stats row with NULL slow_consumers")
	_, c := serve(t, st)
	sv := must(c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{Range: hour()})))
	if n0 := sv.GetServers()[0]; n0.GetSlowConsumersInRange() != 6 {
		t.Fatalf("nats-0 slow consumers in range = %d (set %v), want 6 (0, none, 6)", n0.GetSlowConsumersInRange(), n0.SlowConsumersInRange != nil)
	}
}
