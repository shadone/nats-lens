package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// DeliveryBatch pins the oldest pending batch before any HTTP request. Only
// the system's single delivery worker for this channel may call it.
// ASVS 15.4.3: the write lock belongs to the store and never covers network I/O.
func (s *Store) DeliveryBatch(ctx context.Context, channel string, now time.Time, limit int) ([]OutboxRow, time.Time, error) {
	if err := s.lock(ctx); err != nil {
		return nil, time.Time{}, err
	}
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer tx.Rollback()
	var first, end int64
	var last sql.NullInt64
	var at sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT id,batch_end,batch_at FROM notify_outbox WHERE channel=? AND state='pending' ORDER BY id LIMIT 1`, channel).Scan(&first, &last, &at)
	if err == sql.ErrNoRows {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	if last.Valid {
		end = last.Int64
	} else {
		if err = tx.QueryRowContext(ctx, `SELECT max(id) FROM (SELECT id FROM notify_outbox WHERE channel=? AND state='pending' ORDER BY id LIMIT ?)`, channel, limit).Scan(&end); err != nil {
			return nil, time.Time{}, err
		}
		at = sql.NullTime{Time: now.UTC(), Valid: true}
		// ASVS 1.2.4: values remain SQL parameters.
		if _, err = tx.ExecContext(ctx, `UPDATE notify_outbox SET batch_end=?,batch_at=make_timestamp(?::BIGINT) WHERE channel=? AND id BETWEEN ? AND ? AND state='pending'`, end, now.UnixMicro(), channel, first, end); err != nil {
			return nil, time.Time{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, time.Time{}, err
	}
	rows, err := s.outbox(ctx, `WHERE channel=? AND state='pending' AND id BETWEEN ? AND ? ORDER BY id`, channel, first, end)
	return rows, at.Time.UTC(), err
}

// FinishDelivery records one attempt atomically. Text is deliberately a fixed
// status description supplied by delivery, never a receiver body or URL.
func (s *Store) FinishDelivery(ctx context.Context, channel string, first, last int64, state string, status int, at, next time.Time) error {
	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.mu.Unlock()
	var finished, delivered, scheduled any
	if state != OutboxPending {
		finished = at.UTC()
	}
	if state == OutboxDelivered {
		delivered = at.UTC()
	}
	if !next.IsZero() {
		scheduled = next.UTC()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE notify_outbox SET state=?,attempts=attempts+1,last_status=?,last_error=?,finished_at=?,delivered_at=?,next_attempt_at=? WHERE channel=? AND id BETWEEN ? AND ? AND state='pending'`, state, status, fmt.Sprintf("%s, HTTP %d", state, status), finished, delivered, scheduled, channel, first, last)
	return err
}

// LastDeliveryFinished returns the channel's latest terminal batch outcome.
// It bounds the normal send rate across process restarts as well as in memory.
func (s *Store) LastDeliveryFinished(ctx context.Context, channel string) (time.Time, error) {
	var at sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT max(finished_at) FROM notify_outbox WHERE channel=? AND state <> 'pending'`, channel).Scan(&at)
	if err != nil || !at.Valid {
		return time.Time{}, err
	}
	return at.Time.UTC(), nil
}
