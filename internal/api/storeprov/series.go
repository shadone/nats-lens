package storeprov

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// metricSQL is a query that returns one value per epoch: columns t (the
// epoch's start) and v. Every query takes the same arguments, in order:
// the earliest epoch start to read, the end of the range, and the entity
// parts from entityArgs. sum marks event counts, which a bucket adds up;
// the others are averaged.
type metricSQL struct {
	query string
	sum   bool
}

// maxGapToken in a metric query is replaced by the longest gap between
// two epochs, in milliseconds, over which a rate is still computed.
const maxGapToken = "{max_gap_ms}"

// rate is the per-second increase of a cumulative server_stats counter,
// per server process, summed per epoch. A process restart starts a new
// partition, so a reset counter never yields a negative rate. After a gap
// in collection longer than three intervals (a collector restart) the
// first epoch has no rate, so the increase over the gap is not one sample.
func rate(col, serverFilter string) string {
	return fmt.Sprintf(`
SELECT t, sum(r) AS v FROM (
  SELECT e.epoch, e.started_at AS t,
    CASE WHEN date_diff('millisecond', lag(e.started_at) OVER w, e.started_at) <= %[3]s THEN
      (ss.%[1]s - lag(ss.%[1]s) OVER w) * 1000.0
        / nullif(date_diff('millisecond', lag(e.started_at) OVER w, e.started_at), 0) END AS r
  FROM server_stats ss JOIN epochs e USING (epoch) JOIN servers s USING (server_id)
  WHERE e.started_at >= ? AND e.started_at < ? %[2]s
  WINDOW w AS (PARTITION BY ss.server_id ORDER BY e.epoch))
WHERE r >= 0 GROUP BY epoch, t`, col, serverFilter, maxGapToken)
}

// metricQuery returns the metric's query with the gap bound filled in.
func (p *Provider) metricQuery(m metricSQL) string {
	return strings.ReplaceAll(m.query, maxGapToken, fmt.Sprint((3 * max(p.Interval, time.Second)).Milliseconds()))
}

// gauge sums a server_stats column over servers per epoch.
func gauge(col, serverFilter string) string {
	return fmt.Sprintf(`
SELECT e.started_at AS t, sum(ss.%s) AS v
FROM server_stats ss JOIN epochs e USING (epoch) JOIN servers s USING (server_id)
WHERE e.started_at >= ? AND e.started_at < ? %s
GROUP BY e.epoch, e.started_at`, col, serverFilter)
}

const byServer = `AND s.server_name = ?`

// leaderView reads a column of stream_stats or consumer_stats from the
// leader's row, or the largest value when no server leads.
func leaderView(table, col, where string) string {
	return fmt.Sprintf(`
SELECT e.started_at AS t, coalesce(max(x.%[2]s) FILTER (WHERE x.is_leader), max(x.%[2]s)) AS v
FROM %[1]s x JOIN epochs e USING (epoch)
WHERE e.started_at >= ? AND e.started_at < ? %[3]s
GROUP BY e.epoch, e.started_at`, table, col, where)
}

const (
	byStream   = `AND x.account = ? AND x.stream = ?`
	byConsumer = `AND x.account = ? AND x.stream = ? AND x.consumer = ?`
)

