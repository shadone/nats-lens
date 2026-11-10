package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// CreateMutes stores several mutes in one transaction and returns them
// with their IDs, in the order given: every one is stored, or none is.
func (s *Store) CreateMutes(ctx context.Context, ms []Mute) ([]Mute, error) {
	for _, m := range ms {
		if m.Accepted && m.EntityType == "" {
			return nil, errors.New("create mutes: an acceptance needs an entity")
		}
	}
	if err := s.lock(ctx); err != nil {
		return nil, err
	}
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	out := make([]Mute, 0, len(ms))
	for _, m := range ms {
		var growth, key any
		if m.GrowthPerDay != nil {
			growth = *m.GrowthPerDay
		}
		if m.EntityType != "" {
			key = m.EntityKey
		}
		row := tx.QueryRowContext(ctx, `INSERT INTO mutes (`+muteInsertCols+`)
VALUES (nextval('mute_seq'), ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING `+muteCols,
			m.CheckCode, nullIfEmpty(m.EntityType), key, m.Reason, m.MutedBy, m.CreatedAt.UTC(), m.ExpiresAt.UTC(),
			m.Accepted, growth)
		got, err := scanMute(row)
		if err != nil {
			return nil, fmt.Errorf("create mutes: %w", err)
		}
		out = append(out, got)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("create mutes: %w", err)
	}
	return out, nil
}

// AcceptanceQuery selects acceptances for ListAcceptances.
type AcceptanceQuery struct {
	// Now decides which acceptances are in force.
	Now time.Time
	// Ended includes acceptances that ended: lifted or past their review date.
	Ended bool
	// Account keeps the acceptances of that account (the account itself,
	// its streams and consumers) and of entities of no one account;
	// empty keeps every account.
	Account string
	Offset  int
	Limit   int
}

// accountWhere is the SQL condition, with its arguments, that keeps the
// mutes concerning account as api.FindingAccount reads an entity's
// account: an account's own key, the part of a stream's or a consumer's
// key before the first "/". Entities of no one account are kept.
func accountWhere(account string) (string, []any) {
	return `(entity_type NOT IN ('account', 'stream', 'consumer')
 OR (entity_type = 'account' AND entity_key = ?)
 OR (entity_type IN ('stream', 'consumer') AND split_part(entity_key, '/', 1) = ?))`, []any{account, account}
}

// ListAcceptances returns a page of acceptances: those in force at Now
// first, the soonest review date first, then with Ended the ended ones,
// the latest end first. It also returns how many match in all and, with
// Account, how many of other accounts were left out.
func (s *Store) ListAcceptances(ctx context.Context, q AcceptanceQuery) (ms []Mute, total, others int, err error) {
	now := q.Now.UTC()
	where := ` WHERE coalesce(accepted, false)`
	var args []any
	if !q.Ended {
		where += ` AND unmuted_at IS NULL AND expires_at > ?`
		args = append(args, now)
	}
	all := -1
	if q.Account != "" {
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM mutes`+where, args...).Scan(&all); err != nil {
			return nil, 0, 0, err
		}
		cond, a := accountWhere(q.Account)
		where += ` AND ` + cond
		args = append(args, a...)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM mutes`+where, args...).Scan(&total); err != nil {
		return nil, 0, 0, err
	}
	if all >= 0 {
		others = all - total
	}
	// In force: not lifted and the review date after now, the column
	// compared with the parameter directly, so that the session time zone
	// cannot shift it.
	order := ` ORDER BY (unmuted_at IS NULL AND expires_at > ?) DESC,
 CASE WHEN unmuted_at IS NULL AND expires_at > ? THEN expires_at END ASC,
 coalesce(unmuted_at, expires_at) DESC, id ASC LIMIT ? OFFSET ?`
	rows, err := s.db.QueryContext(ctx, `SELECT `+muteCols+` FROM mutes`+where+order,
		append(args, now, now, q.Limit, q.Offset)...)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanMute(rows)
		if err != nil {
			return nil, 0, 0, err
		}
		ms = append(ms, m)
	}
	return ms, total, others, rows.Err()
}
