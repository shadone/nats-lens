package main

import (
	"context"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/events"
)

// started is what a system runs once its setup succeeded.
type started struct {
	// capture and stopEvents are nil without event capture.
	capture    *events.Capture
	stopEvents func(deadline time.Time)
	stopK8s    func()
	conn       *eventConn
}

// undoWithin bounds the stop of a capture whose setup failed halfway.
const undoWithin = time.Second

// setup starts the system's event capture, Kubernetes watcher and
// collector connection. On failure it stops what it started.
func (rt *systemRuntime) setup(ctx context.Context, col *collect.Collector) (*started, error) {
	cfg, st, log := rt.cfg, rt.st, rt.log
	s := &started{stopK8s: func() {}}
	undo := func() {
		s.stopK8s()
		if s.stopEvents != nil {
			s.stopEvents(time.Now().Add(undoWithin))
		}
	}
	if cfg.Events.Enabled {
		capture, stop, err := startCapture(ctx, cfg, st, log)
		if err != nil {
			return nil, err
		}
		s.capture, s.stopEvents = capture, stop
		// The Kubernetes watcher writes through the capture, so it stops first.
		if cfg.Kubernetes.Enabled {
			stopK8s, err := startKubernetes(ctx, cfg.Kubernetes, st, capture, log)
			if err != nil {
				undo()
				return nil, err
			}
			s.stopK8s = stopK8s
		}
	}
	conn := newTrackedCollectorConn(cfg.NATS, s.capture, col, rt.status, log)
	if err := conn.open(); err != nil {
		undo()
		return nil, err
	}
	s.conn = conn
	return s, nil
}

// startRetrying runs setup until it succeeds or ctx ends. Each failure
// sets the system's status to connection_closed with the error and is
// logged; the other systems are not affected. The waits are those of a
// redialled connection: 2 seconds, doubling to a minute. ok is false when
// ctx ended first.
func (rt *systemRuntime) startRetrying(ctx context.Context, col *collect.Collector) (s *started, ok bool) {
	b := rt.startBackoff
	if b == (redialBackoff{}) {
		b = defaultBackoff()
	}
	wait := b.first
	for {
		s, err := rt.setup(ctx, col)
		if err == nil {
			return s, true
		}
		rt.status.set(api.CollectionConnectionClosed, err)
		if ctx.Err() != nil {
			return nil, false
		}
		rt.log.Error("system not started; trying again", "err", err, "in", wait)
		select {
		case <-ctx.Done():
			return nil, false
		case <-time.After(wait):
		}
		wait = min(2*wait, b.max)
	}
}
