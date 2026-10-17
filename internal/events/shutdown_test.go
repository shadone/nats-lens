package events

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// slowWriter takes perEvent per event and gives up when its context ends,
// like a database under load.
type slowWriter struct {
	mu       sync.Mutex
	perEvent time.Duration
	evs      []Event
}

func (w *slowWriter) WriteEvents(ctx context.Context, evs []Event) error {
	select {
	case <-time.After(time.Duration(len(evs)) * w.perEvent):
	case <-ctx.Done():
		return ctx.Err()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.evs = append(w.evs, evs...)
	return nil
}

// A backlog too large to write before the shutdown deadline is written in
// batches until then, and what is left is counted in a stored
// collector.messages_dropped event instead of vanishing.
func TestShutdownBacklogWrittenOrCounted(t *testing.T) {
	const n = 15000
	w := &slowWriter{perEvent: time.Millisecond}
	c := New(Options{Writer: w, BatchSize: 100000, FlushInterval: time.Hour, Log: quiet()})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = c.Run(ctx); close(done) }()
	record(c, n)
	for len(c.ch) > 0 {
		time.Sleep(10 * time.Millisecond)
	}
	start := time.Now()
	c.SetFinalDeadline(start.Add(2 * time.Second))
	cancel()
	<-done
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("shutdown took %s, past its deadline", took)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	stored, dropped := 0, int64(0)
	for _, e := range w.evs {
		if e.Kind == "collector.messages_dropped" {
			var r struct {
				Dropped int64 `json:"dropped"`
			}
			if err := json.Unmarshal(e.Raw, &r); err != nil {
				t.Fatal(err)
			}
			dropped += r.Dropped
			continue
		}
		stored++
	}
	if stored == 0 || int64(stored)+dropped != n {
		t.Fatalf("stored %d and counted %d dropped of %d", stored, dropped, n)
	}
}
