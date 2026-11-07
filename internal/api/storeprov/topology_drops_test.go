package storeprov_test

import (
	"runtime"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The SQL count keeps the rule: a gateway pair whose connections are gone
// in the epoch is listed with its drop; a site that moved hub dropped
// once; a site without a name is keyed by its IP.
func TestTopologyDropsInSQL(t *testing.T) {
	last := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	st := emptyStore(t)
	r := rows{t, st}
	for _, s := range [][3]string{{"E0", "nats-eu-0", "eu"}, {"E1", "nats-eu-1", "eu"}, {"U0", "nats-us-0", "us"}} {
		r.add("servers", map[string]any{"server_id": s[0], "server_name": s[1], "cluster": s[2], "first_epoch": 1, "last_epoch": 4,
			"first_seen_at": last, "last_seen_at": last})
	}
	at := func(e int64) time.Time { return last.Add(-time.Duration(4-e) * 30 * time.Second) }
	for e := int64(1); e <= 4; e++ {
		names := []string{"nats-eu-0", "nats-eu-1", "nats-us-0"}
		if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, 3, 3, ?, ?, [], 0, false, [], NULL)`, e, at(e), at(e), names, names); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"E0", "E1", "U0"} {
			for _, ep := range []string{"ROUTEZ", "GATEWAYZ", "LEAFZ"} {
				r.add("epoch_endpoints", map[string]any{"epoch": e, "server_id": id, "endpoint": ep, "state": "ok"})
			}
		}
		if e < 4 {
			r.add("gateways", map[string]any{"epoch": e, "server_id": "E0", "gateway": "us", "direction": "outbound", "cid": 1,
				"rtt_ns": int64(80 * time.Millisecond)})
		}
		hub := "E0"
		if e >= 3 {
			hub = "E1"
		}
		r.add("leafnodes", map[string]any{"epoch": e, "server_id": hub, "lid": 1, "name": "pickup-0417", "account": "APP"})
		r.add("leafnodes", map[string]any{"epoch": e, "server_id": "E0", "lid": 10 + min(e, 3), "name": "", "ip": "10.0.0.5", "account": "APP"})
	}
	res := topology(t, st, last.Add(30*time.Second), nil)
	if len(res.GetGateways()) != 1 {
		t.Fatalf("gateways = %v", res.GetGateways())
	}
	if g := res.GetGateways()[0]; g.GetConnections() != 0 || g.GetExpected() != 3 || g.GetDrops_1H() != 1 || g.GetDrops_24H() != 1 {
		t.Errorf("eu-us = %v, want 0 of 3 with 1 drop", g)
	}
	drops := map[string]string{}
	for _, l := range res.GetLeafnodes() {
		drops[l.GetName()] = l.GetHubServer() + " " + drops24(l.Drops_24H)
	}
	if drops["pickup-0417"] != "nats-eu-1 1" || drops["10.0.0.5"] != "nats-eu-0 2" {
		t.Errorf("leaf drops = %v", drops)
	}
}

func drops24(p *int32) string { return drops(p) }

// A day at 30 seconds of a system with 24 servers and 1,000 leaf node
// sites: the read stays small, as only counts and one row per site leave
// the database.
func TestTopologyLargeDay(t *testing.T) {
	if testing.Short() {
		t.Skip("writes 2.9 million rows")
	}
	st := emptyStore(t)
	for _, q := range []string{
		`INSERT INTO servers SELECT 'S'||s, 'nats-'||s, CASE WHEN s < 12 THEN 'eu' ELSE 'us' END, NULL, NULL, NULL, NULL, ['region:x'], NULL, 1, 2880, TIMESTAMP '2026-10-17', TIMESTAMP '2026-10-18' FROM range(24) t(s)`,
		`INSERT INTO epochs SELECT e, TIMESTAMP '2026-10-17' + to_seconds(e*30), TIMESTAMP '2026-10-17' + to_seconds(e*30), 0, 24, 24, [], [], [], 0, false, [], NULL FROM range(1, 2881) t(e)`,
		`INSERT INTO epoch_endpoints SELECT e, 'S'||s, ep, 'ok', NULL FROM range(1, 2881) t(e), range(24) u(s), (VALUES ('ROUTEZ'), ('GATEWAYZ'), ('LEAFZ')) v(ep)`,
		`INSERT INTO gateways (epoch, server_id, gateway, direction, cid, rtt_ns, pending_bytes) SELECT e, 'S'||s, CASE WHEN s < 12 THEN 'us' ELSE 'eu' END, d, 10 + e // 500, 80000000, 0 FROM range(1, 2881) t(e), range(24) u(s), (VALUES ('outbound'), ('inbound')) v(d)`,
		`INSERT INTO leafnodes (epoch, server_id, lid, name, is_spoke, account, ip, rtt_ns, subscriptions, compression) SELECT e, 'S'||(l % 24), l + 1000 * (e // 300), 'site-'||l, false, 'APP', '10.0.0.1', 20000000, 40, 's2_auto' FROM range(1, 2881) t(e), range(1000) u(l)`,
	} {
		if _, err := st.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second}
	end := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC).Add(2881 * 30 * time.Second)
	req := &v1.GetTopologyRequest{Range: &v1.TimeRange{Start: timestamppb.New(end.Add(-time.Hour)), End: timestamppb.New(end)}}
	var duckBefore int64
	_ = st.DB().QueryRow(`SELECT sum(memory_usage_bytes) FROM duckdb_memory()`).Scan(&duckBefore)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	f, err := p.Topology(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	read := time.Since(start)
	runtime.ReadMemStats(&after)
	var duck int64
	_ = st.DB().QueryRow(`SELECT sum(memory_usage_bytes) FROM duckdb_memory()`).Scan(&duck)
	t.Logf("duckdb holds %d MB before the read (the in-memory store), %d MB after", duckBefore>>20, duck>>20)
	res := api.FinishTopology(f)
	t.Logf("read in %v, %d MB allocated, %d leaf drop counts; finished in %v", read, (after.TotalAlloc-before.TotalAlloc)>>20,
		len(f.Drops.Leafs), time.Since(start)-read)
	if res.GetLeafnodesTotal() != 1000 || res.GetLeafnodes()[0].GetDrops_24H() != 9 {
		t.Errorf("%d sites, first with %d drops", res.GetLeafnodesTotal(), res.GetLeafnodes()[0].GetDrops_24H())
	}
	if mb := (after.TotalAlloc - before.TotalAlloc) >> 20; mb > 32 {
		t.Errorf("the read allocated %d MB, want at most 32", mb)
	}
}
