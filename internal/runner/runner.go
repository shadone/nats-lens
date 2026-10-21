// Package runner drives the collector: one epoch per interval, each written
// to the store and then checked, and an hourly retention sweep. The archive
// exports in its own goroutine (archive.Archiver.Run).
package runner

import (
	"context"
	"log/slog"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/store"
)

// SweepEvery is how often the retention sweep runs.
const SweepEvery = time.Hour

// DefaultShutdownGrace is how long an epoch write may continue after
// shutdown began, when Runner.ShutdownGrace is not set.
const DefaultShutdownGrace = 5 * time.Second

// Runner takes epochs on a fixed interval.
type Runner struct {
	Collector *collect.Collector
	Store     *store.Store
	Interval  time.Duration
	Retention time.Duration
	// Log receives the runner's messages; nil uses slog.Default().
	Log *slog.Logger
	// ShutdownGrace bounds an epoch write that is running when ctx ends;
	// zero means DefaultShutdownGrace.
	ShutdownGrace time.Duration
	// Checks run after every stored epoch, outside the epoch's
	// transaction; nil runs none. A check run cut short by shutdown stores
	// nothing for the epoch.
	Checks *checks.Engine
	// Stored, when set, is called with each epoch and the time it started
	// once it is stored, before its checks run.
	Stored func(epoch int64, startedAt time.Time)
	// Connected, when set, reports whether the collector's connection has
	// a server; Run skips epochs while it has none.
	Connected func() bool
}

func (r *Runner) logger() *slog.Logger {
	if r.Log == nil {
		return slog.Default()
	}
	return r.Log
}

// writeContext returns the context for one epoch write. It is not cancelled
// with ctx, so that a shutdown does not abort a write that is about to
// commit, but it ends grace after ctx ends, so that a stuck write cannot
// hold up shutdown. The write's own limit of one interval is applied by the
// store once it holds its lock (Store.WriteSnapshotWithin).
func writeContext(ctx context.Context, grace time.Duration) (context.Context, context.CancelFunc) {
	wctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stop := context.AfterFunc(ctx, func() {
		t := time.AfterFunc(grace, cancel)
		context.AfterFunc(wctx, func() { t.Stop() })
	})
	return wctx, func() {
		stop()
		cancel()
	}
}

