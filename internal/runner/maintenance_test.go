package runner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/duckdb/duckdb-go/v2"

	"github.com/shadone/nats-lens/internal/store"
)

// A fill that fails without a fatal error does not keep the next fill
// from running, and maintenance returns no error; a fatal error from a
// fill is returned and stops the fills.
func TestMaintainFillErrors(t *testing.T) {
	ctx := context.Background()
	r := newRunner(t)
	var ran []string
	fill := func(name string, err error) maintenanceFill {
		return maintenanceFill{name, func(context.Context, time.Time) (int64, bool, error) {
			ran = append(ran, name)
			return 0, false, err
		}}
	}
	r.fills = []maintenanceFill{fill("first", errors.New("disk full")), fill("second", nil)}
	if pending, err := r.maintain(ctx, time.Now(), 0); err != nil || len(pending) > 0 {
		t.Errorf("maintain after a failed fill = %v, %v; want nothing pending and no error", pending, err)
	}
	if len(ran) != 2 {
		t.Errorf("fills run: %v, want both", ran)
	}
	ran = nil
	fatal := &duckdb.Error{Type: duckdb.ErrorTypeFatal, Msg: "fatal"}
	r.fills = []maintenanceFill{fill("first", fatal), fill("second", nil)}
	if _, err := r.maintain(ctx, time.Now(), 0); !store.IsFatal(err) {
		t.Errorf("maintain after a fatal fill error = %v, want the fatal error", err)
	}
	if len(ran) != 1 {
		t.Errorf("fills run after a fatal error: %v, want the first only", ran)
	}
}

// A fill with a large backlog runs for what is left of each interval,
// after each epoch, until nothing is left, and the runner takes its
// epochs in between instead of waiting for the whole fill.
func TestFillBacklogLeavesEpochsRunning(t *testing.T) {
	r := newRunner(t)
	r.Interval = 500 * time.Millisecond
	var mu sync.Mutex
	var calls []int // epochs stored when each fill call began
	backlog := 6
	r.fills = []maintenanceFill{{"backlog", func(_ context.Context, until time.Time) (int64, bool, error) {
		mu.Lock()
		calls = append(calls, epochs(t, r))
		left := backlog > 0
		if left {
			backlog--
		}
		mu.Unlock()
		if !left {
			return 0, false, nil
		}
		// Each batch of the backlog uses the fill's whole time.
		time.Sleep(time.Until(until))
		return 120, backlog > 0, nil
	}}}
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if backlog != 0 {
		t.Fatalf("%d calls of the backlog left after %v", backlog, 7*time.Second)
	}
	// The first call runs before the first epoch, each later one after an
	// epoch of its own.
	for i := 1; i < 7 && i < len(calls); i++ {
		if calls[i] <= calls[i-1] {
			t.Errorf("fill calls began at %v stored epochs: no epoch between calls %d and %d", calls, i-1, i)
		}
	}
	// Seven seconds at 500 ms is 14 ticks; the bound is generous, as this
	// is the one test of the real ticker. TestFillUsesWhatIsLeftOfTheInterval
	// checks the deadlines exactly.
	if n := epochs(t, r); n < 10 {
		t.Errorf("%d epochs in 7 s at a 500 ms interval, want at least 10", n)
	}
}

// fillHarness runs a runner whose ticks the test sends and whose fills
// block until the test releases them.
type fillHarness struct {
	r        *Runner
	ticks    chan time.Time
	calls    chan fillCall
	release  chan bool // the fill's more
	done     chan error
	cancel   context.CancelFunc
	interval time.Duration
	clock    *fakeClock
}

// fakeClock is the runner's clock in the harness: the first read after
// set gives the time set, as the iteration's start, and every later read
// that time plus epoch, as if the epoch's collect and write had taken it.
type fakeClock struct {
	mu    sync.Mutex
	at    time.Time
	read  bool
	epoch time.Duration
}

func (c *fakeClock) set(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at, c.read = at, false
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.read {
		c.read = true
		return c.at
	}
	return c.at.Add(c.epoch)
}

// fakeStart is the fake clock's time when the runner starts.
var fakeStart = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

// tickAt sets the clock to at and sends the tick.
func (h *fillHarness) tickAt(at time.Time) {
	h.clock.set(at)
	h.ticks <- at
}

