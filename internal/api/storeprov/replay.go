package storeprov

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/checks"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// replayRowLimit bounds the events and transitions one Replay reads; the
// earliest are kept, as the story starts at A, and the facts say when the
// limit cut them. A variable so that tests can lower it.
var replayRowLimit = 20000

// Replay reads the facts of the story between A and B, one query after
// another; the handler has checked and clamped A and B. A moment A older
// than the retention is refused, as Replay reads the database only.
func (p *Provider) Replay(ctx context.Context, req *v1.GetReplayRequest) (*api.ReplayFacts, error) {
	now := p.Now()
	a, b := req.GetA().AsTime().UTC(), req.GetB().AsTime().UTC()
	if p.Retention > 0 && a.Before(now.Add(-p.Retention)) {
		return nil, fmt.Errorf("%w: A is older than retention (%s); Replay reads the database only", api.ErrInvalid, retentionText(p.Retention))
	}
	f := &api.ReplayFacts{A: a, B: b, Until: b.Add(api.ReplayLookahead), Interval: p.Interval, RestartWindow: checks.DefaultRestartWindow,
		Kubernetes: p.Kubernetes}
	if f.Until.After(now) {
		f.Until = now
	}
	if p.Checks != nil {
		f.RestartWindow = p.Checks.RestartWindow()
	}
	names, err := p.AccountNames(ctx)
	if err != nil {
		return nil, err
	}
	steps := []func(context.Context, *api.ReplayFacts, map[string]string) error{
		p.replayEpochs, p.replayRestarts, p.replayStops, p.replayEvents, p.replayTransitions, p.replayLeaders, p.replayGone,
		p.replayGaps, p.replayReconnects, p.replayNodes,
		func(ctx context.Context, f *api.ReplayFacts, _ map[string]string) (err error) {
			f.Windows, err = p.k8sWindows(ctx, &v1.TimeRange{Start: ts(f.A), End: ts(f.B)})
			return err
		},
	}
	for _, step := range steps {
		if err := step(ctx, f, names); err != nil {
			return nil, err
		}
	}
	return f, nil
}

// retentionText writes a retention as days when it is whole days: 14d.
func retentionText(d time.Duration) string {
	if d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	}
	return d.String()
}

// epochAt is the newest epoch that started at or before t, within
// ReplayEpochReach intervals; zero when none.
func (p *Provider) epochAt(ctx context.Context, t time.Time) (int64, time.Time, error) {
	var n int64
	var at time.Time
	err := p.db().QueryRowContext(ctx, `SELECT epoch, started_at FROM epochs WHERE started_at <= ? AND started_at >= ?
ORDER BY epoch DESC LIMIT 1`, t, t.Add(-api.ReplayEpochReach*p.Interval)).Scan(&n, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, time.Time{}, nil
	}
	return n, at.UTC(), err
}

// replayEpochs reads the collections at A and B, the start of the
// history, and whether anything was collected and captured.
func (p *Provider) replayEpochs(ctx context.Context, f *api.ReplayFacts, _ map[string]string) error {
	var err error
	if f.EpochA, f.EpochAAt, err = p.epochAt(ctx, f.A); err != nil {
		return err
	}
	if f.EpochB, f.EpochBAt, err = p.epochAt(ctx, f.B); err != nil {
		return err
	}
	var start sql.NullTime
	if err := p.db().QueryRowContext(ctx, `SELECT min(started_at), count(*) FILTER (WHERE started_at >= ? AND started_at <= ?) > 0,
  (SELECT count(*) > 0 FROM events WHERE time <= ?) FROM epochs`, f.A, f.B, f.B).Scan(&start, &f.Collected, &f.EventsCaptured); err != nil {
		return fmt.Errorf("replay epochs: %w", err)
	}
	f.HistoryStart = ntime(start)
	return nil
}

