package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/collect"
)

// WriteSnapshot stores one epoch in a single transaction and returns its
// epoch number. Either every row of the epoch is stored or none is.
func (s *Store) WriteSnapshot(ctx context.Context, snap *collect.Snapshot) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeSnapshot(ctx, snap)
}

// WriteSnapshotWithin is WriteSnapshot with a time limit that starts once
// the store lock is held, so that time spent waiting for a sweep, an export
// or an event write does not count against the epoch's write.
func (s *Store) WriteSnapshotWithin(ctx context.Context, snap *collect.Snapshot, limit time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	wctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	return s.writeSnapshot(wctx, snap)
}

// writeSnapshot writes one epoch; the caller holds mu.
func (s *Store) writeSnapshot(ctx context.Context, snap *collect.Snapshot) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var epoch int64
	if err := tx.QueryRowContext(ctx, `SELECT nextval('epoch_seq')`).Scan(&epoch); err != nil {
		return 0, fmt.Errorf("next epoch: %w", err)
	}
	w := newWriter(ctx, tx, epoch, snap)
	w.log = s.logger()
	if s.warned == nil {
		s.warned = map[assetKey]bool{}
	}
	w.warned = s.warned
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(max(epoch), 0) FROM epochs`).Scan(&w.prevEpoch); err != nil {
		return 0, fmt.Errorf("previous epoch: %w", err)
	}
	w.writeEpoch()
	for _, v := range snap.SortedViews() {
		w.writeServerView(v)
	}
	w.writeAccountDetails()
	for _, acc := range sortedKeys(snap.AccountNames) {
		w.ids.account(acc, snap.AccountNames[acc], false)
	}
	w.writeIdents()
	if len(w.notes) > 0 {
		w.exec(`UPDATE epochs SET errors = list_concat(errors, ?) WHERE epoch = ?`, w.notes, w.epoch)
	}
	w.closeStatements()
	if w.err != nil {
		return 0, fmt.Errorf("write epoch %d: %w", epoch, w.err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit epoch %d: %w", epoch, err)
	}
	return epoch, nil
}

// writer holds the state of one epoch's transaction. The first error is
// kept and every later call becomes a no-op, so writers need no error
// plumbing; WriteSnapshot checks it once.
type writer struct {
	ctx   context.Context
	tx    *sql.Tx
	epoch int64
	snap  *collect.Snapshot
	stmts map[string]*sql.Stmt
	ids   *idents
	peers map[string]string
	// seen maps a server's asset reported in this epoch to its config hash;
	// noted marks the conflicting repeats already recorded in notes.
	seen  map[assetKey]string
	noted map[assetKey]bool
	notes []string
	// warned is the store's set of conflicts already logged.
	warned map[assetKey]bool
	log    *slog.Logger
	// prevEpoch is the newest epoch stored before this one, 0 when none.
	prevEpoch int64
	err       error
}

func newWriter(ctx context.Context, tx *sql.Tx, epoch int64, snap *collect.Snapshot) *writer {
	return &writer{ctx: ctx, tx: tx, epoch: epoch, snap: snap, stmts: map[string]*sql.Stmt{}, ids: newIdents(),
		seen: map[assetKey]string{}, noted: map[assetKey]bool{}, log: slog.Default()}
}

func (w *writer) fail(err error) {
	if w.err == nil && err != nil {
		w.err = err
	}
}

// insert adds one row to table. Statements are prepared once per table per epoch.
func (w *writer) insert(table string, cols []string, vals ...any) {
	if w.err != nil {
		return
	}
	if len(cols) != len(vals) {
		w.fail(fmt.Errorf("%s: %d columns, %d values", table, len(cols), len(vals)))
		return
	}
	stmt, ok := w.stmts[table]
	if !ok {
		q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table,
			strings.Join(cols, ", "), strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", "))
		var err error
		if stmt, err = w.tx.PrepareContext(w.ctx, q); err != nil {
			w.fail(fmt.Errorf("prepare %s: %w", table, err))
			return
		}
		w.stmts[table] = stmt
	}
	if _, err := stmt.ExecContext(w.ctx, vals...); err != nil {
		w.fail(fmt.Errorf("insert %s: %w", table, err))
	}
}

// exec runs one statement.
func (w *writer) exec(q string, args ...any) {
	if w.err != nil {
		return
	}
	if _, err := w.tx.ExecContext(w.ctx, q, args...); err != nil {
		w.fail(fmt.Errorf("%s: %w", firstLine(q), err))
	}
}

func (w *writer) closeStatements() {
	for _, st := range w.stmts {
		_ = st.Close()
	}
}

func firstLine(q string) string {
	q = strings.TrimSpace(q)
	if i := strings.IndexByte(q, '\n'); i > 0 {
		return q[:i]
	}
	return q
}

var epochCols = []string{
	"epoch", "started_at", "finished_at", "duration_ms", "expected_servers", "observed_servers",
	"expected_names", "observed_names", "missing_names", "endpoint_failures", "partial", "errors",
}

func (w *writer) writeEpoch() {
	s := w.snap
	failures := 0
	for _, v := range s.Servers {
		for _, st := range v.Status {
			if st.State != collect.StateOK {
				failures++
			}
		}
	}
	observed := s.Observed()
	w.insert("epochs", epochCols,
		w.epoch, s.StartedAt.UTC(), s.FinishedAt.UTC(), s.FinishedAt.Sub(s.StartedAt).Milliseconds(),
		len(s.Expected), len(observed), strs(s.Expected), strs(observed), strs(s.Missing()),
		failures, s.Partial(), strs(s.Errors),
	)
}

var endpointCols = []string{"epoch", "server_id", "endpoint", "state", "error"}

func (w *writer) writeServerView(v *collect.ServerView) {
	w.writeServerIdent(v)
	for _, e := range sortedKeys(v.Status) {
		st := v.Status[e]
		var errText any
		if st.Error != "" {
			errText = st.Error
		}
		w.insert("epoch_endpoints", endpointCols, w.epoch, v.Info.ID, string(e), string(st.State), errText)
	}
	w.writeServerStats(v)
	w.writeRoutes(v)
	w.writeGateways(v)
	w.writeLeafnodes(v)
	w.writeIpqueues(v)
	w.writeHealth(v)
	w.writeConnections(v)
	w.writeSubscriptions(v)
	w.writeAccountStats(v)
	w.writeJetStream(v)
	w.writeRaft(v)
}
