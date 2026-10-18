package storeprov

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
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
	rows, err := p.db().QueryContext(ctx, `
WITH now_ AS (
  SELECT account, stream,
    coalesce(max(messages) FILTER (WHERE is_leader), max(messages)) AS messages,
    coalesce(max(bytes) FILTER (WHERE is_leader), max(bytes)) AS bytes,
    coalesce(max(leader) FILTER (WHERE is_leader), max(leader) FILTER (WHERE leader <> '')) AS leader,
    count(DISTINCT created) > 1 AS created_differs
  FROM stream_stats WHERE epoch = ? AND (? = '' OR account = ?) AND (? = '' OR stream = ?) GROUP BY ALL),
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
  coalesce(k.durable, 0), coalesce(k.ephemeral, 0), n.created_differs OR coalesce(c.config_differs, false)
FROM now_ n LEFT JOIN cfg c USING (account, stream)
LEFT JOIN cons k USING (account, stream)
ORDER BY n.account, n.stream`, e.epoch, account, account, stream, stream, e.epoch, e.epoch)
	if err != nil {
		return nil, nil, fmt.Errorf("streams: %w", err)
	}
	defer rows.Close()
	var out []*v1.StreamSummary
	totals := &v1.StreamTotals{}
	for rows.Next() {
		var s v1.StreamSummary
		var acc, name string
		var leader sql.NullString
		var durable, ephemeral int32
		// Numbers scan into their optional fields: NULL leaves them unset.
		if err := rows.Scan(&acc, &name, &s.Replicas, &s.Messages, &s.Bytes, &leader, &durable, &ephemeral,
			&s.ServersDisagree); err != nil {
			return nil, nil, err
		}
		s.Entity = refOf(v1.EntityType_ENTITY_TYPE_STREAM, []any{acc, name})
		// The status is no data until streamStatus applies the findings.
		s.Leader, s.Consumers, s.Status = leader.String, ptr(durable+ephemeral), v1.Status_STATUS_NO_DATA
		totals.Bytes += s.GetBytes()
		totals.DurableConsumers += durable
		totals.EphemeralConsumers += ephemeral
		out = append(out, &s)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	reps, err := p.streamReplicas(ctx, e.epoch, account, stream, "")
	if err != nil {
		return nil, nil, err
	}
	for _, s := range out {
		s.Placement = placementOf(reps[s.GetEntity().GetKey()], s.GetReplicas())
	}
	return out, totals, nil
}

// streamGrowth reads every stream's leader bytes in the range, reduced in
// SQL to at most api.GrowthBuckets medians per stream, for api.RobustPerDay.
func (p *Provider) streamGrowth(ctx context.Context, start, end time.Time, account, stream string) (map[string][]api.Sample, error) {
	width := max(end.Sub(start)/api.GrowthBuckets, max(p.Interval, time.Second))
	rows, err := p.db().QueryContext(ctx, `
WITH per AS (
  SELECT x.account, x.stream, e.started_at AS t,
    coalesce(max(x.bytes) FILTER (WHERE x.is_leader), max(x.bytes)) AS v
  FROM stream_stats x JOIN epochs e USING (epoch)
  WHERE e.started_at >= ? AND e.started_at < ? AND (? = '' OR x.account = ?) AND (? = '' OR x.stream = ?)
  GROUP BY x.account, x.stream, e.epoch, e.started_at)
SELECT account, stream, min(t) + (max(t) - min(t)) / 2, median(v)::DOUBLE
FROM per WHERE v IS NOT NULL
GROUP BY account, stream, time_bucket(to_microseconds(?::BIGINT), t, TIMESTAMP '1970-01-01 00:00:00')
ORDER BY 1, 2, 3`, start, end, account, account, stream, stream, width.Microseconds())
	if err != nil {
		return nil, fmt.Errorf("stream growth: %w", err)
	}
	defer rows.Close()
	out := map[string][]api.Sample{}
	for rows.Next() {
		var acc, name string
		var sm api.Sample
		if err := rows.Scan(&acc, &name, &sm.T, &sm.V); err != nil {
			return nil, err
		}
		out[acc+"/"+name] = append(out[acc+"/"+name], sm)
	}
	return out, rows.Err()
}

// sparklineBuckets is the number of buckets of a stream's 7-day series.
const sparklineBuckets = 28

