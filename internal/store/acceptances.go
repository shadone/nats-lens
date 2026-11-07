package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// EntityKey names an entity as the mutes table does.
type EntityKey struct {
	Type, Key string
}

// GrowthPoint is one bucket median of a growing value.
type GrowthPoint struct {
	T time.Time
	V float64
}

// Span is a time span (From, To].
type Span struct{ From, To time.Time }

// GrowthPoints reads, in one query, the bytes of each stream and server
// entity in ents over the epochs that started in any of spans:
// a stream's leader bytes (any replica's when no leader reported), a
// server's JetStream storage (js_storage, by server name, so a restart
// continues the series). Each series is reduced to the medians of buckets
// width wide, as the Streams page reduces it, ready for a growth rate.
// Other entity types and entities without data get no series.
func (s *Store) GrowthPoints(ctx context.Context, spans []Span, width time.Duration, ents []EntityKey) (map[EntityKey][]GrowthPoint, error) {
	var streams, servers []string
	for _, e := range ents {
		switch e.Type {
		case "stream":
			streams = append(streams, sqlString(e.Key))
		case "server":
			servers = append(servers, sqlString(e.Key))
		}
	}
	out := map[EntityKey][]GrowthPoint{}
	if (len(streams) == 0 && len(servers) == 0) || len(spans) == 0 {
		return out, nil
	}
	var within []string
	var spanArgs []any
	for _, sp := range spans {
		within = append(within, "(e.started_at > ? AND e.started_at <= ?)")
		spanArgs = append(spanArgs, sp.From.UTC(), sp.To.UTC())
	}
	when := "(" + strings.Join(within, " OR ") + ")"
	args := append(append(append([]any{}, spanArgs...), spanArgs...), max(width, time.Second).Microseconds())
	in := func(keys []string) string {
		if len(keys) == 0 {
			return "(NULL)"
		}
		return "(" + strings.Join(keys, ", ") + ")"
	}
	rows, err := s.db.QueryContext(ctx, `
WITH per AS (
  SELECT 'stream' AS entity_type, x.account || '/' || x.stream AS entity_key, e.started_at AS t,
    coalesce(max(x.bytes) FILTER (WHERE x.is_leader), max(x.bytes)) AS v
  FROM stream_stats x JOIN epochs e USING (epoch)
  WHERE `+when+` AND x.account || '/' || x.stream IN `+in(streams)+`
  GROUP BY x.account, x.stream, e.epoch, e.started_at
  UNION ALL
  SELECT 'server', s.server_name, e.started_at, max(st.js_storage)
  FROM server_stats st JOIN servers s USING (server_id) JOIN epochs e USING (epoch)
  WHERE `+when+` AND s.server_name IN `+in(servers)+`
  GROUP BY s.server_name, e.epoch, e.started_at)
SELECT entity_type, entity_key, min(t) + (max(t) - min(t)) / 2, median(v)::DOUBLE
FROM per WHERE v IS NOT NULL
GROUP BY entity_type, entity_key, time_bucket(to_microseconds(?::BIGINT), t, TIMESTAMP '1970-01-01 00:00:00')
ORDER BY 1, 2, 3`, args...)
	if err != nil {
		return nil, fmt.Errorf("growth points: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k EntityKey
		var p GrowthPoint
		if err := rows.Scan(&k.Type, &k.Key, &p.T, &p.V); err != nil {
			return nil, err
		}
		p.T = p.T.UTC()
		out[k] = append(out[k], p)
	}
	return out, rows.Err()
}

// AcceptancesToReview returns the acceptances of streams and servers in
// force at t and created at or before it: those the growth review
// compares, or gives a baseline when they recorded no growth.
func (s *Store) AcceptancesToReview(ctx context.Context, t time.Time) ([]Mute, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+muteCols+` FROM mutes
WHERE accepted AND entity_type IN ('stream', 'server')
  AND unmuted_at IS NULL AND created_at <= ? AND expires_at > ? ORDER BY id`, t.UTC(), t.UTC())
	if err != nil {
		return nil, fmt.Errorf("acceptances to review: %w", err)
	}
	defer rows.Close()
	var out []Mute
	for rows.Next() {
		m, err := scanMute(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetAcceptanceBaseline records growth as the growth of an acceptance that
// recorded none, while it still applies at t. It reports whether it set
// it; a row a person lifted meanwhile, one that has a growth, or a plain
// mute is left as it is.
func (s *Store) SetAcceptanceBaseline(ctx context.Context, id int64, t time.Time, growth float64) (bool, error) {
	if err := s.lock(ctx); err != nil {
		return false, err
	}
	defer s.mu.Unlock()
	res, err := s.db.ExecContext(ctx, `UPDATE mutes SET growth_per_day = ?
WHERE id = ? AND accepted AND growth_per_day IS NULL AND unmuted_at IS NULL AND expires_at > ?`, growth, id, t.UTC())
	if err != nil {
		return false, fmt.Errorf("set acceptance baseline %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// EndAcceptance ends an acceptance that still applies at t: unmuted_at t,
// unmuted_by by, end_reason reason. It reports whether it ended one; a
// row a person lifted meanwhile, or a plain mute, is left as it is.
func (s *Store) EndAcceptance(ctx context.Context, id int64, t time.Time, by, reason string) (bool, error) {
	if err := s.lock(ctx); err != nil {
		return false, err
	}
	defer s.mu.Unlock()
	res, err := s.db.ExecContext(ctx, `UPDATE mutes SET unmuted_at = ?, unmuted_by = ?, end_reason = ?
WHERE id = ? AND accepted AND unmuted_at IS NULL AND expires_at > ?`, t.UTC(), by, reason, id, t.UTC())
	if err != nil {
		return false, fmt.Errorf("end acceptance %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// EpochStartedAt returns the time an epoch started.
func (s *Store) EpochStartedAt(ctx context.Context, epoch int64) (time.Time, error) {
	var at time.Time
	if err := s.db.QueryRowContext(ctx, `SELECT started_at FROM epochs WHERE epoch = ?`, epoch).Scan(&at); err != nil {
		return time.Time{}, fmt.Errorf("epoch %d: %w", epoch, err)
	}
	return at.UTC(), nil
}