type fillCall struct {
	what   string
	epochs int
	until  time.Time
}

func startFillHarness(t *testing.T, fills ...string) *fillHarness {
	t.Helper()
	h := &fillHarness{r: newRunner(t), ticks: make(chan time.Time), calls: make(chan fillCall),
		release: make(chan bool), done: make(chan error, 1), interval: 10 * time.Second,
		// The epoch's time stays below the reserve's floor of a tenth of
		// the interval, so the deadline is the same whichever read of
		// the clock the iteration's start is.
		clock: &fakeClock{epoch: 500 * time.Millisecond}}
	h.clock.set(fakeStart)
	h.r.now = h.clock.now
	h.r.Interval = h.interval
	h.r.tick = func(time.Duration) (<-chan time.Time, func()) { return h.ticks, func() {} }
	for _, name := range fills {
		h.r.fills = append(h.r.fills, maintenanceFill{name, func(ctx context.Context, until time.Time) (int64, bool, error) {
			select {
			case h.calls <- fillCall{name, epochs(t, h.r), until}:
			case <-ctx.Done():
				return 0, false, ctx.Err()
			}
			if name == "failing" {
				return 0, false, errors.New("disk full")
			}
			select {
			case more := <-h.release:
				return 1, more, nil
			case <-ctx.Done():
				return 0, false, ctx.Err()
			}
		}})
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go func() { h.done <- h.r.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-h.done
	})
	return h
}

func (h *fillHarness) call(t *testing.T) fillCall {
	t.Helper()
	select {
	case c := <-h.calls:
		return c
	case <-time.After(30 * time.Second):
		t.Fatal("no fill call")
	}
	return fillCall{}
}

// The fill after each epoch has the time left of that epoch's interval,
// less a reserve, so it ends before the next tick: one epoch per tick,
// the k-th fill call after the k-th epoch, and no tick lost while the
// fill keeps to its time. The fill continues after each epoch until it
// reports that nothing is left, and then no longer runs.
func TestFillUsesWhatIsLeftOfTheInterval(t *testing.T) {
	h := startFillHarness(t, "backlog")
	began := fakeStart
	c := h.call(t)
	if c.epochs != 0 || c.until.After(began.Add(h.interval)) {
		t.Fatalf("the fill at start saw %d epochs and runs until %v, %v after the start; want none and at most an interval",
			c.epochs, c.until, c.until.Sub(began))
	}
	h.release <- true
	for k := 1; k <= 4; k++ {
		c := h.call(t)
		if c.epochs != k {
			t.Fatalf("fill call %d after %d epochs, want %d", k, c.epochs, k)
		}
		if c.until.After(began.Add(h.interval)) {
			t.Fatalf("fill call %d runs until %v after its epoch's tick, past the next tick at %v",
				k, c.until.Sub(began), h.interval)
		}
		h.release <- k < 4
		began = began.Add(h.interval)
		h.tickAt(began)
	}
	// Nothing is left: the next epoch is taken and no fill runs.
	deadline := time.Now().Add(30 * time.Second)
	for epochs(t, h.r) < 5 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := epochs(t, h.r); n != 5 {
		t.Fatalf("%d epochs after four ticks, want 5", n)
	}
	select {
	case c := <-h.calls:
		t.Fatalf("a fill ran after it reported nothing left: %+v", c)
	case <-time.After(200 * time.Millisecond):
	}
}

// A fill that fails is not run again after each epoch while another fill
// still has epochs to fill: it waits for the next retention sweep.
func TestFailingFillWaitsForTheSweep(t *testing.T) {
	h := startFillHarness(t, "failing", "backlog")
	if c := h.call(t); c.what != "failing" {
		t.Fatalf("first fill call %+v, want the failing fill", c)
	}
	if c := h.call(t); c.what != "backlog" {
		t.Fatalf("second fill call %+v, want the backlog", c)
	}
	h.release <- true
	for k := 1; k <= 3; k++ {
		c := h.call(t)
		if c.what != "backlog" {
			t.Fatalf("after epoch %d the %s fill ran, want only the backlog", k, c.what)
		}
		h.release <- true
		h.tickAt(fakeStart.Add(time.Duration(k) * h.interval))
	}
}
