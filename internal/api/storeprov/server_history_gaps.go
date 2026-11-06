package storeprov

import (
	"context"
	"database/sql"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Proposed, not yet confirmed: a recorded missing report describes only the
// collection's interval, capped at the next collection. Sparse collection never
// turns server absence into a global outage or an unbounded server-down claim.
func (p *Provider) serverHistoryGaps(ctx context.Context, name string, start, end time.Time) ([]*v1.ServerHistoryGap, error) {
	interval := max(p.Interval, time.Second)
	// ASVS 1.2.4: server names and bounds are parameters, never SQL fragments.
	// Compare timestamp columns directly; no host-zone shift of parameter arithmetic.
	rows, err := p.db().QueryContext(ctx, `
WITH ep AS (
 SELECT epoch, started_at, missing_names, lead(started_at) OVER (ORDER BY started_at, epoch) AS next_at
 FROM epochs WHERE started_at >= ? AND started_at < ?
), reports AS (
 SELECT x.epoch, count(DISTINCT x.endpoint) AS attempted, bool_or(x.state = 'ok') AS answered
 FROM epoch_endpoints x JOIN servers s USING (server_id) JOIN ep e ON e.epoch = x.epoch
 WHERE s.server_name = ? AND x.endpoint IN ('STATSZ', 'VARZ')
 GROUP BY x.epoch
)
SELECT e.started_at, e.next_at,
 CASE WHEN list_contains(e.missing_names, ?) THEN 'Server did not answer'
      ELSE 'No STATSZ or VARZ metrics report (missing or error)' END
FROM ep e LEFT JOIN reports r USING (epoch)
WHERE list_contains(e.missing_names, ?) OR (r.attempted >= 2 AND NOT r.answered)
ORDER BY e.started_at`, start.Add(-interval), end, name, name, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*v1.ServerHistoryGap
	for rows.Next() {
		var at time.Time
		var next sql.NullTime
		var reason string
		if err := rows.Scan(&at, &next, &reason); err != nil {
			return nil, err
		}
		to := minHistoryTime(at.Add(interval), end)
		if next.Valid {
			to = minHistoryTime(to, next.Time)
		}
		from := at
		if from.Before(start) {
			from = start
		}
		if !to.After(from) {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Reason == reason && out[n-1].End.AsTime().Equal(from) {
			out[n-1].End = ts(to)
		} else {
			out = append(out, &v1.ServerHistoryGap{Start: ts(from), End: ts(to), Reason: reason})
		}
	}
	return out, rows.Err()
}

func minHistoryTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
