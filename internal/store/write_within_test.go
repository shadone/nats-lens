package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The write's time limit starts once the store lock is held, so a long
// sweep or export holding the lock cannot make the next epoch fail.
func TestWriteLimitStartsAfterTheLock(t *testing.T) {
	s := openMem(t)
	ctx := &lockDeadlineContext{Context: context.Background(), store: s}
	// An expired write limit prevents any database work. Observing when the
	// deadline is read tests the lock boundary without timing a full write.
	_, err := s.WriteSnapshotWithin(ctx, fixtureSnapshot(t, t0), -time.Nanosecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired write limit: %v", err)
	}
	if ctx.reads == 0 || ctx.unlocked {
		t.Fatalf("deadline reads=%d, read before lock=%t", ctx.reads, ctx.unlocked)
	}
}

type lockDeadlineContext struct {
	context.Context
	store    *Store
	reads    int
	unlocked bool
}

func (c *lockDeadlineContext) Deadline() (time.Time, bool) {
	c.reads++
	if c.store.mu.TryLock() {
		c.unlocked = true
		c.store.mu.Unlock()
	}
	return time.Time{}, false
}

func TestWriteLimitStillApplies(t *testing.T) {
	s := openMem(t)
	if _, err := s.WriteSnapshotWithin(context.Background(), fixtureSnapshot(t, t0), time.Nanosecond); err == nil {
		t.Fatal("a write within 1ns succeeded")
	}
}
