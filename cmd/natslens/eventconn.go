package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

// Default redial waits after an events connection closed: nats.go closes
// a connection for good after repeated authorization errors, which a
// credentials rotation can cause, so it is dialled again.
const (
	redialFirst = 2 * time.Second
	redialMax   = time.Minute
	// redialReset is how long a reopened connection must have lasted for
	// the next redial to start from redialFirst again.
	redialReset = 5 * time.Minute
)

// redialBackoff is the wait between redials: first, doubling to max, back
// to first once a reopened connection lasted reset.
type redialBackoff struct {
	first, max, reset time.Duration
}

func defaultBackoff() redialBackoff {
	return redialBackoff{first: redialFirst, max: redialMax, reset: redialReset}
}

// eventConn keeps one connection open: when nats.go closes it, it is
// dialled and subscribed again with backoff until ctx is done. It serves
// the events connections and the collector's own connection.
type eventConn struct {
	label string
	dial  func() (*nats.Conn, error)
	// subscribe runs on every new connection; nil subscribes nothing.
	subscribe func(*nats.Conn) error
	// reopened, when set, is told about every connection opened by keep.
	reopened func(*nats.Conn)
	log      *slog.Logger
	// backoff is the redial wait; the zero value means defaultBackoff().
	backoff redialBackoff

	mu sync.Mutex
	nc *nats.Conn
}

// open dials and subscribes once.
func (e *eventConn) open() error {
	nc, err := e.dial()
	if err != nil {
		return err
	}
	if e.subscribe != nil {
		if err := e.subscribe(nc); err != nil {
			nc.Close()
			return err
		}
	}
	e.mu.Lock()
	e.nc = nc
	e.mu.Unlock()
	return nil
}

func (e *eventConn) current() *nats.Conn {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.nc
}

// keep redials the connection whenever it closes, or dials it when it has
// none because its first dial failed, until ctx is done. The
// wait doubles with every redial and starts over only after a reopened
// connection lasted backoff.reset; a failing dial never resets it.
func (e *eventConn) keep(ctx context.Context, poll time.Duration) {
	b := e.backoff
	if b == (redialBackoff{}) {
		b = defaultBackoff()
	}
	wait := b.first
	var opened time.Time // last successful reopen; zero before any
	failed := false      // the last dial failed and said so
	for {
		for nc := e.current(); nc != nil && !nc.IsClosed(); nc = e.current() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(poll):
			}
		}
		if !opened.IsZero() && time.Since(opened) >= b.reset {
			wait = b.first
		}
		opened = time.Time{}
		// One line per dial: before the first, why it is dialled; after a
		// failed one, its error with the wait before the next.
		switch {
		case failed:
		case e.current() == nil:
			e.log.Warn("connection not open; dialling", "connection", e.label, "in", wait)
		default:
			e.log.Warn("connection closed; dialling again", "connection", e.label, "in", wait)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(2*wait, b.max)
		if err := e.open(); err != nil {
			e.log.Warn("connection not opened; dialling again", "connection", e.label, "err", err, "in", wait)
			failed = true
			continue
		}
		failed = false
		opened = time.Now()
		if e.reopened != nil {
			e.reopened(e.current())
		}
	}
}

// drain stops the subscriptions after the messages already received are
// handed to the capture, and closes the connection, waiting up to timeout.
func (e *eventConn) drain(timeout time.Duration) {
	nc := e.current()
	if nc == nil {
		return
	}
	if err := nc.Drain(); err != nil {
		nc.Close()
		return
	}
	deadline := time.Now().Add(timeout)
	for !nc.IsClosed() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	nc.Close()
}

// drainAll drains every connection at once and waits for all of them, so
// shutdown takes one drain timeout however many accounts there are.
func drainAll(conns []*eventConn, timeout time.Duration) {
	var wg sync.WaitGroup
	for _, ec := range conns {
		wg.Go(func() { ec.drain(timeout) })
	}
	wg.Wait()
}
