package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
)

// replayEvents reads the events Replay tells, from A minus the restart
// window to Until; pod events only of NATS pods. Only a Kubernetes
// event keeps its payload.
func (p *Provider) replayEvents(ctx context.Context, f *api.ReplayFacts, _ map[string]string) error {
	kinds := api.ReplayEventKinds()
	args := []any{f.A.Add(-f.RestartWindow), f.Until}
	for _, k := range kinds {
		args = append(args, k)
	}
	rows, err := p.db().QueryContext(ctx, `SELECT `+eventCols+`, CASE WHEN e.source = 'k8s' THEN e.raw::VARCHAR END FROM events e
WHERE e.time >= ? AND e.time <= ? AND e.kind IN (?`+strings.Repeat(", ?", len(kinds)-1)+`)
  AND (e.kind NOT LIKE 'k8s.pod.%' OR e.server_name IS NOT NULL)
ORDER BY e.time, e.id LIMIT `+fmt.Sprint(replayRowLimit+1), args...)
	if err != nil {
		return fmt.Errorf("replay events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		if len(f.Events) == replayRowLimit {
			f.EventsTruncated = true
			break
		}
		var raw sql.NullString
		ev, err := scanEvent(rows, &raw)
		if err != nil {
			return err
		}
		// A Kubernetes event's payload names its node.
		ev.Raw = raw.String
		f.Events = append(f.Events, ev)
	}
	return rows.Err()
}

// replayTransitions reads the finding transitions from A to Until, and
// for each finding still firing at A the newest before A and the firing
// that started it, so a span that began before A starts at that firing.
func (p *Provider) replayTransitions(ctx context.Context, f *api.ReplayFacts, _ map[string]string) error {
	rows, err := p.db().QueryContext(ctx, `
SELECT finding_id, check_code, entity_type, entity_key, epoch, time, kind, severity, detail FROM (
    SELECT * FROM finding_transitions WHERE time >= ? AND time <= ?
    UNION ALL
    SELECT * EXCLUDE (rn, frn, last_kind) FROM (
        SELECT *, row_number() OVER w AS rn, first_value(kind) OVER w AS last_kind,
            row_number() OVER (PARTITION BY finding_id, kind = 'firing' ORDER BY time DESC, epoch DESC) AS frn
        FROM finding_transitions WHERE time < ?
        WINDOW w AS (PARTITION BY finding_id ORDER BY time DESC, epoch DESC))
    WHERE last_kind <> 'resolved' AND (rn = 1 OR (kind = 'firing' AND frn = 1)))
ORDER BY time, finding_id LIMIT `+fmt.Sprint(replayRowLimit+1), f.A, f.Until, f.A)
	if err != nil {
		return fmt.Errorf("replay transitions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		if len(f.Transitions) == replayRowLimit {
			f.TransitionsTruncated = true
			break
		}
		var t api.ReplayTransition
		var typ, key, kind, sev string
		if err := rows.Scan(&t.FindingID, &t.Check, &typ, &key, &t.Epoch, &t.Time, &kind, &sev, &t.Detail); err != nil {
			return err
		}
		t.Time, t.Entity, t.State, t.Severity = t.Time.UTC(), entityRef(typ, key), transitionState[kind], severityOfText[sev]
		if p.Checks != nil {
			if c, ok := p.Checks.Lookup(t.Check); ok {
				t.CheckName = c.Name
			}
		}
		f.Transitions = append(f.Transitions, t)
	}
	return rows.Err()
}

// replayLeaders reads the leaders and the meta leader at A.
func (p *Provider) replayLeaders(ctx context.Context, f *api.ReplayFacts, names map[string]string) error {
	if f.EpochA == 0 {
		return nil
	}
	var err error
	if f.LeadersAtA, err = p.leaders(ctx, f.EpochA, "", names); err != nil {
		return err
	}
	var meta sql.NullString
	if err := p.db().QueryRowContext(ctx, `SELECT mode(meta_leader) FROM server_stats WHERE epoch = ? AND coalesce(meta_leader, '') <> ''`,
		f.EpochA).Scan(&meta); err != nil {
		return fmt.Errorf("replay meta leader: %w", err)
	}
	f.MetaLeaderAtA = meta.String
	return nil
}

