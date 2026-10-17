package store

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

func TestWriteSnapshotRows(t *testing.T) {
	s := openMem(t)
	snap := fixtureSnapshot(t, t0)
	epoch, err := s.WriteSnapshot(context.Background(), snap)
	if err != nil {
		t.Fatal(err)
	}
	if epoch != 1 {
		t.Fatalf("epoch = %d", epoch)
	}
	db := s.DB()

	var expected, observed, failures int
	var partial bool
	var missing []any
	if err := db.QueryRow(`SELECT expected_servers, observed_servers, endpoint_failures, partial, missing_names FROM epochs`).
		Scan(&expected, &observed, &failures, &partial, &missing); err != nil {
		t.Fatal(err)
	}
	if expected != 3 || observed != 2 || !partial || len(missing) != 1 || missing[0] != "nats-2" {
		t.Fatalf("epoch row: expected=%d observed=%d partial=%v missing=%v", expected, observed, partial, missing)
	}

	perServer := map[string]int{
		"server_stats": 1, "health": 1, "connections": 1, "routes": 1, "ipqueues": 1,
	}
	for tbl, want := range perServer {
		for _, id := range []string{"NFAKESERVER0ID", "NFAKESERVER1ID"} {
			if n := count(t, db, "SELECT count(*) FROM "+tbl+" WHERE server_id = ?", id); n != want {
				t.Errorf("%s rows for %s = %d, want %d", tbl, id, n, want)
			}
		}
	}

	// R3 streams are reported by both servers; the R1 test-stream only by its host.
	if n := count(t, db, `SELECT count(*) FROM stream_stats WHERE stream = 'order-jobs'`); n != 2 {
		t.Errorf("order-jobs rows = %d", n)
	}
	if n := count(t, db, `SELECT count(*) FROM stream_stats s JOIN servers v USING (server_id)
WHERE s.stream = 'test-stream' AND v.server_name = 'nats-1'`); n != 1 {
		t.Errorf("test-stream on nats-1 = %d", n)
	}
	if n := count(t, db, `SELECT count(*) FROM stream_stats WHERE stream = 'test-stream'`); n != 1 {
		t.Errorf("test-stream rows = %d", n)
	}
	if n := count(t, db, `SELECT count(*) FROM consumer_stats WHERE consumer = 'test-worker'`); n != 1 {
		t.Errorf("test-worker rows = %d", n)
	}
	// Identity rows are deduplicated across servers.
	if n := count(t, db, `SELECT count(*) FROM streams WHERE stream = 'order-jobs'`); n != 1 {
		t.Errorf("streams ident rows = %d", n)
	}
	if n := count(t, db, `SELECT count(*) FROM consumers WHERE stream = 'order-jobs'`); n != 2 {
		t.Errorf("order-jobs consumers = %d", n)
	}
	var mirror string
	if err := db.QueryRow(`SELECT mirror FROM stream_config WHERE stream = 'order-archive' LIMIT 1`).Scan(&mirror); err != nil || mirror != "order-jobs" {
		t.Errorf("archive mirror = %q, %v", mirror, err)
	}
	var retention string
	if err := db.QueryRow(`SELECT retention FROM stream_config WHERE stream = 'catalog-changes' LIMIT 1`).Scan(&retention); err != nil || retention != "limits" {
		t.Errorf("catalog-changes retention = %q, %v", retention, err)
	}
	// The consumer's raft group is linked to its asset.
	if n := count(t, db, `SELECT count(*) FROM raft_groups g JOIN consumer_stats c ON c.raft_group = g.group_id
WHERE g.kind = 'consumer' AND g.consumer = c.consumer AND c.consumer = 'resize-worker'`); n == 0 {
		t.Error("resize-worker raft group not linked")
	}
	if n := count(t, db, `SELECT count(*) FROM raft_groups WHERE group_id = '_meta_' AND kind = 'meta'`); n != 1 {
		t.Error("meta group missing")
	}
	if n := count(t, db, `SELECT count(*) FROM accounts WHERE account = 'SYS' AND is_system`); n != 1 {
		t.Error("system account not marked")
	}
	var rtt int64
	if err := db.QueryRow(`SELECT rtt_ns FROM connections LIMIT 1`).Scan(&rtt); err != nil || rtt != 1_500_000 {
		t.Errorf("rtt_ns = %d, %v", rtt, err)
	}
	var endpointsMissing int
	endpointsMissing = count(t, db, `SELECT count(*) FROM epoch_endpoints WHERE state <> 'ok'`)
	if endpointsMissing != 0 {
		t.Errorf("fixture marks no endpoint missing, got %d", endpointsMissing)
	}
}

