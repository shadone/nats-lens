package storeprov

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/collect"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// The asset count series as they were computed from the stream and
// consumer reports before epoch_asset_counts, kept word for word as the
// reference the new reads must match.
var referenceAssetQueries = map[string]string{
	"cluster.durable_consumers": `
SELECT e.started_at AS t, count(DISTINCT (cs.account, cs.stream, cs.consumer)) AS v
FROM consumer_stats cs JOIN epochs e USING (epoch)
JOIN consumer_config cc ON cc.server_id = cs.server_id AND cc.account = cs.account AND cc.stream = cs.stream
  AND cc.consumer = cs.consumer AND cs.epoch BETWEEN cc.first_epoch AND cc.last_epoch
WHERE e.started_at >= ? AND e.started_at < ? AND coalesce(cc.durable_name, '') <> ''
GROUP BY e.epoch, e.started_at`,
	"cluster.streams":           referenceStreamCount(`NOT starts_with(ss.stream, 'KV_') AND NOT starts_with(ss.stream, 'OBJ_')`),
	"cluster.kv_buckets":        referenceStreamCount(`starts_with(ss.stream, 'KV_')`),
	"cluster.object_stores":     referenceStreamCount(`starts_with(ss.stream, 'OBJ_')`),
	"cluster.assets_replicated": referenceAssetCount("r > 1"),
	"cluster.assets_single":     referenceAssetCount("r = 1"),
}

const referenceJSZObserved = `e.observed_servers > 0 AND e.observed_servers <= (SELECT count(DISTINCT x.server_id) FROM epoch_endpoints x
  WHERE x.epoch = e.epoch AND x.endpoint = 'JSZ' AND x.state = 'ok')`

func referenceStreamCount(filter string) string {
	return fmt.Sprintf(`
SELECT e.started_at AS t, count(DISTINCT (ss.account, ss.stream)) FILTER (WHERE %s) AS v
FROM epochs e LEFT JOIN stream_stats ss ON ss.epoch = e.epoch
WHERE e.started_at >= ? AND e.started_at < ? AND %s
GROUP BY e.epoch, e.started_at`, filter, referenceJSZObserved)
}

func referenceAssetCount(cond string) string {
	return fmt.Sprintf(`
WITH win AS (SELECT epoch, started_at FROM epochs e WHERE started_at >= ? AND started_at < ? AND %[2]s),
st AS (
  SELECT ss.epoch, ss.account, ss.stream, max(sc.replicas) AS r
  FROM stream_stats ss JOIN win USING (epoch)
  JOIN stream_config sc ON sc.server_id = ss.server_id AND sc.account = ss.account AND sc.stream = ss.stream
    AND ss.epoch BETWEEN sc.first_epoch AND sc.last_epoch
  GROUP BY ss.epoch, ss.account, ss.stream),
co AS (
  SELECT cs.epoch, cs.account, cs.stream, cs.consumer, max(cc.replicas) AS r
  FROM consumer_stats cs JOIN win USING (epoch)
  JOIN consumer_config cc ON cc.server_id = cs.server_id AND cc.account = cs.account AND cc.stream = cs.stream
    AND cc.consumer = cs.consumer AND cs.epoch BETWEEN cc.first_epoch AND cc.last_epoch
  WHERE coalesce(cc.durable_name, '') <> ''
  GROUP BY cs.epoch, cs.account, cs.stream, cs.consumer),
assets AS (
  SELECT epoch, r FROM st
  UNION ALL
  SELECT co.epoch, CASE WHEN co.r = 0 THEN st.r ELSE co.r END FROM co LEFT JOIN st USING (epoch, account, stream))
SELECT w.started_at AS t, count(*) FILTER (WHERE %[1]s) AS v
FROM win w LEFT JOIN assets USING (epoch)
GROUP BY w.epoch, w.started_at`, cond, referenceJSZObserved)
}

// assetT0 is the first epoch of the asset store; epochs are 30 s apart.
var assetT0 = time.Date(2026, 10, 17, 13, 0, 0, 0, time.UTC)

