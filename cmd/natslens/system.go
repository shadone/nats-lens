package main

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/archive"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/config"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/natsconn"
	"github.com/shadone/nats-lens/internal/notify"
	"github.com/shadone/nats-lens/internal/runner"
	"github.com/shadone/nats-lens/internal/store"
)

// runtimeProcs is the number of CPUs the process may use at once.
func runtimeProcs() int { return runtime.GOMAXPROCS(0) }

// systemRuntime is one monitored system: its store and archive, and in
// natslens run its collector, event capture, Kubernetes watcher, check
// engine and archiver. Systems share nothing but the process, the API and
// the check definitions.
type systemRuntime struct {
	sys config.System
	// cfg is the configuration as a single-system file would give it for
	// sys; everything that runs one system reads it.
	cfg config.Config
	// archiveDirSet is whether the file sets the system's archive
	// directory, rather than leaving it at its default inside the data
	// directory.
	archiveDirSet bool
	log           *slog.Logger
	// startBackoff is the wait between setup attempts; the zero value
	// means defaultBackoff().
	startBackoff redialBackoff
	status       *collectionStatus

	st   *store.Store
	arch *archive.Archiver
}

func newSystemRuntime(cfg config.Config, sys config.System, log *slog.Logger) *systemRuntime {
	return &systemRuntime{sys: sys, cfg: cfg.ForSystem(sys), archiveDirSet: archiveDirSet(cfg, sys.ID),
		log: log.With("system", sys.ID), status: newCollectionStatus(nil)}
}

// archiveDirSet reports whether cfg sets the archive directory of the
// system with the id: the top-level archive_dir (the root in a list) or
// the system's own. Read before ForSystem fills in the default.
func archiveDirSet(cfg config.Config, id string) bool {
	if cfg.ArchiveDir != "" {
		return true
	}
	for _, s := range cfg.Systems {
		if s.ID == id {
			return s.ArchiveDir != ""
		}
	}
	return false
}

// provider reads the system's store for the API.
func (rt *systemRuntime) provider(set *checks.Set) *storeprov.Provider {
	return &storeprov.Provider{St: rt.st, Interval: rt.cfg.Interval.D(), Retention: rt.sys.Retention.D(), Live: true, Checks: set,
		ToolingNames: rt.sys.ToolingNames(), LoopbackNotTooling: !rt.cfg.Checks.LoopbackTooling(),
		ClientPods: rt.sys.Kubernetes.ClientPodsPattern(), Kubernetes: rt.sys.Kubernetes.Enabled}
}

// notifier queues the system's notifications in its store.
func (rt *systemRuntime) notifier(set *checks.Set) *notify.Reconciler {
	return &notify.Reconciler{Store: rt.st, SystemID: rt.sys.ID, SystemName: rt.sys.Name, Channels: rt.cfg.Notifications.Channels,
		Checks: set, Retention: rt.cfg.Retention.D(), Log: rt.log}
}

// registry lists the systems for the API in configuration order; the
// first is the default.
func registry(rts []*systemRuntime, set *checks.Set) api.Systems {
	out := make(api.Systems, 0, len(rts))
	for _, rt := range rts {
		out = append(out, api.System{
			Ref:      &v1.SystemRef{Id: rt.sys.ID, Name: rt.sys.Name, Environment: rt.sys.Environment},
			Provider: rt.provider(set),
			Status:   rt.status.get,
		})
	}
	return out
}

// run collects the system: in natslens once one epoch, in natslens run one
// epoch per interval until ctx ends. Shutdown starts when ctx ends and
// keeps to deadline, which every system shares: the last epoch write gets
// shutdown_grace, and in parallel the event connections drain for at most
// one flush interval and the last events are written until the deadline.
// The store stays open; the caller closes it after the API stopped, by
// the same deadline.
func (rt *systemRuntime) run(ctx context.Context, cmd string, set *checks.Set, deadline func() time.Time) (err error) {
	cfg, st, log := rt.cfg, rt.st, rt.log
	tracker := collect.NewTracker(cfg.ServerForgetAfter.D())
	seen, err := st.LastSeenServers(ctx)
	if err != nil {
		return fmt.Errorf("system %s: read known servers: %w", rt.sys.ID, err)
	}
	for name, at := range seen {
		tracker.Seed(name, at)
	}

	col := collect.New(nil, collect.Options{
		Window: cfg.Timeout.D(), ConnzLimit: cfg.ConnzLimit, Tracker: tracker, Log: log,
		SubscriptionsInterval: cfg.SubscriptionsInterval.D(), SubszLimit: cfg.SubszLimit,
	})
	eventsDone := make(chan struct{})
	var nc *nats.Conn
	var conn *eventConn
	if cmd == "run" {
		if cfg.Events.Enabled {
			if err := recordStart(ctx, st); err != nil {
				return fmt.Errorf("system %s: %w", rt.sys.ID, err)
			}
		}
		// natslens run keeps running while NATS is unreachable: the API,
		// the other systems, the events of other sources and the
		// Kubernetes watcher go on, and the runner skips epochs until the
		// connection has a server. A connection nats.go closes for good
		// (rejected credentials) is dialled again. A setup that fails
		// (an events connection, the Kubernetes client) isolates this
		// system and is tried again.
		s, ok := rt.startRetrying(ctx, col)
		if !ok {
			return nil
		}
		conn = s.conn
		if s.stopAccounts != nil {
			defer s.stopAccounts()
		}
		if s.stopEvents != nil {
			stopAtCancel := context.AfterFunc(ctx, func() {
				s.stopK8s()
				s.stopEvents(deadline())
				close(eventsDone)
			})
			// A return before ctx ends stops the capture here.
			defer func() {
				if stopAtCancel() {
					s.stopK8s()
					s.stopEvents(deadline())
					close(eventsDone)
				}
				<-eventsDone
			}()
		} else {
			close(eventsDone)
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
			return fmt.Errorf("system %s: %w", rt.sys.ID, err)
		}
		defer nc.Close()
		col.SetConn(nc)
		accs, closeAccs := onceAccountConns(cfg, log)
		defer closeAccs()
		col.SetAccountConns(accs)
	}
	if nc.IsConnected() {
		rt.status.set(api.CollectionCollecting, nil)
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

	if ret := cfg.Retention.D(); ret < api.AcceptRetention {
		log.Warn("retention is shorter than the growth review of acceptances reads; acceptances record no growth and never end early",
			"retention", ret.String(), "needed", api.AcceptRetention.String())
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
		Checks:    &checks.Engine{Set: set, Log: log, Budget: cfg.Interval.D() / 2, ToolingNames: rt.sys.ToolingNames()},
		Connected: connected,
		Stored:    func(_ int64, startedAt time.Time) { rt.status.epoch(startedAt) },
	}
	// Only natslens run notifies: the next run after a once sends the
	// difference between what each channel was told and the findings then.
	if cmd == "run" && len(cfg.Notifications.Channels) > 0 {
		rec := rt.notifier(set)
		r.Notify = func(ctx context.Context, epoch int64) error {
			_, err := rec.Reconcile(ctx, epoch)
			return err
		}
		r.NotifySweep = rec.Sweep
	}
	if cmd == "once" {
		_, err := r.Once(ctx)
		if err != nil {
			return fmt.Errorf("system %s: %w", rt.sys.ID, err)
		}
		return nil
	}
	// The archive exports on its own goroutine, so a long catch-up never
	// delays an epoch.
	archived := make(chan struct{})
	go func() {
		rt.arch.Run(ctx, cfg.Interval.D())
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
	writeFinalState(ctx, rt.arch, deadline(), log)
	return err
}
