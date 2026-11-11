package store

import (
	"context"
	"database/sql/driver"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shadone/nats-lens/internal/admit"
	"github.com/shadone/nats-lens/internal/timing"
)

// Option is a setting of Open and OpenPath.
type Option func(*options)

type options struct{ system string }

// WithSystem names the system whose data the store holds from the start,
// so that the migrations and checkpoint of the open carry its label too.
func WithSystem(id string) Option { return func(o *options) { o.system = id } }

// SetSystem names the system whose data the store holds, for the system
// label of its timings in /metrics; unset, the label is empty.
func (s *Store) SetSystem(id string) {
	if s.guard != nil {
		s.guard.system.Store(&id)
	}
}

func (g *engineGuard) systemLabel() string {
	if p := g.system.Load(); p != nil {
		return *p
	}
	return ""
}

// timed records a statement q that started at start and ends now.
func (g *engineGuard) timed(ctx context.Context, q string, args []driver.NamedValue, start time.Time) {
	sys := g.systemLabel()
	timing.Query(ctx, sys, start)
	if timing.HookSet() {
		vals := make([]any, len(args))
		for i, a := range args {
			vals[i] = a.Value
		}
		timing.Hook(ctx, sys, q, vals, time.Since(start))
	}
}

// timedQuery starts timing a query; the returned func ends it, when its
// rows are closed or it failed.
func (g *engineGuard) timedQuery(ctx context.Context, q string, args []driver.NamedValue) func() {
	start := time.Now()
	return func() { g.timed(ctx, q, args, start) }
}

// joined returns a func that runs a, then b.
func joined(a, b func()) func() {
	return func() { a(); b() }
}

// writeLock is the store's write lock. It times each write's wait for the
// lock and its hold, by operation, and counts the writes waiting. Writes
// take it in arrival order; a write that waits with LockContext leaves the
// queue when its context ends.
type writeLock struct {
	once sync.Once
	// ch holds one token while the lock is held.
	ch chan struct{}
	// system is the engine guard's label; nil for a read view.
	system *atomic.Pointer[string]
	// gate is the engine guard's query budget, which yields to the epoch
	// write while it holds the lock; nil for a read view.
	gate *atomic.Pointer[admit.Gate]
	// op and since describe the current holder, and unyield ends its
	// yield; written while held.
	op      string
	since   time.Time
	unyield func()
}

func (l *writeLock) token() chan struct{} {
	l.once.Do(func() { l.ch = make(chan struct{}, 1) })
	return l.ch
}

func (l *writeLock) label() string {
	if l.system == nil {
		return ""
	}
	if p := l.system.Load(); p != nil {
		return *p
	}
	return ""
}

// Lock takes the lock for the write operation op.
func (l *writeLock) Lock(op string) {
	_ = l.LockContext(context.Background(), op)
}

// LockContext takes the lock for op, or gives up when ctx ends first and
// returns its error; a write that gave up leaves nothing waiting behind.
func (l *writeLock) LockContext(ctx context.Context, op string) error {
	sys := l.label()
	waiting := timing.LockWaiters.With(sys)
	start := time.Now()
	waiting.Add(1)
	select {
	case l.token() <- struct{}{}:
	case <-ctx.Done():
		waiting.Add(-1)
		return ctx.Err()
	}
	waiting.Add(-1)
	l.op, l.since = op, time.Now()
	if op == opEpoch && l.gate != nil {
		l.unyield = l.gate.Load().Yield()
	}
	timing.LockWait.With(sys, op).Observe(l.since.Sub(start).Seconds())
	return nil
}

// Unlock releases the lock and records how long it was held.
func (l *writeLock) Unlock() {
	op, since := l.op, l.since
	if l.unyield != nil {
		l.unyield()
		l.unyield = nil
	}
	<-l.token()
	timing.LockHeld.With(l.label(), op).Observe(time.Since(since).Seconds())
}

// Operations of the write lock, the op label of its timings.
const (
	opEpoch         = "epoch"
	opEvents        = "events"
	opAccountNames  = "account_names"
	opSweep         = "retention_sweep"
	opArchiveRecord = "archive_record"
	opRebuild       = "rebuild"
	opClientPods    = "client_pods"
	opFindings      = "check_apply"
	opInventory     = "inventory"
	opLoopback      = "loopback"
	opMutes         = "mutes"
	opAcks          = "acknowledgements"
	opAcceptances   = "acceptances"
	opNotify        = "notify"
)

// System returns the name SetSystem gave the store, or "".
func (s *Store) System() string { return s.mu.label() }
