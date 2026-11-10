package runner

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/testcluster"
)

func newRunner(t *testing.T) *Runner {
	t.Helper()
	return newRunnerWindow(t, 100*time.Millisecond)
}

// newRunnerWindow is newRunner with the collector's window for each
// request.
func newRunnerWindow(t *testing.T, window time.Duration) *Runner {
	t.Helper()
	c := testcluster.Start(t, 1)
	st, err := store.OpenPath(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	col := collect.New(c.Connect(0, testcluster.SysUser, testcluster.SysPass), collect.Options{
		Window: window, ConnzLimit: 10, Tracker: collect.NewTracker(time.Hour),
	})
	// Log is left nil on purpose: the runner must fall back to slog.Default.
	return &Runner{Collector: col, Store: st, Interval: 300 * time.Millisecond, Retention: time.Hour}
}

func epochs(t *testing.T, r *Runner) int {
	t.Helper()
	var n int
	if err := r.Store.DB().QueryRow(`SELECT count(*) FROM epochs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Run takes epochs one after another and returns once its context ends.
// The context is cancelled when the second epoch is stored, rather than
// after a fixed time, so a slow machine takes longer instead of fewer
// epochs. The window lets a server on a loaded machine answer every
// request in time; it is waited for in full only by the first discovery.
func TestRunTakesEpochsUntilCancelled(t *testing.T) {
	r := newRunnerWindow(t, 2*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stored atomic.Int32
	r.Stored = func(int64, time.Time) {
		if stored.Add(1) == 2 {
			cancel()
		}
	}
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(time.Minute):
		t.Fatalf("Run did not return; %d epochs stored", stored.Load())
	}
	if n := epochs(t, r); n < 2 {
		t.Fatalf("epochs = %d, want at least 2", n)
	}
	var partial bool
	var missing, errs []any
	if err := r.Store.DB().QueryRow(`SELECT partial, missing_names, errors FROM epochs ORDER BY epoch LIMIT 1`).
		Scan(&partial, &missing, &errs); err != nil || partial {
		t.Fatalf("first epoch partial=%v missing=%v errors=%v err=%v", partial, missing, errs, err)
	}
}

func TestOnceWithCancelledContextStoresNothing(t *testing.T) {
	r := newRunner(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Once(ctx); err == nil {
		t.Fatal("expected the context error")
	}
	if n := epochs(t, r); n != 0 {
		t.Fatalf("epochs = %d", n)
	}
}

// The grace is long next to the 50ms first wait, so that a goroutine
// paused on a loaded machine does not see it run out before that wait.
func TestWriteContextOutlivesShutdownByGraceOnly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	wctx, done := writeContext(ctx, time.Second)
	defer done()
	cancel()
	select {
	case <-wctx.Done():
		t.Fatal("write context ended with the parent; it must get the grace period")
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case <-wctx.Done():
	case <-time.After(30 * time.Second):
		t.Fatal("write context outlived the grace period")
	}
}

// Every stored epoch is checked: each check has a run for it.
func TestOnceRunsChecks(t *testing.T) {
	r := newRunner(t)
	set, err := checks.Load(config.Checks{Timeout: config.Duration(5 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	r.Checks = &checks.Engine{Set: set}
	epoch, err := r.Once(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var n, failed int
	if err := r.Store.DB().QueryRow(`SELECT count(*), count(*) FILTER (WHERE status <> 'ok') FROM check_runs WHERE epoch = ?`, epoch).
		Scan(&n, &failed); err != nil {
		t.Fatal(err)
	}
	if n != len(set.Checks()) || failed != 0 {
		t.Fatalf("check runs for epoch %d: %d (%d not ok), want %d", epoch, n, failed, len(set.Checks()))
	}
}

func TestOnceCallsStored(t *testing.T) {
	r := newRunner(t)
	var got []int64
	var at time.Time
	r.Stored = func(epoch int64, startedAt time.Time) { got, at = append(got, epoch), startedAt }
	before := time.Now().Truncate(time.Microsecond) // the precision snapshot times have
	epoch, err := r.Once(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != epoch {
		t.Fatalf("stored called with %v, want [%d]", got, epoch)
	}
	var stored time.Time
	if err := r.Store.DB().QueryRow(`SELECT started_at FROM epochs WHERE epoch = ?`, epoch).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if at.Before(before) || !at.Equal(stored) {
		t.Fatalf("stored called with start %v, want the epoch's started_at %v", at, stored)
	}
}

// The test waits for the runner's own steps rather than for fixed times, so
// that a loaded machine makes it slower, not wrong: two passes that found
// no connection, then the first epoch stored once there is one.
func TestRunSkipsEpochsWhileNotConnected(t *testing.T) {
	r := newRunner(t)
	var connected atomic.Bool
	skipped := make(chan struct{}, 2)
	r.Connected = func() bool {
		ok := connected.Load()
		if !ok {
			select {
			case skipped <- struct{}{}:
			default:
			}
		}
		return ok
	}
	stored := make(chan struct{}, 1)
	r.Stored = func(int64, time.Time) {
		select {
		case stored <- struct{}{}:
		default:
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	for range 2 {
		select {
		case <-skipped:
		case <-time.After(time.Minute):
			t.Fatal("Run did not check the connection")
		}
	}
	if n := epochs(t, r); n != 0 {
		t.Fatalf("%d epochs while not connected", n)
	}
	connected.Store(true)
	select {
	case <-stored:
	case <-time.After(time.Minute):
		t.Fatal("no epoch once connected")
	}
	cancel()
	<-done
}
