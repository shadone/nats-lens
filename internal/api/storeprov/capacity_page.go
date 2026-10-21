package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Capacity reads the range's newest epoch: every server's file store with
// its growth over the range, every stream with its configuration, the
// leader's numbers and their growth over the range, what the inventory
// declares, and the findings firing. Five queries, whatever the number of
// streams.
func (p *Provider) Capacity(ctx context.Context, req *v1.GetCapacityRequest) (*api.CapacityFacts, error) {
	e, ok, err := p.latestEpoch(ctx, req.GetRange())
	if err != nil || !ok {
		return &api.CapacityFacts{}, err
	}
	start, end := span(req.GetRange())
	f := &api.CapacityFacts{Epoch: e.epoch, EpochAt: e.startedAt}
	var from sql.NullTime
	if err := p.db().QueryRowContext(ctx, `SELECT min(started_at) FROM epochs WHERE started_at >= ? AND started_at < ?`, start, end).
		Scan(&from); err != nil {
		return nil, fmt.Errorf("capacity growth start: %w", err)
	}
	f.GrowthFrom = ntime(from)
	if err := p.capacityServers(ctx, f, start, end); err != nil {
		return nil, err
	}
	if err := p.capacityStreams(ctx, f, start, end); err != nil {
		return nil, err
	}
	if f.Findings, err = p.firingFindings(ctx, req.GetRange()); err != nil {
		return nil, err
	}
	return f, nil
}

