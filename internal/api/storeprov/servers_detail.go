package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// serverLimits are one server's limits and settings at the newest epoch
// it answered in.
type serverLimits struct {
	payload, pending, deadlineNS, pingNS, jsStore, jsMem sql.NullInt64
	pingsOut, maxConns                                   sql.NullInt32
}

// fields lists the limits by their ServerLimits field name, in field
// order; a value is nil when not reported.
func (l serverLimits) fields() []struct {
	name string
	v    *int64
} {
	n64 := func(v sql.NullInt64) *int64 {
		if !v.Valid {
			return nil
		}
		return &v.Int64
	}
	n32 := func(v sql.NullInt32) *int64 {
		if !v.Valid {
			return nil
		}
		x := int64(v.Int32)
		return &x
	}
	return []struct {
		name string
		v    *int64
	}{
		{"max_payload", n64(l.payload)}, {"max_pending", n64(l.pending)}, {"write_deadline", n64(l.deadlineNS)},
		{"ping_interval", n64(l.pingNS)}, {"max_pings_out", n32(l.pingsOut)}, {"js_max_storage", n64(l.jsStore)},
		{"js_max_memory", n64(l.jsMem)}, {"max_connections", n32(l.maxConns)},
	}
}

// proto returns the limits as the API message, nil when none is reported.
func (l serverLimits) proto() *v1.ServerLimits {
	out := &v1.ServerLimits{}
	set := false
	if l.payload.Valid {
		out.MaxPayload, set = &l.payload.Int64, true
	}
	if l.pending.Valid {
		out.MaxPending, set = &l.pending.Int64, true
	}
	if l.deadlineNS.Valid {
		out.WriteDeadline, set = dur(time.Duration(l.deadlineNS.Int64)), true
	}
	if l.pingNS.Valid {
		out.PingInterval, set = dur(time.Duration(l.pingNS.Int64)), true
	}
	if l.pingsOut.Valid {
		out.MaxPingsOut, set = &l.pingsOut.Int32, true
	}
	if l.jsStore.Valid {
		out.JsMaxStorage, set = &l.jsStore.Int64, true
	}
	if l.jsMem.Valid {
		out.JsMaxMemory, set = &l.jsMem.Int64, true
	}
	if l.maxConns.Valid {
		out.MaxConnections, set = &l.maxConns.Int32, true
	}
	if !set {
		return nil
	}
	return out
}

// allServerLimits reads the limits of every server seen in the range, by
// name: each limit's newest reported value in the range, up to the range's
// newest epoch. An epoch whose VARZ or JSZ failed leaves its limits NULL
// and does not hide the value reported before it.
func (p *Provider) allServerLimits(ctx context.Context, r *v1.TimeRange, e epochInfo) (map[string]serverLimits, error) {
	start, end := span(r)
	rows, err := p.db().QueryContext(ctx, `
WITH names AS (SELECT DISTINCT server_name FROM servers WHERE last_seen_at >= ? AND first_seen_at < ?)
SELECT s.server_name,
  arg_max(l.max_payload, l.epoch) FILTER (WHERE l.max_payload IS NOT NULL),
  arg_max(l.max_pending, l.epoch) FILTER (WHERE l.max_pending IS NOT NULL),
  arg_max(l.write_deadline_ns, l.epoch) FILTER (WHERE l.write_deadline_ns IS NOT NULL),
  arg_max(l.ping_interval_ns, l.epoch) FILTER (WHERE l.ping_interval_ns IS NOT NULL),
  arg_max(l.max_pings_out, l.epoch) FILTER (WHERE l.max_pings_out IS NOT NULL),
  arg_max(l.js_max_storage, l.epoch) FILTER (WHERE l.js_max_storage IS NOT NULL),
  arg_max(l.js_max_memory, l.epoch) FILTER (WHERE l.js_max_memory IS NOT NULL),
  arg_max(l.max_connections, l.epoch) FILTER (WHERE l.max_connections IS NOT NULL)
FROM server_stats l JOIN servers s USING (server_id) JOIN epochs ep ON ep.epoch = l.epoch
WHERE s.server_name IN (SELECT server_name FROM names) AND ep.started_at >= ? AND l.epoch <= ?
GROUP BY s.server_name`, start, end, start, e.epoch)
	if err != nil {
		return nil, fmt.Errorf("server limits: %w", err)
	}
	defer rows.Close()
	out := map[string]serverLimits{}
	for rows.Next() {
		var name string
		var l serverLimits
		if err := rows.Scan(&name, &l.payload, &l.pending, &l.deadlineNS, &l.pingNS, &l.pingsOut, &l.jsStore, &l.jsMem, &l.maxConns); err != nil {
			return nil, err
		}
		out[name] = l
	}
	return out, rows.Err()
}

