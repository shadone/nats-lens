package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Mute is one row of the mutes table.
type Mute struct {
	ID        int64
	CheckCode string
	// EntityType and EntityKey are empty when the mute covers every entity
	// of the check.
	EntityType string
	EntityKey  string
	Reason     string
	MutedBy    string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	// UnmutedAt is zero while the mute was not lifted.
	UnmutedAt time.Time
	UnmutedBy string
	// Accepted marks an acceptance: the finding is by design, ExpiresAt is
	// its review date, and it has an entity.
	Accepted bool
	// GrowthPerDay is the entity's growth when it was accepted, bytes per
	// day; nil without a rate.
	GrowthPerDay *float64
	// EndReason says why nats-lens ended the acceptance; empty when a
	// person lifted it or it ran out.
	EndReason string
}

// Active reports whether the mute applies at now.
func (m Mute) Active(now time.Time) bool {
	return m.UnmutedAt.IsZero() && m.ExpiresAt.After(now)
}

// Errors of the mute methods.
var (
	ErrMuteNotFound = errors.New("mute not found")
	ErrMuteInactive = errors.New("mute no longer applies")
)

// lock takes the write lock, or gives up when ctx ends first; a mute call
// then fails instead of waiting behind a long epoch write or archive
// export. A lock acquired after ctx ended is released at once.
func (s *Store) lock(ctx context.Context) error {
	got := make(chan struct{})
	go func() {
		s.mu.Lock()
		close(got)
	}()
	select {
	case <-got:
		return nil
	case <-ctx.Done():
		go func() {
			<-got
			s.mu.Unlock()
		}()
		return ctx.Err()
	}
}

const muteCols = `id, check_code, entity_type, entity_key, reason, muted_by, created_at, expires_at, unmuted_at, unmuted_by, coalesce(accepted, false), growth_per_day, end_reason`

// muteInsertCols are the columns CreateMute writes.
const muteInsertCols = `id, check_code, entity_type, entity_key, reason, muted_by, created_at, expires_at, accepted, growth_per_day`

func scanMute(row interface{ Scan(...any) error }) (Mute, error) {
	var m Mute
	var et, ek, ub, er sql.NullString
	var ua sql.NullTime
	var g sql.NullFloat64
	if err := row.Scan(&m.ID, &m.CheckCode, &et, &ek, &m.Reason, &m.MutedBy, &m.CreatedAt, &m.ExpiresAt, &ua, &ub,
		&m.Accepted, &g, &er); err != nil {
		return Mute{}, err
	}
	m.EntityType, m.EntityKey, m.UnmutedBy, m.EndReason = et.String, ek.String, ub.String, er.String
	if g.Valid {
		m.GrowthPerDay = &g.Float64
	}
	if ua.Valid {
		m.UnmutedAt = ua.Time.UTC()
	}
	m.CreatedAt, m.ExpiresAt = m.CreatedAt.UTC(), m.ExpiresAt.UTC()
	return m, nil
}

// CreateMute stores a mute and returns it with its ID. CreatedAt and
// ExpiresAt are taken as given.
func (s *Store) CreateMute(ctx context.Context, m Mute) (Mute, error) {
	if err := s.lock(ctx); err != nil {
		return Mute{}, err
	}
	defer s.mu.Unlock()
	if m.Accepted && m.EntityType == "" {
		return Mute{}, errors.New("create mute: an acceptance needs an entity")
	}
	var growth any
	if m.GrowthPerDay != nil {
		growth = *m.GrowthPerDay
	}
	row := s.db.QueryRowContext(ctx, `INSERT INTO mutes (`+muteInsertCols+`)
VALUES (nextval('mute_seq'), ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING `+muteCols,
		m.CheckCode, nullIfEmpty(m.EntityType), nullIfEmpty(m.EntityKey), m.Reason, m.MutedBy, m.CreatedAt.UTC(), m.ExpiresAt.UTC(),
		m.Accepted, growth)
	out, err := scanMute(row)
	if err != nil {
		return Mute{}, fmt.Errorf("create mute: %w", err)
	}
	return out, nil
}

// UnmuteMute lifts a mute that applies at now. It returns ErrMuteNotFound
// for an unknown ID and ErrMuteInactive for one already lifted or expired.
func (s *Store) UnmuteMute(ctx context.Context, id int64, by string, now time.Time) (Mute, error) {
	if err := s.lock(ctx); err != nil {
		return Mute{}, err
	}
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Mute{}, err
	}
	defer func() { _ = tx.Rollback() }()
	m, err := scanMute(tx.QueryRowContext(ctx, `SELECT `+muteCols+` FROM mutes WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Mute{}, ErrMuteNotFound
	}
	if err != nil {
		return Mute{}, err
	}
	if !m.Active(now) {
		return Mute{}, ErrMuteInactive
	}
	if _, err := tx.ExecContext(ctx, `UPDATE mutes SET unmuted_at = ?, unmuted_by = ? WHERE id = ?`, now.UTC(), by, id); err != nil {
		return Mute{}, fmt.Errorf("unmute %d: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return Mute{}, err
	}
	m.UnmutedAt, m.UnmutedBy = now.UTC(), by
	return m, nil
}

// ListMutes returns up to limit mutes with an ID below before (any ID when
// before is zero), newest first, and how many match in all; without all
// only those that apply at now. Paging by ID keeps the pages in place
// when mutes are added between them.
func (s *Store) ListMutes(ctx context.Context, now time.Time, all bool, limit int, before int64) ([]Mute, int, error) {
	where := ` WHERE true`
	var args []any
	if !all {
		where += ` AND unmuted_at IS NULL AND expires_at > ?`
		args = append(args, now.UTC())
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM mutes`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if before > 0 {
		where += ` AND id < ?`
		args = append(args, before)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+muteCols+` FROM mutes`+where+` ORDER BY id DESC LIMIT ?`,
		append(args, limit)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Mute
	for rows.Next() {
		m, err := scanMute(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}
