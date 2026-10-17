package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// ResolvedBetween counts the resolve transitions in [from, to), like every
// range: a finding that fired and resolved twice counts twice.
func (s *Store) ResolvedBetween(ctx context.Context, from, to time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM finding_transitions WHERE kind = 'resolved' AND time >= ? AND time < ?`,
		from.UTC(), to.UTC()).Scan(&n)
	return n, err
}

// FindingsAt rebuilds, from the transitions, the findings that fired at
// some time in [from, to) as they stood at to: state, severity, detail,
// value and cause from the newest transition before to, since from the
// start of that episode, resolved_at when that transition resolved it,
// and the flapping count of the hour before to. Related configuration
// comes from the finding's current row. Mutes active at to are applied.
// A finding that fired, resolved and fired again in the range appears
// once, in the state of its newest episode.
func (s *Store) FindingsAt(ctx context.Context, from, to time.Time) ([]Finding, error) {
	rows, err := s.db.QueryContext(ctx, `
WITH before AS (
    SELECT finding_id, arg_max(kind, epoch) AS kind FROM finding_transitions WHERE time < ? GROUP BY finding_id
),
ids AS (
    SELECT DISTINCT finding_id FROM finding_transitions WHERE time >= ? AND time < ?
    UNION
    SELECT finding_id FROM before WHERE kind <> 'resolved'
),
hist AS (
    SELECT t.* FROM finding_transitions t JOIN ids USING (finding_id) WHERE t.time < ?
),
last AS (
    SELECT finding_id, arg_max(struct_pack(check_code, entity_type, entity_key, epoch, time, kind, severity, detail,
        value, cause_event_id), epoch) AS t
    FROM hist GROUP BY finding_id
),
starts AS (
    SELECT h.finding_id, max(h.epoch) AS epoch, max(h.time) AS time
    FROM hist h JOIN last l USING (finding_id)
    WHERE h.kind = 'firing' AND h.epoch <= l.t.epoch GROUP BY h.finding_id
),
hour AS (
    SELECT finding_id, count(*) AS n, count(*) FILTER (WHERE kind = 'resolved') AS cycles
    FROM hist WHERE time > ? GROUP BY finding_id
)
SELECT l.finding_id, l.t.check_code, l.t.entity_type, l.t.entity_key, l.t.severity, l.t.kind, l.t.time, l.t.epoch,
    coalesce(st.time, l.t.time), coalesce(st.epoch, l.t.epoch), l.t.detail, l.t.value, l.t.cause_event_id,
    CAST(f.related_config AS VARCHAR), f.related_source, coalesce(hr.n, 0), coalesce(hr.cycles, 0), f.condition_since
FROM last l
LEFT JOIN starts st USING (finding_id)
LEFT JOIN hour hr USING (finding_id)
LEFT JOIN findings f ON f.id = l.finding_id
ORDER BY l.finding_id`, from.UTC(), from.UTC(), to.UTC(), to.UTC(), to.Add(-FlappingWindow).UTC())
	if err != nil {
		return nil, fmt.Errorf("findings at %s: %w", to.UTC().Format(time.RFC3339), err)
	}
	var out []Finding
	for rows.Next() {
		var f Finding
		var kind string
		var lastAt, since time.Time
		var lastEpoch, sinceEpoch int64
		var value sql.NullFloat64
		var cause sql.NullInt64
		var related, source sql.NullString
		var cycles int
		var began sql.NullTime
		if err := rows.Scan(&f.ID, &f.CheckCode, &f.EntityType, &f.EntityKey, &f.Severity, &kind, &lastAt, &lastEpoch,
			&since, &sinceEpoch, &f.Detail, &value, &cause, &related, &source, &f.TransitionsLastHour, &cycles, &began); err != nil {
			rows.Close()
			return nil, err
		}
		f.LastTransition, f.TransitionedAt, f.LastSeenEpoch = kind, lastAt.UTC(), lastEpoch
		f.Since, f.SinceEpoch = since.UTC(), sinceEpoch
		f.State = StateFiring
		if kind == TransitionResolved {
			f.State, f.ResolvedAt = StateResolved, lastAt.UTC()
		}
		f.Flapping = cycles > FlappingAbove
		if began.Valid {
			// The current value: transitions do not record it.
			b := began.Time.UTC()
			f.ConditionSince = &b
		}
		if value.Valid {
			v := value.Float64
			f.Value = &v
		}
		f.CauseEventID, f.RelatedSource = cause.Int64, source.String
		if related.Valid && related.String != "" {
			if err := json.Unmarshal([]byte(related.String), &f.RelatedConfig); err != nil {
				rows.Close()
				return nil, fmt.Errorf("finding %s related_config: %w", f.ID, err)
			}
		}
		out = append(out, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.applyMutesAt(ctx, to, out)
}

// applyMutesAt sets Mute on every firing finding a mute active at t
// covers, with the rules of applyMutes.
func (s *Store) applyMutesAt(ctx context.Context, t time.Time, fs []Finding) error {
	all, _, err := s.ListMutes(ctx, t, true, 1<<30, 0)
	if err != nil {
		return fmt.Errorf("read mutes: %w", err)
	}
	var active []Mute
	for _, m := range all {
		if m.ActiveAt(t) {
			active = append(active, m)
		}
	}
	for i := range fs {
		fs[i].Mute = muteFor(active, fs[i])
	}
	return nil
}

// ActiveAt reports whether the mute applied at t: created by then, not
// yet expired and not lifted.
func (m Mute) ActiveAt(t time.Time) bool {
	return !m.CreatedAt.After(t) && m.ExpiresAt.After(t) && (m.UnmutedAt.IsZero() || m.UnmutedAt.After(t))
}
