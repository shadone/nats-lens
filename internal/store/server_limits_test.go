package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/shadone/nats-lens/internal/collect"
)

// A VARZ reply as nats-server sends it: durations in nanoseconds.
const varzLimitsReply = `{"server":{"name":"nats-0","id":"NLIMITSERVER0","cluster":"example-dev","ver":"2.15.0","jetstream":true},
"data":{"server_id":"NLIMITSERVER0","server_name":"nats-0","version":"2.15.0","max_connections":64000,
"max_payload":8388608,"max_pending":67108864,"write_deadline":10000000000,"ping_interval":120000000000,"ping_max":2}}`

// The collector stores the VARZ limits on server_stats.
func TestVarzLimitsStored(t *testing.T) {
	s := openMem(t)
	snap := collect.NewSnapshot(t0)
	mustApply(t, snap, collect.Varz, []byte(varzLimitsReply))
	if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	var payload, pending, deadline, ping sql.NullInt64
	var pingsOut sql.NullInt32
	if err := s.DB().QueryRow(`SELECT max_payload, max_pending, write_deadline_ns, ping_interval_ns, max_pings_out FROM server_stats`).
		Scan(&payload, &pending, &deadline, &ping, &pingsOut); err != nil {
		t.Fatal(err)
	}
	if payload.Int64 != 8<<20 || pending.Int64 != 64<<20 || deadline.Int64 != 10e9 || ping.Int64 != 120e9 || pingsOut.Int32 != 2 {
		t.Fatalf("limits = %v %v %v %v %v", payload, pending, deadline, ping, pingsOut)
	}
}

// Without VARZ the limits are not known: NULL, not 0.
func TestVarzLimitsNullWithoutVarz(t *testing.T) {
	s := openMem(t)
	snap := collect.NewSnapshot(t0)
	mustApply(t, snap, collect.Statsz, []byte(`{"server":{"name":"nats-0","id":"NLIMITSERVER0","ver":"2.15.0"},"statsz":{"mem":1}}`))
	if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM server_stats WHERE max_payload IS NULL AND max_pending IS NULL
  AND write_deadline_ns IS NULL AND ping_interval_ns IS NULL AND max_pings_out IS NULL`); n != 1 {
		t.Fatalf("rows with NULL limits = %d, want 1", n)
	}
}
