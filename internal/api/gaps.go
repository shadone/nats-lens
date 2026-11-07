package api

import (
	"encoding/json"
	"slices"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The gap rules: one place that finds the times nats-lens knows nothing
// of, so that every page that shows a gap finds the same ones. The
// providers read the rows; the rules here turn them into gaps.

// Gap kinds.
const (
	GapCollections = "collections"
	GapCollector   = "collector"
	GapKubernetes  = "kubernetes"
	// GapEvents is a time a connection that captures events had no server
	// or was closed: from its collector.not_connected or
	// collector.connection_closed to its next collector.reconnected.
	GapEvents = "events"
)

// EventsFailover is how soon an events connection must reconnect for its
// disconnect to be a failover, not a loss of capture: no gap step, no
// count unset, though advisories sent in those seconds may be missing.
const EventsFailover = 10 * time.Second

// EventsConnectionGaps are the times the connections that capture events
// (every collector connection but the collector's own) had no server or
// were closed, from the connection events in time order: each from the
// first loss (collector.disconnected, as core subscriptions lose what
// comes while disconnected, collector.not_connected or
// collector.connection_closed) to the next reconnect of that connection,
// or to nats-lens starting again (collector.started: its connections are
// new); open when neither followed.
func EventsConnectionGaps(evs []*v1.Event) []ReplayGap {
	var out []ReplayGap
	open := map[string]int{}
	for _, e := range evs {
		if e.GetKind() == "collector.started" {
			for c, i := range open {
				out[i].End = e.GetTime().AsTime()
				delete(open, c)
			}
			continue
		}
		var raw struct {
			Connection string `json:"connection"`
		}
		_ = json.Unmarshal([]byte(e.GetRaw()), &raw)
		if raw.Connection == "" || raw.Connection == "collector" {
			continue
		}
		at := e.GetTime().AsTime()
		switch e.GetKind() {
		case "collector.disconnected", "collector.not_connected", "collector.connection_closed":
			if _, ok := open[raw.Connection]; !ok {
				open[raw.Connection] = len(out)
				out = append(out, ReplayGap{Kind: GapEvents, Start: at, Event: e})
			}
		case "collector.reconnected":
			if i, ok := open[raw.Connection]; ok {
				out[i].End = at
				delete(open, raw.Connection)
			}
		}
	}
	// A failover within EventsFailover loses no capture worth a step.
	out = slices.DeleteFunc(out, func(g ReplayGap) bool { return !g.End.IsZero() && g.End.Sub(g.Start) < EventsFailover })
	// Connections lost together are one time without events.
	slices.SortStableFunc(out, func(x, y ReplayGap) int { return x.Start.Compare(y.Start) })
	var merged []ReplayGap
	for _, g := range out {
		if n := len(merged); n > 0 && (merged[n-1].End.IsZero() || !g.Start.After(merged[n-1].End)) {
			if !merged[n-1].End.IsZero() && (g.End.IsZero() || g.End.After(merged[n-1].End)) {
				merged[n-1].End = g.End
			}
			continue
		}
		merged = append(merged, g)
	}
	return merged
}

// ReplayGap is a time without collections (GapCollections), while
// nats-lens was not running (GapCollector, a collector.gap event) or
// while Kubernetes was not watched (GapKubernetes, collector.k8s_gap).
type ReplayGap struct {
	Kind string
	// Start and End bound the gap; End is zero for a gap that had not
	// ended.
	Start, End time.Time
	// BeforeEpoch and AfterEpoch are the collections either side, 0 when
	// none, with their start times.
	BeforeEpoch, AfterEpoch int64
	BeforeAt, AfterAt       time.Time
	// Event is the collector event of the gap; nil for GapCollections.
	Event *v1.Event
}

// EpochPair is an epoch with the one before it: Prev is 0 for the first
// epoch read.
type EpochPair struct {
	Prev   int64
	PrevAt time.Time
	Epoch  int64
	At     time.Time
}

// CollectionGaps finds the gaps longer than reach between epochs that
// overlap A to B: before the first epoch from A, between epochs, and after
// the last, open when no epoch followed. The pairs are in order, from the
// last epoch before A minus reach to the first after B; a pair may be left
// out when it is no gap, but never the first or the last.
func CollectionGaps(a, b time.Time, reach time.Duration, pairs []EpochPair) []ReplayGap {
	var out []ReplayGap
	var last EpochPair
	for _, p := range pairs {
		from := p.PrevAt
		if p.Prev == 0 {
			from = a
		}
		if p.At.Sub(from) > reach && p.At.After(a) && from.Before(b) {
			out = append(out, ReplayGap{Kind: GapCollections, Start: from, End: p.At, BeforeEpoch: p.Prev, BeforeAt: p.PrevAt,
				AfterEpoch: p.Epoch, AfterAt: p.At})
		}
		last = p
	}
	switch {
	case last.Epoch == 0:
		out = append(out, ReplayGap{Kind: GapCollections, Start: a})
	case b.Sub(last.At) > reach && last.At.Before(b):
		out = append(out, ReplayGap{Kind: GapCollections, Start: last.At, BeforeEpoch: last.Epoch, BeforeAt: last.At})
	}
	return out
}
