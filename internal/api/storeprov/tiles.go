package storeprov

import (
	"context"
	"fmt"
	"strings"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/checks"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// tileSeries are the system series the tiles chart.
var tileSeries = []string{"cluster.servers_up", "cluster.meta_leader_changes", "cluster.streams", "cluster.kv_buckets",
	"cluster.object_stores", "cluster.durable_consumers", "cluster.accounts", "cluster.connections",
	"cluster.slow_consumers", "cluster.js_storage_bytes"}

// tiles are the overview's numbers, in the order of api.TileLabels' IDs.
// Their status is set afterwards by tileStatus.
func (p *Provider) tiles(ctx context.Context, r *v1.TimeRange, maxPoints int32, servers *v1.ListServersResponse, cur []*v1.Finding) ([]*v1.Tile, error) {
	sys := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SYSTEM}
	get := map[string]*v1.Series{}
	for _, m := range tileSeries {
		s, err := p.series(ctx, m, sys, r, maxPoints)
		if err != nil {
			return nil, err
		}
		get[m] = s
	}
	tile := func(id string, value *float64, unit v1.Unit, metric string) *v1.Tile {
		return &v1.Tile{Id: id, Label: api.TileLabels[id], Description: api.TileDescriptions[id], Value: value, Unit: unit,
			Status: v1.Status_STATUS_NO_DATA, Series: get[metric]}
	}
	count := v1.Unit_UNIT_COUNT
	e, ok, err := p.latestEpoch(ctx, r)
	if err != nil {
		return nil, err
	}
	var now epochFacts
	if ok {
		if now, err = p.epochFacts(ctx, e.epoch); err != nil {
			return nil, err
		}
	}

	serversTile := tile(api.TileServers, api.LastPoint(get["cluster.servers_up"]), count, "cluster.servers_up")
	if n := len(servers.GetExpected()); n > 0 {
		serversTile.ValueSuffix = fmt.Sprintf("/ %d", n)
	}
	serversTile.Subtitle = api.ServersSubtitle(servers.GetServers())

	meta := tile(api.TileMetaLeader, api.SumPoints(get["cluster.meta_leader_changes"]), count, "cluster.meta_leader_changes")
	meta.ValueText = now.metaLeader
	if meta.Value != nil {
		meta.Subtitle = api.Plural(int(meta.GetValue()), "leader change") + " in range"
	}

	streams := tile(api.TileStreams, api.LastPoint(get["cluster.streams"]), count, "cluster.streams")
	kv := tile(api.TileKV, api.LastPoint(get["cluster.kv_buckets"]), count, "cluster.kv_buckets")
	kv.ValueSuffix = api.CountSuffix(kv.Value, "bucket")
	objects := tile(api.TileObjectStores, api.LastPoint(get["cluster.object_stores"]), count, "cluster.object_stores")
	objects.ValueSuffix = api.CountSuffix(objects.Value, "bucket")
	streams.Subtitle = api.StreamsSubtitle(streams.Value, now.streams.replicated, now.streams.total, now.streams.lagging)
	kv.Subtitle = api.KVSubtitle(kv.Value, now.kv.single, now.kv.lagging)
	objects.Subtitle = api.ObjectStoresSubtitle(objects.Value, now.objects.lagging)

	consumers := tile(api.TileConsumers, api.LastPoint(get["cluster.durable_consumers"]), count, "cluster.durable_consumers")
	consumers.ValueSuffix = api.CountSuffix(consumers.Value, "durable")
	behind := 0
	for _, f := range cur {
		if f.GetCheckCode() == "consumer-002" {
			behind++
		}
	}
	consumers.Subtitle = api.ConsumersSubtitle(consumers.Value, now.behindRan, behind)

	accounts := tile(api.TileAccounts, api.LastPoint(get["cluster.accounts"]), count, "cluster.accounts")
	if now.busiestAccount != "" {
		accounts.Subtitle = fmt.Sprintf("most connections: %s (%d)", now.busiestAccount, now.busiestConns)
	}

	clients := tile(api.TileClients, api.LastPoint(get["cluster.connections"]), count, "cluster.connections")
	clients.ValueSuffix = api.CountSuffix(clients.Value, "conn")
	clients.Subtitle = api.ClientsSubtitle(clients.Value, now.apps, api.SumPoints(get["cluster.slow_consumers"]))

	storage, err := p.storageTile(ctx, r, servers, get["cluster.js_storage_bytes"])
	if err != nil {
		return nil, err
	}
	storage.Label, storage.Description = api.TileLabels[api.TileStorage], api.TileDescriptions[api.TileStorage]
	return []*v1.Tile{serversTile, meta, streams, kv, objects, consumers, accounts, clients, storage}, nil
}

