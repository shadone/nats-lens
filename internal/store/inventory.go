package store

import (
	"context"
	"fmt"
	"time"
)

// InventoryRow is one expected asset from the inventory file. Nil
// settings are not compared.
type InventoryRow struct {
	Account string
	// Kind is stream, kv, object_store or consumer.
	Kind string
	// Name is the name the inventory gives: stream, bucket or consumer.
	Name string
	// Stream holds the asset: the stream itself, KV_<bucket>, OBJ_<bucket>,
	// or the stream a consumer reads.
	Stream string
	// Consumer is set for kind consumer only.
	Consumer          string
	Replicas          *int
	Retention         *string
	Storage           *string
	Discard           *string
	MaxAge            *time.Duration
	MaxBytes          *int64
	MaxMsgs           *int64
	MaxMsgsPerSubject *int64
	AckWait           *time.Duration
	MaxDeliver        *int
	MaxAckPending     *int
	FilterSubjects    []string
}

func ptrVal[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

func durVal(p *time.Duration) any {
	if p == nil {
		return nil
	}
	return p.Nanoseconds()
}

// ReplaceInventory replaces the inventory table with rows, read from
// source at at, in one transaction. No rows empties it.
func (s *Store) ReplaceInventory(ctx context.Context, rows []InventoryRow, source string, at time.Time) error {
	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM inventory`); err != nil {
		return fmt.Errorf("clear inventory: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO inventory (account, kind, name, stream, consumer, replicas, retention,
    storage, discard, max_age_ns, max_bytes, max_msgs, max_msgs_per_subject, ack_wait_ns, max_deliver, max_ack_pending,
    filter_subjects, source, loaded_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		var filters any
		if r.FilterSubjects != nil {
			filters = strs(r.FilterSubjects)
		}
		if _, err := stmt.ExecContext(ctx, r.Account, r.Kind, r.Name, r.Stream, nullIfEmpty(r.Consumer),
			ptrVal(r.Replicas), ptrVal(r.Retention), ptrVal(r.Storage), ptrVal(r.Discard), durVal(r.MaxAge),
			ptrVal(r.MaxBytes), ptrVal(r.MaxMsgs), ptrVal(r.MaxMsgsPerSubject), durVal(r.AckWait),
			ptrVal(r.MaxDeliver), ptrVal(r.MaxAckPending), filters, source, at.UTC()); err != nil {
			return fmt.Errorf("inventory %s %s/%s: %w", r.Kind, r.Account, r.Name, err)
		}
	}
	return tx.Commit()
}
