package api

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// summaryResult is what one run of a system's summary produced.
type summaryResult struct {
	sum *v1.SystemSummary
	err error
	// status is the runtime's collection status, when there is one.
	status *CollectionStatus
}

// summaryFlight is one run of a system's summary, shared by the calls
// that ask for the same range while it runs.
type summaryFlight struct {
	key     string
	started time.Time
	cancel  context.CancelFunc
	// ready is closed once res is set: the provider answered, failed, or
	// the run's context ended.
	ready chan struct{}
	res   summaryResult
	// waiters counts the calls waiting on the run, and dead is set once
	// the last of them left and the run was cancelled; both guarded by
	// summaryFlights.mu.
	waiters int
	dead    bool
}

// maxSummaryRuns is the most summary runs per system whose provider has
// not returned; a provider that ignores its context holds no more
// goroutines than that.
const maxSummaryRuns = 2

// summaryFlights are the summary runs whose provider has not returned,
// by system ID.
type summaryFlights struct {
	mu sync.Mutex
	m  map[string][]*summaryFlight
}

// join returns the run of the system for the key, starting one when none
// runs; it returns nil when a run of the system has outlived the timeout,
// or when maxSummaryRuns runs of the system have not returned, and then
// nothing is started. A new run calls start.
func (fs *summaryFlights) join(id, key string, timeout time.Duration, start func(*summaryFlight)) *summaryFlight {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	var same *summaryFlight
	for _, f := range fs.m[id] {
		if time.Since(f.started) >= timeout {
			return nil
		}
		if f.dead {
			continue
		}
		select {
		case <-f.ready:
		default:
			if f.key == key {
				same = f
			}
		}
	}
	if same != nil {
		same.waiters++
		return same
	}
	if len(fs.m[id]) >= maxSummaryRuns {
		return nil
	}
	f := &summaryFlight{key: key, started: time.Now(), ready: make(chan struct{}), waiters: 1}
	if fs.m == nil {
		fs.m = map[string][]*summaryFlight{}
	}
	fs.m[id] = append(fs.m[id], f)
	start(f)
	return f
}

// leave drops a waiter that stops waiting before the result; the last one
// to leave cancels the run.
func (fs *summaryFlights) leave(f *summaryFlight) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	f.waiters--
	if f.waiters == 0 {
		f.dead = true
		f.cancel()
	}
}

// finish removes the run once its provider has returned.
func (fs *summaryFlights) finish(id string, f *summaryFlight) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	list := fs.m[id]
	out := make([]*summaryFlight, 0, len(list))
	for _, g := range list {
		if g != f {
			out = append(out, g)
		}
	}
	if len(out) == 0 {
		delete(fs.m, id)
		return
	}
	fs.m[id] = out
}

// summaryKey names the range and buckets of a normalized request. A live
// range, one whose end the caller left unset, is named by its span to the
// second, so that calls a moment apart share a run.
func summaryKey(req *v1.ListSystemsRequest, live bool) string {
	r := req.GetRange()
	if live {
		span := r.GetEnd().AsTime().Sub(r.GetStart().AsTime()).Round(time.Second)
		return fmt.Sprintf("live/%s/%d", span, req.GetStripBuckets())
	}
	return fmt.Sprintf("%d/%d/%d", r.GetStart().AsTime().UnixNano(), r.GetEnd().AsTime().UnixNano(), req.GetStripBuckets())
}

// runSummary runs the provider for the flight within its context. The
// provider runs in a goroutine of its own, so that one that ignores its
// context does not hold up the result; the flight stays listed until the
// provider returns. A failure other than a cancel is logged once.
func (h *Handler) runSummary(ctx context.Context, sys System, req *v1.ListSystemsRequest, f *summaryFlight) {
	done := make(chan summaryResult, 1)
	go func() {
		defer h.summaryFlights.finish(sys.Ref.GetId(), f)
		defer func() {
			if r := recover(); r != nil {
				done <- summaryResult{err: fmt.Errorf("panic: %v", r)}
			}
		}()
		var r summaryResult
		if sys.Status != nil {
			st := sys.Status()
			r.status = &st
		}
		r.sum, r.err = sys.Provider.SystemSummary(ctx, req)
		if r.err == nil {
			// The worst finding names its account, as every other response does.
			h.withAccountNames(ctx, sys.Provider, r.sum)
		}
		done <- r
	}()
	go func() {
		defer f.cancel()
		var r summaryResult
		select {
		case r = <-done:
		case <-ctx.Done():
			r.err = ctx.Err()
		}
		if r.err != nil && !errors.Is(r.err, context.Canceled) {
			h.warnSummary(sys, r.err)
		}
		f.res = r
		close(f.ready)
	}()
}
