package storeprov_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/store"
)

// followupStore writes five collections 30 seconds apart, the last at
// last, of eu (nats-eu-0 to nats-eu-2) and ap (nats-ap-0):
//   - Routes, two pooled per pair, listed by both ends. nats-eu-1's ROUTEZ
//     failed in epoch 1, while a route of nats-eu-0 to it was replaced
//     (no drop); one route of the pair was replaced between epochs 2 and
//     3 (one drop). nats-eu-2 restarted before epoch 4 (no drop on its
//     pairs); in epoch 5 neither nats-eu-0 nor nats-eu-2 lists the other
//     (one drop, and the pair is down).
//   - Memory in epoch 5: nats-eu-2 at 1 GiB, the others about 210 MiB,
//     with server-008 firing on nats-eu-2.
//   - Streams: ORDERS in eu; ARCHIVE in ap mirrors it, 1,500 behind after
//     1,200; EXT in ap mirrors it from an external API; IDX in ap sources
//     it and GONE (no server reports GONE), never having heard from
//     ORDERS; LOCAL in eu mirrors it. stream-015 fires on ARCHIVE and
//     stream-022 on IDX, naming its ORDERS source. A leaf node server of
//     the JetStream domain leaf, in cluster edge, leads a stream ORDERS of
//     its own, which is no origin of the copies in the default domain.
func followupStore(t *testing.T, last time.Time) *store.Store {
	t.Helper()
	st := emptyStore(t)
	r := rows{t, st}
	at := func(e int64) time.Time { return last.Add(-time.Duration(5-e) * 30 * time.Second) }
	type srv struct {
		id, name, cluster string
		from, to          int64
	}
	servers := []srv{{"E0", "nats-eu-0", "eu", 1, 5}, {"E1", "nats-eu-1", "eu", 1, 5}, {"E2", "nats-eu-2", "eu", 1, 3},
		{"E2B", "nats-eu-2", "eu", 4, 5}, {"A0", "nats-ap-0", "ap", 1, 5}}
	for _, s := range servers {
		r.add("servers", map[string]any{"server_id": s.id, "server_name": s.name, "cluster": s.cluster,
			"first_epoch": s.from, "last_epoch": s.to, "first_seen_at": at(s.from), "last_seen_at": at(s.to)})
	}
	idOf := func(name string, e int64) string {
		for _, s := range servers {
			if s.name == name && e >= s.from && e <= s.to {
				return s.id
			}
		}
		return ""
	}
	r.add("servers", map[string]any{"server_id": "A1", "server_name": "nats-leaf-0", "cluster": "edge", "domain": "leaf",
		"first_epoch": 1, "last_epoch": 5, "first_seen_at": at(1), "last_seen_at": at(5)})
	route := func(e int64, from, to string, rid int64) {
		r.add("routes", map[string]any{"epoch": e, "server_id": idOf(from, e), "rid": rid, "remote_id": idOf(to, e), "remote_name": to,
			"rtt_ns": int64(400 * time.Microsecond)})
	}
	names := []string{"nats-eu-0", "nats-eu-1", "nats-eu-2", "nats-ap-0"}
	for e := int64(1); e <= 5; e++ {
		if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, 4, 4, ?, ?, [], 0, false, [], NULL)`, e, at(e), at(e), names, names); err != nil {
			t.Fatal(err)
		}
		for _, n := range names {
			state := "ok"
			if n == "nats-eu-1" && e == 1 {
				state = "error"
			}
			r.add("epoch_endpoints", map[string]any{"epoch": e, "server_id": idOf(n, e), "endpoint": "ROUTEZ", "state": state})
		}
		// nats-eu-0 and nats-eu-1.
		first := map[bool]int64{true: 1, false: 11}[e <= 2]
		if e == 1 {
			first = 5
		} else {
			route(e, "nats-eu-1", "nats-eu-0", first)
			route(e, "nats-eu-1", "nats-eu-0", 2)
			route(e, "nats-eu-1", "nats-eu-2", 3)
		}
		route(e, "nats-eu-0", "nats-eu-1", first)
		route(e, "nats-eu-0", "nats-eu-1", 2)
		route(e, "nats-eu-2", "nats-eu-1", 30+e/4)
		if e < 5 {
			route(e, "nats-eu-0", "nats-eu-2", 20+e/4)
			route(e, "nats-eu-2", "nats-eu-0", 40+e/4)
		}
	}
	for name, mem := range map[string]int64{"nats-eu-0": 200, "nats-eu-1": 210, "nats-eu-2": 1024, "nats-ap-0": 220} {
		r.add("server_stats", map[string]any{"epoch": 5, "server_id": idOf(name, 5), "mem_bytes": mem << 20,
			"js_memory": int64(64 << 20), "js_max_memory": int64(1 << 30)})
	}
	for e := int64(4); e <= 5; e++ {
		stat := func(stream, server, cluster string, lag, active any) {
			r.add("stream_stats", map[string]any{"epoch": e, "server_id": idOf(server, e), "account": "APP", "stream": stream,
				"cluster": cluster, "is_leader": true, "mirror_lag": lag, "mirror_active_ns": active})
		}
		stat("ORDERS", "nats-eu-0", "eu", nil, nil)
		r.add("stream_stats", map[string]any{"epoch": e, "server_id": "A1", "account": "APP", "stream": "ORDERS", "cluster": "edge", "is_leader": true})
		stat("ARCHIVE", "nats-ap-0", "ap", 1200+300*(e-4), int64(time.Second))
		stat("EXT", "nats-ap-0", "ap", 0, int64(time.Second))
		stat("IDX", "nats-ap-0", "ap", nil, nil)
		stat("LOCAL", "nats-eu-1", "eu", 0, int64(time.Second))
		for _, src := range []string{"ORDERS", "GONE"} {
			r.add("stream_source_stats", map[string]any{"epoch": e, "server_id": "A0", "account": "APP", "stream": "IDX",
				"source_id": "id-" + src, "name": src, "config": `{"name":"` + src + `"}`, "reported": true, "lag": 0, "active_ns": -1})
		}
	}
	for stream, cfg := range map[string]string{
		"ARCHIVE": `{"mirror":{"name":"ORDERS"}}`, "EXT": `{"mirror":{"name":"ORDERS","external":{"api":"$JS.hub.API"}}}`,
		"LOCAL": `{"mirror":{"name":"ORDERS"}}`,
	} {
		server := "A0"
		if stream == "LOCAL" {
			server = "E1"
		}
		r.add("stream_config", map[string]any{"server_id": server, "account": "APP", "stream": stream, "first_epoch": 1, "last_epoch": 5,
			"config_hash": "h-" + stream, "mirror": "ORDERS", "config": cfg})
	}
	finding := func(code, entityType, key, related string) {
		r.add("findings", map[string]any{"id": code + ":" + key, "check_code": code, "entity_type": entityType, "entity_key": key,
			"severity": "warning", "state": "firing", "last_transition": "firing", "since": at(4), "since_epoch": 4, "transitioned_at": at(4),
			"last_seen_epoch": 5, "updated_epoch": 5, "detail": code + " detail", "related_config": related})
	}
	finding("server-008", "server", "nats-eu-2", "{}")
	finding("stream-015", "stream", "APP/ARCHIVE", "{}")
	finding("stream-022", "stream", "APP/IDX", `{"source_ids":"id-ORDERS"}`)
	return st
}

// Route drops, memory and replication between clusters read from the
// store, in a session time zone far from UTC.
func TestTopologyFollowupsFromStore(t *testing.T) {
	last := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	st := followupStore(t, last)
	if _, err := st.DB().Exec(`SET GLOBAL TimeZone = 'Pacific/Honolulu'`); err != nil {
		t.Fatal(err)
	}
	res := topology(t, st, last.Add(30*time.Second), nil)

	var routes []string
	for _, rt := range res.GetRoutes() {
		routes = append(routes, fmt.Sprintf("%s-%s %s/%s %v", rt.GetServerA(), rt.GetServerB(), drops(rt.Drops_1H), drops(rt.Drops_24H), rt.GetDown()))
	}
	want := []string{"nats-eu-0-nats-eu-2 1/1 true", "nats-eu-0-nats-eu-1 1/1 false", "nats-eu-1-nats-eu-2 0/0 false"}
	if !slices.Equal(routes, want) {
		t.Errorf("routes = %v, want %v", routes, want)
	}

	high := map[string]string{}
	for _, c := range res.GetClusters() {
		for _, s := range c.GetServers() {
			high[s.GetName()] = fmt.Sprintf("%d %q %d/%d", s.GetMemBytes()>>20, s.GetMemoryFindingId(), s.GetJsMemoryBytes()>>20, s.GetJsMaxMemoryBytes()>>20)
		}
	}
	if high["nats-eu-2"] != `1024 "server-008:nats-eu-2" 64/1024` || high["nats-eu-1"] != `210 "" 64/1024` {
		t.Errorf("memory = %v", high)
	}

	var repl []string
	for _, x := range res.GetReplication() {
		repl = append(repl, fmt.Sprintf("%s %s@%s <- %s@%s lag %d before %d never %v source %q finding %q", x.GetKind(), x.GetStream(), x.GetCluster(),
			x.GetOriginStream(), x.GetOriginCluster(), x.GetLag(), x.GetLagBefore(), x.GetNeverHeard(), x.GetSourceId(), x.GetFindingId()))
	}
	wantRepl := []string{
		`mirror ARCHIVE@ap <- ORDERS@eu lag 1500 before 1200 never false source "" finding "stream-015:APP/ARCHIVE"`,
		`source IDX@ap <- ORDERS@eu lag 0 before 0 never true source "id-ORDERS" finding "stream-022:APP/IDX"`,
	}
	if !slices.Equal(repl, wantRepl) {
		t.Errorf("replication =\n%v\nwant\n%v", repl, wantRepl)
	}
}
