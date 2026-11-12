package storeprov

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
)

// streamRows reads the streams of epoch e, optionally one account or one
// stream, with growth since the range's first epoch.
func (p *Provider) streamRows(ctx context.Context, e epochInfo, r *v1.TimeRange, account, stream string) ([]*v1.StreamSummary, *v1.StreamTotals, error) {
	start, end := span(r)
	out, totals, err := p.streamBase(ctx, e, account, stream)
	if err != nil {
		return nil, nil, err
	}
	growth, err := p.streamGrowth(ctx, start, end, account, stream)
	if err != nil {
		return nil, nil, err
	}
	for _, s := range out {
		s.BytesPerDay = api.RobustPerDay(growth[s.GetEntity().GetKey()])
	}
	week := &v1.TimeRange{Start: ts(end.Add(-7 * 24 * time.Hour)), End: r.GetEnd()}
	lines, err := p.sparklines(ctx, week, account, stream)
	if err != nil {
		return nil, nil, err
	}
	for _, s := range out {
		s.Bytes_7D = lines[s.GetEntity().GetKey()]
		if s.Bytes_7D == nil {
			s.Bytes_7D = &v1.Series{Metric: "stream.bytes", Unit: v1.Unit_UNIT_BYTES}
		}
		s.Bytes_7D.Entity = s.GetEntity()
	}
	return out, totals, nil
}

// streamBase reads the streams of epoch e, optionally one account or one
// stream, with their numbers and placement but without growth or series.
func (p *Provider) streamBase(ctx context.Context, e epochInfo, account, stream string) ([]*v1.StreamSummary, *v1.StreamTotals, error) {
	return p.streamBaseFiltered(ctx, e, account, stream, true)
}

