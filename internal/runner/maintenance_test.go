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
	if pending, err := r.maintain(ctx); err != nil || pending {
		t.Errorf("maintain after a failed fill = %v, %v; want nothing pending and no error", pending, err)
	}
	if len(ran) != 2 {
		t.Errorf("fills run: %v, want both", ran)
	}
	ran = nil
	fatal := &duckdb.Error{Type: duckdb.ErrorTypeFatal, Msg: "fatal"}
	r.fills = []maintenanceFill{fill("first", fatal), fill("second", nil)}
	if _, err := r.maintain(ctx); !store.IsFatal(err) {
		t.Errorf("maintain after a fatal fill error = %v, want the fatal error", err)
	}
	if len(ran) != 1 {
		t.Errorf("fills run after a fatal error: %v, want the first only", ran)
	}
}

// A fill with a large backlog runs for about one interval at a time,
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
	// Seven seconds at 500 ms, the fill taking an interval after each of
	// the first six epochs: without the bound the first fill alone takes
	// three seconds and drops the ticks of the epochs it overlaps.
	if n := epochs(t, r); n < 11 {
		t.Errorf("%d epochs in 7 s at a 500 ms interval, want at least 11", n)
	}
}
