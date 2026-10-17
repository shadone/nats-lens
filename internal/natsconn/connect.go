// Package natsconn opens connections to the monitored system: the system
// account, and the JetStream accounts whose advisories are captured.
package natsconn

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/shadone/nats-lens/internal/config"
)

// Options turns the configuration into nats.go options. Errors and
// reconnects are logged without the URL's user info.
func Options(c config.NATS, log *slog.Logger) []nats.Option {
	opts := []nats.Option{
		nats.Name(c.Name),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil {
				log.Warn("nats disconnected", "err", err)
			}
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Info("nats reconnected", "server", nc.ConnectedServerName())
		}),
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
			log.Warn("nats async error", "err", err)
		}),
	}
	if c.Creds != "" {
		opts = append(opts, nats.UserCredentials(c.Creds))
	}
	if c.User != "" {
		opts = append(opts, nats.UserInfo(c.User, c.Password))
	}
	if c.TLS.CA != "" {
		opts = append(opts, nats.RootCAs(c.TLS.CA))
	}
	if c.TLS.Cert != "" {
		opts = append(opts, nats.ClientCert(c.TLS.Cert, c.TLS.Key))
	}
	return opts
}

// Connect dials the connection c describes. Options in extra come after the
// defaults and replace handlers of the same kind.
func Connect(c config.NATS, log *slog.Logger, extra ...nats.Option) (*nats.Conn, error) {
	nc, err := nats.Connect(c.URL, append(Options(c, log), extra...)...)
	if err != nil {
		return nil, fmt.Errorf("connect to nats: %w", err)
	}
	return nc, nil
}
