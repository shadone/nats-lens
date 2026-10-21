package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Finding is one row of the findings table with the mute that applies.
type Finding struct {
	ID         string
	CheckCode  string
	EntityType string
	EntityKey  string
	Severity   string
	// State is StateFiring or StateResolved.
	State string
	// LastTransition is the kind of the latest transition.
	LastTransition string
	Since          time.Time
	SinceEpoch     int64
	TransitionedAt time.Time
	// ResolvedAt is zero while firing.
	ResolvedAt    time.Time
	LastSeenEpoch int64
	Detail        string
	Value         *float64
	// CauseEventID is 0 when no cause is known.
	CauseEventID        int64
	RelatedConfig       map[string]string
	RelatedSource       string
	TransitionsLastHour int
	Flapping            bool
	// ConditionSince is when the condition began, when the check knows it.
	ConditionSince *time.Time
	// Mute is the active mute that covers the finding at the time of the
	// read; nil when none does. Resolved findings carry none.
	Mute *Mute
}

// Firing reports whether the finding fires.
func (f Finding) Firing() bool { return f.State == StateFiring }

// FindingTransition is one row of finding_transitions.
type FindingTransition struct {
	FindingID    string
	CheckCode    string
	EntityType   string
	EntityKey    string
	Epoch        int64
	Time         time.Time
	Kind         string
	Severity     string
	Detail       string
	Value        *float64
	CauseEventID int64
}

// FindingQuery selects findings. Zero fields do not filter.
type FindingQuery struct {
	// From and To select findings that fired at some time in [From, To):
	// started before To and not resolved before From.
	From, To time.Time
	// FiringOnly leaves out resolved findings.
	FiringOnly bool
	CheckCode  string
	// IDs selects these findings; CauseEventIDs the findings caused by
	// these events. Either one set limits the result to its matches.
	IDs           []string
	CauseEventIDs []int64
}

// ErrFindingNotFound is returned for an unknown finding ID.
var ErrFindingNotFound = errors.New("finding not found")

const findingCols = `id, check_code, entity_type, entity_key, severity, state, last_transition, since, since_epoch,
transitioned_at, resolved_at, last_seen_epoch, detail, value, cause_event_id, CAST(related_config AS VARCHAR), related_source,
transitions_last_hour, flapping, condition_since`

func scanFinding(row interface{ Scan(...any) error }) (Finding, error) {
	var f Finding
	var resolved sql.NullTime
	var value sql.NullFloat64
	var cause sql.NullInt64
	var related, source sql.NullString
	var began sql.NullTime
	if err := row.Scan(&f.ID, &f.CheckCode, &f.EntityType, &f.EntityKey, &f.Severity, &f.State, &f.LastTransition,
		&f.Since, &f.SinceEpoch, &f.TransitionedAt, &resolved, &f.LastSeenEpoch, &f.Detail, &value, &cause,
		&related, &source, &f.TransitionsLastHour, &f.Flapping, &began); err != nil {
		return Finding{}, err
	}
	if began.Valid {
		b := began.Time.UTC()
		f.ConditionSince = &b
	}
	f.Since, f.TransitionedAt = f.Since.UTC(), f.TransitionedAt.UTC()
	if resolved.Valid {
		f.ResolvedAt = resolved.Time.UTC()
	}
	if value.Valid {
		v := value.Float64
		f.Value = &v
	}
	f.CauseEventID = cause.Int64
	f.RelatedSource = source.String
	if related.Valid && related.String != "" {
		if err := json.Unmarshal([]byte(related.String), &f.RelatedConfig); err != nil {
			return Finding{}, fmt.Errorf("finding %s related_config: %w", f.ID, err)
		}
	}
	return f, nil
}