// sparklines reads stream.bytes for every stream over the range in one
// query, keyed by account/stream, with the buckets of series.
func (p *Provider) sparklines(ctx context.Context, r *v1.TimeRange, account, stream string) (map[string]*v1.Series, error) {
	step := api.Step(r, sparklineBuckets, max(p.Interval, time.Second))
	start, end := span(r)
	rows, err := p.db().QueryContext(ctx, `
WITH per AS (
  SELECT x.account, x.stream, e.started_at AS t,
    coalesce(max(x.bytes) FILTER (WHERE x.is_leader), max(x.bytes)) AS v
  FROM stream_stats x JOIN epochs e USING (epoch)
  WHERE e.started_at >= ? AND e.started_at < ? AND (? = '' OR x.account = ?) AND (? = '' OR x.stream = ?)
  GROUP BY x.account, x.stream, e.epoch, e.started_at)
SELECT account, stream, time_bucket(to_microseconds(?::BIGINT), t, TIMESTAMP '1970-01-01 00:00:00') AS b, avg(v)::DOUBLE
FROM per WHERE v IS NOT NULL GROUP BY ALL ORDER BY account, stream, b`,
		start, end, account, account, stream, stream, step.Microseconds())
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
	e, ok, err := p.latestEpoch(ctx, req.GetRange())
	if err != nil || !ok {
		return &v1.ListStreamsResponse{Totals: &v1.StreamTotals{}}, err
	}
	all, totals, err := p.streamRows(ctx, e, req.GetRange(), req.GetAccount(), "")
	if err != nil {
		return nil, err
	}
	if err := p.streamStatus(ctx, req.GetRange(), e, all); err != nil {
		return nil, err
	}
	res := &v1.ListStreamsResponse{Totals: totals}
	for _, s := range all {
		if req.GetIncludeKv() || !isKV(s.GetEntity().GetStream()) {
			res.Streams = append(res.Streams, s)
		}
	}
	return api.LimitStreams(res, req.GetLimit()), nil
}

// streamStatus sets the streams' statuses, finding codes and expected
// consumers.
func (p *Provider) streamStatus(ctx context.Context, r *v1.TimeRange, e epochInfo, ss []*v1.StreamSummary) error {
	cur, err := p.firingFindings(ctx, r)
	if err != nil {
		return err
	}
	ran, err := p.checksRan(ctx, e.epoch)
	if err != nil {
		return err
	}
	inv, named, err := p.inventory(ctx)
	if err != nil {
		return err
	}
	applyStreamStatus(ss, cur, ran, expectedPerStream(inv), named)
	return nil
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
	if err := p.streamStatus(ctx, req.GetRange(), e, list); err != nil {
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
	cur, err := p.firingFindings(ctx, req.GetRange())
	if err != nil {
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
// lag come from the leader's replica list.
func (p *Provider) streamViews(ctx context.Context, epoch int64, account, stream string) ([]*v1.StreamServerView, error) {
	m, err := p.streamViewsBy(ctx, epoch, account, stream)
	return m[account+"/"+stream], err
}

// streamViewsBy reads each server's view of every stream of the epoch,
// optionally one account or stream, keyed by stream key.
func (p *Provider) streamViewsBy(ctx context.Context, epoch int64, account, stream string) (map[string][]*v1.StreamServerView, error) {
	rows, err := p.db().QueryContext(ctx, `
WITH lead AS (
  SELECT rp.account, rp.stream, rp.peer_name, rp.current, rp.offline, rp.lag FROM stream_replicas rp
  JOIN stream_stats l ON l.epoch = rp.epoch AND l.server_id = rp.server_id AND l.account = rp.account
    AND l.stream = rp.stream AND l.is_leader
  WHERE rp.epoch = ? AND (? = '' OR rp.account = ?) AND (? = '' OR rp.stream = ?))
SELECT x.account, x.stream, s.server_name, coalesce(x.is_leader, false), x.messages, x.bytes,
  x.first_seq, x.last_seq, x.created, sc.config_hash, r.current, r.offline, r.lag
FROM stream_stats x JOIN servers s USING (server_id)
LEFT JOIN stream_config sc ON sc.server_id = x.server_id AND sc.account = x.account AND sc.stream = x.stream
  AND x.epoch BETWEEN sc.first_epoch AND sc.last_epoch
LEFT JOIN lead r ON r.account = x.account AND r.stream = x.stream AND r.peer_name = s.server_name
WHERE x.epoch = ? AND (? = '' OR x.account = ?) AND (? = '' OR x.stream = ?) ORDER BY s.server_name`,
		epoch, account, account, stream, stream, epoch, account, account, stream, stream)
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
