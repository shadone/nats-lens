// Command natslens collects monitoring snapshots and events of one or more
// NATS systems, each into its own DuckDB, and archives each closed hour to
// Parquet.
//
// Usage:
//
//	natslens [run] [flags]                      collect one epoch per interval of every system
//	natslens once [-system ID] [flags]          collect and store a single epoch of every system, or one
//	natslens sql [-config FILE] [-system ID] [-data-dir DIR] QUERY
//	                                            query a database no collector has open
//	natslens checks list                        list the checks with their thresholds
//	natslens checks run [-config FILE] [-system ID] [-data-dir DIR]
//	                                            evaluate the checks on a database no collector has open
//	natslens demo [-listen ADDR]                serve the API with a fixed scenario, without NATS
//
// natslens run also serves the API and UI on -listen (default
// 127.0.0.1:8080).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/sqlcli"
	"github.com/shadone/nats-lens/internal/store"
)

func main() {
	// Until the configuration sets the level, messages such as its
	// warnings go out as JSON like every later one.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
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

	var only string
	cfg, err := config.ParseWith("natslens "+cmd, args, func(fs *flag.FlagSet) {
		if cmd == "once" {
			fs.StringVar(&only, "system", "", "collect only the system with this id (default: every system)")
		}
	})
	if err != nil {
		return err
	}
	level, _ := config.ParseLevel(cfg.LogLevel)
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	return runConfig(ctx, cmd, cfg, only, log, nil)
}

// apiOptionsFor are the API settings of the configuration.
func apiOptionsFor(cfg config.Config, ready func() bool) apiOptions {
	return apiOptions{listen: cfg.Listen, allowedHosts: cfg.AllowedHosts, iapAudience: cfg.IAPAudience, ready: ready,
		metricsTimeout: cfg.MetricsTimeout.D()}
}

// runConfig serves natslens run and once for the configured systems, or
// only the system with the id only. observe, when set, is told the API's
// systems once they are open; for tests.
func runConfig(ctx context.Context, cmd string, cfg config.Config, only string, log *slog.Logger, observe func(api.Systems)) error {
	log.Info("starting", "command", cmd, "version", version, "config", cfg)
	if cfg.AllowUnauthenticated && cfg.IAPAudience == "" && cmd == "run" && cfg.Listen != "" {
		log.Warn("the API is served without IAP verification: anyone who reaches it can mute findings", "listen", cfg.Listen)
	}
	systems := cfg.ResolvedSystems()
	if only != "" {
		sys, err := pickSystem(cfg, only)
		if err != nil {
			return err
		}
		systems = []config.System{sys}
	}
	set, err := loadChecks(cfg)
	if err != nil {
		return err
	}
	warnUnusedDatabase(cfg, log)

	// Before any system starts, every system is checked; any problem
	// refuses the start, naming every broken system.
	if err := checkSystems(systems, cmd == "run" && cfg.Events.Enabled, log); err != nil {
		return err
	}
	// Every store is open, and any rebuild finished, before the API
	// serves; a store that does not open fails the start.
	rts, err := openSystems(ctx, cfg, systems, log)
	if err != nil {
		return stoppedDuringStart(ctx, err, log)
	}
	// The stores close after the API stopped, in parallel, by the shutdown
	// deadline, given at least minStateWait like the last state.json; before
	// the systems run there is no deadline and every close is waited for.
	closeBy := func() time.Time { return time.Time{} }
	defer func() { closeSystems(rts, closeBy()) }()
	reg := registry(rts, set)
	if observe != nil {
		observe(reg)
	}

	// The API reports ready once it serves: every store is open by now.
	// Collection does not affect readiness.
	ready := newReadiness(nil)
	// The API reads the same databases: DuckDB lets one process hold a
	// file. It starts its shutdown at the signal, gives calls in progress
	// up to 5 seconds from then, and is waited for before the stores close.
	if cmd == "run" && cfg.Listen != "" {
		actx, stopAPI := context.WithCancel(ctx)
		wait, err := startAPI(actx, apiOptionsFor(cfg, ready.ready), reg, log)
		if err != nil {
			stopAPI()
			return err
		}
		defer func() { stopAPI(); wait() }()
		ready.serving()
	}

	// A connection or setup failure isolates its system (see
	// startRetrying), and so does a store read inside the setup (the
	// stored Kubernetes state the watcher starts from): it is retried
	// with the setup. A store failure before the setup, in LastSeenServers
	// or recordStart, ends the system's run early, and natslens run then
	// stops every system, as a store that fails to open fails the start.
	// once collects every system it can.
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Shutdown starts when the systems are told to stop and must finish
	// within shutdown_grace plus one events flush interval, for every
	// system at once.
	var shutdownBy time.Time
	var byOnce sync.Once
	deadline := func() time.Time {
		byOnce.Do(func() { shutdownBy = time.Now().Add(cfg.ShutdownGrace.D() + cfg.Events.FlushInterval.D()) })
		return shutdownBy
	}
	context.AfterFunc(rctx, func() { deadline() })
	closeBy = func() time.Time { return stateDeadline(deadline(), time.Now()) }

	errs := make([]error, len(rts))
	var wg sync.WaitGroup
	for i, rt := range rts {
		wg.Go(func() {
			errs[i] = rt.run(rctx, cmd, set, deadline)
			if errs[i] != nil && cmd == "run" && ctx.Err() == nil {
				rt.log.Error("system's store failed; stopping the others", "err", errs[i])
				cancel()
			}
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}

// stoppedDuringStart turns an error caused by a stop signal before the
// start completed into a clean exit: the process was asked to stop, and
// nothing failed.
func stoppedDuringStart(ctx context.Context, err error, log *slog.Logger) error {
	if ctx.Err() == nil {
		return err
	}
	log.Info("stopped before the start completed", "err", err)
	return nil
}

// pickSystem returns the configured system with the id.
func pickSystem(cfg config.Config, id string) (config.System, error) {
	var ids []string
	for _, s := range cfg.ResolvedSystems() {
		if s.ID == id {
			return s, nil
		}
		ids = append(ids, s.ID)
	}
	return config.System{}, fmt.Errorf("unknown system %q; configured systems: %s", id, strings.Join(ids, ", "))
}

// chosenSystem is the system a command for one database reads: the one
// with the id, or the only one when id is empty. With several systems an
// id is required.
func chosenSystem(cfg config.Config, id string) (config.System, error) {
	if id != "" {
		return pickSystem(cfg, id)
	}
	systems := cfg.ResolvedSystems()
	if len(systems) == 1 {
		return systems[0], nil
	}
	ids := make([]string, len(systems))
	for i, s := range systems {
		ids[i] = s.ID
	}
	return config.System{}, fmt.Errorf("the configuration lists several systems; choose one with -system: %s", strings.Join(ids, ", "))
}

func runSQL(ctx context.Context, args []string) error {
	return runSQLTo(ctx, args, os.Stdout)
}

// runSQLTo serves natslens sql, printing to out.
func runSQLTo(ctx context.Context, args []string, out io.Writer) error {
	const usage = "usage: natslens sql [-config FILE] [-system ID] [-data-dir DIR] QUERY"
	fs := flag.NewFlagSet("natslens sql", flag.ContinueOnError)
	db := oneDatabaseFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New(usage)
	}
	_, _, dir, err := db.resolve()
	if err != nil {
		return err
	}
	return sqlcli.Run(ctx, filepath.Join(dir, store.FileName), fs.Arg(0), out)
}
