package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// streamSamplesSQL writes the stream_samples rows of the epochs from the
// first to the second parameter (epoch numbers) that have no samples yet.
// Every epoch below the first must have its samples, so that
// stream_sample_heads holds each stream's newest sample before them. The
// values follow the rules the per-stream series applied to stream_stats
// before the table existed:
//
//   - bytes, messages and seq are the leader's report, else the largest
//     any server reports (a row without is_leader true is no leader);
//   - replica_bytes sums every server's bytes;
//   - created is known only when every report has one and all agree;
//   - the previous sample is the stream's newest sample in an earlier
//     epoch, however far back, as lag over the stream's samples in epoch
//     order gives it.
//
// Reports of an epoch without an epochs row are left out, as the series
// join epochs.
const streamSamplesSQL = `
INSERT INTO stream_samples (epoch, account, stream, bytes, messages, replica_bytes, seq, created, prev_epoch, prev_seq, prev_created)
WITH win AS (
  SELECT epoch FROM epochs
  WHERE epoch BETWEEN $1 AND $2 AND epoch NOT IN (SELECT epoch FROM stream_sample_epochs WHERE epoch BETWEEN $1 AND $2)),
cur AS (
  SELECT x.epoch, x.account, x.stream,
    coalesce(max(x.bytes) FILTER (WHERE x.is_leader), max(x.bytes)) AS bytes,
    coalesce(max(x.messages) FILTER (WHERE x.is_leader), max(x.messages)) AS messages,
    sum(x.bytes) AS replica_bytes,
    coalesce(max(x.last_seq) FILTER (WHERE x.is_leader), max(x.last_seq)) AS seq,
    CASE WHEN count(x.created) = count(*) AND count(DISTINCT x.created) = 1 THEN max(x.created) END AS created
  FROM stream_stats x JOIN win USING (epoch)
  WHERE x.epoch BETWEEN $1 AND $2
  GROUP BY x.epoch, x.account, x.stream),
chain AS (
  SELECT epoch, account, stream, bytes, messages, replica_bytes, seq, created, false AS head FROM cur
  UNION ALL
  SELECT h.epoch, h.account, h.stream, NULL, NULL, NULL, h.seq, h.created, true
  FROM stream_sample_heads h SEMI JOIN cur USING (account, stream)),
linked AS (
  SELECT *, lag(epoch) OVER w AS prev_epoch, lag(seq) OVER w AS prev_seq, lag(created) OVER w AS prev_created
  FROM chain WINDOW w AS (PARTITION BY account, stream ORDER BY epoch))
SELECT epoch, account, stream, bytes, messages, replica_bytes, seq, created, prev_epoch, prev_seq, prev_created
FROM linked WHERE NOT head`

// streamSampleHeadsSQL replaces the heads of the streams sampled in the
// epochs from the first to the second parameter with their newest sample
// there.
var streamSampleHeadsSQL = []string{
	`DELETE FROM stream_sample_heads h
USING (SELECT DISTINCT account, stream FROM stream_samples WHERE epoch BETWEEN $1 AND $2) n
WHERE h.account = n.account AND h.stream = n.stream`,
	`INSERT INTO stream_sample_heads (account, stream, epoch, seq, created)
SELECT account, stream, epoch, seq, created FROM stream_samples WHERE epoch BETWEEN $1 AND $2
QUALIFY row_number() OVER (PARTITION BY account, stream ORDER BY epoch DESC) = 1`,
}

// streamSampleMarkSQL marks the epochs from the first to the second
// parameter as sampled; its row count is the number of epochs filled.
const streamSampleMarkSQL = `INSERT INTO stream_sample_epochs (epoch)
SELECT epoch FROM epochs WHERE epoch BETWEEN $1 AND $2 AND epoch NOT IN (SELECT epoch FROM stream_sample_epochs)`

// streamSamplesUnsampledBelow counts the epochs before the parameter
// without samples: an epoch gets its samples only when it is 0.
const streamSamplesUnsampledBelow = `SELECT count(*) FROM epochs
WHERE epoch < $1 AND epoch NOT IN (SELECT epoch FROM stream_sample_epochs)`

// streamSampleBatch is how many epochs one fill transaction covers, as for
// the asset counts.
const streamSampleBatch = 120

// beforeStreamSampleBatch, when set, runs before each fill batch; for
// tests.
var beforeStreamSampleBatch func()

