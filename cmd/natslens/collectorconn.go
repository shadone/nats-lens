package main

import (
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go"

	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/events"
	"github.com/shadone/nats-lens/internal/natsconn"
)

// newCollectorConn returns the keeper of the collector's system account
// connection. nats.go closes a connection for good after repeated
// authorization errors (revoked or rotated credentials); the keeper dials it
// again with the events connections' backoff and hands every new connection
// to the collector. With capture set, losses, closes and redials are
// recorded as collector events.
func newCollectorConn(c config.NATS, capture *events.Capture, col *collect.Collector, log *slog.Logger) *eventConn {
	return newTrackedCollectorConn(c, capture, col, nil, log)
}

// newTrackedCollectorConn is newCollectorConn that also records the
// connection's state in status, when it is set.
func newTrackedCollectorConn(c config.NATS, capture *events.Capture, col *collect.Collector, status *collectionStatus,
	log *slog.Logger) *eventConn {
	const label = "collector"
	return &eventConn{
		label: label, log: log,
		dial: func() (*nats.Conn, error) {
			var opts []nats.Option
			if capture != nil {
				opts = capture.ConnHandlers(label)
			}
			// Keep running while NATS is unreachable; epochs are skipped
			// until the connection has a server.
			opts = append(opts, nats.RetryOnFailedConnect(true))
			if status != nil {
				opts = status.handlers(opts)
			}
			nc, err := natsconn.Connect(c, log, opts...)
			if err != nil {
				return nil, fmt.Errorf("collector connection: %w", err)
			}
			col.SetConn(nc)
			return nc, nil
		},
		reopened: func(nc *nats.Conn) {
			log.Info("collector connection dialled again", "server", nc.ConnectedServerName())
			if capture != nil {
				capture.RecordReopened(label, nc)
			}
		},
	}
}
