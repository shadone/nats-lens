package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Applications reads the facts of the Applications page: the range's
// newest epoch's connections with the epoch's tooling rule, whether a
// server's list was cut at the connz limit, the client events of the
// api.ApplicationDay before the epoch counted by name, account and the
// tooling rule of each event's epoch, whether event capture stopped in
// that day (captureGaps), the hourly traffic of api.TrafficHours, and the
// findings firing at the epoch. The traffic's loopback rows count as
// tooling by the page's epoch's loopback decision, for every hour. A
// request's account is applied in SQL to the connections and the traffic
// as well as in api.FinishApplications; the events are read for every
// account, as a name's drops and connects pair across accounts. The
// number of queries is fixed.
func (p *Provider) Applications(ctx context.Context, req *v1.GetApplicationsRequest) (*api.ApplicationFacts, error) {
	e, ok, err := p.latestEpoch(ctx, req.GetRange())
	if err != nil || !ok {
		return &api.ApplicationFacts{}, err
	}
	f := &api.ApplicationFacts{Epoch: e.epoch, EpochAt: e.startedAt}
	if f.Missing, err = p.connzMissing(ctx, e); err != nil {
		return nil, err
	}
	rule, err := p.toolingAt(ctx, e.epoch)
	if err != nil {
		return nil, err
	}
	acc := req.GetAccount()
	if f.Connections, err = p.appConnections(ctx, e.epoch, rule, acc); err != nil {
		return nil, err
	}
	if err := p.db().QueryRowContext(ctx, `
SELECT EXISTS (SELECT 1 FROM server_stats WHERE epoch = $1 AND connz_total IS NOT NULL AND connz_returned < connz_total)`,
		e.epoch).Scan(&f.ConnzCut); err != nil {
		return nil, fmt.Errorf("applications epoch: %w", err)
	}
	// Whether events were captured, and stopped for part of the day, by the
	// Accounts and auth page's rule.
	capture := &api.AccountsFacts{EpochAt: e.startedAt, WindowStart: e.startedAt.Add(-api.ApplicationDay)}
	if err := p.captureGaps(ctx, capture); err != nil {
		return nil, err
	}
	f.EventsCaptured, f.CaptureGaps = capture.EventsCaptured, capture.CaptureGaps
	if f.Events, err = p.appEvents(ctx, e.startedAt); err != nil {
		return nil, err
	}
	if f.Traffic, err = p.appTraffic(ctx, e.startedAt, rule, acc); err != nil {
		return nil, err
	}
	if f.Findings, err = p.firingFindings(ctx, req.GetRange()); err != nil {
		return nil, err
	}
	if p.ClientPodWatch {
		f.ClientPodsWatched = true
		if err := p.appPods(ctx, e, acc, f.Connections); err != nil {
			return nil, err
		}
		if f.ClientPodsUnwatched, err = p.clientPodsUnwatched(ctx, e.startedAt); err != nil {
			return nil, err
		}
	}
	return f, nil
}

// appPods matches the epoch's connections in the scope to the client
// pods whose address their IP was when the epoch started: one pod that
// is not on the host network is the connection's pod; a host-network pod
// or more than one pod make it ambiguous.
func (p *Provider) appPods(ctx context.Context, e epochInfo, account string, conns []api.AppConnectionFact) error {
	rows, err := p.db().QueryContext(ctx, `
SELECT s.server_name, c.cid, count(DISTINCT cp.uid), bool_or(cp.host_network),
  any_value(cp.namespace), any_value(cp.pod), any_value(cp.node), any_value(cp.workload_kind), any_value(cp.workload)
FROM connections c JOIN servers s USING (server_id)
JOIN client_pods cp ON cp.ip = c.ip AND cp.first_seen <= `+usParam+` AND (cp.ended_at IS NULL OR cp.ended_at >= `+usParam+`)
WHERE c.epoch = ? AND `+accountIn("c.account")+`
GROUP BY ALL`, us(e.startedAt), us(e.startedAt), e.epoch, account, account)
	if err != nil {
		return fmt.Errorf("application pods: %w", err)
	}
	defer rows.Close()
	type key struct {
		server string
		cid    int64
	}
	at := map[key]int{}
	for i, c := range conns {
		at[key{c.Server, c.CID}] = i
	}
	for rows.Next() {
		var k key
		var n int
		var host bool
		var pod api.ClientPodFact
		if err := rows.Scan(&k.server, &k.cid, &n, &host, &pod.Namespace, &pod.Name, &pod.Node, &pod.WorkloadKind, &pod.Workload); err != nil {
			return err
		}
		i, ok := at[k]
		if !ok {
			continue
		}
		if n > 1 || host {
			conns[i].PodAmbiguous = true
			continue
		}
		conns[i].Pod = &pod
	}
	return rows.Err()
}

