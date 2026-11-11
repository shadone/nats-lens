package timing

import (
	"context"
	"sync/atomic"
	"time"
)

type sourceKey struct{}
type callKey struct{}

// WithSource marks the queries run with ctx as coming from source.
func WithSource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, sourceKey{}, source)
}

// Source returns the source set on ctx, or SourceOther.
func Source(ctx context.Context) string {
	if s, ok := ctx.Value(sourceKey{}).(string); ok {
		return s
	}
	return SourceOther
}

// Call counts the queries of one API call and the seconds they ran.
type Call struct {
	// Procedure names the call for a query hook.
	Procedure string
	queries   atomic.Int64
	nanos     atomic.Int64
}

// WithCall returns ctx carrying c, whose counts the store's queries add to.
func WithCall(ctx context.Context, c *Call) context.Context {
	return context.WithValue(ctx, callKey{}, c)
}

// Queries returns the queries counted.
func (c *Call) Queries() int64 { return c.queries.Load() }

// QuerySeconds returns the seconds the counted queries ran, summed.
func (c *Call) QuerySeconds() float64 { return float64(c.nanos.Load()) / 1e9 }

// Query records one query of the system that started at start and ended
// now: its duration by source, and the call's counts when ctx carries
// one.
func Query(ctx context.Context, system string, start time.Time) {
	d := time.Since(start)
	QueryDuration.With(system, Source(ctx)).Observe(d.Seconds())
	if c, ok := ctx.Value(callKey{}).(*Call); ok {
		c.queries.Add(1)
		c.nanos.Add(int64(d))
	}
}

// Since observes the seconds since start in h.
func Since(h *Histogram, start time.Time) { h.Observe(time.Since(start).Seconds()) }