func TestConfigVersions(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	for i := range 2 {
		if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0.Add(time.Duration(i)*30*time.Second))); err != nil {
			t.Fatal(err)
		}
	}
	db := s.DB()
	q := `SELECT count(*) FROM stream_config WHERE stream = 'catalog-changes' AND first_epoch = 1 AND last_epoch = 2`
	if n := count(t, db, q); n != 2 {
		t.Fatalf("unchanged config should extend one row per server, got %d", n)
	}

	// Change max_bytes on one server's view only: a new row for that server,
	// and the two servers now disagree.
	snap := fixtureSnapshot(t, t0.Add(time.Minute))
	changeStream(snap, "NFAKESERVER1ID", "catalog-changes", func(c *server.StreamConfig) { c.MaxBytes = 1 << 30 })
	if _, err := s.WriteSnapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT count(*) FROM stream_config WHERE stream = 'catalog-changes'`); n != 3 {
		t.Fatalf("config rows = %d, want 3", n)
	}
	if n := count(t, db, `SELECT count(DISTINCT config_hash) FROM stream_config
WHERE stream = 'catalog-changes' AND first_epoch <= 3 AND last_epoch >= 3`); n != 2 {
		t.Fatalf("servers should disagree at epoch 3, distinct hashes = %d", n)
	}
	if n := count(t, db, `SELECT count(*) FROM consumer_config WHERE last_epoch = 3`); n == 0 {
		t.Fatal("consumer config not extended")
	}
}

func changeStream(snap *collect.Snapshot, serverID, stream string, f func(*server.StreamConfig)) {
	for _, a := range snap.Servers[serverID].Jsz.AccountDetails {
		for i := range a.Streams {
			if a.Streams[i].Name == stream {
				f(a.Streams[i].Config)
			}
		}
	}
}

func TestMissingEndpointsRecorded(t *testing.T) {
	s := openMem(t)
	snap := fixtureSnapshot(t, t0)
	v := snap.Servers["NFAKESERVER0ID"]
	v.Status[collect.Leafz] = collect.EndpointStatus{State: collect.StateMissing}
	v.Status[collect.Gatewayz] = collect.EndpointStatus{State: collect.StateError, Error: "boom"}
	if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	db := s.DB()
	if n := count(t, db, `SELECT endpoint_failures FROM epochs`); n != 2 {
		t.Fatalf("endpoint_failures = %d", n)
	}
	if n := count(t, db, `SELECT count(*) FROM epoch_endpoints WHERE endpoint = 'GATEWAYZ' AND state = 'error' AND error = 'boom'`); n != 1 {
		t.Fatal("error endpoint not recorded")
	}
}

// The configuration hash leaves the creation time out, so that a replica
// rebuilt at another time does not show as a configuration difference; a
// new creation time still starts a new run, so a recreated asset counts as
// changed.
func TestConfigHashLeavesCreatedOut(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0)); err != nil {
		t.Fatal(err)
	}
	snap := fixtureSnapshot(t, t0.Add(30*time.Second))
	for _, a := range snap.Servers["NFAKESERVER1ID"].Jsz.AccountDetails {
		for i := range a.Streams {
			if a.Streams[i].Name == "catalog-changes" {
				a.Streams[i].Created = a.Streams[i].Created.Add(time.Hour)
			}
		}
	}
	if _, err := s.WriteSnapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	db := s.DB()
	if n := count(t, db, `SELECT count(DISTINCT config_hash) FROM stream_config
WHERE stream = 'catalog-changes' AND first_epoch <= 2 AND last_epoch >= 2`); n != 1 {
		t.Errorf("a creation time alone made %d distinct hashes, want 1", n)
	}
	if n := count(t, db, `SELECT count(*) FROM stream_config WHERE stream = 'catalog-changes' AND server_id = 'NFAKESERVER1ID'`); n != 2 {
		t.Errorf("a new creation time should start a new run, rows = %d", n)
	}
}
