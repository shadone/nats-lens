// Package runner drives the collector: one epoch per interval, each written
// to the store and then checked, and an hourly retention sweep. The archive
// exports in its own goroutine (archive.Archiver.Run).
package runner

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/checkaccept"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/timing"
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
	// Notify, when set, is called with each epoch whose checks were
	// applied, after the acceptance review; it queues the epoch's
	// notifications. A failure is logged, and the next epoch's call sends
	// the difference.
	Notify func(ctx context.Context, epoch int64) error
	// NotifySweep, when set, runs with each retention sweep; it removes
	// old sent notifications.
	NotifySweep func(ctx context.Context, now time.Time) error
	// fills are the fills maintenance runs; nil runs the store's asset
	// count and stream sample fills. Set by tests.
	fills []maintenanceFill
}

// maintenanceFill fills what it can of one derived table, starting no
// batch at or after until, and reports whether epochs remain unfilled.
type maintenanceFill struct {
	what string
	fill func(ctx context.Context, until time.Time) (filled int64, more bool, err error)
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
// nonfatal write is logged and the next epoch still runs. A typed fatal
// stops the runner; the store refuses every later database operation.
func (r *Runner) Run(ctx context.Context) error {
	pending, err := r.maintain(ctx)
	if store.IsFatal(err) {
		return err
	}
	lastSweep := time.Now()
	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()
	skipping := false
	for {
		began := time.Now()
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
			if _, err := r.Once(ctx); store.IsFatal(err) {
				return err
			}
		}
		maintained := true
		switch {
		case time.Since(lastSweep) >= SweepEvery:
			pending, err = r.maintain(ctx)
			lastSweep = time.Now()
		case pending:
			pending, err = r.fill(ctx)
		default:
			maintained = false
		}
		if store.IsFatal(err) {
			return err
		}
		// The ticker keeps one tick: an iteration longer than two
		// intervals drops the epochs of the ticks in between.
		if took := time.Since(began); maintained && took >= 2*r.Interval {
			r.logger().Warn("epochs skipped: the epoch and the maintenance after it took longer than two intervals",
				"took", took.Round(time.Millisecond), "skipped", int(took/r.Interval)-1)
		}
		select {
		case <-ctx.Done():
			return r.Store.Failure()
		case <-r.Store.Failed():
			return r.Store.Failure()
		case <-ticker.C:
		}
	}
}

