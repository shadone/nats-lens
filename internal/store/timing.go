package store

import (
	"context"
	"database/sql/driver"
	"sync"
	"sync/atomic"
	"time"

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
// lock and its hold, by operation, and counts the writes waiting.
type writeLock struct {
	m sync.Mutex
	// system is the engine guard's label; nil for a read view.
	system *atomic.Pointer[string]
	// op and since describe the current holder; written under m.
	op    string
	since time.Time
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
	sys := l.label()
	waiting := timing.LockWaiters.With(sys)
	start := time.Now()
	waiting.Add(1)
	l.m.Lock()
	waiting.Add(-1)
	l.op, l.since = op, time.Now()
	timing.LockWait.With(sys, op).Observe(l.since.Sub(start).Seconds())
}

// Unlock releases the lock and records how long it was held.
func (l *writeLock) Unlock() {
	op, since := l.op, l.since
	l.m.Unlock()
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