var metricQueries = map[string]metricSQL{
	"cluster.servers_up":       {query: `SELECT started_at AS t, observed_servers AS v FROM epochs WHERE started_at >= ? AND started_at < ?`},
	"cluster.connections":      {query: gauge("connections", "")},
	"cluster.js_storage_bytes": {query: gauge("js_storage", "")},
	"cluster.in_msgs_rate":     {query: rate("in_msgs", "")},
	"cluster.out_msgs_rate":    {query: rate("out_msgs", "")},
	"cluster.in_bytes_rate":    {query: rate("in_bytes", "")},
	"cluster.out_bytes_rate":   {query: rate("out_bytes", "")},
	// The slow consumer counter's increase per epoch: a sum, not a rate.
	// A restarted process's first epoch has no increase, nor has the first
	// epoch after a collection gap of more than three intervals, so the
	// sum agrees with the servers' server.slow_consumers.
	"cluster.slow_consumers": {sum: true, query: `
SELECT t, sum(d) AS v FROM (
  SELECT e.epoch, e.started_at AS t,
    CASE WHEN date_diff('millisecond', lag(e.started_at) OVER w, e.started_at) <= ` + maxGapToken + ` THEN
      ss.slow_consumers - lag(ss.slow_consumers) OVER w END AS d
  FROM server_stats ss JOIN epochs e USING (epoch)
  WHERE e.started_at >= ? AND e.started_at < ?
  WINDOW w AS (PARTITION BY ss.server_id ORDER BY e.epoch))
WHERE d >= 0 GROUP BY epoch, t`},
	"cluster.durable_consumers": {query: `
SELECT e.started_at AS t, count(DISTINCT (cs.account, cs.stream, cs.consumer)) AS v
FROM consumer_stats cs JOIN epochs e USING (epoch)
JOIN consumer_config cc ON cc.server_id = cs.server_id AND cc.account = cs.account AND cc.stream = cs.stream
  AND cc.consumer = cs.consumer AND cs.epoch BETWEEN cc.first_epoch AND cc.last_epoch
WHERE e.started_at >= ? AND e.started_at < ? AND coalesce(cc.durable_name, '') <> ''
GROUP BY e.epoch, e.started_at`},
	"cluster.streams":       {query: streamCount(`NOT starts_with(ss.stream, 'KV_') AND NOT starts_with(ss.stream, 'OBJ_')`)},
	"cluster.kv_buckets":    {query: streamCount(`starts_with(ss.stream, 'KV_')`)},
	"cluster.object_stores": {query: streamCount(`starts_with(ss.stream, 'OBJ_')`)},
	// Accounts with a connection or a stream on any server, the system
	// account left out. The connections come from ACCSTATZ, which every
	// server must have answered; the streams come from JSZ and count only
	// when every server answered that too, so a system without JetStream
	// still counts accounts by their connections.
	"cluster.accounts": {query: `
SELECT t, count(DISTINCT account) FILTER (WHERE (conns > 0 OR (js_streams > 0 AND jsz)) AND NOT coalesce(is_system, false)) AS v
FROM (
  SELECT e.epoch, e.started_at AS t, a.account, a.conns, a.js_streams, ac.is_system, ` + answeredAll("JSZ") + ` AS jsz
  FROM epochs e LEFT JOIN account_stats a ON a.epoch = e.epoch LEFT JOIN accounts ac ON ac.account = a.account
  WHERE e.started_at >= ? AND e.started_at < ? AND ` + answeredAll("ACCSTATZ") + `)
GROUP BY epoch, t`},
	// The meta leader is the one more than half of the expected servers
	// (the answering ones when none is expected) report; a tie, a lone
	// stale answer or no leader leaves the epoch without one, which is no
	// change. A change from the last epoch that had a leader
	// counts once. The lookback of series() reaches about two intervals
	// before the range, so a change at the range's first epoch counts
	// only when an epoch with a leader lies within that lookback.
	"cluster.meta_leader_changes": {sum: true, query: `
WITH ep AS (
  SELECT DISTINCT e.epoch, e.started_at AS t, coalesce(nullif(e.expected_servers, 0), e.observed_servers) AS quorum
  FROM server_stats ss JOIN epochs e USING (epoch)
  WHERE e.started_at >= ? AND e.started_at < ?),
votes AS (
  SELECT ss.epoch, ss.meta_leader AS l, count(*) AS n FROM server_stats ss JOIN ep USING (epoch)
  WHERE coalesce(ss.meta_leader, '') <> '' GROUP BY ss.epoch, ss.meta_leader),
lead AS (SELECT epoch, l FROM votes JOIN ep USING (epoch) WHERE 2 * n > quorum)
SELECT t, CASE WHEN l <> lag(l IGNORE NULLS) OVER (ORDER BY epoch) THEN 1 ELSE 0 END AS v
FROM ep LEFT JOIN lead USING (epoch)`},
	"cluster.assets_replicated": {query: assetCount("r > 1")},
	"cluster.assets_single":     {query: assetCount("r = 1")},
	// The slow consumer counter's increase per epoch and process. As in
	// cluster.slow_consumers, a restarted process's first epoch has no
	// increase, so slow consumers before its first report are not
	// counted; nor is an increase across a collection gap of more than
	// three intervals credited to one epoch.
	"server.slow_consumers": {sum: true, query: `
SELECT t, sum(d) AS v FROM (
  SELECT e.epoch, e.started_at AS t,
    CASE WHEN date_diff('millisecond', lag(e.started_at) OVER w, e.started_at) <= ` + maxGapToken + ` THEN
      ss.slow_consumers - lag(ss.slow_consumers) OVER w END AS d
  FROM server_stats ss JOIN epochs e USING (epoch) JOIN servers s USING (server_id)
  WHERE e.started_at >= ? AND e.started_at < ? ` + byServer + `
  WINDOW w AS (PARTITION BY ss.server_id ORDER BY e.epoch))
WHERE d >= 0 GROUP BY epoch, t`},
	"server.connections":      {query: gauge("connections", byServer)},
	"server.mem_bytes":        {query: gauge("mem_bytes", byServer)},
	"server.cpu_percent":      {query: gauge("cpu_percent", byServer)},
	"server.js_storage_bytes": {query: gauge("js_storage", byServer)},
	"server.in_msgs_rate":     {query: rate("in_msgs", byServer)},
	"server.out_msgs_rate":    {query: rate("out_msgs", byServer)},
	"server.in_bytes_rate":    {query: rate("in_bytes", byServer)},
	"server.out_bytes_rate":   {query: rate("out_bytes", byServer)},
	// The leader's last sequence per epoch, as a per-second increase; a
	// recreated stream (a lower sequence) has no rate in that epoch, nor
	// does the first epoch after a collection gap.
	"stream.msgs_rate": {query: `
SELECT t, r AS v FROM (
  SELECT t, CASE WHEN date_diff('millisecond', lag(t) OVER w, t) <= ` + maxGapToken + ` THEN
      (v - lag(v) OVER w) * 1000.0 / nullif(date_diff('millisecond', lag(t) OVER w, t), 0) END AS r FROM (
    ` + leaderView("stream_stats", "last_seq", byStream) + `)
  WINDOW w AS (ORDER BY t))
WHERE r >= 0`},
	"stream.messages":          {query: leaderView("stream_stats", "messages", byStream)},
	"stream.bytes":             {query: leaderView("stream_stats", "bytes", byStream)},
	"consumer.num_pending":     {query: leaderView("consumer_stats", "num_pending", byConsumer)},
	"consumer.num_ack_pending": {query: leaderView("consumer_stats", "num_ack_pending", byConsumer)},
	"consumer.num_redelivered": {query: leaderView("consumer_stats", "num_redelivered", byConsumer)},
	// The leader's delivered consumer sequence as a per-second increase,
	// per leading server and creation time: a leader move or a recreated
	// consumer starts a new run whose first epoch has no rate, as servers
	// and incarnations count apart. An epoch without a leader row has no
	// value, and the first epoch after a collection gap no rate.
	"consumer.delivered_rate": {query: `
SELECT t, r AS v FROM (
  SELECT t, CASE WHEN date_diff('millisecond', lag(t) OVER w, t) <= ` + maxGapToken + ` THEN
      (v - lag(v) OVER w) * 1000.0 / nullif(date_diff('millisecond', lag(t) OVER w, t), 0) END AS r FROM (
    SELECT e.started_at AS t, x.server_id, x.created, x.delivered_consumer_seq AS v
    FROM consumer_stats x JOIN epochs e USING (epoch)
    WHERE e.started_at >= ? AND e.started_at < ? ` + byConsumer + ` AND x.is_leader
    QUALIFY row_number() OVER (PARTITION BY e.epoch ORDER BY x.delivered_consumer_seq DESC, x.server_id) = 1)
  WINDOW w AS (PARTITION BY server_id, created ORDER BY t))
WHERE r >= 0`},
}