const assetEpochs = 26

// assetStream is a stream of the asset store: its replicas, the servers
// that report it (leader first) and the epochs it exists in.
type assetStream struct {
	account, name string
	replicas      func(e int) int
	servers       func(e int) []int
	present       func(e int) bool
	mirror        string
	consumers     []assetConsumer
	// noConfig leaves the configuration out of a server's report.
	noConfig func(e, si int) bool
}

type assetConsumer struct {
	name     func(e int) string
	durable  bool
	replicas int
	present  func(e int) bool
}

func always(int) bool { return true }
func fixed(n int) func(int) int {
	return func(int) int { return n }
}
func on(servers ...int) func(int) []int {
	return func(int) []int { return servers }
}
func named(n string) func(int) string { return func(int) string { return n } }

// assetStreams has KV buckets, object stores, a mirror, R1 and R3
// streams, one in two accounts, assets that appear and disappear, a
// replica change, servers that disagree on a stream's replicas, and
// durable consumers with replicas 0, 1 and 3 next to ephemeral ones.
var assetStreams = []assetStream{
	{account: "AACCOUNTONE", name: "orders", replicas: fixed(3), servers: on(0, 1, 2), present: always, consumers: []assetConsumer{
		{name: named("worker"), durable: true, replicas: 0, present: always},
		{name: func(e int) string { return fmt.Sprintf("inbox-%d", e) }, replicas: 1, present: always},
		{name: named("late"), durable: true, replicas: 3, present: func(e int) bool { return e >= 12 }},
	}},
	{account: "AACCOUNTTWO", name: "orders", replicas: fixed(1), servers: on(0), present: always, consumers: []assetConsumer{
		{name: named("billing"), durable: true, replicas: 0, present: func(e int) bool { return e%4 != 1 }},
	}},
	{account: "AACCOUNTONE", name: "audit", replicas: fixed(1), servers: on(3), present: always, consumers: []assetConsumer{
		{name: named("tail"), durable: true, replicas: 0, present: always},
	}},
	{account: "AACCOUNTONE", name: "orders-mirror", replicas: fixed(1), servers: on(2), mirror: "orders",
		present: func(e int) bool { return e < 10 || e >= 14 }, consumers: []assetConsumer{
			{name: named("replay"), durable: true, replicas: 0, present: always},
		}},
	{account: "AACCOUNTONE", name: "KV_config", replicas: fixed(3), servers: on(0, 1, 2), present: always, consumers: []assetConsumer{
		{name: named("watcher"), durable: true, replicas: 1, present: always},
		{name: func(e int) string { return fmt.Sprintf("kv-watch-%d", e%3) }, replicas: 1, present: always},
	}},
	{account: "AACCOUNTONE", name: "KV_flags", replicas: fixed(1), servers: on(1), present: func(e int) bool { return e >= 6 }},
	{account: "AACCOUNTONE", name: "OBJ_blobs", replicas: fixed(3), servers: on(1, 2, 3), present: always, consumers: []assetConsumer{
		{name: named("gc"), durable: true, replicas: 3, present: func(e int) bool { return e >= 12 }},
	}},
	{account: "AACCOUNTONE", name: "OBJ_thumbs", replicas: fixed(1), servers: on(3), present: func(e int) bool { return e < 18 }},
	{account: "AACCOUNTONE", name: "resize", present: always,
		replicas: func(e int) int {
			if e < 9 {
				return 1
			}
			return 3
		},
		servers: func(e int) []int {
			if e < 9 {
				return []int{0}
			}
			return []int{0, 1, 2}
		},
		consumers: []assetConsumer{{name: named("thumbnailer"), durable: true, replicas: 0, present: always}}},
	// skew is configured R3 on nats-0 and R1 on nats-1: the largest wins.
	{account: "AACCOUNTONE", name: "skew", replicas: fixed(3), servers: on(0, 1), present: always},
	// legacy has no configuration on nats-1, and none at all in even epochs;
	// KV_legacy never has one. Both count as streams, but in neither replica
	// count, nor does legacy's consumer with replicas 0.
	{account: "AACCOUNTONE", name: "legacy", replicas: fixed(3), servers: on(0, 1), present: always,
		noConfig:  func(e, si int) bool { return si == 1 || e%2 == 0 },
		consumers: []assetConsumer{{name: named("old"), durable: true, replicas: 0, present: always}}},
	{account: "AACCOUNTTWO", name: "KV_legacy", replicas: fixed(1), servers: on(2), present: always,
		noConfig: func(int, int) bool { return true }},
}