// storageTile is JetStream storage: the servers' newest reports summed,
// their limits summed, and the growth per day over the range.
func (p *Provider) storageTile(ctx context.Context, r *v1.TimeRange, servers *v1.ListServersResponse, ser *v1.Series) (*v1.Tile, error) {
	t := &v1.Tile{Id: api.TileStorage, Unit: v1.Unit_UNIT_BYTES, Status: v1.Status_STATUS_NO_DATA, Series: ser}
	var limit, used float64
	reporting, limitKnown := false, true
	for _, s := range servers.GetServers() {
		if s.GetPresent() && s.JsStorageBytes != nil {
			used += float64(s.GetJsStorageBytes())
			reporting = true
			// One server without a limit leaves the sum unknown.
			limitKnown = limitKnown && s.JsMaxStorageBytes != nil
			limit += float64(s.GetJsMaxStorageBytes())
		}
	}
	if reporting {
		t.Value = ptr(used)
		// The servers' newest report may lie outside the bucket averages;
		// the chart's range takes it in.
		if len(ser.GetPoints()) > 0 {
			ser.Min, ser.Max = ptr(min(ser.GetMin(), used)), ptr(max(ser.GetMax(), used))
		}
	}
	if reporting && limitKnown && limit > 0 {
		t.Limit = &limit
		t.ValueSuffix = "of " + api.FmtBytes(limit)
	}
	growth, err := p.storageGrowth(ctx, r)
	if err != nil {
		return nil, err
	}
	t.Subtitle = api.StorageSubtitle(t.Value, t.Limit, growth)
	return t, nil
}