// Findings returns the findings that match q, with the mutes active at now
// applied, ordered by ID. Sorting for display and paging are the caller's.
func (s *Store) Findings(ctx context.Context, now time.Time, q FindingQuery) ([]Finding, error) {
	var where []string
	var args []any
	if !q.To.IsZero() {
		where = append(where, `since < ?`)
		args = append(args, q.To.UTC())
	}
	if !q.From.IsZero() {
		where = append(where, `(resolved_at IS NULL OR resolved_at >= ?)`)
		args = append(args, q.From.UTC())
	}
	if q.FiringOnly {
		where = append(where, `state = 'firing'`)
	}
	if q.CheckCode != "" {
		where = append(where, `check_code = ?`)
		args = append(args, q.CheckCode)
	}
	if q.IDs != nil || q.CauseEventIDs != nil {
		var or []string
		if len(q.IDs) > 0 {
			or = append(or, `id IN (SELECT unnest(from_json(?::VARCHAR, '["VARCHAR"]')))`)
			args = append(args, jsonList(q.IDs))
		}
		if len(q.CauseEventIDs) > 0 {
			or = append(or, `cause_event_id IN (SELECT unnest(from_json(?::VARCHAR, '["BIGINT"]')))`)
			args = append(args, jsonList(q.CauseEventIDs))
		}
		if len(or) == 0 {
			return nil, nil
		}
		where = append(where, `(`+strings.Join(or, ` OR `)+`)`)
	}
	query := `SELECT ` + findingCols + ` FROM findings`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, ` AND `)
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("read findings: %w", err)
	}
	var out []Finding
	for rows.Next() {
		f, err := scanFinding(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.applyMutes(ctx, now, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Finding returns one finding by ID with the mute active at now.
func (s *Store) Finding(ctx context.Context, id string, now time.Time) (Finding, error) {
	f, err := scanFinding(s.db.QueryRowContext(ctx, `SELECT `+findingCols+` FROM findings WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Finding{}, ErrFindingNotFound
	}
	if err != nil {
		return Finding{}, err
	}
	one := []Finding{f}
	if err := s.applyMutes(ctx, now, one); err != nil {
		return Finding{}, err
	}
	return one[0], nil
}

// applyMutes sets Mute on every firing finding an active mute covers (see
// muteFor for which one).
func (s *Store) applyMutes(ctx context.Context, now time.Time, fs []Finding) error {
	mutes, _, err := s.ListMutes(ctx, now, false, 1<<30, 0)
	if err != nil {
		return fmt.Errorf("read mutes: %w", err)
	}
	for i := range fs {
		fs[i].Mute = muteFor(mutes, fs[i])
	}
	return nil
}

// muteFor returns the mute that covers f among mutes ordered newest
// first, or nil. A mute for the finding's entity wins over one for the
// whole check; among those for the entity an acceptance wins over a plain
// mute, so the finding reads as accepted while both apply; otherwise the
// newest wins (proposed, not yet confirmed).
func muteFor(mutes []Mute, f Finding) *Mute {
	if !f.Firing() {
		return nil
	}
	var whole, entity *Mute
	for i := range mutes {
		m := mutes[i]
		if m.CheckCode != f.CheckCode {
			continue
		}
		if m.EntityType == "" {
			if whole == nil {
				whole = &m
			}
			continue
		}
		if m.EntityType == f.EntityType && m.EntityKey == f.EntityKey && (entity == nil || (m.Accepted && !entity.Accepted)) {
			entity = &m
		}
	}
	if entity != nil {
		return entity
	}
	return whole
}

// FindingTally counts the findings firing at now and those resolved in the
// 24 hours before it, with the mutes active at now applied.
type FindingTally struct {
	// Critical, Warning and Info count unmuted firing findings.
	Critical, Warning, Info int
	// Muted counts firing findings an active mute covers.
	Muted int
	// Flapping counts firing findings that flap, muted or not.
	Flapping int
	// Resolved24h counts resolve transitions in the 24 hours up to now.
	Resolved24h int
}

// Tally returns the finding counts at now.
func (s *Store) Tally(ctx context.Context, now time.Time) (FindingTally, error) {
	var t FindingTally
	fs, err := s.Findings(ctx, now, FindingQuery{From: now.Add(-24 * time.Hour)})
	if err != nil {
		return t, err
	}
	for _, f := range fs {
		if !f.Firing() {
			continue
		}
		if f.Flapping {
			t.Flapping++
		}
		switch {
		case f.Mute != nil:
			t.Muted++
		case f.Severity == "critical":
			t.Critical++
		case f.Severity == "warning":
			t.Warning++
		default:
			t.Info++
		}
	}
	// Resolves are counted from the transitions: a finding that resolved,
	// fired again and resolved again counts twice, and one firing again
	// now still counts its earlier resolve.
	t.Resolved24h, err = s.ResolvedBetween(ctx, now.Add(-24*time.Hour), now)
	return t, err
}

// TransitionQuery selects finding transitions. Zero fields do not filter.
type TransitionQuery struct {
	FindingID string
	CheckCode string
	// From and To bound the transition time: From <= time < To.
	From, To time.Time
	// Limit caps the rows; 0 means no cap.
	Limit int
}

// FindingTransitions returns transitions matching q, newest first.
func (s *Store) FindingTransitions(ctx context.Context, q TransitionQuery) ([]FindingTransition, error) {
	var where []string
	var args []any
	if q.FindingID != "" {
		where = append(where, `finding_id = ?`)
		args = append(args, q.FindingID)
	}
	if q.CheckCode != "" {
		where = append(where, `check_code = ?`)
		args = append(args, q.CheckCode)
	}
	if !q.From.IsZero() {
		where = append(where, `time >= ?`)
		args = append(args, q.From.UTC())
	}
	if !q.To.IsZero() {
		where = append(where, `time < ?`)
		args = append(args, q.To.UTC())
	}
	query := `SELECT finding_id, check_code, entity_type, entity_key, epoch, time, kind, severity, detail, value, cause_event_id
FROM finding_transitions`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, ` AND `)
	}
	query += ` ORDER BY epoch DESC, finding_id`
	if q.Limit > 0 {
		query += fmt.Sprintf(` LIMIT %d`, q.Limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read transitions: %w", err)
	}
	defer rows.Close()
	var out []FindingTransition
	for rows.Next() {
		var t FindingTransition
		var value sql.NullFloat64
		var cause sql.NullInt64
		if err := rows.Scan(&t.FindingID, &t.CheckCode, &t.EntityType, &t.EntityKey, &t.Epoch, &t.Time, &t.Kind,
			&t.Severity, &t.Detail, &value, &cause); err != nil {
			return nil, err
		}
		t.Time = t.Time.UTC()
		if value.Valid {
			v := value.Float64
			t.Value = &v
		}
		t.CauseEventID = cause.Int64
		out = append(out, t)
	}
	return out, rows.Err()
}

// CheckRunRow is one check's outcome for one epoch.
type CheckRunRow struct {
	Epoch     int64
	Time      time.Time
	CheckCode string
	Status    string
	Rows      int
	Error     string
}

// CheckRuns returns the check runs of the epochs that started in
// [from, to), oldest first.
func (s *Store) CheckRuns(ctx context.Context, from, to time.Time) ([]CheckRunRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.epoch, e.started_at, r.check_code, r.status, r.rows, coalesce(r.error, '')
FROM check_runs r JOIN epochs e USING (epoch)
WHERE e.started_at >= ? AND e.started_at < ?
ORDER BY r.epoch, r.check_code`, from.UTC(), to.UTC())
	if err != nil {
		return nil, fmt.Errorf("read check runs: %w", err)
	}
	defer rows.Close()
	var out []CheckRunRow
	for rows.Next() {
		var r CheckRunRow
		if err := rows.Scan(&r.Epoch, &r.Time, &r.CheckCode, &r.Status, &r.Rows, &r.Error); err != nil {
			return nil, err
		}
		r.Time = r.Time.UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}

// jsonList encodes a list as a JSON array for a from_json parameter.
func jsonList[T any](v []T) string {
	b, _ := json.Marshal(v)
	return string(b)
}
