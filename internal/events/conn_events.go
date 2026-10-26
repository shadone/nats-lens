package events

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

// DefaultNotConnectedAfter is how long a collector connection may be
// without a server before it is recorded as not connected.
const DefaultNotConnectedAfter = 30 * time.Second

// notConnectedRepeat is how often collector.not_connected is recorded
// again while the connection stays without a server.
const notConnectedRepeat = 10 * time.Minute

// connWatch is the state the handlers of one connection share.
type connWatch struct {
	c     *Capture
	label string
	log   *slog.Logger

	mu       sync.Mutex
	server   string    // last server the connection was on
	lastErr  error     // last connect or reconnect error
	loggedAt time.Time // last reconnect error logged
	timer    *time.Timer
	// redial is a connection dialled again after nats.go closed the one
	// before, or after a first dial that opened none: its first connect is
	// a reconnect.
	redial bool
	// neverOpened is a redial whose earlier dials opened no connection.
	neverOpened bool
}

// setServer records the connected server and returns the last known one;
// nats.go no longer reports it once the connection is lost.
func (w *connWatch) setServer(nc *nats.Conn) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if name := nc.ConnectedServerName(); name != "" {
		w.server = name
	}
	return w.server
}

// arm starts the not-connected timer unless it runs already.
func (w *connWatch) arm(nc *nats.Conn) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer == nil {
		w.timer = time.AfterFunc(w.c.o.NotConnectedAfter, func() { w.fire(nc) })
	}
}

// disarm stops the not-connected timer: the connection has a server.
func (w *connWatch) disarm() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	w.lastErr = nil
}

// fire records that the connection has had no server for a while, and
// does so again every notConnectedRepeat until it connects.
func (w *connWatch) fire(nc *nats.Conn) {
	if nc.IsConnected() || nc.IsClosed() {
		return
	}
	w.mu.Lock()
	reason := "no server reachable"
	if w.lastErr != nil {
		reason = w.lastErr.Error()
	}
	if w.timer != nil {
		w.timer.Reset(notConnectedRepeat)
	}
	w.mu.Unlock()
	w.log.Error("nats connection has no server", "err", reason)
	w.c.Record(w.c.collectorEvent("collector.not_connected", w.label, nc, reason,
		fmt.Sprintf("collector connection %s has no server: %s; events are not captured", w.label, reason)))
}

// reconnectFailed remembers the error and logs it at most every
// notConnectedRepeat, as nats.go retries every few seconds.
func (w *connWatch) reconnectFailed(nc *nats.Conn, err error) {
	w.mu.Lock()
	w.lastErr = err
	logNow := time.Since(w.loggedAt) >= notConnectedRepeat
	if logNow {
		w.loggedAt = time.Now()
	}
	w.mu.Unlock()
	if logNow {
		w.log.Warn("nats connect attempt failed", "err", err)
	}
	w.arm(nc)
}

// ConnHandlers returns nats.go options that log the connection's state
// changes and record them as collector events. label names the connection
// (collector, sys, or the account label). They replace the logging
// handlers of natsconn.Options, so they log as well. A deliberate Close or
// Drain is neither logged as a loss nor recorded. A connection without a
// server for NotConnectedAfter, also one that never connected, is recorded
// as collector.not_connected.
func (c *Capture) ConnHandlers(label string) []nats.Option {
	return c.connHandlers(label, false, false)
}

// RedialHandlers are ConnHandlers for a connection dialled again after
// nats.go closed the one before: its first connect, whenever it comes, is
// recorded once as collector.reconnected; a redial that never connects
// records none.
func (c *Capture) RedialHandlers(label string) []nats.Option {
	return c.connHandlers(label, true, false)
}

// FirstOpenHandlers are RedialHandlers for a connection whose earlier
// dials opened no connection (see NotOpened): its first connect is
// recorded as collector.reconnected with the reason "connected after the
// first dial failed".
func (c *Capture) FirstOpenHandlers(label string) []nats.Option {
	return c.connHandlers(label, true, true)
}