func (p *Provider) streamBaseFiltered(ctx context.Context, e epochInfo, account, stream string, includeKV bool) ([]*v1.StreamSummary, *v1.StreamTotals, error) {
	var out []*v1.StreamSummary
	totals := &v1.StreamTotals{}
	budget := newMetadataBudget()
	budget.rows = api.MaxListRankingRows
	err := p.readMetadata(ctx, `
WITH now_ AS (
  SELECT account, stream,
    coalesce(max(messages) FILTER (WHERE is_leader), max(messages)) AS messages,
    coalesce(max(bytes) FILTER (WHERE is_leader), max(bytes)) AS bytes,
    coalesce(max(leader) FILTER (WHERE is_leader), max(leader) FILTER (WHERE leader <> '')) AS leader,
    count(DISTINCT created) > 1 AS created_differs
  FROM stream_stats WHERE epoch = ? AND (? = '' OR account = ?) AND (? = '' OR stream = ?)
    AND (? OR (NOT starts_with(stream, 'KV_') AND NOT starts_with(stream, 'OBJ_'))) GROUP BY ALL),
cfg AS (
  SELECT account, stream, max(replicas) AS replicas, count(DISTINCT config_hash) > 1 AS config_differs
  FROM stream_config WHERE ? BETWEEN first_epoch AND last_epoch GROUP BY ALL),
cons AS (
  SELECT x.account, x.stream,
    count(DISTINCT x.consumer) FILTER (WHERE coalesce(cc.durable_name, '') <> '') AS durable,
    count(DISTINCT x.consumer) FILTER (WHERE coalesce(cc.durable_name, '') = '') AS ephemeral
  FROM consumer_stats x LEFT JOIN consumer_config cc ON cc.server_id = x.server_id AND cc.account = x.account
    AND cc.stream = x.stream AND cc.consumer = x.consumer AND x.epoch BETWEEN cc.first_epoch AND cc.last_epoch
  WHERE x.epoch = ? GROUP BY ALL)
SELECT n.account, n.stream, c.replicas, n.messages, n.bytes, n.leader,
  coalesce(k.durable, 0) AS durable, coalesce(k.ephemeral, 0) AS ephemeral, n.created_differs OR coalesce(c.config_differs, false) AS disagree
FROM now_ n LEFT JOIN cfg c USING (account, stream)
LEFT JOIN cons k USING (account, stream)
ORDER BY n.account, n.stream`, []any{e.epoch, account, account, stream, stream, includeKV, e.epoch, e.epoch}, budget, func(raw []byte) error {
		var v struct {
			Account, Stream, Leader string
			Replicas                *int32
			Messages, Bytes         *int64
			Durable, Ephemeral      int32
			Disagree                bool
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		s := &v1.StreamSummary{Entity: refOf(v1.EntityType_ENTITY_TYPE_STREAM, []any{v.Account, v.Stream}),
			Replicas: v.Replicas, Messages: v.Messages, Bytes: v.Bytes, Leader: v.Leader, Consumers: ptr(v.Durable + v.Ephemeral),
			ServersDisagree: v.Disagree, Status: v1.Status_STATUS_NO_DATA}
		totals.Bytes += s.GetBytes()
		totals.DurableConsumers += v.Durable
		totals.EphemeralConsumers += v.Ephemeral
		out = append(out, s)
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("streams: %w", err)
	}
	keys := make([]string, 0, len(out))
	for _, s := range out {
		keys = append(keys, s.GetEntity().GetKey())
	}
	reps, err := p.streamReplicasScoped(ctx, e.epoch, account, stream, "", keys, includeKV)
	if err != nil {
		return nil, nil, err
	}
	placementBudget := newMetadataBudget()
	for _, s := range out {
		n := max(len(reps[s.GetEntity().GetKey()]), int(s.GetReplicas()))
		if n > placementBudget.rows {
			return nil, nil, metadataOverflow()
		}
		placementBudget.rows -= n
		s.Placement = placementOf(reps[s.GetEntity().GetKey()], s.GetReplicas())
		placementBudget.bytes -= proto.Size(s)
		if placementBudget.bytes < 0 {
			return nil, nil, metadataOverflow()
		}
	}
	return out, totals, nil
}

// growthBucket is the growth bucket streamGrowth and streamPoints share,
// so that the Streams and Capacity pages read one bytes growth; both take
// a stream's value in an epoch from streamPerEpochSQL.
const growthBucket = `time_bucket(to_microseconds(?::BIGINT), t, TIMESTAMP '1970-01-01 00:00:00')`

// leaderValue is a stream's value of col in one epoch: the leader's
// report, else the largest any server reports.
func leaderValue(col string) string {
	return "coalesce(max(x." + col + ") FILTER (WHERE x.is_leader), max(x." + col + "))"
}

// growthWidth is the width of a growth bucket over a range.
func (p *Provider) growthWidth(start, end time.Time) time.Duration {
	return max(end.Sub(start)/api.GrowthBuckets, max(p.Interval, time.Second))
}

// streamGrowth reads every stream's bytes in the range, reduced in SQL to
// at most api.GrowthBuckets+1 aligned medians per stream, for api.RobustPerDay. Its
// bytes medians are streamPoints' for the same range, without the messages
// ListStreams does not read.
func (p *Provider) streamGrowth(ctx context.Context, start, end time.Time, account, stream string) (map[string][]api.Sample, error) {
	return p.streamGrowthKeys(ctx, start, end, account, stream, nil)
}
func (p *Provider) streamGrowthKeys(ctx context.Context, start, end time.Time, account, stream string, keys []string) (map[string][]api.Sample, error) {
	// ASVS 1.2.4: timestamps and selected entity keys remain query parameters.
	start = api.GrowthStart(start, end, p.Interval)
	sampled, err := p.streamSamplesCover(ctx, start, end)
	if err != nil {
		return nil, err
	}
	rows, err := p.db().QueryContext(ctx, `
WITH per AS (
  `+streamPerEpochSQL(sampled, ` AND (? OR x.account || '/' || x.stream IN (SELECT unnest(?::VARCHAR[])))`, [2]string{"bytes", "v"})+`)
SELECT account, stream, min(t) + (max(t) - min(t)) / 2, median(v)::DOUBLE, min(t), max(t)
FROM per WHERE v IS NOT NULL
GROUP BY account, stream, `+growthBucket+`
ORDER BY 1, 2, 3`, start, end, account, account, stream, stream, keys == nil, keys, p.growthWidth(start, end).Microseconds())
	if err != nil {
		return nil, fmt.Errorf("stream growth: %w", err)
	}
	defer rows.Close()
	out := map[string][]api.Sample{}
	for rows.Next() {
		var acc, name string
		var sm api.Sample
		if err := rows.Scan(&acc, &name, &sm.T, &sm.V, &sm.From, &sm.To); err != nil {
			return nil, err
		}
		out[acc+"/"+name] = append(out[acc+"/"+name], sm)
	}
	return out, rows.Err()
}

// streamPts are a stream's bucket medians of bytes and of messages.
type streamPts struct{ bytes, msgs []api.Sample }

// streamPoints reads every stream's bytes and messages in the range for
// the Capacity page (the leader's report, else the largest any server
// reports), reduced in SQL to at most api.GrowthBuckets+1 aligned medians per stream
// and keyed by account/stream: one query for every stream.
func (p *Provider) streamPoints(ctx context.Context, start, end time.Time, account, stream string) (map[string]streamPts, error) {
	// ASVS 1.2.4: timestamps and selected entity keys remain query parameters.
	start = api.GrowthStart(start, end, p.Interval)
	sampled, err := p.streamSamplesCover(ctx, start, end)
	if err != nil {
		return nil, err
	}
	rows, err := p.db().QueryContext(ctx, `
WITH per AS (
  `+streamPerEpochSQL(sampled, "", [2]string{"bytes", "v"}, [2]string{"messages", "m"})+`)
SELECT account, stream, `+growthBucket+` AS b,
  min(t) FILTER (WHERE v IS NOT NULL) + (max(t) FILTER (WHERE v IS NOT NULL) - min(t) FILTER (WHERE v IS NOT NULL)) / 2,
  median(v)::DOUBLE,
  min(t) FILTER (WHERE m IS NOT NULL) + (max(t) FILTER (WHERE m IS NOT NULL) - min(t) FILTER (WHERE m IS NOT NULL)) / 2,
  median(m)::DOUBLE,
  min(t) FILTER (WHERE v IS NOT NULL), max(t) FILTER (WHERE v IS NOT NULL),
  min(t) FILTER (WHERE m IS NOT NULL), max(t) FILTER (WHERE m IS NOT NULL)
FROM per WHERE v IS NOT NULL OR m IS NOT NULL
GROUP BY ALL
ORDER BY 1, 2, 3`, start, end, account, account, stream, stream, p.growthWidth(start, end).Microseconds())
	if err != nil {
		return nil, fmt.Errorf("stream points: %w", err)
	}
	defer rows.Close()
	out := map[string]streamPts{}
	for rows.Next() {
		var acc, name string
		var b time.Time
		var bt, mt, bf, bl, mf, ml sql.NullTime
		var bv, mv sql.NullFloat64
		if err := rows.Scan(&acc, &name, &b, &bt, &bv, &mt, &mv, &bf, &bl, &mf, &ml); err != nil {
			return nil, err
		}
		pts := out[acc+"/"+name]
		if bt.Valid && bv.Valid {
			pts.bytes = append(pts.bytes, api.Sample{T: bt.Time, V: bv.Float64, From: bf.Time, To: bl.Time})
		}
		if mt.Valid && mv.Valid {
			pts.msgs = append(pts.msgs, api.Sample{T: mt.Time, V: mv.Float64, From: mf.Time, To: ml.Time})
		}
		out[acc+"/"+name] = pts
	}
	return out, rows.Err()
}

// sparklineBuckets is the number of buckets of a stream's 7-day series.
const sparklineBuckets = 28

// sparklines reads stream.bytes for every stream over the range in one
// query, keyed by account/stream, with the buckets of series.
func (p *Provider) sparklines(ctx context.Context, r *v1.TimeRange, account, stream string) (map[string]*v1.Series, error) {
	return p.sparklinesKeys(ctx, r, account, stream, nil)
}
func (p *Provider) sparklinesKeys(ctx context.Context, r *v1.TimeRange, account, stream string, keys []string) (map[string]*v1.Series, error) {
	step := api.Step(r, sparklineBuckets, max(p.Interval, time.Second))
	start, end := span(r)
	sampled, err := p.streamSamplesCover(ctx, start, end)
	if err != nil {
		return nil, err
	}
	rows, err := p.db().QueryContext(ctx, `
WITH per AS (
  `+streamPerEpochSQL(sampled, `
  AND (? OR x.account || '/' || x.stream IN (SELECT unnest(?::VARCHAR[])))`, [2]string{"bytes", "v"})+`)
SELECT account, stream, time_bucket(to_microseconds(?::BIGINT), t, TIMESTAMP '1970-01-01 00:00:00') AS b, avg(v)::DOUBLE
FROM per WHERE v IS NOT NULL GROUP BY ALL ORDER BY account, stream, b`,
		start, end, account, account, stream, stream, keys == nil, keys, step.Microseconds())
	if err != nil {
		return nil, fmt.Errorf("sparklines: %w", err)
	}
	defer rows.Close()
	out := map[string]*v1.Series{}
	for rows.Next() {
		var acc, name string
		var b time.Time
		var v float64
		if err := rows.Scan(&acc, &name, &b, &v); err != nil {
			return nil, err
		}
		key := acc + "/" + name
		s := out[key]
		if s == nil {
			s = &v1.Series{Metric: "stream.bytes", Unit: v1.Unit_UNIT_BYTES, Step: dur(step)}
			out[key] = s
		}
		s.Points = append(s.Points, &v1.Point{Time: ts(b.UTC()), Value: v})
		if s.Min == nil || v < s.GetMin() {
			s.Min = ptr(v)
		}
		if s.Max == nil || v > s.GetMax() {
			s.Max = ptr(v)
		}
	}
	return out, rows.Err()
}

func isKV(name string) bool { return strings.HasPrefix(name, "KV_") || strings.HasPrefix(name, "OBJ_") }

// ListStreams reads the streams of the range's newest epoch.
func (p *Provider) ListStreams(ctx context.Context, req *v1.ListStreamsRequest) (*v1.ListStreamsResponse, error) {
	n, err := api.EntityLimit(req.GetLimit())
	if err != nil {
		return nil, err
	}
	scopeReq := proto.Clone(req).(*v1.ListStreamsRequest)
	scopeReq.Limit = 0
	scopeReq.PageToken = ""
	snap, offset, err := p.listPages.Begin(ctx, req.GetPageToken(), api.EntityScope(scopeReq), func() (*api.EntitySnapshot, error) {
		e, ok, err := p.latestEpoch(ctx, req.GetRange())
		if err != nil {
			return nil, err
		}
		snap := &api.EntitySnapshot{Epoch: e.epoch, Range: req.GetRange(), Totals: &v1.StreamTotals{}}
		if !ok {
			return snap, nil
		}
		snap.Streams, snap.Totals, err = p.streamBaseFiltered(ctx, e, req.GetAccount(), "", req.GetIncludeKv())
		if err != nil {
			return nil, err
		}
		if err = p.listStreamStatus(ctx, req.GetRange(), e, snap.Streams); err != nil {
			return nil, err
		}
		return snap, nil
	})
	if err != nil {
		return nil, err
	}
	if err = p.listEpochStored(ctx, snap.Epoch); err != nil {
		return nil, err
	}
	res := &v1.ListStreamsResponse{Totals: proto.Clone(snap.Totals).(*v1.StreamTotals), TotalCount: int32(len(snap.Streams)), Range: snap.Range, Epoch: snap.Epoch}
	for _, base := range snap.Streams[offset:min(offset+n, len(snap.Streams))] {
		res.Streams = append(res.Streams, proto.Clone(base).(*v1.StreamSummary))
	}
	if !req.GetSummaryOnly() {
		if err = p.enrichStreamPage(ctx, res.Streams, snap.Range); err != nil {
			return nil, err
		}
	}
	for !api.FitsEntityPage(res) && len(res.Streams) > 1 {
		res.Streams = res.Streams[:len(res.Streams)-1]
	}
	if !api.FitsEntityPage(res) {
		return nil, fmt.Errorf("%w: stream summary exceeds page byte budget", api.ErrResourceExhausted)
	}
	res.NextPageToken = snap.Next(offset + len(res.Streams))
	return res, nil
}

// streamStatus sets the streams' statuses, finding codes and expected
// consumers, returning the same firing evidence for request-local reuse.
func (p *Provider) streamStatus(ctx context.Context, r *v1.TimeRange, e epochInfo, ss []*v1.StreamSummary) ([]*v1.Finding, error) {
	cur, err := p.firingFindings(ctx, r)
	if err != nil {
		return nil, err
	}
	ran, err := p.checksRan(ctx, e.epoch)
	if err != nil {
		return nil, err
	}
	inv, named, err := p.inventory(ctx)
	if err != nil {
		return nil, err
	}
	applyStreamStatus(ss, cur, ran, expectedPerStream(inv), named)
	return cur, nil
}

// GetStream reads one stream of the range's newest epoch.
func (p *Provider) GetStream(ctx context.Context, req *v1.GetStreamRequest) (*v1.GetStreamResponse, error) {
	e, ok, err := p.latestEpoch(ctx, req.GetRange())
	if err != nil {
		return nil, err
	}
	var list []*v1.StreamSummary
	if ok {
		if list, _, err = p.streamRows(ctx, e, req.GetRange(), req.GetAccount(), req.GetStream()); err != nil {
			return nil, err
		}
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("stream %s/%s in range: %w", req.GetAccount(), req.GetStream(), api.ErrNotFound)
	}
	cur, err := p.streamStatus(ctx, req.GetRange(), e, list)
	if err != nil {
		return nil, err
	}
	res := &v1.GetStreamResponse{Stream: list[0]}
	if res.Servers, err = p.streamViews(ctx, e.epoch, req.GetAccount(), req.GetStream()); err != nil {
		return nil, err
	}
	res.Disagreements = disagreements(res.GetServers(), func(v *v1.StreamServerView) (string, map[string]string) {
		return v.GetServer(), map[string]string{"config_hash": v.GetConfigHash(), "created": fmtTime(v.GetCreated().AsTime())}
	})
	if res.Config, err = p.streamConfig(ctx, e.epoch, req.GetAccount(), req.GetStream()); err != nil {
		return nil, err
	}
	if res.Consumers, err = p.consumersWithMissing(ctx, req.GetRange(), e, req.GetAccount(), req.GetStream(), ""); err != nil {
		return nil, err
	}
	res.Findings = findingsFor(cur, res.GetStream().GetEntity())
	return res, nil
}

func fmtTime(t time.Time) string {
	if t.IsZero() || t.Unix() == 0 {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// disagreements lists the fields whose values differ between servers.
func disagreements[T any](views []T, fields func(T) (string, map[string]string)) []*v1.Disagreement {
	byField := map[string][]*v1.ServerValue{}
	distinct := map[string]map[string]bool{}
	for _, v := range views {
		server, vals := fields(v)
		for f, val := range vals {
			byField[f] = append(byField[f], &v1.ServerValue{Server: server, Value: val})
			if distinct[f] == nil {
				distinct[f] = map[string]bool{}
			}
			distinct[f][val] = true
		}
	}
	var out []*v1.Disagreement
	for f, vals := range byField {
		if len(distinct[f]) > 1 {
			out = append(out, &v1.Disagreement{Field: f, Values: vals})
		}
	}
	slices.SortFunc(out, func(a, b *v1.Disagreement) int { return cmp.Compare(a.GetField(), b.GetField()) })
	return out
}

// streamViews reads each server's view of a stream; current, offline and
// lag come from the replica list of the leader placementOf chooses.
func (p *Provider) streamViews(ctx context.Context, epoch int64, account, stream string) ([]*v1.StreamServerView, error) {
	m, err := p.streamViewsBy(ctx, epoch, account, stream)
	return m[account+"/"+stream], err
}

// streamViewsBy reads each server's view of every stream of the epoch,
// optionally one account or stream, keyed by stream key.
func (p *Provider) streamViewsBy(ctx context.Context, epoch int64, account, stream string) (map[string][]*v1.StreamServerView, error) {
	rows, err := p.db().QueryContext(ctx, `
WITH chosen AS (
  -- The leader placementOf chooses: the first server by name whose own
  -- report claims the lead, whether or not it lists replicas.
  SELECT l.account, l.stream, arg_min(l.server_id, ls.server_name) AS server_id
  FROM stream_stats l JOIN servers ls USING (server_id)
  WHERE l.epoch = ? AND l.is_leader GROUP BY ALL),
lead AS (
  SELECT rp.account, rp.stream, rp.peer_name, rp.current, rp.offline, rp.lag FROM stream_replicas rp
  JOIN chosen c ON c.server_id = rp.server_id AND c.account = rp.account AND c.stream = rp.stream
  WHERE rp.epoch = ? AND (? = '' OR rp.account = ?) AND (? = '' OR rp.stream = ?))
SELECT x.account, x.stream, s.server_name, coalesce(x.is_leader, false), x.messages, x.bytes,
  x.first_seq, x.last_seq, x.created, sc.config_hash, r.current, r.offline, r.lag
FROM stream_stats x JOIN servers s USING (server_id)
LEFT JOIN stream_config sc ON sc.server_id = x.server_id AND sc.account = x.account AND sc.stream = x.stream
  AND x.epoch BETWEEN sc.first_epoch AND sc.last_epoch
LEFT JOIN lead r ON r.account = x.account AND r.stream = x.stream AND r.peer_name = s.server_name
WHERE x.epoch = ? AND (? = '' OR x.account = ?) AND (? = '' OR x.stream = ?) ORDER BY s.server_name`,
		epoch, epoch, account, account, stream, stream, epoch, account, account, stream, stream)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]*v1.StreamServerView{}
	for rows.Next() {
		var v v1.StreamServerView
		var acc, st string
		var created sql.NullTime
		var hash sql.NullString
		var current, offline sql.NullBool
		if err := rows.Scan(&acc, &st, &v.Server, &v.IsLeader, &v.Messages, &v.Bytes, &v.FirstSeq, &v.LastSeq, &created, &hash,
			&current, &offline, &v.Lag); err != nil {
			return nil, err
		}
		v.Created, v.ConfigHash = ts(ntime(created)), hash.String
		v.Current, v.Offline = v.GetIsLeader() || current.Bool, offline.Bool
		out[acc+"/"+st] = append(out[acc+"/"+st], &v)
	}
	return out, rows.Err()
}

// enrichStreamPage issues two batched history reads for the selected keys only.
func (p *Provider) enrichStreamPage(ctx context.Context, ss []*v1.StreamSummary, r *v1.TimeRange) error {
	if len(ss) == 0 {
		return ctx.Err()
	}
	keys := make([]string, 0, len(ss))
	for _, s := range ss {
		keys = append(keys, s.GetEntity().GetKey())
	}
	start, end := span(r)
	growth, err := p.streamGrowthKeys(ctx, start, end, "", "", keys)
	if err != nil {
		return err
	}
	lines, err := p.sparklinesKeys(ctx, &v1.TimeRange{Start: ts(end.Add(-7 * 24 * time.Hour)), End: r.End}, "", "", keys)
	if err != nil {
		return err
	}
	for _, s := range ss {
		s.BytesPerDay = api.RobustPerDay(growth[s.GetEntity().GetKey()])
		s.Bytes_7D = lines[s.GetEntity().GetKey()]
		if s.Bytes_7D == nil {
			s.Bytes_7D = &v1.Series{Metric: "stream.bytes", Unit: v1.Unit_UNIT_BYTES}
		}
		s.Bytes_7D.Entity = s.Entity
	}
	return ctx.Err()
}

func (p *Provider) listEpochStored(ctx context.Context, epoch int64) error {
	if epoch == 0 {
		return ctx.Err()
	}
	var n int
	if err := p.db().QueryRowContext(ctx, "SELECT count(*) FROM epochs WHERE epoch = ?", epoch).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: list epoch is no longer stored; read the first page again", api.ErrFailedPrecondition)
	}
	return nil
}
