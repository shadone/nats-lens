package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/shadone/nats-lens/internal/timing"
)

// DeliveryBatch pins the oldest pending batch before any HTTP request. Only
// the system's single delivery worker for this channel may call it.
// ASVS 15.4.3: the write lock belongs to the store and never covers network I/O.
func (s *Store) DeliveryBatch(ctx context.Context, channel string, now time.Time, limit int) ([]OutboxRow, time.Time, error) {
	ctx = timing.WithSource(ctx, timing.SourceNotify)
	if err := s.lock(ctx, opNotify); err != nil {
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
	ctx = timing.WithSource(ctx, timing.SourceNotify)
	if err := s.lock(ctx, opNotify); err != nil {
		return err
	}
	defer s.mu.Unlock()
	var finished, delivered, scheduled, lastError any
	if state != OutboxDelivered {
		lastError = fmt.Sprintf("%s, HTTP %d", state, status)
	}
	if state != OutboxPending {
		finished = at.UTC()
	}
	if state == OutboxDelivered {
		delivered = at.UTC()
	}
	if !next.IsZero() {
		scheduled = next.UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE notify_outbox SET state=?,attempts=attempts+1,last_status=?,last_error=?,finished_at=?,delivered_at=?,next_attempt_at=? WHERE channel=? AND id BETWEEN ? AND ? AND state='pending'`, state, status, lastError, finished, delivered, scheduled, channel, first, last)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed > 0 && state != OutboxPending {
		// This watermark outlives retained outcomes, even for very long intervals.
		// Record the current collection, not the epoch that queued an old message.
		_, err = tx.ExecContext(ctx, `INSERT INTO notify_delivery_rate (channel,last_finished_at,last_success_epoch)
   VALUES (?,make_timestamp(?::BIGINT),CASE WHEN ? THEN (SELECT max(epoch) FROM epochs) ELSE NULL END)
   ON CONFLICT (channel) DO UPDATE SET last_finished_at=excluded.last_finished_at,
   last_success_epoch=coalesce(excluded.last_success_epoch,notify_delivery_rate.last_success_epoch)`, channel, at.UnixMicro(), state == OutboxDelivered)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LastDeliveryFinished returns the channel's latest terminal batch outcome.
// It bounds the normal send rate across process restarts as well as in memory.
func (s *Store) LastDeliveryFinished(ctx context.Context, channel string) (time.Time, error) {
	var at sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT last_finished_at FROM notify_delivery_rate WHERE channel=?`, channel).Scan(&at)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil || !at.Valid {
		return time.Time{}, err
	}
	return at.Time.UTC(), nil
}

// NotificationEpochSent prevents a second successful batch before another
// collection, including when the collector is disconnected or after a restart.
func (s *Store) NotificationEpochSent(ctx context.Context, channel string) (bool, error) {
	var sent bool
	err := s.db.QueryRowContext(ctx, `SELECT coalesce((SELECT last_success_epoch FROM notify_delivery_rate WHERE channel=?) >= coalesce((SELECT max(epoch) FROM epochs),0), false)`, channel).Scan(&sent)
	return sent, err
}

// NotificationIncarnation identifies this database, not its archived NATS data.
// It survives a reopen; a rebuild creates a fresh random, non-secret identity.
func (s *Store) NotificationIncarnation(ctx context.Context) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT incarnation::VARCHAR FROM notify_identity WHERE singleton`).Scan(&id)
	return id, err
}