func (c *Capture) connHandlers(label string, redial, neverOpened bool) []nats.Option {
	w := &connWatch{c: c, label: label, log: c.o.Log.With("connection", label), redial: redial, neverOpened: neverOpened}
	return []nats.Option{
		nats.ConnectHandler(func(nc *nats.Conn) {
			to := w.setServer(nc)
			// With RetryOnFailedConnect the first connect after failed
			// attempts comes here, not to the reconnect handler.
			w.mu.Lock()
			failed, redial, neverOpened := w.lastErr != nil || w.timer != nil, w.redial, w.neverOpened
			w.redial = false
			w.mu.Unlock()
			w.disarm()
			if redial && neverOpened {
				w.log.Info("nats connected after the first dial failed", "server", to)
				c.Record(c.collectorEvent("collector.reconnected", label, nc, "connected after the first dial failed",
					fmt.Sprintf("collector connection %s connected to %s after its first dial failed", label, orDash(to))))
				return
			}
			if redial {
				w.log.Info("nats connected after the connection was dialled again", "server", to)
				c.Record(c.collectorEvent("collector.reconnected", label, nc, "dialled again after the connection closed",
					fmt.Sprintf("collector connection %s dialled again after it closed, connected to %s", label, orDash(to))))
				return
			}
			if failed {
				w.log.Info("nats connected after failed attempts", "server", to)
				c.Record(c.collectorEvent("collector.reconnected", label, nc, "",
					fmt.Sprintf("collector connection %s connected to %s after failed attempts", label, orDash(to))))
			}
		}),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			if nc.IsClosed() && err == nil {
				return
			}
			reason := "connection lost"
			if err != nil {
				reason = err.Error()
			}
			lost := w.setServer(nc)
			w.log.Warn("nats disconnected", "server", lost, "err", err)
			ev := c.collectorEvent("collector.disconnected", label, nc, reason,
				fmt.Sprintf("collector connection %s lost server %s: %s", label, orDash(lost), reason))
			ev.ServerName = lost
			c.Record(ev)
			w.arm(nc)
		}),
		nats.ReconnectErrHandler(w.reconnectFailed),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			to := w.setServer(nc)
			w.disarm()
			w.log.Info("nats reconnected", "server", to)
			c.Record(c.collectorEvent("collector.reconnected", label, nc, "",
				fmt.Sprintf("collector connection %s reconnected to %s", label, orDash(to))))
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			w.disarm()
			// LastError keeps the latest transient error too, such as a
			// refused publish, which never closes a connection; a close
			// with only such an error is a deliberate one.
			err := nc.LastError()
			if err == nil || errors.Is(err, nats.ErrPermissionViolation) {
				return
			}
			w.log.Error("nats connection closed", "err", err)
			c.Record(c.collectorEvent("collector.connection_closed", label, nc, err.Error(),
				fmt.Sprintf("collector connection %s closed: %v", label, err)))
		}),
		nats.ErrorHandler(func(nc *nats.Conn, sub *nats.Subscription, err error) {
			// A refused account name lookup is handled by resolveAccount
			// and is no subscription problem.
			if c.lookupDenied(label, err) {
				c.denyResolve(label)
				return
			}
			w.log.Warn("nats async error", "err", err)
			kind := "collector.subscription_error"
			if errors.Is(err, nats.ErrSlowConsumer) {
				kind = "collector.messages_dropped"
			}
			subj := ""
			if sub != nil {
				subj = sub.Subject
			}
			c.Record(c.collectorEvent(kind, label, nc, err.Error(),
				fmt.Sprintf("collector connection %s subscription %s: %v", label, orDash(subj), err)))
		}),
	}
}

// NotOpened records that the connection label could not be opened, as
// collector.not_connected, so that its events count as not captured until
// it connects; a connection dialled with FirstOpenHandlers then records
// collector.reconnected.
func (c *Capture) NotOpened(label string, err error) {
	reason := err.Error()
	c.Record(c.collectorEvent("collector.not_connected", label, nil, reason,
		fmt.Sprintf("collector connection %s not opened: %s; events are not captured", label, reason)))
}

func (c *Capture) collectorEvent(kind, label string, nc *nats.Conn, reason, summary string) Event {
	now := c.now().UTC()
	server := ""
	if nc != nil {
		server = nc.ConnectedServerName()
	}
	raw := mustJSON(map[string]string{"connection": label, "reason": reason})
	return Event{Time: now, ReceivedAt: now, Source: SourceCollector, Kind: kind, ServerName: server,
		Reason: reason, Summary: summary, Raw: raw}
}