// limitsDiffer names the limits of server name whose value differs from
// the plurality of the servers that report the limit (api.OffPlurality).
func limitsDiffer(all map[string]serverLimits, name string) []string {
	own, ok := all[name]
	if !ok {
		return nil
	}
	var out []string
	for i, f := range own.fields() {
		values := make([]*int64, 0, len(all))
		for _, l := range all {
			values = append(values, l.fields()[i].v)
		}
		if api.OffPlurality(f.v, values) {
			out = append(out, f.name)
		}
	}
	return out
}

// serverConnections fills the route views, gateway and leaf node counts
// and the configuration load time from the server's newest row up to
// epoch.
func (p *Provider) serverConnections(ctx context.Context, res *v1.GetServerResponse, serverID string, epoch int64) error {
	var at sql.NullInt64
	var loaded sql.NullTime
	err := p.db().QueryRowContext(ctx, `
SELECT epoch, leafnodes, config_load_time FROM server_stats WHERE server_id = ? AND epoch <= ? ORDER BY epoch DESC LIMIT 1`,
		serverID, epoch).Scan(&at, &res.Leafnodes, &loaded)
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	res.ConfigLoadedAt = ts(ntime(loaded))
	// The routes of the newest epoch whose ROUTEZ answered: an epoch
	// without that reply has no route rows, not zero routes.
	rows, err := p.db().QueryContext(ctx, `
SELECT remote_name, rtt_ns, pending_bytes FROM routes
WHERE server_id = ? AND epoch = (SELECT max(epoch) FROM epoch_endpoints
  WHERE server_id = ? AND epoch <= ? AND endpoint = 'ROUTEZ' AND state = 'ok')
ORDER BY remote_name, rid`, serverID, serverID, epoch)
	if err != nil {
		return fmt.Errorf("routes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rv v1.RouteView
		var remote sql.NullString
		var rtt sql.NullInt64
		if err := rows.Scan(&remote, &rtt, &rv.PendingBytes); err != nil {
			return err
		}
		rv.RemoteName = nstr(remote)
		if rtt.Valid {
			rv.Rtt = dur(time.Duration(rtt.Int64))
		}
		res.RouteViews = append(res.RouteViews, &rv)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	// Remote clusters connected in either direction; unset unless GATEWAYZ
	// answered in that epoch.
	return p.db().QueryRowContext(ctx, `
SELECT CASE WHEN EXISTS (SELECT 1 FROM epoch_endpoints WHERE server_id = ? AND epoch = ? AND endpoint = 'GATEWAYZ' AND state = 'ok')
  THEN (SELECT count(DISTINCT gateway) FROM gateways WHERE server_id = ? AND epoch = ?) END`,
		serverID, at.Int64, serverID, at.Int64).Scan(&res.Gateways)
}

// kubernetesPlacement reads where the server's pod runs from the newest
// Kubernetes event that names its pod, up to the range's end. It is nil
// when no such event is stored, as when the watcher is off.
func (p *Provider) kubernetesPlacement(ctx context.Context, name string, r *v1.TimeRange) (*v1.KubernetesPlacement, error) {
	start, end := span(r)
	var pod, ns, node, kind sql.NullString
	err := p.db().QueryRowContext(ctx, `
SELECT json_extract_string(raw, '$.pod'), json_extract_string(raw, '$.namespace'), json_extract_string(raw, '$.node'), kind
FROM events
WHERE source = 'k8s' AND server_name = ? AND time < ? AND json_extract_string(raw, '$.pod') IS NOT NULL
ORDER BY time DESC, id DESC LIMIT 1`, name, end).Scan(&pod, &ns, &node, &kind)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("kubernetes placement: %w", err)
	}
	k := &v1.KubernetesPlacement{Pod: nstr(pod), Namespace: nstr(ns), Node: nstr(node)}
	if kind.String == "k8s.pod.deleted" {
		// The pod is gone and runs on no node.
		k.Node = ""
	}
	var scheduled sql.NullTime
	err = p.db().QueryRowContext(ctx, `
SELECT max(time) FILTER (WHERE kind = 'k8s.pod.scheduled' AND json_extract_string(raw, '$.pod') = ?
    AND json_extract_string(raw, '$.namespace') IS NOT DISTINCT FROM ?),
  count(*) FILTER (WHERE kind = 'k8s.pod.restarted' AND time >= ?)
FROM events WHERE source = 'k8s' AND server_name = ? AND time < ?`, k.Pod, ns, start, name, end).Scan(&scheduled, &k.RestartsInRange)
	if err != nil {
		return nil, fmt.Errorf("kubernetes placement: %w", err)
	}
	k.ScheduledAt = ts(ntime(scheduled))
	return k, nil
}
