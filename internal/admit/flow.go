package admit

import (
	"context"
	"sync"
	"sync/atomic"
)

// Flow is one caller's sequence of queries, such as an API call or one
// system's part of a /metrics scrape. A query of a flow that already
// holds a slot of the same gate, one that runs while the flow's earlier
// rows are still open, runs on that slot without queueing: a flow that
// waited for a second slot while holding the first could otherwise wait
// for itself once every slot is taken by such flows. The slot is given
// back when the flow's last query on the gate finishes.
//
// Goroutines that run queries side by side each need a flow of their own
// (Fork), or they share one slot.
type Flow struct {
	parent *Flow
	mu     sync.Mutex
	held   map[*Gate]int
	// overloaded is set when one of the flow's queries, or a forked
	// flow's, was refused for ErrOverloaded.
	overloaded atomic.Bool
}

type flowKey struct{}

// WithFlow returns ctx carrying a new flow.
func WithFlow(ctx context.Context) context.Context {
	return context.WithValue(ctx, flowKey{}, &Flow{})
}

// Fork returns ctx carrying a new flow whose overload is also reported to
// the flow ctx carries.
func Fork(ctx context.Context) context.Context {
	return context.WithValue(ctx, flowKey{}, &Flow{parent: FlowOf(ctx)})
}

// FlowOf returns the flow ctx carries, nil for none.
func FlowOf(ctx context.Context) *Flow {
	f, _ := ctx.Value(flowKey{}).(*Flow)
	return f
}

// Overloaded reports whether a query of the flow, or of a flow forked
// from it, was refused for ErrOverloaded.
func (f *Flow) Overloaded() bool { return f != nil && f.overloaded.Load() }

func (f *Flow) markOverloaded() {
	for ; f != nil; f = f.parent {
		f.overloaded.Store(true)
	}
}

// join adds a query to a slot the flow holds on g, and reports whether
// it did.
func (f *Flow) join(g *Gate) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.held[g] == 0 {
		return false
	}
	f.held[g]++
	return true
}

// add records a slot the flow acquired on g and reports whether it is the
// flow's only one: false when another goroutine of the flow took a slot
// of g at the same time, and the query joined that one instead.
func (f *Flow) add(g *Gate) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.held == nil {
		f.held = map[*Gate]int{}
	}
	f.held[g]++
	return f.held[g] == 1
}

// leave ends one query of the flow on g and reports whether it was the
// last, whose slot goes back to the gate.
func (f *Flow) leave(g *Gate) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.held[g]--
	if f.held[g] > 0 {
		return false
	}
	delete(f.held, g)
	return true
}

// Request says how a query asks for a slot.
type Request struct {
	// Source is the query's timing source, the source label of the
	// gate's wait histogram.
	Source string
	// Priority queues the query ahead of those without it.
	Priority bool
	// Abort, when it closes, ends the wait with AbortErr's error: the
	// engine failed.
	Abort    <-chan struct{}
	AbortErr func() error
}

// Enter takes a slot for one query of the flow ctx carries and returns the
// func that gives it back, to call once when the query's rows are closed.
// A nil gate admits every query.
func (g *Gate) Enter(ctx context.Context, r Request) (func(), error) {
	if g == nil {
		return func() {}, nil
	}
	f := FlowOf(ctx)
	if f != nil && f.join(g) {
		return onceFunc(func() { g.leave(f) }), nil
	}
	if err := g.acquire(ctx, r.Source, r.Priority, r.Abort, r.AbortErr); err != nil {
		if f != nil && isOverload(err) {
			f.markOverloaded()
		}
		return nil, err
	}
	if f == nil {
		return onceFunc(g.release), nil
	}
	if !f.add(g) {
		// Another goroutine of the flow took a slot meanwhile; the query
		// runs on that one, and the flow keeps a single slot.
		g.release()
	}
	return onceFunc(func() { g.leave(f) }), nil
}

func (g *Gate) leave(f *Flow) {
	if f.leave(g) {
		g.release()
	}
}

func isOverload(err error) bool {
	_, ok := err.(*OverloadError)
	return ok
}

func onceFunc(f func()) func() {
	var done atomic.Bool
	return func() {
		if done.CompareAndSwap(false, true) {
			f()
		}
	}
}
