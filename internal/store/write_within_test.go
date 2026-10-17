package store

import (
	"context"
	"testing"
	"time"
)

// The write's time limit starts once the store lock is held, so a long
// sweep or export holding the lock cannot make the next epoch fail.
func TestWriteLimitStartsAfterTheLock(t *testing.T) {
	s := openMem(t)
	s.mu.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := s.WriteSnapshotWithin(context.Background(), fixtureSnapshot(t, t0), 300*time.Millisecond)
		done <- err
	}()
	time.Sleep(600 * time.Millisecond)
	s.mu.Unlock()
	if err := <-done; err != nil {
		t.Fatalf("write after waiting for the lock: %v", err)
	}
}

func TestWriteLimitStillApplies(t *testing.T) {
	s := openMem(t)
	if _, err := s.WriteSnapshotWithin(context.Background(), fixtureSnapshot(t, t0), time.Nanosecond); err == nil {
		t.Fatal("a write within 1ns succeeded")
	}
}
