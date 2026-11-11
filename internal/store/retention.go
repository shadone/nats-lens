package store

import (
	"context"
	"fmt"
	"time"
)

// epochTables hold one row per epoch per entity and are swept by epoch.
var epochTables = []string{
	"epoch_endpoints", "server_stats", "routes", "gateways", "leafnodes", "ipqueues", "health",
	"connections", "account_stats", "account_details", "stream_stats", "stream_source_stats", "stream_replicas",
	"consumer_stats", "consumer_replicas", "raft_stats", "subscriptions", "subscription_lists",
}

// accountEpochTables hold one row per epoch per account entity, asked
// over the account connections: they are swept and archived by epoch like
// epochTables, but describe no server.
var accountEpochTables = []string{"micro_requests", "micro_instances", "micro_stats", "micro_endpoints"}

// checkTables hold the checks' rows of an epoch: they are swept and
// archived by epoch like epochTables, but describe no server.
var checkTables = []string{"check_runs", "finding_transitions"}

// derivedEpochTables hold one row per epoch computed from the other tables
// as the epoch is written: they are swept by epoch but not archived, as a
// rebuild computes them again.
var derivedEpochTables = []string{"epoch_asset_counts"}

// hourTables hold sums per hour, filled as each epoch is written; they are
// swept with the hours wholly before the retention window and archived by
// hour.
var hourTables = []string{"micro_endpoint_hours", "client_app_hours"}

// rangeTables hold rows that cover a range of epochs and are swept by last_epoch.
var rangeTables = []string{
	"servers", "accounts", "streams", "consumers", "raft_groups", "stream_config", "consumer_config",
}

// SweepResult reports what a retention sweep removed.
type SweepResult struct {
	// Cutoff is the oldest epoch kept; rows of older epochs were deleted.
	Cutoff int64
	// Epochs is the number of epochs deleted.
	Epochs int64
	// Events is the number of events deleted: those received before now
	// minus retention.
	Events int64
}

// Sweep deletes epochs that started before now minus retention, with every
// row that belongs only to them (check runs and finding transitions
// included, except the current episode of every finding kept), resolved
// findings last changed before them, acknowledgements that ended, or whose
// episode ended, before then, events received before then, and client pod
// addresses last seen before then, in one transaction.
func (s *Store) Sweep(ctx context.Context, now time.Time, retention time.Duration) (SweepResult, error) {
	s.mu.Lock(opSweep)
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SweepResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	// The cutoff is the first epoch inside the retention window; when none
	// is, every stored epoch is older and the cutoff is past the newest.
	var cutoff int64
	if err := tx.QueryRowContext(ctx, `
SELECT coalesce(
    (SELECT min(epoch) FROM epochs WHERE started_at >= ?),
    (SELECT coalesce(max(epoch), 0) + 1 FROM epochs))`,
		now.Add(-retention).UTC()).Scan(&cutoff); err != nil {
		return SweepResult{}, fmt.Errorf("retention cutoff: %w", err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM epochs WHERE epoch < ?`, cutoff)
	if err != nil {
		return SweepResult{}, fmt.Errorf("sweep epochs: %w", err)
	}
	n, _ := res.RowsAffected()
	for _, t := range append(append(append(append([]string{}, epochTables...), accountEpochTables...), derivedEpochTables...), "check_runs") {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+t+" WHERE epoch < ?", cutoff); err != nil {
			return SweepResult{}, fmt.Errorf("sweep %s: %w", t, err)
		}
	}
	// The hourly sums go with the hours wholly before the window; the
	// hour the window starts in keeps its sum.
	for _, t := range hourTables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+t+" WHERE hour < date_trunc('hour', make_timestamp(?::BIGINT))",
			now.Add(-retention).UTC().UnixMicro()); err != nil {
			return SweepResult{}, fmt.Errorf("sweep %s: %w", t, err)
		}
	}
	for _, t := range rangeTables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+t+" WHERE last_epoch < ?", cutoff); err != nil {
			return SweepResult{}, fmt.Errorf("sweep %s: %w", t, err)
		}
	}
	// A resolved finding goes once its last change is older than the
	// oldest kept epoch; a firing one stays however old it is.
	if _, err := tx.ExecContext(ctx, `DELETE FROM findings WHERE state = 'resolved' AND updated_epoch < ?`, cutoff); err != nil {
		return SweepResult{}, fmt.Errorf("sweep findings: %w", err)
	}
	// An acknowledgement goes once it, or its episode, ended before the
	// window.
	if err := sweepAcks(ctx, tx, now.Add(-retention)); err != nil {
		return SweepResult{}, err
	}
	// Transitions go with their epoch, except the current episode of every
	// finding still kept: from its latest firing transition on, so that a
	// finding firing since a swept epoch still says when it started.
	if _, err := tx.ExecContext(ctx, `DELETE FROM finding_transitions t WHERE t.epoch < ? AND NOT EXISTS (
    SELECT 1 FROM findings f
    JOIN (SELECT finding_id, max(epoch) AS epoch FROM finding_transitions WHERE kind = 'firing' GROUP BY finding_id) s
      ON s.finding_id = f.id
    WHERE f.id = t.finding_id AND t.epoch >= s.epoch)`, cutoff); err != nil {
		return SweepResult{}, fmt.Errorf("sweep finding_transitions: %w", err)
	}
	// A client pod's address goes once it was last seen before the window.
	if _, err := tx.ExecContext(ctx, `DELETE FROM client_pods WHERE last_seen < ?`, now.Add(-retention).UTC()); err != nil {
		return SweepResult{}, fmt.Errorf("sweep client_pods: %w", err)
	}
	ev, err := tx.ExecContext(ctx, `DELETE FROM events WHERE received_at < ?`, now.Add(-retention).UTC())
	if err != nil {
		return SweepResult{}, fmt.Errorf("sweep events: %w", err)
	}
	ne, _ := ev.RowsAffected()
	if err := tx.Commit(); err != nil {
		return SweepResult{}, err
	}
	return SweepResult{Cutoff: cutoff, Epochs: n, Events: ne}, nil
}
