package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// serverSummaries reads every server name seen in the range, with its
// newest process and that process's numbers at the newest epoch it
// answered in, up to the range's newest epoch. name limits the result to
// one server when set.
func (p *Provider) serverSummaries(ctx context.Context, r *v1.TimeRange, e epochInfo, name string) ([]*v1.ServerSummary, error) {
	start, end := span(r)
	rows, err := p.db().QueryContext(ctx, `
WITH procs AS (
  SELECT *, row_number() OVER (PARTITION BY server_name ORDER BY last_epoch DESC) AS rn
  FROM servers WHERE last_seen_at >= ? AND first_seen_at < ? AND (? = '' OR server_name = ?)),
latest AS (
  SELECT ss.*, row_number() OVER (PARTITION BY ss.server_id ORDER BY ss.epoch DESC) AS rn
  FROM server_stats ss WHERE ss.epoch <= ? AND ss.server_id IN (SELECT server_id FROM procs WHERE rn = 1)),
-- The slow consumer counter's increase per process inside the range, as
-- the cluster.slow_consumers series counts it; a restart starts a new count.
slow AS (
  SELECT s.server_name, coalesce(sum(d) FILTER (WHERE d >= 0), 0) AS inc, count(sc) AS n FROM (
    SELECT ss.server_id, ss.slow_consumers AS sc,
      ss.slow_consumers - lag(ss.slow_consumers) OVER (PARTITION BY ss.server_id ORDER BY ss.epoch) AS d
    FROM server_stats ss JOIN epochs e USING (epoch)
    WHERE e.started_at >= ? AND e.started_at < ? AND ss.epoch <= ?) x
  JOIN servers s USING (server_id) WHERE (? = '' OR s.server_name = ?) GROUP BY s.server_name)
SELECT p.server_name, p.server_id, coalesce(l.version, p.version), p.cluster, coalesce(l.start_time, p.start_time),
  l.epoch = ?, l.js_enabled, l.meta_leader, h.status, h.error,
  l.connections, l.js_storage, l.js_max_storage, l.mem_bytes, l.cpu_percent,
  coalesce((SELECT list(start_time ORDER BY start_time) FROM servers o
     WHERE o.server_name = p.server_name AND o.start_time >= ? AND o.start_time < ?), []),
  le.started_at, l.js_streams_leader, l.js_consumers_leader, coalesce(p.tags, []),
  CASE WHEN sl.n > 0 THEN sl.inc END
FROM procs p
LEFT JOIN latest l ON l.server_id = p.server_id AND l.rn = 1
LEFT JOIN epochs le ON le.epoch = l.epoch
LEFT JOIN health h ON h.server_id = l.server_id AND h.epoch = l.epoch
LEFT JOIN slow sl ON sl.server_name = p.server_name
WHERE p.rn = 1 ORDER BY p.server_name`, start, end, name, name, e.epoch, start, end, e.epoch, name, name, e.epoch, start, end)
	if err != nil {
		return nil, fmt.Errorf("servers: %w", err)
	}
	defer rows.Close()
	var out []*v1.ServerSummary
	for rows.Next() {
		var s v1.ServerSummary
		var version, cluster, leader, health, healthErr sql.NullString
		var started, seenAt sql.NullTime
		var present, js sql.NullBool
		var restarts, tags []any
		if err := rows.Scan(&s.Name, &s.ServerId, &version, &cluster, &started, &present, &js, &leader, &health, &healthErr,
			&s.Connections, &s.JsStorageBytes, &s.JsMaxStorageBytes, &s.MemBytes, &s.CpuPercent, &restarts, &seenAt,
			&s.StreamLeaders, &s.ConsumerLeaders, &tags, &s.SlowConsumersInRange); err != nil {
			return nil, err
		}
		for _, x := range tags {
			if tag, ok := x.(string); ok {
				s.Tags = append(s.Tags, tag)
			}
		}
		s.Version, s.Cluster, s.Healthz, s.HealthzError = nstr(version), nstr(cluster), nstr(health), nstr(healthErr)
		s.Present = present.Bool
		// Uptime as of the newest epoch the server answered in, not the
		// range's end: a server that stopped answering has no uptime now.
		if st, at := ntime(started), ntime(seenAt); !st.IsZero() && !at.IsZero() {
			s.StartTime, s.Uptime = ts(st), dur(at.Sub(st).Truncate(time.Second))
		}
		switch {
		case js.Valid && !js.Bool:
			s.Role = v1.ServerRole_SERVER_ROLE_NO_JETSTREAM
		case leader.String == s.GetName():
			s.Role = v1.ServerRole_SERVER_ROLE_META_LEADER
		case leader.Valid:
			s.Role = v1.ServerRole_SERVER_ROLE_FOLLOWER
		}
		for _, x := range restarts {
			if t, ok := x.(time.Time); ok {
				s.Restarts = append(s.Restarts, ts(t.UTC()))
			}
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

func (p *Provider) listServers(ctx context.Context, r *v1.TimeRange) (*v1.ListServersResponse, error) {
	e, ok, err := p.latestEpoch(ctx, r)
	if err != nil || !ok {
		return &v1.ListServersResponse{}, err
	}
	servers, err := p.serverSummaries(ctx, r, e, "")
	if err != nil {
		return nil, err
	}
	cur, err := p.firingFindings(ctx, r)
	if err != nil {
		return nil, err
	}
	ran, err := p.checksRan(ctx, e.epoch)
	if err != nil {
		return nil, err
	}
	applyServerStatus(servers, cur, ran)
	return &v1.ListServersResponse{Servers: servers, Expected: e.expected, Missing: e.missing}, nil
}

// ListServers reads every server seen in the range.
func (p *Provider) ListServers(ctx context.Context, req *v1.ListServersRequest) (*v1.ListServersResponse, error) {
	return p.listServers(ctx, req.GetRange())
}

// GetServer reads one server with its endpoints and process lifetimes.
func (p *Provider) GetServer(ctx context.Context, req *v1.GetServerRequest) (*v1.GetServerResponse, error) {
	e, ok, err := p.latestEpoch(ctx, req.GetRange())
	if err != nil {
		return nil, err
	}
	var list []*v1.ServerSummary
	if ok {
		if list, err = p.serverSummaries(ctx, req.GetRange(), e, req.GetName()); err != nil {
			return nil, err
		}
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("server %q in range: %w", req.GetName(), api.ErrNotFound)
	}
	cur, err := p.firingFindings(ctx, req.GetRange())
	if err != nil {
		return nil, err
	}
	ran, err := p.checksRan(ctx, e.epoch)
	if err != nil {
		return nil, err
	}
	applyServerStatus(list, cur, ran)
	res := &v1.GetServerResponse{Server: list[0]}
	id := list[0].GetServerId()
	var digest, leader sql.NullString
	err = p.db().QueryRowContext(ctx, `
SELECT subscriptions, slow_consumers, routes, js_streams, js_consumers, meta_leader, config_digest
FROM server_stats WHERE server_id = ? AND epoch <= ? ORDER BY epoch DESC LIMIT 1`, id, e.epoch).
		Scan(&res.Subscriptions, &res.SlowConsumers, &res.Routes, &res.JsStreams, &res.JsConsumers, &leader, &digest)
	if err != nil && !isNoRows(err) {
		return nil, err
	}
	res.MetaLeader, res.ConfigDigest = nstr(leader), nstr(digest)
	if res.Endpoints, err = p.endpoints(ctx, id, e.epoch); err != nil {
		return nil, err
	}
	if err = p.serverConnections(ctx, res, id, e.epoch); err != nil {
		return nil, err
	}
	limits, err := p.allServerLimits(ctx, req.GetRange(), e)
	if err != nil {
		return nil, err
	}
	res.Limits, res.LimitsDiffer = limits[req.GetName()].proto(), limitsDiffer(limits, req.GetName())
	if res.Kubernetes, err = p.kubernetesPlacement(ctx, req.GetName(), req.GetRange()); err != nil {
		return nil, err
	}
	res.Processes, err = p.processes(ctx, req.GetName(), req.GetRange())
	return res, err
}

func (p *Provider) endpoints(ctx context.Context, serverID string, epoch int64) ([]*v1.EndpointOutcome, error) {
	rows, err := p.db().QueryContext(ctx, `SELECT endpoint, state, error FROM epoch_endpoints WHERE server_id = ? AND epoch = ? ORDER BY endpoint`, serverID, epoch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*v1.EndpointOutcome
	for rows.Next() {
		var o v1.EndpointOutcome
		var msg sql.NullString
		if err := rows.Scan(&o.Endpoint, &o.State, &msg); err != nil {
			return nil, err
		}
		o.Error = msg.String
		out = append(out, &o)
	}
	return out, rows.Err()
}

func (p *Provider) processes(ctx context.Context, name string, r *v1.TimeRange) ([]*v1.ServerProcess, error) {
	start, end := span(r)
	rows, err := p.db().QueryContext(ctx, `
SELECT server_id, start_time, first_seen_at, last_seen_at FROM servers
WHERE server_name = ? AND last_seen_at >= ? AND first_seen_at < ? ORDER BY first_epoch`, name, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*v1.ServerProcess
	for rows.Next() {
		var sp v1.ServerProcess
		var st sql.NullTime
		var first, last time.Time
		if err := rows.Scan(&sp.ServerId, &st, &first, &last); err != nil {
			return nil, err
		}
		sp.StartTime, sp.FirstSeen, sp.LastSeen = ts(ntime(st)), ts(first.UTC()), ts(last.UTC())
		out = append(out, &sp)
	}
	return out, rows.Err()
}
