package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Capacity reads the range's newest epoch: every server's file store with
// its growth over the range, every stream with its configuration, its
// numbers and their growth over the range, what the inventory declares,
// and the findings firing. The number of queries is fixed (a few for the
// epoch, the stores, the streams and their growth, and the findings'),
// none per stream or server.
func (p *Provider) Capacity(ctx context.Context, req *v1.GetCapacityRequest) (*api.CapacityFacts, error) {
	e, ok, err := p.latestEpoch(ctx, req.GetRange())
	if err != nil || !ok {
		return &api.CapacityFacts{}, err
	}
	start, end := span(req.GetRange())
	f := &api.CapacityFacts{Epoch: e.epoch, EpochAt: e.startedAt}
	var from sql.NullTime
	if err := p.db().QueryRowContext(ctx, `SELECT min(started_at) FROM epochs WHERE started_at >= ? AND started_at < ?`, api.GrowthStart(start, end, p.Interval), end).
		Scan(&from); err != nil {
		return nil, fmt.Errorf("capacity growth start: %w", err)
	}
	f.GrowthFrom = ntime(from)
	// Streams first: whether the servers' memory growth is read depends on
	// them (api.MemoryShown).
	if err := p.capacityStreams(ctx, f, start, end); err != nil {
		return nil, err
	}
	if err := p.capacityServers(ctx, f, start, end); err != nil {
		return nil, err
	}
	if err := p.capacityAccounts(ctx, f, start, end); err != nil {
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
        max(st.js_storage) AS used, max(st.js_max_storage) AS lim, max(st.js_memory) AS mem, max(st.js_max_memory) AS mem_lim
    FROM server_stats st JOIN servers s USING (server_id) WHERE st.epoch = $1 GROUP BY s.server_name
), names AS (
    SELECT DISTINCT unnest(expected_names || observed_names || missing_names) AS name FROM epochs WHERE epoch = $1
    UNION SELECT name FROM cur
), known AS (
    SELECT server_name AS name, arg_max(cluster, last_epoch) AS cluster, arg_max(jetstream, last_epoch) AS js
    FROM servers WHERE first_epoch <= $1 GROUP BY server_name
)
SELECT n.name, coalesce(nullif(c.cluster, ''), k.cluster, ''), c.enabled, c.used, c.lim, k.js, c.mem, c.mem_lim
FROM names n LEFT JOIN cur c USING (name) LEFT JOIN known k USING (name)
ORDER BY n.name`, f.Epoch)
	if err != nil {
		return fmt.Errorf("capacity servers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s api.ServerStoreFact
		var enabled, js sql.NullBool
		var used, lim, mem, memLim sql.NullInt64
		if err := rows.Scan(&s.Name, &s.Cluster, &enabled, &used, &lim, &js, &mem, &memLim); err != nil {
			return err
		}
		switch {
		case enabled.Valid && !enabled.Bool, !used.Valid && js.Valid && !js.Bool:
			f.ServersWithoutJetStream++
			continue
		}
		s.Reported, s.Used, s.Limit = used.Valid, int64Ptr(used), int64Ptr(lim)
		s.MemUsed, s.MemLimit = int64Ptr(mem), int64Ptr(memLim)
		f.Servers = append(f.Servers, s)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	// Memory growth is read only when a memory card can be sent.
	kinds := "('file')"
	if api.MemoryShown(f) {
		kinds = "('file'), ('memory')"
	}
	growth, err := p.storeGrowth(ctx, fmt.Sprintf(serverStoreSeries, kinds), start, end)
	if err != nil {
		return fmt.Errorf("capacity server growth: %w", err)
	}
	for i, s := range f.Servers {
		f.Servers[i].PerDay = api.RobustPerDay(growth[s.Name+"/"+api.StorageFile])
		f.Servers[i].MemPerDay = api.RobustPerDay(growth[s.Name+"/"+api.StorageMemory])
	}
	return nil
}

// serverStoreSeries is every server's file and memory store in use in
// each epoch, by server name (so a restart continues the series), keyed
// name/file and name/memory; %s lists the kinds read, ('file') or
// ('file'), ('memory').
const serverStoreSeries = `
  SELECT s.server_name || '/' || k AS name, e.started_at AS t, max(CASE k WHEN 'file' THEN st.js_storage ELSE st.js_memory END) AS v
  FROM server_stats st JOIN servers s USING (server_id) JOIN epochs e USING (epoch), (VALUES %s) kinds(k)
  WHERE e.started_at >= ? AND e.started_at < ?
  GROUP BY 1, e.epoch, e.started_at`

// accountStoreSeries is the JetStream file and memory use of the accounts
// listed by %s (one placeholder per account), in each epoch, the largest
// any server reports, keyed account/file and account/memory.
const accountStoreSeries = `
  SELECT a.account || '/' || k AS name, e.started_at AS t, max(CASE k WHEN 'file' THEN a.js_storage ELSE a.js_memory END) AS v
  FROM account_stats a JOIN epochs e USING (epoch), (VALUES ('file'), ('memory')) kinds(k)
  WHERE e.started_at >= ? AND e.started_at < ? AND a.account IN (%s)
  GROUP BY 1, e.epoch, e.started_at`

// storeGrowth reads a series of the range (series selects name, t and v,
// with the range's start and end as its first two parameters, then
// args), reduced in SQL to at most api.GrowthBuckets+1 aligned medians
// per name.
func (p *Provider) storeGrowth(ctx context.Context, series string, start, end time.Time, args ...any) (map[string][]api.Sample, error) {
	start = api.GrowthStart(start, end, p.Interval)
	width := p.growthWidth(start, end)
	rows, err := p.db().QueryContext(ctx, `
WITH per AS (`+series+`)
SELECT name, min(t) + (max(t) - min(t)) / 2, median(v)::DOUBLE, min(t), max(t)
FROM per WHERE v IS NOT NULL
GROUP BY name, time_bucket(to_microseconds(?::BIGINT), t, TIMESTAMP '1970-01-01 00:00:00')
ORDER BY 1, 2`, append(append([]any{start, end}, args...), width.Microseconds())...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]api.Sample{}
	for rows.Next() {
		var name string
		var sm api.Sample
		if err := rows.Scan(&name, &sm.T, &sm.V, &sm.From, &sm.To); err != nil {
			return nil, err
		}
		out[name] = append(out[name], sm)
	}
	return out, rows.Err()
}

// capacityStreams reads the streams of the epoch, as ListStreams lists
// them and with its numbers: the leader's report, else the largest any
// server reports (the earliest first message), so both pages agree; with a
// leader, its first message even when it has none. The
// configuration is that of the server stream-005 reads (the leader, else
// the first by name); what the inventory declares follows stream-005's
// rules; growth of bytes and messages is over the range.
func (p *Provider) capacityStreams(ctx context.Context, f *api.CapacityFacts, start, end time.Time) error {
	rows, err := p.db().QueryContext(ctx, `
WITH st AS (
    SELECT account, stream,
        coalesce(max(messages) FILTER (WHERE is_leader), max(messages)) AS messages,
        coalesce(max(bytes) FILTER (WHERE is_leader), max(bytes)) AS bytes,
        -- The leader's first message and subjects whenever a leader row
        -- exists (an empty stream's leader has none), else the earliest
        -- and the most any server reports.
        CASE WHEN bool_or(is_leader) THEN max(first_ts) FILTER (WHERE is_leader) ELSE min(first_ts) END AS first_ts,
        CASE WHEN bool_or(is_leader) THEN max(num_subjects) FILTER (WHERE is_leader) ELSE max(num_subjects) END AS num_subjects
    FROM stream_stats WHERE epoch = $1 GROUP BY account, stream
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

// capacityAccounts reads the accounts the epoch lists that have a JWT
// with a JetStream limit above 0, as the Accounts page reads them: the
// newest lookup up to the epoch read at most api.AccountDetailsMaxAge
// before it, and the largest use any server reports in the epoch; with
// the growth of that use over the range.
func (p *Provider) capacityAccounts(ctx context.Context, f *api.CapacityFacts, start, end time.Time) error {
	rows, err := p.db().QueryContext(ctx, `
WITH js AS (
    SELECT account, max(js_storage) AS js_storage, max(js_memory) AS js_memory
    FROM account_stats WHERE epoch = $1 GROUP BY account
), d AS (
    SELECT * FROM account_details WHERE epoch <= $1 AND looked_up_at >= $2
    QUALIFY row_number() OVER (PARTITION BY account ORDER BY epoch DESC, looked_up_at DESC) = 1
)
SELECT a.account, d.looked_up_at, d.limit_js_disk, d.limit_js_memory, js.js_storage, js.js_memory
FROM accounts a JOIN d USING (account) LEFT JOIN js USING (account)
WHERE a.first_epoch <= $1 AND a.last_epoch >= $1 AND d.has_jwt AND (d.limit_js_disk > 0 OR d.limit_js_memory > 0)
ORDER BY a.account`, f.Epoch, f.EpochAt.Add(-api.AccountDetailsMaxAge))
	if err != nil {
		return fmt.Errorf("capacity accounts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var a api.AccountStoreFact
		d := &api.AccountDetails{HasJWT: true}
		var disk, mem, used, usedMem sql.NullInt64
		if err := rows.Scan(&a.Account, &d.At, &disk, &mem, &used, &usedMem); err != nil {
			return err
		}
		d.At = d.At.UTC()
		d.LimitJSDisk, d.LimitJSMemory = int64Ptr(disk), int64Ptr(mem)
		a.Details, a.JSStorage, a.JSMemory = d, int64Ptr(used), int64Ptr(usedMem)
		f.Accounts = append(f.Accounts, a)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(f.Accounts) == 0 {
		return nil
	}
	marks, keys := make([]string, len(f.Accounts)), make([]any, len(f.Accounts))
	for i, a := range f.Accounts {
		marks[i], keys[i] = "?", a.Account
	}
	growth, err := p.storeGrowth(ctx, fmt.Sprintf(accountStoreSeries, strings.Join(marks, ", ")), start, end, keys...)
	if err != nil {
		return fmt.Errorf("capacity account growth: %w", err)
	}
	for i, a := range f.Accounts {
		f.Accounts[i].StoragePerDay = api.RobustPerDay(growth[a.Account+"/"+api.StorageFile])
		f.Accounts[i].MemoryPerDay = api.RobustPerDay(growth[a.Account+"/"+api.StorageMemory])
	}
	return nil
}