type execQueryRower interface {
	execQuerier
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// writeStreamSamples writes the epoch's stream samples, when every earlier
// epoch has them; the epoch's stream reports must be written before. An
// epoch written while earlier ones lack samples (the fill did not finish)
// gets none, and the readers read the reports for a range that includes
// it, until a later fill writes them in order.
func (w *writer) writeStreamSamples() {
	if w.err != nil {
		return
	}
	var missing int64
	if err := w.tx.QueryRowContext(w.ctx, streamSamplesUnsampledBelow, w.epoch).Scan(&missing); err != nil {
		w.fail(fmt.Errorf("epochs without stream samples: %w", err))
		return
	}
	if missing > 0 {
		return
	}
	w.exec(streamSamplesSQL, w.epoch, w.epoch)
	for _, q := range streamSampleHeadsSQL {
		w.exec(q, w.epoch, w.epoch)
	}
	w.exec(streamSampleMarkSQL, w.epoch, w.epoch)
}

// FillStreamSamples writes the stream samples of every stored epoch that
// has none, in epoch order and in batches of streamSampleBatch epochs,
// each its own transaction under the write lock, so that an epoch write
// waits for one batch at most. It returns how many epochs it filled and
// logs them. The epochs a release before migration 0042 wrote have none;
// the collector calls it once the store's thread count and memory limit
// are set.
func (s *Store) FillStreamSamples(ctx context.Context) (int64, error) {
	n, _, err := s.FillStreamSamplesUntil(ctx, time.Time{})
	return n, err
}

// FillStreamSamplesUntil is FillStreamSamples that starts no batch at or
// after until (none when zero); more reports that epochs without samples
// remain.
func (s *Store) FillStreamSamplesUntil(ctx context.Context, until time.Time) (n int64, more bool, err error) {
	begin := time.Now()
	n, more, err = fillStreamSamples(ctx, s.db, func() func() {
		s.mu.Lock(opStreamSamples)
		return s.mu.Unlock
	}, until)
	if n > 0 {
		s.logger().Info("stream samples filled", "epochs", n, "took", time.Since(begin).Round(time.Millisecond), "more", more)
	}
	return n, more, err
}

// fillStreamSamples fills the missing stream samples through db, which is
// a *sql.DB (each batch then is a transaction of its own) or a
// transaction. lock, when not nil, is held around each batch. No batch
// starts at or after until, unless it is zero; more reports that epochs
// without samples may remain then.
func fillStreamSamples(ctx context.Context, db execQueryRower, lock func() func(), until time.Time) (int64, bool, error) {
	var n int64
	for {
		if !until.IsZero() && !time.Now().Before(until) {
			return n, true, nil
		}
		added, done, err := fillStreamSampleBatch(ctx, db, lock)
		n += added
		if err != nil || done {
			return n, !done, err
		}
	}
}

// fillStreamSampleBatch fills the lowest streamSampleBatch epochs without
// samples. Every epoch below them has samples, so their previous samples
// are in stream_sample_heads. done reports that no epoch lacked samples.
func fillStreamSampleBatch(ctx context.Context, db execQueryRower, lock func() func()) (added int64, done bool, err error) {
	if lock != nil {
		defer lock()()
	}
	if beforeStreamSampleBatch != nil {
		beforeStreamSampleBatch()
	}
	var lo, hi sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT min(epoch), max(epoch) FROM (
  SELECT epoch FROM epochs WHERE epoch NOT IN (SELECT epoch FROM stream_sample_epochs) ORDER BY epoch LIMIT ?)`,
		streamSampleBatch).Scan(&lo, &hi); err != nil {
		return 0, false, fmt.Errorf("epochs without stream samples: %w", err)
	}
	if !lo.Valid {
		return 0, true, nil
	}
	ex := execQuerier(db)
	var tx *sql.Tx
	if b, ok := db.(interface {
		BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	}); ok {
		if tx, err = b.BeginTx(ctx, nil); err != nil {
			return 0, false, err
		}
		defer func() { _ = tx.Rollback() }()
		ex = tx
	}
	if _, err := ex.ExecContext(ctx, streamSamplesSQL, lo.Int64, hi.Int64); err != nil {
		return 0, false, fmt.Errorf("stream samples of epochs %d to %d: %w", lo.Int64, hi.Int64, err)
	}
	for _, q := range streamSampleHeadsSQL {
		if _, err := ex.ExecContext(ctx, q, lo.Int64, hi.Int64); err != nil {
			return 0, false, fmt.Errorf("stream sample heads of epochs %d to %d: %w", lo.Int64, hi.Int64, err)
		}
	}
	res, err := ex.ExecContext(ctx, streamSampleMarkSQL, lo.Int64, hi.Int64)
	if err != nil {
		return 0, false, fmt.Errorf("marking epochs %d to %d sampled: %w", lo.Int64, hi.Int64, err)
	}
	if added, err = res.RowsAffected(); err != nil {
		return 0, false, err
	}
	if tx != nil {
		if err := tx.Commit(); err != nil {
			return 0, false, err
		}
	}
	return added, false, nil
}