// Once takes and stores one epoch.
func (r *Runner) Once(ctx context.Context) (int64, error) {
	if err := r.Store.Failure(); err != nil {
		return 0, err
	}
	sys := r.Store.System()
	start := time.Now()
	snap := r.Collector.Collect(ctx)
	timing.Since(timing.JobDuration.With(sys, timing.JobCollect), start)
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	// The write gets its own context so a shutdown during the write does
	// not leave half an epoch; the transaction commits or rolls back whole.
	grace := r.ShutdownGrace
	if grace <= 0 {
		grace = DefaultShutdownGrace
	}
	wctx, cancel := writeContext(timing.WithSource(ctx, timing.SourceEpoch), grace)
	defer cancel()
	epoch, err := r.Store.WriteSnapshotWithin(wctx, snap, r.Interval)
	if err != nil {
		r.logger().Error("epoch not stored", withSpillCause(err, "err", err)...)
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
	return epoch, r.Store.Failure()
}

// spillCause names, in the log, why a write failed when the database ran
// out of memory and spill space: other work, often API reads, held both,
// and the next write runs once they are released.
const spillCause = "the database's memory limit and spill bound (duckdb_max_temp_size) were used up"

// withSpillCause adds the cause to a failed write's log attributes when
// err is DuckDB running out of memory or spill space.
func withSpillCause(err error, args ...any) []any {
	if store.IsOutOfMemory(err) {
		return append(args, "cause", spillCause)
	}
	return args
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
	ctx = timing.WithSource(ctx, timing.SourceChecks)
	start := time.Now()
	res, err := r.Checks.Run(ctx, r.Store, epoch)
	timing.Since(timing.JobDuration.With(r.Store.System(), timing.JobChecks), start)
	if err != nil {
		if ctx.Err() != nil {
			r.logger().Info("checks abandoned: shutting down", "epoch", epoch)
			return
		}
		r.logger().Error("checks not applied", withSpillCause(err, "epoch", epoch, "err", err)...)
		return
	}
	if res.Firing+res.Changed+res.Resolved > 0 {
		r.logger().Info("findings changed", "epoch", epoch, "firing", res.Firing, "changed", res.Changed, "resolved", res.Resolved)
	}
	r.review(ctx, epoch)
	if r.Notify == nil || ctx.Err() != nil {
		return
	}
	if err := r.Notify(timing.WithSource(ctx, timing.SourceNotify), epoch); err != nil && ctx.Err() == nil {
		r.logger().Error("notifications not queued", "epoch", epoch, "err", err)
	}
}

// review runs the growth review of acceptances for the epoch and logs
// what it did.
func (r *Runner) review(ctx context.Context, epoch int64) {
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

// maintain runs the retention sweep, then fill. A failed fill is logged;
// only a typed fatal error is returned.
func (r *Runner) maintain(ctx context.Context) (pending bool, err error) {
	err = r.sweep(ctx)
	if store.IsFatal(err) || ctx.Err() != nil {
		return false, err
	}
	pending, ferr := r.fill(ctx)
	if store.IsFatal(ferr) {
		return pending, ferr
	}
	return pending, err
}

// fill fills the asset counts and stream samples of epochs that have none,
// for about one interval, and reports whether any remain. The fills run
// when the system's store opens; one that failed there would leave every
// later epoch without stream samples, as an epoch gets them only when
// every earlier one has them, so they are retried after each sweep and,
// while epochs remain unfilled, after each epoch, so that the runner's own
// epochs are not held up by a long fill. Each fill takes the write lock
// per batch and starts no batch after its time is up. A failed fill is
// logged and retried after the next retention sweep, so that a lasting
// failure is not logged every epoch; a typed fatal error is returned.
func (r *Runner) fill(ctx context.Context) (pending bool, err error) {
	ctx = timing.WithSource(ctx, timing.SourceRetention)
	fills := r.fills
	if fills == nil {
		fills = []maintenanceFill{{"asset counts", r.Store.FillAssetCountsUntil}, {"stream samples", r.Store.FillStreamSamplesUntil}}
	}
	until := time.Now().Add(r.Interval)
	for _, f := range fills {
		_, more, ferr := f.fill(ctx, until)
		if ferr == nil {
			pending = pending || more
			continue
		}
		ferr = store.SafeError(f.what+" fill", ferr)
		if store.IsFatal(ferr) {
			return pending, ferr
		}
		if ctx.Err() == nil {
			r.logger().Warn(f.what+" not filled; retried after the next retention sweep", "err", ferr)
		}
	}
	return pending, nil
}

func (r *Runner) sweep(ctx context.Context) error {
	ctx = timing.WithSource(ctx, timing.SourceRetention)
	start := time.Now()
	res, err := r.Store.Sweep(ctx, time.Now(), r.Retention)
	timing.Since(timing.JobDuration.With(r.Store.System(), timing.JobRetentionSweep), start)
	if err != nil {
		err = store.SafeError("retention sweep", err)
		r.logger().Error("retention sweep failed", "err", err)
		return err
	}
	if res.Epochs > 0 || res.Events > 0 {
		r.logger().Info("retention sweep", "deleted_epochs", res.Epochs, "deleted_events", res.Events, "oldest_kept", res.Cutoff)
	}
	if r.NotifySweep != nil {
		if err := r.NotifySweep(ctx, time.Now()); err != nil {
			err = store.SafeError("notifications sweep", err)
			r.logger().Error("notifications sweep failed", "err", err)
			return err
		}
	}
	return nil
}

// UnacceptableAcceptances counts the acceptances in force at now whose
// check is no longer acceptable (checkaccept.Acceptable), and names those
// checks in order. They stay in force until their review date, and the
// growth review skips them.
func UnacceptableAcceptances(ctx context.Context, st *store.Store, now time.Time) (int, []string, error) {
	byCheck, err := st.AcceptanceChecks(ctx, now)
	if err != nil {
		return 0, nil, err
	}
	n := 0
	var codes []string
	for code, c := range byCheck {
		if !checkaccept.Acceptable(code) {
			n += c
			codes = append(codes, code)
		}
	}
	slices.Sort(codes)
	return n, codes, nil
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
// force at the epoch whose check has the growth review
// (checkaccept.GrowthReview), by the entity's week's growth at the
// epoch's time (api.WeeklyGrowth). An acceptance that recorded a growth is ended when
// the growth now differs from it by more than api.GrowthChanged allows;
// one that recorded none (the entity was under 8 days old) gets the growth
// now as its baseline, and is not ended that time. An entity whose growth
// cannot be measured now (under 8 days of data, a gap, the entity gone)
// changes nothing. One query reads the acceptances and one the growth of
// all their entities.
func ReviewAcceptances(ctx context.Context, st *store.Store, epoch int64, interval time.Duration) (ReviewResult, error) {
	return reviewAcceptances(ctx, st, epoch, interval, checkaccept.GrowthReview)
}

// reviewAcceptances is ReviewAcceptances with the rule that says which
// checks have the growth review.
func reviewAcceptances(ctx context.Context, st *store.Store, epoch int64, interval time.Duration, growthReview func(code string) bool) (ReviewResult, error) {
	var res ReviewResult
	at, err := st.EpochStartedAt(ctx, epoch)
	if err != nil {
		return res, err
	}
	all, err := st.AcceptancesToReview(ctx, at)
	if err != nil {
		return res, err
	}
	// Only a check with the growth review ends early; an acceptance of any
	// other check, including one stored before its check lost the review,
	// ends at its review date.
	ms := slices.DeleteFunc(all, func(m store.Mute) bool { return !growthReview(m.CheckCode) })
	if len(ms) == 0 {
		return res, nil
	}
	ents := make([]store.EntityKey, 0, len(ms))
	for _, m := range ms {
		ents = append(ents, store.EntityKey{Type: m.EntityType, Key: m.EntityKey})
	}
	width := api.AcceptGrowthWidth(interval)
	pts, err := st.GrowthPoints(ctx, growthSpans(at), width, ents)
	if err != nil {
		return res, err
	}
	for _, m := range ms {
		now := api.WeeklyGrowth(samples(pts[store.EntityKey{Type: m.EntityType, Key: m.EntityKey}]), at, width)
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