// replayGaps reads the times without collections (no epoch for more than
// ReplayEpochReach intervals, the series rule, leading and trailing gaps
// included) and the collector's gap events that overlap A to B.
func (p *Provider) replayGaps(ctx context.Context, f *api.ReplayFacts, _ map[string]string) error {
	reach := api.ReplayEpochReach * p.Interval
	rows, err := p.db().QueryContext(ctx, `
SELECT epoch, started_at FROM (SELECT epoch, started_at FROM epochs WHERE started_at < ? ORDER BY epoch DESC LIMIT 1)
UNION ALL SELECT epoch, started_at FROM epochs WHERE started_at >= ? AND started_at <= ?
UNION ALL SELECT epoch, started_at FROM (SELECT epoch, started_at FROM epochs WHERE started_at > ? ORDER BY epoch LIMIT 1)
ORDER BY epoch`, f.A.Add(-reach), f.A.Add(-reach), f.B, f.B)
	if err != nil {
		return fmt.Errorf("replay gaps: %w", err)
	}
	type ep struct {
		n  int64
		at time.Time
	}
	var eps []ep
	for rows.Next() {
		var e ep
		if err := rows.Scan(&e.n, &e.at); err != nil {
			rows.Close()
			return err
		}
		e.at = e.at.UTC()
		eps = append(eps, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	f.Gaps = append(f.Gaps, collectionGaps(f.A, f.B, reach, func(yield func(int64, time.Time) bool) {
		for _, e := range eps {
			if !yield(e.n, e.at) {
				return
			}
		}
	})...)
	evs, err := p.db().QueryContext(ctx, `SELECT `+eventCols+`, e.received_at FROM events e
WHERE e.kind IN ('collector.gap', 'collector.k8s_gap') AND e.time <= ? AND e.received_at >= ? ORDER BY e.time, e.id`, f.B, f.A.Add(-f.RestartWindow))
	if err != nil {
		return fmt.Errorf("replay collector gaps: %w", err)
	}
	defer evs.Close()
	for evs.Next() {
		var end time.Time
		ev, err := scanEvent(evs, &end)
		if err != nil {
			return err
		}
		g := api.ReplayGap{Kind: api.GapCollector, Start: ev.GetTime().AsTime(), End: end.UTC(), Event: ev}
		if ev.GetKind() == "collector.k8s_gap" {
			g.Kind = api.GapKubernetes
		}
		f.Gaps = append(f.Gaps, g)
	}
	return evs.Err()
}

// collectionGaps finds the gaps longer than reach between the epochs (in
// order, the last before A minus reach and the first after B included)
// that overlap A to B: before the first epoch from A, between epochs, and
// after the last, open when no epoch followed.
func collectionGaps(a, b time.Time, reach time.Duration, epochs func(func(int64, time.Time) bool)) []api.ReplayGap {
	var out []api.ReplayGap
	var prev int64
	var prevAt time.Time
	for n, at := range epochs {
		from := prevAt
		if prev == 0 {
			from = a
		}
		if at.Sub(from) > reach && at.After(a) && from.Before(b) {
			out = append(out, api.ReplayGap{Kind: api.GapCollections, Start: from, End: at, BeforeEpoch: prev, BeforeAt: prevAt,
				AfterEpoch: n, AfterAt: at})
		}
		prev, prevAt = n, at
	}
	switch {
	case prev == 0:
		out = append(out, api.ReplayGap{Kind: api.GapCollections, Start: a})
	case b.Sub(prevAt) > reach && prevAt.Before(b):
		out = append(out, api.ReplayGap{Kind: api.GapCollections, Start: prevAt, BeforeEpoch: prev, BeforeAt: prevAt})
	}
	return out
}

// replayReconnects counts the client reconnects between A and B as
// client-004 pairs them: per connection name the smaller of the
// disconnects not closed by the client and the connects (a churn event's
// connects included). Disconnects by a server stopping count here.
// Tooling clients are left out by the rule of routineSQL.
func (p *Provider) replayReconnects(ctx context.Context, f *api.ReplayFacts, _ map[string]string) error {
	if !f.EventsCaptured {
		return nil
	}
	var n sql.NullInt64
	err := p.db().QueryRowContext(ctx, `
WITH t AS (
    SELECT e.kind, e.client_name, e.reason, e.raw,
        (list_contains(?::VARCHAR[], coalesce(e.client_name, ''))
         OR (? AND coalesce(e.client_host, '') IN ('127.0.0.1', '::1') AND coalesce(x.loopback_tooling, true))) AS tooling
    FROM events e ASOF LEFT JOIN epochs x ON e.time >= x.started_at
    WHERE e.kind IN ('client.disconnect', 'client.connect', 'client.churn') AND e.time >= ? AND e.time <= ?
),
drops AS (
    SELECT client_name, count(*) AS n FROM t
    WHERE kind = 'client.disconnect' AND coalesce(reason, '') <> 'Client Closed' AND NOT tooling GROUP BY client_name
),
connects AS (
    SELECT client_name, sum(CASE WHEN kind = 'client.churn' THEN coalesce(TRY_CAST(raw->>'connects' AS BIGINT), 0) ELSE 1 END) AS n
    FROM t WHERE kind IN ('client.connect', 'client.churn') AND NOT tooling GROUP BY client_name
)
SELECT sum(least(d.n, c.n)) FROM drops d JOIN connects c ON c.client_name IS NOT DISTINCT FROM d.client_name`,
		append([]string{}, p.ToolingNames...), !p.LoopbackNotTooling, f.A, f.B).Scan(&n)
	if err != nil {
		return fmt.Errorf("replay reconnects: %w", err)
	}
	f.ClientReconnects = int32(n.Int64)
	return nil
}

// replayNodes counts the nodes upgraded and drained between A and B and
// the node pools of the upgraded ones.
func (p *Provider) replayNodes(ctx context.Context, f *api.ReplayFacts, _ map[string]string) error {
	if !p.Kubernetes {
		return nil
	}
	err := p.db().QueryRowContext(ctx, `
SELECT count(DISTINCT raw->>'node') FILTER (WHERE kind = 'k8s.node.upgrade' AND coalesce(raw->>'node', '') <> ''),
    count(DISTINCT raw->>'node') FILTER (WHERE kind = 'k8s.node.drained' AND coalesce(raw->>'node', '') <> ''),
    count(DISTINCT raw->>'pool') FILTER (WHERE kind = 'k8s.node.upgrade' AND coalesce(raw->>'pool', '') <> ''),
    count(*) FILTER (WHERE coalesce(raw->>'node', '') = '') > 0
FROM events WHERE source = 'k8s' AND kind IN ('k8s.node.upgrade', 'k8s.node.drained') AND time >= ? AND time <= ?`,
		f.A, f.B).Scan(&f.Nodes.Upgraded, &f.Nodes.Drained, &f.Nodes.Pools, &f.Nodes.Unnamed)
	if err != nil {
		return fmt.Errorf("replay nodes: %w", err)
	}
	f.Nodes.PoolLabels = f.Nodes.Pools > 0
	return nil
}