// assetSnapshot builds epoch e (0-based) of the asset store. nats-3 does
// not answer in every fifth epoch, nats-2 answers without JSZ in every
// seventh, epoch 17 has no consumers at all and in epoch 20 no server
// answers.
func assetSnapshot(e int) *collect.Snapshot {
	at := assetT0.Add(time.Duration(e) * 30 * time.Second)
	snap := collect.NewSnapshot(at)
	snap.Expected = []string{"nats-0", "nats-1", "nats-2", "nats-3"}
	snap.FinishedAt = at.Add(300 * time.Millisecond)
	if e == 20 {
		return snap
	}
	started := assetT0.Add(-time.Hour)
	for si := range 4 {
		if si == 3 && e%5 == 3 {
			continue
		}
		name, id := fmt.Sprintf("nats-%d", si), fmt.Sprintf("NASSETSERVER%dID", si)
		info := server.ServerInfo{Name: name, ID: id, Cluster: "shop-east", Version: "2.15.2", JetStream: true}
		v := &collect.ServerView{Info: info, Status: map[collect.Endpoint]collect.EndpointStatus{
			collect.Varz: {State: collect.StateOK}, collect.Jsz: {State: collect.StateOK}}}
		v.Varz = &server.Varz{ID: id, Name: name, Version: info.Version, Start: started, Now: at, Mem: 1 << 20, Cores: 2, MaxProcs: 2}
		if si == 2 && e%7 == 4 {
			v.Status[collect.Jsz] = collect.EndpointStatus{State: collect.StateError, Error: "timeout"}
			snap.Servers[id] = v
			continue
		}
		accs := map[string]*server.AccountDetail{}
		var order []string
		for _, s := range assetStreams {
			if !s.present(e) {
				continue
			}
			servers := s.servers(e)
			self := false
			for _, o := range servers {
				self = self || o == si
			}
			if !self {
				continue
			}
			replicas := s.replicas(e)
			if s.name == "skew" && si == 1 {
				replicas = 1
			}
			cfg := &server.StreamConfig{Name: s.name, Subjects: []string{s.name + ".>"}, Retention: server.LimitsPolicy,
				Storage: server.FileStorage, Replicas: replicas, MaxConsumers: -1, MaxMsgs: -1, MaxBytes: -1, MaxMsgsPer: -1,
				MaxMsgSize: -1, Discard: server.DiscardOld}
			if s.mirror != "" {
				cfg.Subjects = nil
				cfg.Mirror = &server.StreamSource{Name: s.mirror}
			}
			sd := server.StreamDetail{Name: s.name, Created: started, Config: cfg, Cluster: assetCluster(s.name, servers, si),
				State: server.StreamState{Msgs: uint64(10 + e), Bytes: uint64(1000 + e), FirstSeq: 1, LastSeq: uint64(10 + e), LastTime: at}}
			if s.noConfig != nil && s.noConfig(e, si) {
				sd.Config = nil
			}
			for _, c := range s.consumers {
				if e == 17 || !c.present(e) {
					continue
				}
				cc := &server.ConsumerConfig{DeliverPolicy: server.DeliverAll, AckPolicy: server.AckExplicit, AckWait: 30 * time.Second,
					MaxDeliver: -1, MaxAckPending: 1000, Replicas: c.replicas}
				if c.durable {
					cc.Durable = c.name(e)
				}
				lastActive := at.Add(-time.Second)
				sd.Consumer = append(sd.Consumer, &server.ConsumerInfo{Stream: s.name, Name: c.name(e), Created: started, Config: cc,
					Delivered: server.SequenceInfo{Consumer: uint64(e), Stream: uint64(e), Last: &lastActive},
					AckFloor:  server.SequenceInfo{Consumer: uint64(e), Stream: uint64(e), Last: &lastActive},
					Cluster:   assetCluster(s.name+"/"+c.name(e), servers, si), TimeStamp: at})
			}
			acc, ok := accs[s.account]
			if !ok {
				acc = &server.AccountDetail{Name: s.account, Id: s.account}
				accs[s.account] = acc
				order = append(order, s.account)
			}
			acc.Streams = append(acc.Streams, sd)
		}
		jsz := &server.JSInfo{ID: id, Now: at, Meta: &server.MetaClusterInfo{Name: "shop-east", Leader: "nats-0", Size: 4}}
		for _, a := range order {
			jsz.AccountDetails = append(jsz.AccountDetails, accs[a])
		}
		v.Jsz = jsz
		snap.Servers[id] = v
	}
	return snap
}