// Run takes an epoch now and then every interval until ctx is done. A failed
// write is logged and the next epoch still runs.
func (r *Runner) Run(ctx context.Context) error {
	r.sweep(ctx)
	lastSweep := time.Now()
	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()
	skipping := false
	for {
		if r.Connected != nil && !r.Connected() {
			if !skipping {
				r.logger().Warn("nats not connected; epochs skipped until it is")
			}
			skipping = true
		} else {
			if skipping {
				r.logger().Info("nats connected; epochs resumed")
			}
			skipping = false
			_, _ = r.Once(ctx)
		}
		if time.Since(lastSweep) >= SweepEvery {
			r.sweep(ctx)
			lastSweep = time.Now()
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Once takes and stores one epoch.
func (r *Runner) Once(ctx context.Context) (int64, error) {
	snap := r.Collector.Collect(ctx)
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	// The write gets its own context so a shutdown during the write does
	// not leave half an epoch; the transaction commits or rolls back whole.
	grace := r.ShutdownGrace
	if grace <= 0 {
		grace = DefaultShutdownGrace
	}
	wctx, cancel := writeContext(ctx, grace)
	defer cancel()
	epoch, err := r.Store.WriteSnapshotWithin(wctx, snap, r.Interval)
	if err != nil {
		r.logger().Error("epoch not stored", "err", err)
		return 0, err
	}
	level := slog.LevelInfo
	if snap.Partial() {
		level = slog.LevelWarn
	}
	r.logger().Log(ctx, level, "epoch stored",
		"epoch", epoch,
		"duration_ms", snap.FinishedAt.Sub(snap.StartedAt).Milliseconds(),
		"expected", len(snap.Expected),
		"observed", len(snap.Servers),
		"missing", snap.Missing(),
		"partial", snap.Partial(),
		"errors", snap.Errors,
	)
	if r.Stored != nil {
		r.Stored(epoch, snap.StartedAt)
	}
	r.check(ctx, epoch)
	return epoch, nil
}

// check runs the checks for a stored epoch. A failure is logged and the
// collector goes on; a check that fails on its own is the engine's
// system-001 finding, not an error here.
func (r *Runner) check(ctx context.Context, epoch int64) {
	if r.Checks == nil {
		return
	}
	if ctx.Err() != nil {
		r.logger().Info("checks skipped: shutting down", "epoch", epoch)
		return
	}
	res, err := r.Checks.Run(ctx, r.Store, epoch)
	if err != nil {
		if ctx.Err() != nil {
			r.logger().Info("checks abandoned: shutting down", "epoch", epoch)
			return
		}
		r.logger().Error("checks not applied", "epoch", epoch, "err", err)
		return
	}
	if res.Firing+res.Changed+res.Resolved > 0 {
		r.logger().Info("findings changed", "epoch", epoch, "firing", res.Firing, "changed", res.Changed, "resolved", res.Resolved)
	}
	rev, err := ReviewAcceptances(ctx, r.Store, epoch, r.Interval)
	if err != nil {
		if ctx.Err() == nil {
			r.logger().Error("acceptances not reviewed", "epoch", epoch, "err", err)
		}
		return
	}
	for _, m := range rev.Baselined {
		r.logger().Info("acceptance growth baseline set", "epoch", epoch, "mute", m.ID, "check", m.CheckCode,
			"entity", m.EntityType+" "+m.EntityKey, "growth_per_day", *m.GrowthPerDay)
	}
	for _, m := range rev.Ended {
		r.logger().Info("acceptance ended", "epoch", epoch, "mute", m.ID, "check", m.CheckCode,
			"entity", m.EntityType+" "+m.EntityKey, "reason", m.EndReason)
	}
}

func (r *Runner) sweep(ctx context.Context) {
	res, err := r.Store.Sweep(ctx, time.Now(), r.Retention)
	if err != nil {
		r.logger().Error("retention sweep failed", "err", err)
		return
	}
	if res.Epochs > 0 || res.Events > 0 {
		r.logger().Info("retention sweep", "deleted_epochs", res.Epochs, "deleted_events", res.Events, "oldest_kept", res.Cutoff)
	}
}

// ReviewResult is what one growth review did.
type ReviewResult struct {
	// Ended are the acceptances it ended, with their end reason.
	Ended []store.Mute
	// Baselined are the acceptances that recorded no growth and got the
	// growth measured now as their baseline; none of them was ended.
	Baselined []store.Mute
}

// ReviewAcceptances reviews every acceptance of a stream or server in
// force at the epoch, by the entity's week's growth at the epoch's time
// (api.WeeklyGrowth). An acceptance that recorded a growth is ended when
// the growth now differs from it by more than api.GrowthChanged allows;
// one that recorded none (the entity was under 8 days old) gets the growth
// now as its baseline, and is not ended that time. An entity whose growth
// cannot be measured now (under 8 days of data, a gap, the entity gone)
// changes nothing. One query reads the acceptances and one the growth of
// all their entities.
func ReviewAcceptances(ctx context.Context, st *store.Store, epoch int64, interval time.Duration) (ReviewResult, error) {
	var res ReviewResult
	at, err := st.EpochStartedAt(ctx, epoch)
	if err != nil {
		return res, err
	}
	ms, err := st.AcceptancesToReview(ctx, at)
	if err != nil || len(ms) == 0 {
		return res, err
	}
	ents := make([]store.EntityKey, 0, len(ms))
	for _, m := range ms {
		ents = append(ents, store.EntityKey{Type: m.EntityType, Key: m.EntityKey})
	}
	pts, err := st.GrowthPoints(ctx, growthSpans(at), api.AcceptGrowthWidth(interval), ents)
	if err != nil {
		return res, err
	}
	for _, m := range ms {
		now := api.WeeklyGrowth(samples(pts[store.EntityKey{Type: m.EntityType, Key: m.EntityKey}]), at)
		if now == nil {
			continue
		}
		if m.GrowthPerDay == nil {
			ok, err := st.SetAcceptanceBaseline(ctx, m.ID, at, now.PerDay)
			if err != nil {
				return res, err
			}
			if ok {
				m.GrowthPerDay = &now.PerDay
				res.Baselined = append(res.Baselined, m)
			}
			continue
		}
		if !api.GrowthChanged(*m.GrowthPerDay, now.PerDay) {
			continue
		}
		reason := now.EndReason(*m.GrowthPerDay)
		ok, err := st.EndAcceptance(ctx, m.ID, at, api.AcceptEndedBy, reason)
		if err != nil {
			return res, err
		}
		if ok {
			m.UnmutedAt, m.UnmutedBy, m.EndReason = at, api.AcceptEndedBy, reason
			res.Ended = append(res.Ended, m)
		}
	}
	return res, nil
}

// growthSpans are api.AcceptGrowthSpans as the store takes them.
func growthSpans(t time.Time) []store.Span {
	var out []store.Span
	for _, sp := range api.AcceptGrowthSpans(t) {
		out = append(out, store.Span{From: sp.From, To: sp.To})
	}
	return out
}

func samples(pts []store.GrowthPoint) []api.Sample {
	out := make([]api.Sample, 0, len(pts))
	for _, p := range pts {
		out = append(out, api.Sample{T: p.T, V: p.V})
	}
	return out
}
