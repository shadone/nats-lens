package runner

import (
	"context"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/timing"
)

// One epoch with checks, and a sweep, record the runner's job timings and
// mark the epoch write's queries, labelled with the store's system.
func TestOnceRecordsJobTimings(t *testing.T) {
	r := newRunner(t)
	r.Store.SetSystem("runner-timing")
	set, err := checks.Load(config.Checks{Timeout: config.Duration(5 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	r.Checks = &checks.Engine{Set: set}
	if _, err := r.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, job := range []string{timing.JobCollect, timing.JobChecks, timing.JobCheckEvaluation, timing.JobRetentionSweep} {
		if timing.JobDuration.Count("runner-timing", job) == 0 {
			t.Errorf("job %s not timed", job)
		}
	}
	for _, src := range []string{timing.SourceEpoch, timing.SourceChecks, timing.SourceRetention} {
		if timing.QueryDuration.Count("runner-timing", src) == 0 {
			t.Errorf("no query of source %s", src)
		}
	}
	if timing.LockHeld.Count("runner-timing", "epoch") != 1 {
		t.Errorf("epoch write held the lock %d times, want 1", timing.LockHeld.Count("runner-timing", "epoch"))
	}
}
