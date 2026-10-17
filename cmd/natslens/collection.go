package main

import (
	"errors"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/shadone/nats-lens/internal/api"
)

// collectionStatus follows one system's collector: its connection's state
// and the newest stored epoch. The API reads it as the system's Status.
type collectionStatus struct {
	now func() time.Time

	mu sync.Mutex
	s  api.CollectionStatus
}

// newCollectionStatus starts in the starting state; now nil means
// time.Now.
func newCollectionStatus(now func() time.Time) *collectionStatus {
	if now == nil {
		now = time.Now
	}
	return &collectionStatus{now: now, s: api.CollectionStatus{State: api.CollectionStarting, Since: now()}}
}

func (c *collectionStatus) get() api.CollectionStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.s
}

// set records a state and, when err is set, the newest error. Since moves
// only when the state changes. A connection closed for good stays
// connection_closed while it is dialled again and has no server yet, so
// that the redials do not hide the close.
func (c *collectionStatus) set(state api.CollectionState, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.s.LastError = err.Error()
	}
	if state == api.CollectionNotConnected && c.s.State == api.CollectionConnectionClosed {
		return
	}
	if state != c.s.State {
		c.s.State, c.s.Since = state, c.now()
	}
}

// epoch records the start of the newest stored epoch.
func (c *collectionStatus) epoch(startedAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.s.LastEpochAt = startedAt
}

// handlers returns opts with the connection handlers in it extended to
// also record the state; the handlers opts sets still run first.
func (c *collectionStatus) handlers(opts []nats.Option) []nats.Option {
	var base nats.Options
	for _, o := range opts {
		_ = o(&base)
	}
	return append(opts,
		nats.ConnectHandler(func(nc *nats.Conn) {
			if base.ConnectedCB != nil {
				base.ConnectedCB(nc)
			}
			c.set(api.CollectionCollecting, nil)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			if base.ReconnectedCB != nil {
				base.ReconnectedCB(nc)
			}
			c.set(api.CollectionCollecting, nil)
		}),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			if base.DisconnectedErrCB != nil {
				base.DisconnectedErrCB(nc, err)
			}
			if nc.IsClosed() && err == nil {
				return
			}
			if err == nil {
				err = errors.New("connection lost")
			}
			c.set(api.CollectionNotConnected, err)
		}),
		nats.ReconnectErrHandler(func(nc *nats.Conn, err error) {
			if base.ReconnectErrCB != nil {
				base.ReconnectErrCB(nc, err)
			}
			c.set(api.CollectionNotConnected, err)
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			if base.ClosedCB != nil {
				base.ClosedCB(nc)
			}
			// As for the capture: a close without an error, or with only
			// a refused publish, is a deliberate one.
			if err := nc.LastError(); err != nil && !errors.Is(err, nats.ErrPermissionViolation) {
				c.set(api.CollectionConnectionClosed, err)
			}
		}),
	)
}