// replayRestarts reads the processes that replaced an earlier one of the
// same name and started after A and at or before B, as server-004 pairs
// them, with what each process held and the old one led. The new process
// must also have been collected by B: one first collected after B is
// read as a stop that answered again after B (proposed, not yet
// confirmed).
func (p *Provider) replayRestarts(ctx context.Context, f *api.ReplayFacts, names map[string]string) error {
	rows, err := p.db().QueryContext(ctx, `
WITH procs AS (
    SELECT server_id, server_name, version, coalesce(start_time, first_seen_at) AS started, first_epoch, first_seen_at,
        lag(server_id) OVER w AS prev_id, lag(version) OVER w AS prev_version, lag(last_epoch) OVER w AS prev_last,
        lag(last_seen_at) OVER w AS prev_last_seen
    FROM servers
    WINDOW w AS (PARTITION BY server_name ORDER BY first_epoch)
)
SELECT server_name, prev_id, server_id, prev_last_seen, first_seen_at, started, prev_last, first_epoch,
    coalesce(prev_version, ''), coalesce(version, ''),
    EXISTS (SELECT 1 FROM epoch_endpoints e WHERE e.server_id = p.prev_id AND e.epoch = p.prev_last AND e.endpoint = 'JSZ' AND e.state = 'ok'),
    (SELECT count(*) FROM stream_stats x WHERE x.server_id = p.prev_id AND x.epoch = p.prev_last),
    (SELECT count(*) FROM consumer_stats x WHERE x.server_id = p.prev_id AND x.epoch = p.prev_last),
    EXISTS (SELECT 1 FROM epoch_endpoints e WHERE e.server_id = p.server_id AND e.epoch = p.first_epoch AND e.endpoint = 'JSZ' AND e.state = 'ok'),
    (SELECT count(*) FROM stream_stats x WHERE x.server_id = p.server_id AND x.epoch = p.first_epoch),
    (SELECT count(*) FROM consumer_stats x WHERE x.server_id = p.server_id AND x.epoch = p.first_epoch)
FROM procs p
WHERE prev_id IS NOT NULL AND started > ? AND started <= ? AND first_seen_at <= ?
ORDER BY started, server_name`, f.A, f.B, f.B)
	if err != nil {
		return fmt.Errorf("replay restarts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r api.ReplayRestart
		var jsBefore, jsAfter bool
		var sb, cb, sa, ca int32
		if err := rows.Scan(&r.Server, &r.OldID, &r.NewID, &r.LastSeen, &r.FirstSeen, &r.Started, &r.OldEpoch, &r.NewEpoch,
			&r.OldVersion, &r.NewVersion, &jsBefore, &sb, &cb, &jsAfter, &sa, &ca); err != nil {
			return err
		}
		r.LastSeen, r.FirstSeen, r.Started = r.LastSeen.UTC(), r.FirstSeen.UTC(), r.Started.UTC()
		if jsBefore {
			r.StreamsBefore, r.ConsumersBefore = &sb, &cb
		}
		if jsAfter {
			r.StreamsAfter, r.ConsumersAfter = &sa, &ca
		}
		f.Restarts = append(f.Restarts, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range f.Restarts {
		r := &f.Restarts[i]
		led, err := p.leaders(ctx, r.OldEpoch, r.OldID, names)
		if err != nil {
			return err
		}
		for _, l := range led {
			r.LedBefore = append(r.LedBefore, l.Entity)
		}
	}
	return nil
}

// leaders reads the streams and consumers each server led in an epoch,
// one leader per asset (the first by name when two claim it); serverID,
// when set, keeps only that process's.
func (p *Provider) leaders(ctx context.Context, epoch int64, serverID string, names map[string]string) ([]api.ReplayLeader, error) {
	rows, err := p.db().QueryContext(ctx, `
SELECT account, stream, NULL, min(s.server_name) FROM stream_stats x JOIN servers s USING (server_id)
WHERE x.epoch = ? AND x.is_leader AND (? = '' OR x.server_id = ?) GROUP BY ALL
UNION ALL
SELECT account, stream, consumer, min(s.server_name) FROM consumer_stats x JOIN servers s USING (server_id)
WHERE x.epoch = ? AND x.is_leader AND (? = '' OR x.server_id = ?) GROUP BY ALL
ORDER BY 1, 2, 3`, epoch, serverID, serverID, epoch, serverID, serverID)
	if err != nil {
		return nil, fmt.Errorf("replay leaders: %w", err)
	}
	defer rows.Close()
	var out []api.ReplayLeader
	for rows.Next() {
		var acc, stream, server string
		var consumer sql.NullString
		if err := rows.Scan(&acc, &stream, &consumer, &server); err != nil {
			return nil, err
		}
		out = append(out, api.ReplayLeader{Entity: assetRef(acc, stream, consumer.String, names), Server: server})
	}
	return out, rows.Err()
}

// assetRef is a stream's reference, or a consumer's when consumer is set,
// with the account's name when known.
func assetRef(account, stream, consumer string, names map[string]string) *v1.EntityRef {
	ref := refOf(v1.EntityType_ENTITY_TYPE_STREAM, []any{account, stream})
	if consumer != "" {
		ref = refOf(v1.EntityType_ENTITY_TYPE_CONSUMER, []any{account, stream, consumer})
	}
	ref.AccountName = names[account]
	return ref
}

// replayStops reads the servers that answered in a collection after A and
// not in the collection at B, and when one answered again after B.
func (p *Provider) replayStops(ctx context.Context, f *api.ReplayFacts, _ map[string]string) error {
	if f.EpochB == 0 {
		return nil
	}
	rows, err := p.db().QueryContext(ctx, `
WITH seen AS (
    SELECT s.server_name, max(e.epoch) AS last_epoch
    FROM epoch_endpoints e JOIN servers s USING (server_id) JOIN epochs x ON x.epoch = e.epoch
    WHERE x.started_at >= ? AND e.epoch < ?
    GROUP BY s.server_name
),
back AS (
    SELECT s.server_name, min(e.epoch) AS epoch
    FROM epoch_endpoints e JOIN servers s USING (server_id) JOIN epochs x ON x.epoch = e.epoch
    WHERE x.started_at > ? AND x.started_at <= ?
    GROUP BY s.server_name
)
SELECT seen.server_name, seen.last_epoch, x.started_at, back.epoch, bx.started_at
FROM seen JOIN epochs x ON x.epoch = seen.last_epoch
LEFT JOIN back USING (server_name) LEFT JOIN epochs bx ON bx.epoch = back.epoch
WHERE NOT list_contains((SELECT observed_names FROM epochs WHERE epoch = ?), seen.server_name)
ORDER BY x.started_at, seen.server_name`, f.A, f.EpochB, f.B, f.Until, f.EpochB)
	if err != nil {
		return fmt.Errorf("replay stops: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s api.ReplayStop
		var back sql.NullInt64
		var backAt sql.NullTime
		if err := rows.Scan(&s.Server, &s.LastEpoch, &s.LastSeen, &back, &backAt); err != nil {
			return err
		}
		s.LastSeen, s.BackEpoch, s.BackAt = s.LastSeen.UTC(), back.Int64, ntime(backAt)
		f.Stops = append(f.Stops, s)
	}
	return rows.Err()
}
