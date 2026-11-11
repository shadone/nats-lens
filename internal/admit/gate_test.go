package admit

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/timing"
)

// waitFor polls cond until it holds or 10 seconds pass.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func queued(g *Gate) int { _, q := g.Load(); return q }
func active(g *Gate) int { a, _ := g.Load(); return a }

func api() Request { return Request{Source: timing.SourceAPI} }

// enterAsync asks for a slot in a goroutine and reports the outcome on the
// returned channel.
func enterAsync(ctx context.Context, g *Gate, r Request) <-chan func() {
	out := make(chan func(), 1)
	go func() {
		rel, err := g.Enter(ctx, r)
		if err != nil {
			out <- nil
			return
		}
		out <- rel
	}()
	return out
}

func TestGateAdmitsUpToItsLimitThenQueuesInOrder(t *testing.T) {
	g := New(2, func() string { return "test-fifo" })
	ctx := context.Background()
	a, _ := g.Enter(ctx, api())
	b, _ := g.Enter(ctx, api())
	if active(g) != 2 || g.Idle() {
		t.Fatalf("active %d after two", active(g))
	}
	var waits []<-chan func()
	for i := range 3 {
		waits = append(waits, enterAsync(ctx, g, api()))
		waitFor(t, "waiter queued", func() bool { return queued(g) == i+1 })
	}
	a()
	first := <-waits[0]
	select {
	case <-waits[1]:
		t.Fatal("the second waiter got a slot before a second release")
	default:
	}
	b()
	second := <-waits[1]
	first()
	third := <-waits[2]
	second()
	third()
	if active(g) != 0 || queued(g) != 0 {
		t.Fatalf("active %d, queued %d after every release", active(g), queued(g))
	}
	if got := timing.AdmissionActive.With("test-fifo").Load(); got != 0 {
		t.Fatalf("active gauge %d", got)
	}
}

func TestCanceledWaitersLeaveTheQueue(t *testing.T) {
	g := New(1, func() string { return "test-cancel" })
	hold, _ := g.Enter(context.Background(), api())
	ctx, cancel := context.WithCancel(context.Background())
	const n = QueuePerSlot
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Go(func() {
			_, err := g.Enter(ctx, api())
			errs <- err
		})
	}
	waitFor(t, "all queued", func() bool { return queued(g) == n })
	if got := timing.AdmissionQueued.With("test-cancel").Load(); got != n {
		t.Fatalf("queued gauge %d, want %d", got, n)
	}
	cancel()
	wg.Wait()
	close(errs)
	for err := range errs {
		if !errors.Is(err, context.Canceled) || errors.Is(err, ErrOverloaded) {
			t.Fatalf("a canceled waiter got %v", err)
		}
	}
	if queued(g) != 0 {
		t.Fatalf("%d waiters left after the cancel", queued(g))
	}
	hold()
	if active(g) != 0 {
		t.Fatalf("active %d: a canceled waiter kept a slot", active(g))
	}
}

func TestDeadlineWhileQueuedIsOverload(t *testing.T) {
	g := New(1, nil)
	hold, _ := g.Enter(context.Background(), api())
	defer hold()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	ctx = WithFlow(ctx)
	_, err := g.Enter(ctx, api())
	if !errors.Is(err, ErrOverloaded) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want an overload that is also a deadline", err)
	}
	if !FlowOf(ctx).Overloaded() {
		t.Fatal("the flow was not marked overloaded")
	}
}

func TestFullQueueRefusesAtOnce(t *testing.T) {
	g := New(1, nil)
	hold, _ := g.Enter(context.Background(), api())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for range QueuePerSlot {
		enterAsync(ctx, g, api())
	}
	waitFor(t, "queue full", func() bool { return queued(g) == QueuePerSlot })
	_, err := g.Enter(context.Background(), api())
	var oe *OverloadError
	if !errors.As(err, &oe) || !oe.QueueFull || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want a full queue", err)
	}
	cancel()
	waitFor(t, "queue empty", func() bool { return queued(g) == 0 })
	hold()
}

func TestCloseEndsWaitsAndRefusesLaterQueries(t *testing.T) {
	g := New(1, nil)
	hold, _ := g.Enter(context.Background(), api())
	errs := make(chan error, 5)
	for range 5 {
		go func() { _, err := g.Enter(context.Background(), api()); errs <- err }()
	}
	waitFor(t, "queued", func() bool { return queued(g) == 5 })
	g.Close()
	for range 5 {
		if err := <-errs; !errors.Is(err, ErrClosed) {
			t.Fatalf("a waiter got %v at close", err)
		}
	}
	if _, err := g.Enter(context.Background(), api()); !errors.Is(err, ErrClosed) {
		t.Fatalf("after close: %v", err)
	}
	hold()
	if active(g) != 0 {
		t.Fatalf("active %d", active(g))
	}
}

func TestAbortEndsTheWait(t *testing.T) {
	g := New(1, nil)
	hold, _ := g.Enter(context.Background(), api())
	defer hold()
	abort := make(chan struct{})
	fatal := errors.New("engine failed")
	errs := make(chan error, 1)
	go func() {
		_, err := g.Enter(context.Background(), Request{Source: timing.SourceAPI, Abort: abort, AbortErr: func() error { return fatal }})
		errs <- err
	}()
	waitFor(t, "queued", func() bool { return queued(g) == 1 })
	close(abort)
	if err := <-errs; !errors.Is(err, fatal) {
		t.Fatalf("got %v", err)
	}
}

