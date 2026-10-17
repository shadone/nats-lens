package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/shadone/nats-lens/internal/archive"
)

// waitUntil waits for done to close until deadline and reports whether it
// closed. Shutdown uses it so that no step can outlast the stop timeout.
func waitUntil(done <-chan struct{}, deadline time.Time) bool {
	t := time.NewTimer(time.Until(deadline))
	defer t.Stop()
	select {
	case <-done:
		return true
	case <-t.C:
		return false
	}
}

// minStateWait is the least time the last state.json write gets after the
// events finished, even when they used up the shutdown deadline: without
// it the state would miss the last numbers handed out.
const minStateWait = 2 * time.Second

// stateDeadline returns when the last state.json write must end: the
// shutdown deadline, or minStateWait from now when that is later.
func stateDeadline(shutdownBy, now time.Time) time.Time {
	if by := now.Add(minStateWait); by.After(shutdownBy) {
		return by
	}
	return shutdownBy
}

// writeFinalState writes the last state.json once the last events are
// stored, with until stateDeadline to do it.
func writeFinalState(ctx context.Context, arch *archive.Archiver, shutdownBy time.Time, log *slog.Logger) {
	sctx, cancel := context.WithDeadline(context.WithoutCancel(ctx), stateDeadline(shutdownBy, time.Now()))
	defer cancel()
	if err := arch.WriteState(sctx, time.Now()); err != nil {
		log.Error("archive state not written", "err", err)
	}
}
