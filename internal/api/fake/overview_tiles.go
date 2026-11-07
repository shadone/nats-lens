package fake

import (
	"fmt"
	"strings"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// overviewTiles are a demo system's nine tiles, built from its scenario
// the way the store builds them from snapshots. servers are the server
// summaries and cur the findings firing at the range's end, whose checks
// set the statuses by api.TileRules. A system that stopped collecting
// has no status.
func (s scen) overviewTiles(r *v1.TimeRange, maxPoints int32, servers []*v1.ServerSummary, cur []*v1.Finding) []*v1.Tile {
	sys := s.sc.systemRef()
	tile := func(id, metric string, value func(*v1.Series) *float64, unit v1.Unit) *v1.Tile {
		ser := s.series(metric, sys, r, maxPoints)
		return &v1.Tile{Id: id, Label: api.TileLabels[id], Description: api.TileDescriptions[id], Value: value(ser), Unit: unit, Series: ser}
	}
	cnt := v1.Unit_UNIT_COUNT

	srv := tile(api.TileServers, "cluster.servers_up", api.LastPoint, cnt)
	srv.ValueSuffix = fmt.Sprintf("/ %d", len(s.sc.servers))
	srv.Subtitle = api.ServersSubtitle(servers)

	meta := tile(api.TileMetaLeader, "cluster.meta_leader_changes", api.SumPoints, cnt)
	meta.ValueText = s.metaLeaderAt(s.epochTime(r))
	if meta.Value != nil {
		meta.Subtitle = api.Plural(int(meta.GetValue()), "leader change") + " in range"
	}

	streams := tile(api.TileStreams, "cluster.streams", api.LastPoint, cnt)
	total, repl, _, lag := s.streamCounts("")
	streams.Subtitle = api.StreamsSubtitle(streams.Value, repl, total, lag)
	kv := tile(api.TileKV, "cluster.kv_buckets", api.LastPoint, cnt)
	kv.ValueSuffix = api.CountSuffix(kv.Value, "bucket")
	_, _, single, lag := s.streamCounts("KV_")
	kv.Subtitle = api.KVSubtitle(kv.Value, single, lag)
	objects := tile(api.TileObjectStores, "cluster.object_stores", api.LastPoint, cnt)
	objects.ValueSuffix = api.CountSuffix(objects.Value, "bucket")
	_, _, _, lag = s.streamCounts("OBJ_")
	objects.Subtitle = api.ObjectStoresSubtitle(objects.Value, lag)

	consumers := tile(api.TileConsumers, "cluster.durable_consumers", api.LastPoint, cnt)
	consumers.ValueSuffix = api.CountSuffix(consumers.Value, "durable")
	behind := 0
	for _, f := range cur {
		if f.GetCheckCode() == "consumer-002" {
			behind++
		}
	}
	// The demo's checks ran while it collects.
	consumers.Subtitle = api.ConsumersSubtitle(consumers.Value, s.sc.stale == 0, behind)

	accounts := tile(api.TileAccounts, "cluster.accounts", api.LastPoint, cnt)
	clients := tile(api.TileClients, "cluster.connections", api.LastPoint, cnt)
	clients.ValueSuffix = api.CountSuffix(clients.Value, "conn")
	if clients.Value != nil {
		if name, n := api.BusiestAccount(s.accounts(s.epochTime(r))); name != "" {
			accounts.Subtitle = api.AccountsSubtitle(name, n)
		}
	}
	// The applications of the epoch's connections, tooling and unnamed
	// ones left out, as the store counts them.
	apps := map[string]bool{}
	for _, c := range s.connections(s.epochTime(r)) {
		if app, _, _ := strings.Cut(c.GetName(), "@"); app != "" && !c.GetTooling() {
			apps[app] = true
		}
	}
	clients.Subtitle = api.ClientsSubtitle(clients.Value, len(apps), api.SumPoints(s.series("cluster.slow_consumers", sys, r, maxPoints)))

	storage := tile(api.TileStorage, "cluster.js_storage_bytes", api.LastPoint, v1.Unit_UNIT_BYTES)
	var limit float64
	for _, sp := range s.sc.servers {
		limit += float64(sp.maxStore)
	}
	if limit > 0 {
		storage.Limit, storage.ValueSuffix = ptr(limit), "of "+api.FmtBytes(limit)
	}
	storage.Subtitle = api.StorageSubtitle(storage.Value, storage.Limit, seriesGrowth(storage.GetSeries()))

	tiles := []*v1.Tile{srv, meta, streams, kv, objects, consumers, accounts, clients, storage}
	s.tileStatus(tiles, cur)
	return tiles
}

// tileStatus sets each tile's status from the firing findings its rule
// covers; the demo's checks all ran while it collects.
func (s scen) tileStatus(tiles []*v1.Tile, cur []*v1.Finding) {
	for _, t := range tiles {
		rule, ok := api.TileRules[t.GetId()]
		if t.Value == nil || !ok || s.sc.stale != 0 {
			t.Status = v1.Status_STATUS_NO_DATA
			continue
		}
		t.Status = v1.Status_STATUS_OK
		for _, f := range cur {
			if !rule.Concerns(f) {
				continue
			}
			st := statusOf(f.GetSeverity())
			if f.GetMute() != nil {
				st = v1.Status_STATUS_MUTED
			}
			if statusRank(st) < statusRank(t.Status) {
				t.Status = st
			}
		}
	}
}