// capacityServers reads the file store of every server the epoch named
// (expected, observed or missed) or that reported in it, by name. A server
// whose JetStream numbers the epoch lacks is not reported; it gets a card
// unless it answered with JetStream disabled or the servers table says it
// runs without JetStream.
func (p *Provider) capacityServers(ctx context.Context, f *api.CapacityFacts, start, end time.Time) error {
	rows, err := p.db().QueryContext(ctx, `
WITH cur AS (
    SELECT s.server_name AS name, any_value(s.cluster) AS cluster, bool_or(st.js_enabled) AS enabled,
        max(st.js_storage) AS used, max(st.js_max_storage) AS lim
    FROM server_stats st JOIN servers s USING (server_id) WHERE st.epoch = $1 GROUP BY s.server_name
), names AS (
    SELECT DISTINCT unnest(expected_names || observed_names || missing_names) AS name FROM epochs WHERE epoch = $1
    UNION SELECT name FROM cur
), known AS (
    SELECT server_name AS name, arg_max(cluster, last_epoch) AS cluster, arg_max(jetstream, last_epoch) AS js
    FROM servers WHERE first_epoch <= $1 GROUP BY server_name
)
SELECT n.name, coalesce(nullif(c.cluster, ''), k.cluster, ''), c.enabled, c.used, c.lim, k.js
FROM names n LEFT JOIN cur c USING (name) LEFT JOIN known k USING (name)
ORDER BY n.name`, f.Epoch)
	if err != nil {
		return fmt.Errorf("capacity servers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s api.ServerStoreFact
		var enabled, js sql.NullBool
		var used, lim sql.NullInt64
		if err := rows.Scan(&s.Name, &s.Cluster, &enabled, &used, &lim, &js); err != nil {
			return err
		}
		switch {
		case enabled.Valid && !enabled.Bool, !used.Valid && js.Valid && !js.Bool:
			f.ServersWithoutJetStream++
			continue
		}
		s.Reported, s.Used, s.Limit = used.Valid, int64Ptr(used), int64Ptr(lim)
		f.Servers = append(f.Servers, s)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	growth, err := p.serverStoreGrowth(ctx, start, end)
	if err != nil {
		return err
	}
	for i, s := range f.Servers {
		f.Servers[i].PerDay = api.RobustPerDay(growth[s.Name])
	}
	return nil
}

// serverStoreGrowth reads every server's file store in use over the range,
// by server name (so a restart continues the series), reduced in SQL to at
// most api.GrowthBuckets medians per server.
func (p *Provider) serverStoreGrowth(ctx context.Context, start, end time.Time) (map[string][]api.Sample, error) {
	width := max(end.Sub(start)/api.GrowthBuckets, max(p.Interval, time.Second))
	rows, err := p.db().QueryContext(ctx, `
WITH per AS (
  SELECT s.server_name AS name, e.started_at AS t, max(st.js_storage) AS v
  FROM server_stats st JOIN servers s USING (server_id) JOIN epochs e USING (epoch)
  WHERE e.started_at >= ? AND e.started_at < ?
  GROUP BY s.server_name, e.epoch, e.started_at)
SELECT name, min(t) + (max(t) - min(t)) / 2, median(v)::DOUBLE
FROM per WHERE v IS NOT NULL
GROUP BY name, time_bucket(to_microseconds(?::BIGINT), t, TIMESTAMP '1970-01-01 00:00:00')
ORDER BY 1, 2`, start, end, width.Microseconds())
	if err != nil {
		return nil, fmt.Errorf("capacity server growth: %w", err)
	}
	defer rows.Close()
	out := map[string][]api.Sample{}
	for rows.Next() {
		var name string
		var sm api.Sample
		if err := rows.Scan(&name, &sm.T, &sm.V); err != nil {
			return nil, err
		}
		out[name] = append(out[name], sm)
	}
	return out, rows.Err()
}

// capacityStreams reads the streams of the epoch, as ListStreams lists
// them: the numbers of the leader's report (else the first reporting
// server's by name), the configuration of the server stream-005 reads
// (the leader, else the first by name), what the inventory declares by
// stream-005's rules, and the growth of bytes and messages over the range.
func (p *Provider) capacityStreams(ctx context.Context, f *api.CapacityFacts, start, end time.Time) error {
	rows, err := p.db().QueryContext(ctx, `
WITH st AS (
    SELECT x.account, x.stream, x.messages, x.bytes, x.first_ts, x.num_subjects
    FROM stream_stats x JOIN servers s USING (server_id)
    WHERE x.epoch = $1
    QUALIFY row_number() OVER (PARTITION BY x.account, x.stream ORDER BY coalesce(x.is_leader, false) DESC, s.server_name) = 1
), cf AS (
    SELECT c.account, c.stream, c.retention, c.storage, c.replicas, c.mirror, len(coalesce(c.sources, [])) AS sources,
        c.max_msgs, c.max_bytes, c.max_age_ns, c.max_msgs_per_subject, c.discard, coalesce(c.allow_msg_ttl, false) AS allow_msg_ttl
    FROM stream_config c JOIN servers s USING (server_id)
    LEFT JOIN stream_stats ss
      ON ss.epoch = $1 AND ss.server_id = c.server_id AND ss.account = c.account AND ss.stream = c.stream
    WHERE c.first_epoch <= $1 AND c.last_epoch >= $1
    QUALIFY row_number() OVER (PARTITION BY c.account, c.stream ORDER BY coalesce(ss.is_leader, false) DESC, s.server_name) = 1
), inv AS (
    SELECT coalesce((SELECT a.account FROM accounts a WHERE i.account IN (a.account, a.account_name)
                     ORDER BY a.account = i.account DESC LIMIT 1), i.account) AS account, i.stream,
        bool_or(i.max_age_ns = 0 AND i.max_bytes = -1) AS unbounded, bool_or(i.kind = 'kv') AS listed_kv
    FROM inventory i WHERE i.kind IN ('stream', 'kv', 'object_store')
    GROUP BY ALL
)
SELECT st.account, st.stream, coalesce(cf.retention, ''), coalesce(cf.storage, ''), coalesce(cf.replicas, 0),
    coalesce(cf.mirror, ''), coalesce(cf.sources, 0), coalesce(cf.discard, ''), coalesce(cf.allow_msg_ttl, false),
    st.messages, st.bytes, st.num_subjects, st.first_ts, cf.max_msgs, cf.max_bytes, cf.max_age_ns, cf.max_msgs_per_subject,
    CASE WHEN coalesce(inv.unbounded, false) THEN 'inventory'
         WHEN st.stream LIKE 'KV\_%' ESCAPE '\' AND coalesce(cf.allow_msg_ttl, false) AND coalesce(inv.listed_kv, false) THEN 'writer_ttls'
         ELSE '' END
FROM st LEFT JOIN cf USING (account, stream) LEFT JOIN inv USING (account, stream)
ORDER BY st.account, st.stream`, f.Epoch)
	if err != nil {
		return fmt.Errorf("capacity streams: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s api.CapacityStreamFact
		var msgs, bytes, keys, maxMsgs, maxBytes, maxAge, perSubject sql.NullInt64
		var first sql.NullTime
		if err := rows.Scan(&s.Account, &s.Stream, &s.Retention, &s.Storage, &s.Replicas, &s.Mirror, &s.Sources, &s.Discard,
			&s.AllowMsgTTL, &msgs, &bytes, &keys, &first, &maxMsgs, &maxBytes, &maxAge, &perSubject, &s.Declared); err != nil {
			return err
		}
		s.Messages, s.Bytes, s.Keys = int64Ptr(msgs), int64Ptr(bytes), int64Ptr(keys)
		s.MaxMsgs, s.MaxBytes, s.MaxMsgsPerSubject = int64Ptr(maxMsgs), int64Ptr(maxBytes), int64Ptr(perSubject)
		if maxAge.Valid {
			s.MaxAge = ptr(time.Duration(maxAge.Int64))
		}
		// The writer stores a stream without messages with no first time.
		if first.Valid && first.Time.Unix() > 0 {
			s.FirstTS = ptr(first.Time.UTC())
		}
		f.Streams = append(f.Streams, s)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	pts, err := p.streamPoints(ctx, start, end, "", "")
	if err != nil {
		return err
	}
	for i, s := range f.Streams {
		g := pts[s.Account+"/"+s.Stream]
		f.Streams[i].BytesPerDay, f.Streams[i].MsgsPerDay = api.RobustPerDay(g.bytes), api.RobustPerDay(g.msgs)
	}
	return nil
}
