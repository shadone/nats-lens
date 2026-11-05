package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/shadone/nats-lens/internal/api"
)

// replayEvents reads the events Replay tells, from ReplayReadFrom to
// Until; pod events only of NATS pods. Only a Kubernetes
// event keeps its payload.
func (p *Provider) replayEvents(ctx context.Context, f *api.ReplayFacts, _ map[string]string) error {
	kinds := api.ReplayEventKinds()
	f.ReadFrom = api.ReplayReadFrom(f)
	args := []any{f.ReadFrom, f.Until}
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
			f.EventsTruncated, f.EventsReadUntil = true, f.Events[len(f.Events)-1].GetTime().AsTime()
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
			f.TransitionsTruncated, f.TransitionsReadUntil = true, f.Transitions[len(f.Transitions)-1].Time
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

// replayGaps reads the times without collections that overlap A to B,
// and the collector's gap events and the lost events connections from
// ReplayReadFrom to B.
func (p *Provider) replayGaps(ctx context.Context, f *api.ReplayFacts, _ map[string]string) error {
	from := api.ReplayReadFrom(f)
	for _, read := range []func() ([]api.ReplayGap, error){
		func() ([]api.ReplayGap, error) { return p.collectionGaps(ctx, f.A, f.B) },
		func() ([]api.ReplayGap, error) { return p.collectorGaps(ctx, from, f.B) },
		func() ([]api.ReplayGap, error) { return p.eventsConnectionGaps(ctx, from, f.B) },
	} {
		gs, err := read()
		if err != nil {
			return err
		}
		f.Gaps = append(f.Gaps, gs...)
	}
	return nil
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
	tooling, args := p.eventToolingSQL("e.client_name", "e.client_host")
	err := p.db().QueryRowContext(ctx, `
WITH t AS (
    SELECT e.kind, e.client_name, e.reason, e.raw, `+tooling+` AS tooling
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
		append(args, f.A, f.B)...).Scan(&n)
	if err != nil {
		return fmt.Errorf("replay reconnects: %w", err)
	}
	f.ClientReconnects = int32(n.Int64)
	return nil
}

// nodeOfEvent is the node a Kubernetes event names, its raw column read
// as raw (a table alias and a dot, or nothing): its node, else the Node
// its Event is about, as Events of a node stored before the watcher named
// the node at the top of the payload give it.
func nodeOfEvent(alias string) string {
	raw := alias + "raw"
	return `coalesce(` + raw + `->>'node', CASE WHEN ` + raw + `->'object'->>'kind' = 'Node' THEN ` + raw + `->'object'->>'name' END)`
}

// replayNodes counts the nodes upgraded and drained between A and B and
// the node pools of the upgraded ones.
func (p *Provider) replayNodes(ctx context.Context, f *api.ReplayFacts, _ map[string]string) error {
	if !p.Kubernetes {
		return nil
	}
	var labelled int32
	err := p.db().QueryRowContext(ctx, `
WITH n AS (
    SELECT kind, coalesce(`+nodeOfEvent("")+`, '') AS node, coalesce(raw->>'pool', '') AS pool
    FROM events WHERE source = 'k8s' AND kind IN ('k8s.node.upgrade', 'k8s.node.drained') AND time >= ? AND time <= ?
)
SELECT count(DISTINCT node) FILTER (WHERE kind = 'k8s.node.upgrade' AND node <> ''),
    count(DISTINCT node) FILTER (WHERE kind = 'k8s.node.drained' AND node <> ''),
    count(DISTINCT pool) FILTER (WHERE kind = 'k8s.node.upgrade' AND pool <> ''),
    count(*) FILTER (WHERE node = '') > 0,
    count(DISTINCT node) FILTER (WHERE kind = 'k8s.node.upgrade' AND node <> '' AND pool <> '')
FROM n`,
		f.A, f.B).Scan(&f.Nodes.Upgraded, &f.Nodes.Drained, &f.Nodes.Pools, &f.Nodes.Unnamed, &labelled)
	if err != nil {
		return fmt.Errorf("replay nodes: %w", err)
	}
	// Every upgraded node's pool is known: a node Event stored before the
	// watcher named the pool counts as a node without one.
	// Without an upgraded node the pools upgraded are 0, observed.
	f.Nodes.PoolLabels = labelled == f.Nodes.Upgraded
	return nil
}
