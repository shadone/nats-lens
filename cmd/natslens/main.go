// Command natslens collects monitoring snapshots and events of a NATS system
// into DuckDB and archives each closed hour to Parquet.
//
// Usage:
//
//	natslens [run] [flags]            collect one epoch per interval
//	natslens once [flags]             collect and store a single epoch
//	natslens sql -data-dir DIR QUERY  query a database no collector has open
//	natslens checks list              list the checks with their thresholds
//	natslens checks run -data-dir DIR evaluate the checks on a database no collector has open
//	natslens demo [-listen ADDR]      serve the API with a fixed scenario, without NATS
//
// natslens run also serves the API and UI on -listen (default
// 127.0.0.1:8080).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/events"
	"github.com/shadone/nats-lens/internal/natsconn"
	"github.com/shadone/nats-lens/internal/runner"
	"github.com/shadone/nats-lens/internal/sqlcli"
	"github.com/shadone/nats-lens/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, "natslens:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	cmd := "run"
	if len(args) > 0 && (args[0] == "run" || args[0] == "once" || args[0] == "sql" || args[0] == "demo" || args[0] == "checks") {
		cmd, args = args[0], args[1:]
	}
	if cmd == "sql" {
		return runSQL(ctx, args)
	}
	if cmd == "checks" {
		return runChecksCmd(ctx, args, os.Stdout)
	}
	if cmd == "demo" {
		return runDemo(ctx, args)
	}

	cfg, err := config.Parse("natslens "+cmd, args)
	if err != nil {
		return err
	}
	level, _ := config.ParseLevel(cfg.LogLevel)
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	log.Info("starting", "command", cmd, "version", version, "config", cfg)
	if cfg.AllowUnauthenticated && cfg.IAPAudience == "" && cmd == "run" && cfg.Listen != "" {
		log.Warn("the API is served without IAP verification: anyone who reaches it can mute findings", "listen", cfg.Listen)
	}
	set, err := loadChecks(cfg)
	if err != nil {
		return err
	}

	st, arch, err := openStore(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := loadInventory(ctx, cfg, st, log); err != nil {
		return err
	}

	// The API reports ready once it serves: the store is open by now, so
	// any rebuild has finished. Collection does not affect readiness.
	ready := newReadiness(nil)
	// The API reads the same database: DuckDB lets one process hold the
	// file. It starts its shutdown at the signal, gives calls in progress
	// up to 5 seconds from then, and is waited for before the store closes.
	if cmd == "run" && cfg.Listen != "" {
		actx, stopAPI := context.WithCancel(ctx)
		wait, err := startAPI(actx, apiOptions{listen: cfg.Listen, allowedHosts: cfg.AllowedHosts, iapAudience: cfg.IAPAudience, ready: ready.ready,
			metricsTimeout: cfg.MetricsTimeout.D()},
			singleSystem(&storeprov.Provider{St: st, Interval: cfg.Interval.D(), Retention: cfg.Retention.D(), Live: true, Checks: set,
				ToolingNames: cfg.ToolingNames(), LoopbackNotTooling: !cfg.Checks.LoopbackTooling(),
				ClientPods: cfg.Kubernetes.ClientPodsPattern()}), log)
		if err != nil {
			stopAPI()
			return err
		}
		defer func() { stopAPI(); wait() }()
		ready.serving()
	}

	tracker := collect.NewTracker(cfg.ServerForgetAfter.D())
	seen, err := st.LastSeenServers(ctx)
	if err != nil {
		return fmt.Errorf("read known servers: %w", err)
	}
	for name, at := range seen {
		tracker.Seed(name, at)
	}

	// Shutdown starts when ctx ends and must finish within shutdown_grace
	// plus one events flush interval: the last epoch write gets the grace,
	// and in parallel the event connections drain for at most one flush
	// interval and the last events are written until the same deadline.
	var shutdownBy time.Time
	var byOnce sync.Once
	deadline := func() time.Time {
		byOnce.Do(func() { shutdownBy = time.Now().Add(cfg.ShutdownGrace.D() + cfg.Events.FlushInterval.D()) })
		return shutdownBy
	}
	context.AfterFunc(ctx, func() { deadline() })

	// With event capture on, the collector's own connection records its
	// losses and reconnects as events too.
	var capture *events.Capture
	eventsDone := make(chan struct{})
	if cmd == "run" && cfg.Events.Enabled {
		var stopEvents func(time.Time)
		capture, stopEvents, err = startEvents(ctx, cfg, st, log)
		if err != nil {
			return err
		}
		// The Kubernetes watcher writes through the capture, so it stops first.
		stopK8s := func() {}
		if cfg.Kubernetes.Enabled {
			if stopK8s, err = startKubernetes(ctx, cfg.Kubernetes, st, capture, log); err != nil {
				stopEvents(deadline())
				return err
			}
		}
		stopAtCancel := context.AfterFunc(ctx, func() {
			stopK8s()
			stopEvents(deadline())
			close(eventsDone)
		})
		// A return before ctx ends (a failed connect) stops the capture here.
		defer func() {
			if stopAtCancel() {
				stopK8s()
				stopEvents(deadline())
				close(eventsDone)
			}
			<-eventsDone
		}()
	} else {
		close(eventsDone)
	}

	col := collect.New(nil, collect.Options{
		Window: cfg.Timeout.D(), ConnzLimit: cfg.ConnzLimit, Tracker: tracker, Log: log,
	})
	var nc *nats.Conn
	var conn *eventConn
	if cmd == "run" {
		// natslens run keeps running while NATS is unreachable: the API,
		// the events of other sources and the Kubernetes watcher go on, and
		// the runner skips epochs until the connection has a server. A
		// connection nats.go closes for good (rejected credentials) is
		// dialled again.
		conn = newCollectorConn(cfg.NATS, capture, col, log)
		if err := conn.open(); err != nil {
			return err
		}
		nc = conn.current()
		kctx, stopKeep := context.WithCancel(ctx)
		kept := make(chan struct{})
		go func() { conn.keep(kctx, time.Second); close(kept) }()
		defer func() {
			stopKeep()
			<-kept
			if c := conn.current(); c != nil {
				c.Close()
			}
		}()
	} else {
		// once fails at once instead.
		if nc, err = natsconn.Connect(cfg.NATS, log); err != nil {
			return err
		}
		defer nc.Close()
		col.SetConn(nc)
	}
	if nc.IsConnected() {
		log.Info("connected", "server", nc.ConnectedServerName())
	} else {
		log.Warn("nats not reachable; retrying, no epochs until it is")
	}
	connected := nc.IsConnected
	if conn != nil {
		connected = func() bool {
			c := conn.current()
			return c != nil && c.IsConnected()
		}
	}

	r := &runner.Runner{
		Collector: col,
		Store:     st,
		Interval:  cfg.Interval.D(),
		Retention: cfg.Retention.D(),
		Log:       log,
		// The process manager's stop timeout must exceed the longer of
		// this plus the events flush interval plus minStateWait, and the
		// API's 5-second shutdown, or the last epoch, events, state.json or
		// API calls are cut off.
		ShutdownGrace: cfg.ShutdownGrace.D(),
		// The checks of an epoch get half an interval, so that they never
		// delay the next epoch.
		Checks:    &checks.Engine{Set: set, Log: log, Budget: cfg.Interval.D() / 2},
		Connected: connected,
	}
	if cmd == "once" {
		_, err := r.Once(ctx)
		return err
	}
	// The archive exports on its own goroutine, so a long catch-up never
	// delays an epoch.
	archived := make(chan struct{})
	go func() {
		arch.Run(ctx, cfg.Interval.D())
		close(archived)
	}()
	err = r.Run(ctx)
	// An export interrupted by the signal normally ends at once; one that
	// does not is abandoned at the shutdown deadline.
	if !waitUntil(archived, deadline()) {
		log.Warn("archive export still running at the shutdown deadline; abandoned")
	}
	// The last state goes to the archive after the last events, so that it
	// covers every number handed out.
	<-eventsDone
	writeFinalState(ctx, arch, deadline(), log)
	return err
}

func runSQL(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("natslens sql", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "./data", "directory holding "+store.FileName)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: natslens sql [-data-dir DIR] QUERY")
	}
	return sqlcli.Run(ctx, filepath.Join(*dataDir, store.FileName), fs.Arg(0), os.Stdout)
}