// clientPodsUnwatched are the client pod watches failing at at: those
// whose latest collector.k8s_not_watching received up to at was followed
// by no collector.k8s_gap of the same watch and no collector.started.
// Order is by when they were received, as a gap's time is its start.
func (p *Provider) clientPodsUnwatched(ctx context.Context, at time.Time) ([]*v1.ClientPodWatchGap, error) {
	rows, err := p.db().QueryContext(ctx, `
WITH s AS (
  SELECT e.id, e.kind, e.time, e.received_at, coalesce(json_extract_string(e.raw, '$.source'), '') AS src,
    coalesce(json_extract_string(e.raw, '$.error'), '') AS error
  FROM events e
  WHERE e.source = 'collector' AND e.kind IN ('collector.started', 'collector.k8s_gap', 'collector.k8s_not_watching') AND e.received_at <= ?
)
SELECT n.src, n.time, n.error FROM s n
WHERE n.kind = 'collector.k8s_not_watching' AND n.src LIKE 'client pods %'
  AND NOT EXISTS (SELECT 1 FROM s r WHERE r.received_at > n.received_at
    AND (r.kind = 'collector.started' OR (r.kind = 'collector.k8s_gap' AND r.src = n.src)))
QUALIFY row_number() OVER (PARTITION BY n.src ORDER BY n.received_at DESC, n.id DESC) = 1
ORDER BY n.src`, at.UTC())
	if err != nil {
		return nil, fmt.Errorf("client pod watches: %w", err)
	}
	defer rows.Close()
	var out []*v1.ClientPodWatchGap
	for rows.Next() {
		var g v1.ClientPodWatchGap
		var since time.Time
		if err := rows.Scan(&g.Source, &since, &g.Error); err != nil {
			return nil, err
		}
		g.Since = timestamppb.New(since.UTC())
		out = append(out, &g)
	}
	return out, rows.Err()
}

