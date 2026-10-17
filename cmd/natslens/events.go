package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/events"
	"github.com/shadone/nats-lens/internal/natsconn"
	"github.com/shadone/nats-lens/internal/store"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// startEvents records the start and any capture gap, opens the event
// connections and starts the capture. Call the returned stop once to shut
// the capture down: it drains the connections in parallel for at most one
// flush interval, then writes the remaining events until deadline.
func startEvents(ctx context.Context, cfg config.Config, st *store.Store, log *slog.Logger) (*events.Capture, func(deadline time.Time), error) {
	last, err := st.LastActivity(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("read last activity: %w", err)
	}
	if err := st.WriteEvents(ctx, events.StartEvents(last, time.Now(), version)); err != nil {
		return nil, nil, fmt.Errorf("record start: %w", err)
	}
	capture := events.New(events.Options{
		Writer: st, APIAudit: cfg.Events.APIAudit, AccountConnections: cfg.Events.AccountConnections,
		FlushInterval: cfg.Events.FlushInterval.D(),
		BatchSize:     cfg.Events.BatchSize, Log: log,
		Churn: events.ChurnOptions{Collapse: cfg.Events.Churn.Collapse, PairWithin: cfg.Events.Churn.PairWithin.D(),
			Window: cfg.Events.Churn.Window.D()},
	})
	// The event connections retry a failed first connect instead of
	// stopping the collector; the capture records each state change.
	newConn := func(label string, c config.NATS, subscribe func(*nats.Conn) error) *eventConn {
		return &eventConn{
			label: label, subscribe: subscribe, log: log,
			dial: func() (*nats.Conn, error) {
				opts := append(capture.ConnHandlers(label), nats.RetryOnFailedConnect(true))
				nc, err := natsconn.Connect(c, log.With("connection", label), opts...)
				if err != nil {
					return nil, fmt.Errorf("%s events connection: %w", label, err)
				}
				return nc, nil
			},
		}
	}
	sysConn := cfg.NATS
	sysConn.Name += "-events"
	conns := []*eventConn{newConn("sys", sysConn, capture.SubscribeSys)}
	for _, acc := range cfg.Accounts {
		conns = append(conns, newConn(acc.Name, acc.Connection(cfg.NATS), func(nc *nats.Conn) error {
			return capture.SubscribeAccount(acc.Name, nc)
		}))
	}
	for _, ec := range conns {
		if err := ec.open(); err != nil {
			for _, open := range conns {
				if nc := open.current(); nc != nil {
					nc.Close()
				}
			}
			return nil, nil, err
		}
	}
	if len(cfg.Accounts) == 0 {
		log.Warn("no accounts configured: stream and consumer advisories are not captured")
	}
	kctx, stopKeepers := context.WithCancel(ctx)
	var keepers sync.WaitGroup
	for _, ec := range conns {
		keepers.Go(func() { ec.keep(kctx, time.Second) })
	}
	// The capture outlives ctx: on shutdown the connections are drained
	// first, so that messages already received are written too.
	cctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	go func() {
		_ = capture.Run(cctx)
		close(done)
	}()
	return capture, func(deadline time.Time) {
		stopKeepers()
		keepers.Wait()
		// Drain every connection at once, for at most one flush interval,
		// then give the capture the rest of the time for its last write.
		drainFor := max(min(cfg.Events.FlushInterval.D(), time.Until(deadline)), 0)
		drainAll(conns, drainFor)
		capture.SetFinalDeadline(deadline)
		cancel()
		<-done
	}, nil
}
