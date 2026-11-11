// Package admit bounds the database queries that API calls and /metrics
// run at once on one store. A Gate holds the store's query budget: a query
// takes a slot before it runs and gives it back when its rows are closed;
// a query that finds every slot taken waits in a first-come queue until a
// slot frees, its context ends, the store closes or the engine fails.
// Epoch writes, checks and the other background work never take a slot,
// so the budget only limits how much of the database the readers use.
package admit

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/shadone/nats-lens/internal/timing"
)

// ErrOverloaded marks a query refused because the store's query budget
// stayed in use: the queue was full, or the query's deadline passed while
// it waited. The API answers it with resource_exhausted.
var ErrOverloaded = errors.New("query budget in use")

// ErrClosed is returned to queries that wait for, or ask for, a slot of a
// gate whose store has closed.
var ErrClosed = errors.New("store closed")

// QueuePerSlot is how many queries may wait for each slot of the budget;
// past it a query is refused at once. Each waiter holds a pooled DuckDB
// connection while it waits, so the queue bounds those too: 32 at a
// budget of 4, above the 16 to 18 waiters the mixed load reached.
// Proposed, not yet confirmed.
const QueuePerSlot = 8

// OverloadError is a query refused for ErrOverloaded. It wraps the
// context's error when the deadline passed while it waited, so code that
// treats a deadline as running out of time keeps doing so.
type OverloadError struct {
	// QueueFull is set when the queue was full; otherwise the deadline
	// passed while the query waited.
	QueueFull bool
	Limit     int
	Queued    int
	Waited    time.Duration
	cause     error
}

func (e *OverloadError) Error() string {
	if e.QueueFull {
		return fmt.Sprintf("%s: all %d query slots are taken and %d queries already wait", ErrOverloaded, e.Limit, e.Queued)
	}
	return fmt.Sprintf("%s: waited %s for one of %d query slots", ErrOverloaded, e.Waited.Round(time.Millisecond), e.Limit)
}

// Is makes errors.Is(err, ErrOverloaded) true.
func (e *OverloadError) Is(target error) bool { return target == ErrOverloaded }

// Unwrap returns the context's error, nil for a full queue.
func (e *OverloadError) Unwrap() error { return e.cause }

// Outcomes of a request for a slot, the outcome label of
// timing.AdmissionWait.
const (
	OutcomeAdmitted  = "admitted"
	OutcomeDeadline  = "deadline"
	OutcomeCanceled  = "canceled"
	OutcomeQueueFull = "queue_full"
	OutcomeClosed    = "closed"
)

// Gate is the query budget of one store.
type Gate struct {
	limit    int
	maxQueue int
	label    func() string

	mu     sync.Mutex
	active int
	// high are the waiters of /metrics, served before low, the API's.
	high, low list.List
	closed    bool
	// yielding counts the writes the gate yields to; while it is above
	// zero the gate admits up to YieldLimit queries.
	yielding int
	// done is closed by Close.
	done chan struct{}
}

type waiter struct {
	ready   chan struct{}
	granted bool
}

// New returns a gate of limit slots, at least one, whose waiters are
// bounded by limit*QueuePerSlot. label names the store's system for the
// gate's series in /metrics; it is read on every change.
func New(limit int, label func() string) *Gate {
	limit = max(limit, 1)
	if label == nil {
		label = func() string { return "" }
	}
	g := &Gate{limit: limit, maxQueue: limit * QueuePerSlot, label: label, done: make(chan struct{})}
	timing.AdmissionLimit.With(label()).Store(int64(limit))
	return g
}

// YieldLimit is the most queries the gate admits while it yields to a
// write. Proposed, not yet confirmed.
const YieldLimit = 1

// Limit returns the number of slots.
func (g *Gate) Limit() int { return g.limit }

// effective is the number of queries the gate admits now; held with mu
// locked.
func (g *Gate) effective() int {
	if g.yielding > 0 {
		return min(g.limit, YieldLimit)
	}
	return g.limit
}

