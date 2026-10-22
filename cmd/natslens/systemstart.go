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
	// stopAccounts closes the account connections opened for micro
	// services without event capture; nil when there are none.
	stopAccounts func()
}

// abandon stops what a setup started that is no longer wanted, within
// undoWithin.
func (s *started) abandon() {
	if s.stopAccounts != nil {
		s.stopAccounts()
	}
	if s.stopK8s != nil {
		s.stopK8s()
	}
	if s.stopEvents != nil {
		s.stopEvents(time.Now().Add(undoWithin))
	}
	if s.conn != nil {
		if nc := s.conn.current(); nc != nil {
			nc.Close()
		}
	}
}

// attempt runs one setup in its own goroutine and returns when it ends or
// ctx ends, whichever is first. An attempt cut short by ctx finishes on
// its own; what it started is then stopped and its result dropped.
func (rt *systemRuntime) attempt(ctx context.Context, col *collect.Collector) (*started, error) {
	type result struct {
		s   *started
		err error
	}
	ch := make(chan result, 1)
	go func() {
		s, err := setupRuntime(rt, ctx, col)
		ch <- result{s, err}
	}()
	select {
	case r := <-ch:
		return r.s, r.err
	case <-ctx.Done():
		go func() {
			if r := <-ch; r.s != nil {
				r.s.abandon()
			}
		}()
		return nil, ctx.Err()
	}
}

// setupRuntime runs one setup attempt; a variable so that tests can slow
// it.
var setupRuntime = (*systemRuntime).setup

// undoWithin bounds the stop of a capture whose setup failed halfway.
const undoWithin = time.Second

// setup starts the system's event capture, Kubernetes watcher and
// collector connection. On failure it stops what it started.
func (rt *systemRuntime) setup(ctx context.Context, col *collect.Collector) (*started, error) {
	cfg, st, log := rt.cfg, rt.st, rt.log
	s := &started{stopK8s: func() {}}
	undo := s.abandon
	var micro []collect.AccountConn
	if cfg.Events.Enabled {
		capture, stop, conns, err := startCapture(ctx, cfg, st, log)
		if err != nil {
			return nil, err
		}
		s.capture, s.stopEvents, micro = capture, stop, conns
		// The Kubernetes watcher writes through the capture, so it stops first.
		if cfg.Kubernetes.Enabled {
			stopK8s, err := startKubernetes(ctx, cfg.Kubernetes, st, capture, log)
			if err != nil {
				undo()
				return nil, err
			}
			s.stopK8s = stopK8s
		}
	} else {
		// Without event capture the account connections are opened here,
		// only to ask the accounts' micro services.
		conns, stop, err := startAccountConns(ctx, cfg, log)
		if err != nil {
			undo()
			return nil, err
		}
		micro, s.stopAccounts = conns, stop
	}
	conn := newTrackedCollectorConn(cfg.NATS, s.capture, col, rt.status, log)
	if err := conn.open(); err != nil {
		undo()
		return nil, err
	}
	s.conn = conn
	col.SetAccountConns(micro)
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
		s, err := rt.attempt(ctx, col)
		if err == nil {
			return s, true
		}
		if ctx.Err() != nil {
			return nil, false
		}
		rt.status.set(api.CollectionConnectionClosed, err)
		rt.log.Error("system not started; trying again", "err", err, "in", wait)
		select {
		case <-ctx.Done():
			return nil, false
		case <-time.After(wait):
		}
		wait = min(2*wait, b.max)
	}
}