func TestPriorityGoesFirst(t *testing.T) {
	g := New(1, nil)
	hold, _ := g.Enter(context.Background(), api())
	low := enterAsync(context.Background(), g, api())
	waitFor(t, "low queued", func() bool { return queued(g) == 1 })
	high := enterAsync(context.Background(), g, Request{Source: timing.SourceMetrics, Priority: true})
	waitFor(t, "high queued", func() bool { return queued(g) == 2 })
	hold()
	rel := <-high
	select {
	case <-low:
		t.Fatal("the API query went before /metrics")
	default:
	}
	rel()
	(<-low)()
}

func TestAFlowRunsNestedQueriesOnItsSlot(t *testing.T) {
	g := New(1, nil)
	ctx := WithFlow(context.Background())
	outer, err := g.Enter(ctx, api())
	if err != nil {
		t.Fatal(err)
	}
	other := enterAsync(context.Background(), g, api())
	waitFor(t, "other queued", func() bool { return queued(g) == 1 })
	inner, err := g.Enter(ctx, api())
	if err != nil {
		t.Fatalf("a nested query waited: %v", err)
	}
	outer()
	select {
	case <-other:
		t.Fatal("the slot went back while the flow's nested query ran")
	default:
	}
	inner()
	inner()
	(<-other)()
	if active(g) != 0 {
		t.Fatalf("active %d", active(g))
	}
}

func TestForkedFlowsTakeSlotsOfTheirOwn(t *testing.T) {
	g := New(1, nil)
	parent := WithFlow(context.Background())
	a := Fork(parent)
	rel, _ := g.Enter(a, api())
	ctx, cancel := context.WithTimeout(Fork(parent), 20*time.Millisecond)
	defer cancel()
	if _, err := g.Enter(ctx, api()); !errors.Is(err, ErrOverloaded) {
		t.Fatalf("a forked flow shared its sibling's slot: %v", err)
	}
	if !FlowOf(parent).Overloaded() {
		t.Fatal("the fork's overload did not reach the parent")
	}
	rel()
}

// TestCancellationStorm asks for slots from many goroutines, canceling
// most of them at random points; the gate must end empty.
func TestCancellationStorm(t *testing.T) {
	g := New(3, func() string { return "test-storm" })
	var wg sync.WaitGroup
	for i := range 400 {
		wg.Go(func() {
			r := rand.New(rand.NewPCG(uint64(i), 7))
			ctx, cancel := context.WithCancel(WithFlow(context.Background()))
			defer cancel()
			if r.IntN(3) > 0 {
				delay := time.Duration(r.IntN(5000)) * time.Microsecond
				go func() {
					time.Sleep(delay)
					cancel()
				}()
			}
			rel, err := g.Enter(ctx, api())
			if err != nil {
				return
			}
			if r.IntN(2) == 0 {
				nested, err := g.Enter(ctx, api())
				if err == nil {
					nested()
				}
			}
			time.Sleep(time.Duration(r.IntN(200)) * time.Microsecond)
			rel()
		})
	}
	wg.Wait()
	if a, q := g.Load(); a != 0 || q != 0 {
		t.Fatalf("after the storm: active %d, queued %d", a, q)
	}
	if timing.AdmissionActive.With("test-storm").Load() != 0 || timing.AdmissionQueued.With("test-storm").Load() != 0 {
		t.Fatal("gauges not back to 0")
	}
}

func TestNilGateAdmitsEverything(t *testing.T) {
	var g *Gate
	rel, err := g.Enter(context.Background(), api())
	if err != nil {
		t.Fatal(err)
	}
	rel()
}

func TestYieldAdmitsOneQueryUntilItEnds(t *testing.T) {
	g := New(3, func() string { return "test-yield" })
	ctx := context.Background()
	running, _ := g.Enter(ctx, api())
	other, _ := g.Enter(ctx, api())
	end := g.Yield()
	if got := timing.AdmissionLimit.With("test-yield").Load(); got != YieldLimit {
		t.Fatalf("limit gauge %d while yielding", got)
	}
	// Queries that run keep their slots; a new one waits while two run.
	waiting := enterAsync(ctx, g, api())
	waitFor(t, "queued", func() bool { return queued(g) == 1 })
	running()
	select {
	case <-waiting:
		t.Fatal("admitted while yielding with one query still running")
	default:
	}
	other()
	first := <-waiting
	second := enterAsync(ctx, g, api())
	waitFor(t, "second queued", func() bool { return queued(g) == 1 })
	nested := g.Yield()
	end()
	select {
	case <-second:
		t.Fatal("admitted while a nested yield holds")
	default:
	}
	nested()
	(<-second)()
	first()
	if a, q := g.Load(); a != 0 || q != 0 || !g.Idle() {
		t.Fatalf("active %d, queued %d after the yields", a, q)
	}
	if got := timing.AdmissionLimit.With("test-yield").Load(); got != 3 {
		t.Fatalf("limit gauge %d after the yields", got)
	}
}