// connzMissing are the servers whose connections the epoch does not
// know: those whose CONNZ request did not end ok (epoch_endpoints), and
// those that answered nothing (epochs.missing_names). A failure of another
// endpoint leaves the connections whole.
func (p *Provider) connzMissing(ctx context.Context, e epochInfo) ([]string, error) {
	rows, err := p.db().QueryContext(ctx, `
SELECT DISTINCT s.server_name FROM epoch_endpoints x JOIN servers s USING (server_id)
WHERE x.epoch = ? AND x.endpoint = 'CONNZ' AND x.state <> 'ok'`, e.epoch)
	if err != nil {
		return nil, fmt.Errorf("applications connz: %w", err)
	}
	defer rows.Close()
	out := append([]string{}, e.missing...)
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// accountIn is the condition "the account column col is in the scope"
// (empty for every account), an empty account read as
// api.GlobalAccount; its two placeholders both take the scope.
func accountIn(col string) string {
	return `(? = '' OR coalesce(nullif(` + col + `, ''), '` + api.GlobalAccount + `') = ?)`
}

// usParam is a TIMESTAMP placeholder given by us, as tsParam for
// queries with ? placeholders.
const usParam = `make_timestamp(?::BIGINT)`

// appConnections reads the epoch's connections in the scope with their
// server names and the tooling rule applied.
func (p *Provider) appConnections(ctx context.Context, epoch int64, rule toolingRule, account string) ([]api.AppConnectionFact, error) {
	rows, err := p.db().QueryContext(ctx, `
SELECT s.server_name, c.cid, coalesce(c.name, ''), coalesce(c.account, ''), coalesce(c.ip, ''), coalesce(c.lang, ''),
  coalesce(c.version, ''), c.rtt_ns, c.subscriptions, c.pending_bytes
FROM connections c JOIN servers s USING (server_id)
WHERE c.epoch = ? AND `+accountIn("c.account")+`
ORDER BY s.server_name, c.cid`, epoch, account, account)
	if err != nil {
		return nil, fmt.Errorf("application connections: %w", err)
	}
	defer rows.Close()
	var out []api.AppConnectionFact
	for rows.Next() {
		var c api.AppConnectionFact
		var rtt, subs, pending sql.NullInt64
		if err := rows.Scan(&c.Server, &c.CID, &c.Name, &c.Account, &c.IP, &c.Lang, &c.Version, &rtt, &subs, &pending); err != nil {
			return nil, err
		}
		if rtt.Valid {
			d := time.Duration(rtt.Int64)
			c.RTT = &d
		}
		if subs.Valid {
			c.Subscriptions = &subs.Int64
		}
		if pending.Valid {
			c.PendingBytes = &pending.Int64
		}
		c.Tooling = rule.is(c.Name, c.IP)
		out = append(out, c)
	}
	return out, rows.Err()
}

// appEvents counts the client events of the api.ApplicationDay before
// at by name, account and tooling, as api.CountClientEvent does: one
// event's tooling by its own epoch's rule (eventToolingSQL). It reads
// every account whatever the page's scope: a name's drops and connects
// pair across accounts, as client-004 pairs them.
func (p *Provider) appEvents(ctx context.Context, at time.Time) ([]api.AppEventFact, error) {
	tooling, targs := p.eventToolingSQL("e.client_name", "e.client_host")
	args := append(targs, us(at.Add(-api.ApplicationDay)), us(at))
	rows, err := p.db().QueryContext(ctx, `
WITH t AS (
  SELECT e.kind, coalesce(e.client_name, '') AS name, coalesce(e.account, '') AS account, coalesce(e.reason, '') AS reason,
    CASE WHEN e.kind = 'client.churn' THEN coalesce(TRY_CAST(e.raw->>'connects' AS BIGINT), 0) ELSE 0 END AS churn,
    `+tooling+` AS tooling
  FROM events e ASOF LEFT JOIN epochs x ON e.time >= x.started_at
  WHERE e.kind IN ('client.disconnect', 'client.connect', 'client.churn')
    AND e.time > `+usParam+` AND e.time <= `+usParam+`
)
SELECT name, account, tooling,
  count(*) FILTER (WHERE kind = 'client.disconnect' AND reason NOT IN ('Client Closed', 'Server Shutdown')),
  (count(*) FILTER (WHERE kind = 'client.connect') + coalesce(sum(churn), 0))::BIGINT,
  count(*) FILTER (WHERE kind = 'client.disconnect' AND reason ILIKE '%slow consumer%'),
  count(*) FILTER (WHERE kind = 'client.disconnect' AND reason = '`+api.StaleReason+`'),
  coalesce(sum(churn), 0)::BIGINT
FROM t GROUP BY ALL ORDER BY ALL`, args...)
	if err != nil {
		return nil, fmt.Errorf("application events: %w", err)
	}
	defer rows.Close()
	var out []api.AppEventFact
	for rows.Next() {
		var e api.AppEventFact
		if err := rows.Scan(&e.Name, &e.Account, &e.Tooling, &e.Drops, &e.Connects, &e.Slow, &e.Stale, &e.ShortLived); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// appTraffic reads the hourly rollup of api.TrafficHours for the epoch
// at at: a row is tooling when its application is a tooling name, or its
// connections came from a loopback address while the epoch's rule holds.
func (p *Provider) appTraffic(ctx context.Context, at time.Time, rule toolingRule, account string) ([]api.AppTrafficFact, error) {
	hours := api.TrafficHours(at)
	rows, err := p.db().QueryContext(ctx, `
SELECT app, account, list_contains(?::VARCHAR[], app) OR (? AND loopback), hour,
  sum(in_msgs)::BIGINT, sum(out_msgs)::BIGINT, sum(in_bytes)::BIGINT, sum(out_bytes)::BIGINT
FROM client_app_hours
WHERE hour >= `+usParam+` AND hour < `+usParam+` AND `+accountIn("account")+`
GROUP BY ALL ORDER BY ALL`, rule.names, rule.loopback, us(hours[0]), us(hours[len(hours)-1].Add(time.Hour)), account, account)
	if err != nil {
		return nil, fmt.Errorf("application traffic: %w", err)
	}
	defer rows.Close()
	var out []api.AppTrafficFact
	for rows.Next() {
		var t api.AppTrafficFact
		if err := rows.Scan(&t.App, &t.Account, &t.Tooling, &t.Hour, &t.InMsgs, &t.OutMsgs, &t.InBytes, &t.OutBytes); err != nil {
			return nil, err
		}
		t.Hour = t.Hour.UTC()
		out = append(out, t)
	}
	return out, rows.Err()
}