// Yield makes the gate admit at most YieldLimit queries until the returned
// func is called, for a write that must not compete with the readers:
// queries that already run keep their slots, and new ones wait. Yields
// nest; the gate admits its full limit again once the last one ends.
func (g *Gate) Yield() func() {
	if g == nil {
		return func() {}
	}
	g.mu.Lock()
	g.yielding++
	g.publish()
	g.mu.Unlock()
	return onceFunc(func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.yielding--
		g.grant()
		g.publish()
	})
}

// grant hands free slots to the first waiters; held with mu locked.
func (g *Gate) grant() {
	for g.active < g.effective() {
		w := g.pop()
		if w == nil {
			return
		}
		g.active++
		w.granted = true
		close(w.ready)
	}
}

// pop removes and returns the first waiter, nil for none; held with mu
// locked.
func (g *Gate) pop() *waiter {
	for _, q := range []*list.List{&g.high, &g.low} {
		if e := q.Front(); e != nil {
			return q.Remove(e).(*waiter)
		}
	}
	return nil
}

// Load returns the slots taken and the queries waiting now.
func (g *Gate) Load() (active, queued int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.active, g.high.Len() + g.low.Len()
}

// Idle reports whether a query asking now would take a slot at once.
func (g *Gate) Idle() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.active < g.effective() && g.high.Len()+g.low.Len() == 0
}

// publish writes the gauges; held with mu locked.
func (g *Gate) publish() {
	sys := g.label()
	timing.AdmissionActive.With(sys).Store(int64(g.active))
	timing.AdmissionQueued.With(sys).Store(int64(g.high.Len() + g.low.Len()))
	timing.AdmissionLimit.With(sys).Store(int64(g.effective()))
}

// acquire takes a slot for one query. priority queues it ahead of the
// others; abort, when it closes, ends the wait with abortErr().
func (g *Gate) acquire(ctx context.Context, source string, priority bool, abort <-chan struct{}, abortErr func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	start := time.Now()
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		g.observe(source, OutcomeClosed, start)
		return ErrClosed
	}
	ahead := g.high.Len()
	if !priority {
		ahead += g.low.Len()
	}
	if g.active < g.effective() && ahead == 0 {
		g.active++
		g.publish()
		g.mu.Unlock()
		g.observe(source, OutcomeAdmitted, start)
		return nil
	}
	if queued := g.high.Len() + g.low.Len(); queued >= g.maxQueue {
		g.mu.Unlock()
		g.observe(source, OutcomeQueueFull, start)
		return &OverloadError{QueueFull: true, Limit: g.limit, Queued: queued}
	}
	w := &waiter{ready: make(chan struct{})}
	q := &g.low
	if priority {
		q = &g.high
	}
	e := q.PushBack(w)
	g.publish()
	g.mu.Unlock()

	var err error
	select {
	case <-w.ready:
		g.observe(source, OutcomeAdmitted, start)
		return nil
	case <-ctx.Done():
		err = ctx.Err()
	case <-abort:
		err = abortErr()
	case <-g.done:
		err = ErrClosed
	}
	g.mu.Lock()
	if w.granted {
		// A slot was handed over as the wait ended: pass it on.
		g.mu.Unlock()
		g.release()
	} else {
		q.Remove(e)
		g.publish()
		g.mu.Unlock()
	}
	switch {
	case errors.Is(err, ErrClosed):
		g.observe(source, OutcomeClosed, start)
	case errors.Is(err, context.DeadlineExceeded):
		g.observe(source, OutcomeDeadline, start)
		return &OverloadError{Limit: g.limit, Waited: time.Since(start), cause: err}
	default:
		g.observe(source, OutcomeCanceled, start)
	}
	return err
}

func (g *Gate) observe(source, outcome string, start time.Time) {
	timing.Since(timing.AdmissionWait.With(g.label(), source, outcome), start)
}

// release gives a slot back, handing it to the first waiter while the
// gate admits more queries than run.
func (g *Gate) release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.active--
	g.grant()
	g.publish()
}

// Close refuses every later request and ends every wait with ErrClosed.
// Queries that hold a slot keep it until they finish.
func (g *Gate) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return
	}
	g.closed = true
	close(g.done)
}