// storageGrowth is the Theil-Sen slope of the summed JetStream storage
// over the range, per day; nil when the range is too short.
func (p *Provider) storageGrowth(ctx context.Context, r *v1.TimeRange) (*float64, error) {
	start, end := span(r)
	rows, err := p.db().QueryContext(ctx, `SELECT t, v::DOUBLE FROM (`+p.metricQuery(metricQueries["cluster.js_storage_bytes"])+
		`) WHERE v IS NOT NULL ORDER BY t`, start, end)
	if err != nil {
		return nil, fmt.Errorf("storage growth: %w", err)
	}
	defer rows.Close()
	var s []sample
	for rows.Next() {
		var x sample
		if err := rows.Scan(&x.t, &x.v); err != nil {
			return nil, err
		}
		s = append(s, x)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return robustPerDay(s), nil
}

// assetCounts are counts over a kind of stream in one epoch.
type assetCounts struct {
	total, replicated, single, lagging int
}

// epochFacts are the newest epoch's facts the tiles' subtitles use.
type epochFacts struct {
	metaLeader           string
	streams, kv, objects assetCounts
	apps                 int
	behindRan            bool // consumer-002 ran ok in the epoch
	busiestAccount       string
	busiestConns         int64
}

// epochFacts reads the meta leader, the stream counts, the applications
// and the busiest account of an epoch.
func (p *Provider) epochFacts(ctx context.Context, epoch int64) (epochFacts, error) {
	var f epochFacts
	var err error
	if f.metaLeader, err = p.metaLeaderAt(ctx, epoch); err != nil {
		return f, err
	}
	if err = p.streamFacts(ctx, epoch, &f); err != nil {
		return f, err
	}
	if f.apps, err = p.appsAt(ctx, epoch); err != nil {
		return f, err
	}
	var runs int
	if err = p.db().QueryRowContext(ctx, `SELECT count(*) FROM check_runs WHERE epoch = ? AND check_code = 'consumer-002' AND status = 'ok'`,
		epoch).Scan(&runs); err != nil {
		return f, err
	}
	f.behindRan = runs > 0
	err = p.db().QueryRowContext(ctx, `
SELECT coalesce(any_value(ac.account_name), a.account), sum(a.conns)::BIGINT AS n
FROM account_stats a LEFT JOIN accounts ac USING (account)
WHERE a.epoch = ? AND NOT coalesce(ac.is_system, false)
GROUP BY a.account HAVING sum(a.conns) > 0 ORDER BY n DESC, 1 LIMIT 1`, epoch).Scan(&f.busiestAccount, &f.busiestConns)
	if err != nil && !isNoRows(err) {
		return f, err
	}
	return f, nil
}

// metaLeaderAt is the meta leader more than half of the expected servers
// report (the answering servers when none is expected), the rule of
// cluster.meta_leader_changes; empty without such a majority.
func (p *Provider) metaLeaderAt(ctx context.Context, epoch int64) (string, error) {
	var leader string
	err := p.db().QueryRowContext(ctx, `
WITH v AS (SELECT meta_leader AS l, count(*) AS n FROM server_stats
  WHERE epoch = ? AND coalesce(meta_leader, '') <> '' GROUP BY meta_leader)
SELECT l FROM v WHERE 2 * n > (SELECT coalesce(nullif(expected_servers, 0), observed_servers) FROM epochs WHERE epoch = ?)`,
		epoch, epoch).Scan(&leader)
	if isNoRows(err) {
		return "", nil
	}
	return leader, err
}

// streamFacts counts the epoch's streams, KV buckets and object stores by
// replicas and those with a lagging replica.
func (p *Provider) streamFacts(ctx context.Context, epoch int64, f *epochFacts) error {
	reps, err := p.streamReplicas(ctx, epoch, "", "", "")
	if err != nil {
		return err
	}
	rows, err := p.db().QueryContext(ctx, `
SELECT x.account, x.stream, max(c.replicas)
FROM (SELECT DISTINCT account, stream FROM stream_stats WHERE epoch = ?) x
LEFT JOIN stream_config c ON c.account = x.account AND c.stream = x.stream AND ? BETWEEN c.first_epoch AND c.last_epoch
GROUP BY ALL`, epoch, epoch)
	if err != nil {
		return fmt.Errorf("stream facts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var acc, name string
		var replicas *int32
		if err := rows.Scan(&acc, &name, &replicas); err != nil {
			return err
		}
		c := &f.streams
		switch {
		case strings.HasPrefix(name, "KV_"):
			c = &f.kv
		case strings.HasPrefix(name, "OBJ_"):
			c = &f.objects
		}
		// A stream whose replicas are unknown is neither replicated nor
		// single, so it is left out of the total the "of M" shows.
		if replicas != nil {
			c.total++
		}
		if replicas != nil && *replicas > 1 {
			c.replicated++
		}
		if replicas != nil && *replicas == 1 {
			c.single++
		}
		if lagging(placementOf(reps[acc+"/"+name], 0)) {
			c.lagging++
		}
	}
	return rows.Err()
}

// appsAt counts the distinct applications among the epoch's connections:
// the connection name up to an "@", nats-lens's own connections left out
// by the rule the client checks use.
func (p *Provider) appsAt(ctx context.Context, epoch int64) (int, error) {
	stored, err := checks.StoredLoopback(ctx, p.db(), epoch)
	if err != nil {
		return 0, err
	}
	names := append([]string{}, p.ToolingNames...)
	var n int
	err = p.db().QueryRowContext(ctx, `
SELECT count(DISTINCT split_part(c.name, '@', 1)) FROM connections c
WHERE c.epoch = ? AND coalesce(c.name, '') <> '' AND split_part(c.name, '@', 1) <> ''
  AND NOT (list_contains(?::VARCHAR[], c.name) OR (? AND coalesce(c.ip, '') IN ('127.0.0.1', '::1')))`,
		epoch, names, !p.LoopbackNotTooling && stored).Scan(&n)
	return n, err
}
