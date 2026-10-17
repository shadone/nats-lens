package fake

import (
	"fmt"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// last is the series' newest value; nil without points.
func last(s *v1.Series) *float64 {
	if n := len(s.GetPoints()); n > 0 {
		return ptr(s.GetPoints()[n-1].GetValue())
	}
	return nil
}

// sum is the total of the series' values; nil without points.
func sum(s *v1.Series) *float64 {
	if len(s.GetPoints()) == 0 {
		return nil
	}
	var t float64
	for _, p := range s.GetPoints() {
		t += p.GetValue()
	}
	return &t
}

func count(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

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

	srv := tile(api.TileServers, "cluster.servers_up", last, cnt)
	srv.ValueSuffix = fmt.Sprintf("/ %d", len(s.sc.servers))
	srv.Subtitle = api.ServersSubtitle(servers)

	meta := tile(api.TileMetaLeader, "cluster.meta_leader_changes", sum, cnt)
	meta.ValueText = s.sc.metaLeader
	if meta.Value != nil {
		meta.Subtitle = count(int(meta.GetValue()), "leader change") + " in range"
	}

	streams := tile(api.TileStreams, "cluster.streams", last, cnt)
	repl, _, lag := s.streamCounts("")
	streams.Subtitle = fmt.Sprintf("%d replicated · %d lagging", repl, lag)
	kv := tile(api.TileKV, "cluster.kv_buckets", last, cnt)
	kv.ValueSuffix = "buckets"
	_, single, lag := s.streamCounts("KV_")
	kv.Subtitle = fmt.Sprintf("%d R1 · %d lagging", single, lag)
	objects := tile(api.TileObjectStores, "cluster.object_stores", last, cnt)
	objects.ValueSuffix = "buckets"
	_, _, lag = s.streamCounts("OBJ_")
	objects.Subtitle = fmt.Sprintf("%d lagging", lag)

	consumers := tile(api.TileConsumers, "cluster.durable_consumers", last, cnt)
	consumers.ValueSuffix = "durable"
	behind := 0
	for _, f := range cur {
		if f.GetCheckCode() == "consumer-002" {
			behind++
		}
	}
	consumers.Subtitle = fmt.Sprintf("%d behind", behind)

	accounts := tile(api.TileAccounts, "cluster.accounts", last, cnt)
	clients := tile(api.TileClients, "cluster.connections", last, cnt)
	clients.ValueSuffix = "conns"
	if clients.Value != nil {
		// The demo's connections are all in one account.
		accounts.Subtitle = fmt.Sprintf("most connections: %s (%.0f)", Account, clients.GetValue())
	}
	apps := map[string]bool{}
	for _, c := range s.sc.clients {
		apps[strings.SplitN(c, "@", 2)[0]] = true
	}
	clients.Subtitle = count(len(apps), "app")
	if slow := sum(s.series("cluster.slow_consumers", sys, r, maxPoints)); slow != nil {
		clients.Subtitle += fmt.Sprintf(" · %.0f slow", *slow)
	}

	storage := tile(api.TileStorage, "cluster.js_storage_bytes", last, v1.Unit_UNIT_BYTES)
	var limit float64
	for _, sp := range s.sc.servers {
		limit += float64(sp.maxStore)
	}
	storage.Limit, storage.ValueSuffix = ptr(limit), "of "+api.FmtBytes(limit)
	var parts []string
	if storage.Value != nil {
		parts = append(parts, fmt.Sprintf("%.1f%% used", storage.GetValue()/limit*100))
	}
	if g := growth(storage.GetSeries()); g != nil {
		parts = append(parts, api.FmtGrowth(*g))
	}
	storage.Subtitle = strings.Join(parts, " · ")

	tiles := []*v1.Tile{srv, meta, streams, kv, objects, consumers, accounts, clients, storage}
	s.tileStatus(tiles, cur)
	return tiles
}

// growth is the change per day between a series' first and last point;
// nil when they lie less than six hours apart, as the store requires.
func growth(s *v1.Series) *float64 {
	pts := s.GetPoints()
	if len(pts) < 2 {
		return nil
	}
	a, b := pts[0], pts[len(pts)-1]
	d := b.GetTime().AsTime().Sub(a.GetTime().AsTime())
	if d < 6*time.Hour {
		return nil
	}
	return ptr((b.GetValue() - a.GetValue()) / d.Hours() * 24)
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
