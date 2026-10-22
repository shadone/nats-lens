package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/natsconn"
)

// microConns returns the account connections the collector asks micro
// services over: those of conns whose account in accs asks them. conns
// holds one connection per account, in the order of accs.
func microConns(accs []config.Account, conns []*eventConn) []collect.AccountConn {
	var out []collect.AccountConn
	for i, a := range accs {
		if !a.AsksMicro() || i >= len(conns) {
			continue
		}
		out = append(out, collect.AccountConn{Label: a.Name, Conn: conns[i].current})
	}
	return out
}

// newAccountConn is one account connection that only asks: it subscribes
// to nothing of its own and keeps running while NATS is unreachable.
func newAccountConn(label string, c config.NATS, log *slog.Logger) *eventConn {
	return &eventConn{
		label: label, log: log,
		dial: func() (*nats.Conn, error) {
			nc, err := natsconn.Connect(c, log.With("connection", label), nats.RetryOnFailedConnect(true))
			if err != nil {
				return nil, fmt.Errorf("%s account connection: %w", label, err)
			}
			return nc, nil
		},
		reopened: func(*nats.Conn) { log.Info("account connection dialled again", "connection", label) },
	}
}

// startAccountConns opens a connection for every account that asks its
// micro services, for natslens run without event capture, which would
// otherwise open them, and keeps each open. An account that cannot be
// dialled (a missing credentials file) is logged and dialled again with
// the backoff of a closed connection; until then its requests are recorded
// as not connected, and the other accounts and the system go on. stop
// closes them.
func startAccountConns(ctx context.Context, cfg config.Config, log *slog.Logger) (conns []collect.AccountConn,
	stop func()) {
	var accs []config.Account
	var ecs []*eventConn
	for _, a := range cfg.Accounts {
		if !a.AsksMicro() {
			continue
		}
		accs = append(accs, a)
		ecs = append(ecs, newAccountConn(a.Name, a.Connection(cfg.NATS), log))
	}
	for _, ec := range ecs {
		if err := ec.open(); err != nil {
			log.Error("account connection not opened; its micro services are not asked until it is", "account", ec.label, "err", err)
		}
	}
	kctx, stopKeepers := context.WithCancel(ctx)
	var keepers sync.WaitGroup
	for _, ec := range ecs {
		keepers.Go(func() { ec.keep(kctx, time.Second) })
	}
	return microConns(accs, ecs), func() {
		stopKeepers()
		keepers.Wait()
		closeAll(ecs)
	}
}

// onceAccountConns connects every account that asks its micro services
// for natslens once. An account that does not connect is logged and asked
// over no connection, which records its requests as failed.
func onceAccountConns(cfg config.Config, log *slog.Logger) (conns []collect.AccountConn, closeConns func()) {
	var opened []*nats.Conn
	for _, a := range cfg.Accounts {
		if !a.AsksMicro() {
			continue
		}
		nc, err := natsconn.Connect(a.Connection(cfg.NATS), log.With("connection", a.Name))
		if err != nil {
			log.Warn("account connection failed; its micro services are not asked", "account", a.Name, "err", err)
		} else {
			opened = append(opened, nc)
		}
		conns = append(conns, collect.AccountConn{Label: a.Name, Conn: func() *nats.Conn { return nc }})
	}
	return conns, func() {
		for _, nc := range opened {
			nc.Close()
		}
	}
}

// closeAll closes every open connection of ecs.
func closeAll(ecs []*eventConn) {
	for _, ec := range ecs {
		if nc := ec.current(); nc != nil {
			nc.Close()
		}
	}
}