// answeredAll keeps the epochs e in which every server that answered at
// all answered the endpoint: with one reply missing a count would dip, so
// such an epoch has no point, as does one where no server answered.
func answeredAll(endpoint string) string {
	return fmt.Sprintf(`e.observed_servers > 0 AND e.observed_servers <= (SELECT count(DISTINCT x.server_id) FROM epoch_endpoints x
  WHERE x.epoch = e.epoch AND x.endpoint = '%s' AND x.state = 'ok')`, endpoint)
}

// jszObserved keeps the epochs whose JSZ replies are complete.
var jszObserved = answeredAll("JSZ")

// streamCount counts the streams any server reported per epoch whose row
// ss matches the filter.
func streamCount(filter string) string {
	return fmt.Sprintf(`
SELECT e.started_at AS t, count(DISTINCT (ss.account, ss.stream)) FILTER (WHERE %s) AS v
FROM epochs e LEFT JOIN stream_stats ss ON ss.epoch = e.epoch
WHERE e.started_at >= ? AND e.started_at < ? AND %s
GROUP BY e.epoch, e.started_at`, filter, jszObserved)
}

// assetCount counts the streams and durable consumers per epoch whose
// configured replica count r matches the condition. A consumer's
// replicas 0 means its stream's count. Servers that disagree on a count
// give the largest.
func assetCount(cond string) string {
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
GROUP BY w.epoch, w.started_at`, cond, jszObserved)
}

// entityArgs splits an entity key into the query arguments of its type.
func entityArgs(e *v1.EntityRef) []any {
	switch e.GetType() {
	case v1.EntityType_ENTITY_TYPE_SERVER:
		return []any{e.GetKey()}
	case v1.EntityType_ENTITY_TYPE_STREAM, v1.EntityType_ENTITY_TYPE_CONSUMER:
		parts := strings.Split(e.GetKey(), "/")
		out := make([]any, len(parts))
		for i, s := range parts {
			out[i] = s
		}
		return out
	}
	return nil
}

// checkEntity returns the entity with its parts filled in, or ErrNotFound
// when the database never saw it.
func (p *Provider) checkEntity(ctx context.Context, e *v1.EntityRef) (*v1.EntityRef, error) {
	var q string
	want := 0
	switch e.GetType() {
	case v1.EntityType_ENTITY_TYPE_SYSTEM:
		return &v1.EntityRef{Type: e.GetType()}, nil
	case v1.EntityType_ENTITY_TYPE_SERVER:
		q, want = `SELECT count(*) FROM servers WHERE server_name = ?`, 1
	case v1.EntityType_ENTITY_TYPE_STREAM:
		q, want = `SELECT count(*) FROM streams WHERE account = ? AND stream = ?`, 2
	case v1.EntityType_ENTITY_TYPE_CONSUMER:
		q, want = `SELECT count(*) FROM consumers WHERE account = ? AND stream = ? AND consumer = ?`, 3
	}
	args := entityArgs(e)
	var n int
	if len(args) == want && want > 0 {
		if err := p.db().QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			return nil, err
		}
	}
	if n == 0 {
		return nil, fmt.Errorf("%s %q: %w", e.GetType(), e.GetKey(), api.ErrNotFound)
	}
	return refOf(e.GetType(), args), nil
}

func refOf(t v1.EntityType, parts []any) *v1.EntityRef {
	s := make([]string, len(parts))
	for i, x := range parts {
		s[i] = x.(string)
	}
	r := &v1.EntityRef{Type: t, Key: strings.Join(s, "/")}
	switch t {
	case v1.EntityType_ENTITY_TYPE_SERVER:
		r.Name, r.Server = s[0], s[0]
	case v1.EntityType_ENTITY_TYPE_STREAM:
		r.Account, r.Stream, r.Name = s[0], s[1], s[1]
	case v1.EntityType_ENTITY_TYPE_CONSUMER:
		r.Account, r.Stream, r.Consumer, r.Name = s[0], s[1], s[2], s[2]
	}
	return r
}

// series reads a metric in buckets of the step for the range. Buckets are
// aligned to the Unix epoch, so the first may begin before the range and
// hold only its part of it. A bucket's time is its start; buckets without
// epochs have no point.
func (p *Provider) series(ctx context.Context, metric string, e *v1.EntityRef, r *v1.TimeRange, maxPoints int32) (*v1.Series, error) {
	if col, ok := bandMetrics[metric]; ok {
		return p.bandSeries(ctx, metric, col, e, r, maxPoints)
	}
	m, ok := metricQueries[metric]
	if !ok {
		return nil, fmt.Errorf("%w: metric %q has no query", api.ErrInvalid, metric)
	}
	step := api.Step(r, maxPoints, max(p.Interval, time.Second))
	start, end := span(r)
	agg := "avg(v)"
	// Low and high are NULL unless a count fills them.
	edges := "NULL::DOUBLE, NULL::DOUBLE"
	switch {
	case m.sum:
		agg = "sum(v)"
	case api.Metrics[metric].Unit == v1.Unit_UNIT_COUNT:
		// A count stays whole: a bucket shows its last value, as a tile
		// does, never an average such as 16.9 connections. Its lowest and
		// highest epoch keep a dip inside the bucket.
		agg = "arg_max(v, t)"
		edges = "min(v)::DOUBLE, max(v)::DOUBLE"
	}
	// Rates and deltas need the epoch before the range's first one.
	args := append([]any{start.Add(-2 * max(p.Interval, time.Minute)), end}, entityArgs(e)...)
	args = append(args, step.Microseconds(), start, end)
	q := `WITH per_epoch AS (` + p.metricQuery(m) + `)
SELECT time_bucket(to_microseconds(?::BIGINT), t, TIMESTAMP '1970-01-01 00:00:00') AS b, ` + agg + `::DOUBLE, ` + edges + `
FROM per_epoch WHERE t >= ? AND t < ? AND v IS NOT NULL GROUP BY b ORDER BY b`
	rows, err := p.db().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("series %s: %w", metric, err)
	}
	defer rows.Close()
	out := &v1.Series{Metric: metric, Entity: e, Unit: api.Metrics[metric].Unit, Step: dur(step)}
	for rows.Next() {
		var b time.Time
		var v float64
		var lo, hi sql.NullFloat64
		if err := rows.Scan(&b, &v, &lo, &hi); err != nil {
			return nil, err
		}
		pt := &v1.Point{Time: ts(b.UTC()), Value: v}
		low, high := v, v
		if lo.Valid && hi.Valid {
			pt.Low, pt.High = ptr(lo.Float64), ptr(hi.Float64)
			// As for a band, min and max take in the bucket's edges.
			low, high = lo.Float64, hi.Float64
		}
		out.Points = append(out.Points, pt)
		if out.Min == nil || low < out.GetMin() {
			out.Min = ptr(low)
		}
		if out.Max == nil || high > out.GetMax() {
			out.Max = ptr(high)
		}
	}
	return out, rows.Err()
}

// GetTimeSeries reads one metric of an entity the database knows.
func (p *Provider) GetTimeSeries(ctx context.Context, req *v1.GetTimeSeriesRequest) (*v1.GetTimeSeriesResponse, error) {
	e, err := p.checkEntity(ctx, req.GetEntity())
	if err != nil {
		return nil, err
	}
	s, err := p.series(ctx, req.GetMetric(), e, req.GetRange(), req.GetMaxPoints())
	if err != nil {
		return nil, err
	}
	return &v1.GetTimeSeriesResponse{Series: s}, nil
}

var errNoRows = sql.ErrNoRows

func isNoRows(err error) bool { return errors.Is(err, errNoRows) }