func assetCluster(group string, servers []int, self int) *server.ClusterInfo {
	ci := &server.ClusterInfo{Name: "shop-east", RaftGroup: group, Leader: fmt.Sprintf("nats-%d", servers[0])}
	for _, o := range servers {
		if o != self {
			ci.Replicas = append(ci.Replicas, &server.PeerInfo{Name: fmt.Sprintf("nats-%d", o), Current: true})
		}
	}
	return ci
}

// assetProvider writes the asset store through WriteSnapshot in the time
// zone Pacific/Honolulu, so that a time shifted by the session zone shows.
func assetProvider(t *testing.T) *Provider {
	t.Helper()
	t.Setenv("TZ", "Pacific/Honolulu")
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.DB().Exec(`SET GLOBAL TimeZone = 'Pacific/Honolulu'`); err != nil {
		t.Fatal(err)
	}
	for e := range assetEpochs {
		if _, err := st.WriteSnapshot(ctx, assetSnapshot(e)); err != nil {
			t.Fatalf("epoch %d: %v", e, err)
		}
	}
	last := assetT0.Add(assetEpochs * 30 * time.Second)
	return &Provider{St: st, Interval: 30 * time.Second, Clock: func() time.Time { return last }}
}

// assetRanges are ranges over the asset store: all of it, inside it, from
// before its first epoch, ending inside it and of one epoch.
func assetRanges() []*v1.TimeRange {
	at := func(d time.Duration) *timestamppb.Timestamp { return timestamppb.New(assetT0.Add(d)) }
	return []*v1.TimeRange{
		{Start: at(-10 * time.Minute), End: at(20 * time.Minute)},
		{Start: at(2*time.Minute + 15*time.Second), End: at(9 * time.Minute)},
		{Start: at(-time.Minute), End: at(4 * time.Minute)},
		{Start: at(7 * time.Minute), End: at(13*time.Minute + 1*time.Second)},
		{Start: at(5 * time.Minute), End: at(5*time.Minute + 30*time.Second)},
	}
}

var assetMaxPoints = []int32{0, 1, 4, 7, 500}

// The counts stored with each epoch give exactly the series, the
// observation summaries and the Overview's tiles that counting the
// reports gave, over several ranges and bucket sizes.
func TestAssetCountsMatchReportCounts(t *testing.T) {
	p := assetProvider(t)
	ctx := context.Background()
	var n int
	if err := p.St.DB().QueryRow(`SELECT count(*) FROM epoch_asset_counts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != assetEpochs {
		t.Fatalf("epoch_asset_counts holds %d rows, want one per epoch (%d)", n, assetEpochs)
	}
	sys := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SYSTEM}
	points := 0
	for metric, ref := range referenceAssetQueries {
		m := metricQueries[metric]
		if m.scan == "" {
			t.Fatalf("%s does not read the asset counts", metric)
		}
		for _, r := range assetRanges() {
			for _, mp := range assetMaxPoints {
				missing, err := p.resolveMetric(ctx, m, p.seriesLookback(metric, r.Start.AsTime()), r.End.AsTime())
				if err != nil {
					t.Fatal(err)
				}
				if missing.query != m.query {
					t.Fatalf("%s over %v: an epoch has no counts", metric, r)
				}
				got, err := p.seriesOf(ctx, metric, m, sys, r, mp)
				if err != nil {
					t.Fatal(err)
				}
				want, err := p.seriesOf(ctx, metric, metricSQL{query: ref}, sys, r, mp)
				if err != nil {
					t.Fatal(err)
				}
				if !proto.Equal(got, want) {
					t.Errorf("%s over %v, %d points:\ncounts  %v\nreports %v", metric, r, mp, got, want)
				}
				points += len(got.GetPoints())
			}
		}
	}
	if points == 0 {
		t.Fatal("no series had a point")
	}

	// The observation summaries and the Overview read the same metrics.
	type result struct {
		obs   map[string]*v1.ObservationSummary
		tiles []*v1.Tile
	}
	read := func() result {
		out := result{obs: map[string]*v1.ObservationSummary{}}
		for _, r := range assetRanges() {
			for metric := range referenceAssetQueries {
				o, err := p.observationSummary(ctx, metric, sys, r)
				if err != nil {
					t.Fatal(err)
				}
				out.obs[metric+" "+r.String()] = o
			}
			for _, mp := range assetMaxPoints {
				res, err := p.GetOverview(ctx, &v1.GetOverviewRequest{Range: r, MaxPoints: mp})
				if err != nil {
					t.Fatal(err)
				}
				out.tiles = append(out.tiles, res.GetTiles()...)
			}
		}
		return out
	}
	got := read()
	saved := map[string]metricSQL{}
	for metric, ref := range referenceAssetQueries {
		saved[metric] = metricQueries[metric]
		metricQueries[metric] = metricSQL{query: ref}
	}
	want := read()
	for metric, m := range saved {
		metricQueries[metric] = m
	}
	for k, o := range want.obs {
		if !proto.Equal(got.obs[k], o) {
			t.Errorf("observations %s:\ncounts  %v\nreports %v", k, got.obs[k], o)
		}
	}
	if len(got.tiles) != len(want.tiles) {
		t.Fatalf("%d tiles, want %d", len(got.tiles), len(want.tiles))
	}
	for i := range got.tiles {
		if !proto.Equal(got.tiles[i], want.tiles[i]) {
			t.Errorf("tile %s:\ncounts  %v\nreports %v", got.tiles[i].GetId(), got.tiles[i], want.tiles[i])
		}
	}
}

// Where an epoch of the range has no counts, the series counts the
// reports, and filling the counts gives the same series again.
func TestAssetCountsMissingFallBackToReports(t *testing.T) {
	p := assetProvider(t)
	ctx := context.Background()
	res, err := p.St.DB().Exec(`DELETE FROM epoch_asset_counts WHERE epoch % 3 = 0`)
	if err != nil {
		t.Fatal(err)
	}
	deleted, _ := res.RowsAffected()
	sys := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SYSTEM}
	r := assetRanges()[0]
	check := func(stage string) {
		t.Helper()
		for metric, ref := range referenceAssetQueries {
			got, err := p.series(ctx, metric, sys, r, 7)
			if err != nil {
				t.Fatal(err)
			}
			want, err := p.seriesOf(ctx, metric, metricSQL{query: ref}, sys, r, 7)
			if err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(got, want) {
				t.Errorf("%s, %s:\ngot  %v\nwant %v", stage, metric, got, want)
			}
		}
	}
	check("counts missing")
	n, err := p.St.FillAssetCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != deleted || n == 0 {
		t.Errorf("filled %d epochs, want the %d deleted", n, deleted)
	}
	m := metricQueries["cluster.streams"]
	if res, err := p.resolveMetric(ctx, m, r.Start.AsTime(), r.End.AsTime()); err != nil || res.query != m.query {
		t.Fatalf("after the fill an epoch still has no counts (%v)", err)
	}
	check("counts filled")
}
