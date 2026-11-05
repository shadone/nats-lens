package api

import (
	"cmp"
	"encoding/json"
	"slices"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Data history limits.
const (
	// MaxHistoryGaps is the most gaps a response lists of each class:
	// the times without collections with the time before the history
	// (gaps_total counts them all), and the others (listed_gaps_total).
	MaxHistoryGaps = 200
	// HistoryEventsLookback is how long before the range a provider reads
	// the collector events, so that a gap that began before the range is
	// found.
	HistoryEventsLookback = 12 * time.Hour
	// HistoryStartSlack is how soon after the history start a finding
	// first seen then counts as already true when nats-lens started.
	HistoryStartSlack = time.Minute
	// HistoryDay is the need of the uses that read the 24 hours before a
	// collection: Applications traffic, failed logins and drops.
	HistoryDay = 24 * time.Hour
)

// HistoryEventKinds are the collector events a provider reads for Data
// history.
var HistoryEventKinds = []string{"collector.gap", "collector.started", "collector.not_connected", "collector.connection_closed",
	"collector.disconnected", "collector.reconnected", "collector.k8s_gap", "collector.k8s_not_watching"}

// HistoryForecastChecks are the checks whose forecasts the uses table
// lists whatever their history: the store and stream forecasts.
var HistoryForecastChecks = []string{"server-014", "stream-006"}

// CheckLookback is a check's window, after overrides.
type CheckLookback struct {
	Code     string
	Lookback time.Duration
}

// ArchiveState is the archive as the archiver last saw it; the zero value
// is no archiver.
type ArchiveState struct {
	Enabled bool
	// Location is the archive directory.
	Location string
	// Retention is archive_retention; 0 keeps every hour.
	Retention time.Duration
	// OldestHour and NewestHour bound the hours with a manifest, Hours
	// counts them; zero before the first listing.
	OldestHour, NewestHour time.Time
	Hours                  int32
	// LastExportAt is when the newest export finished; NewestFinal whether
	// the newest hour was exported after it closed.
	LastExportAt time.Time
	NewestFinal  bool
	// LastError is the newest export error since the last export that
	// succeeded, at LastErrorAt.
	LastError   string
	LastErrorAt time.Time
	// ListedAt is when the archiver last listed its manifests.
	ListedAt time.Time
}

// HistoryFacts are what a provider read for Data history; FinishDataHistory
// turns them into the response, so that the store and the demo apply the
// same rules.
type HistoryFacts struct {
	// Start and End are the range; Now the provider's present.
	Start, End, Now time.Time
	// Interval is the collect interval; Retention how long collections
	// and events are kept (0: forever).
	Interval, Retention time.Duration
	// HistoryStart and NewestEpochAt are the oldest and newest stored
	// collections, EventsStart the oldest stored event; zero when none.
	HistoryStart, NewestEpochAt, EventsStart time.Time
	// Epochs and PartialEpochs count the collections that started in the
	// range, and those that missed a server.
	Epochs, PartialEpochs int64
	// Collections are the times without collections that overlap the
	// range, by CollectionGaps.
	Collections []ReplayGap
	// Events are the collector events of HistoryEventKinds from
	// HistoryEventsLookback before the range to its end, in time order,
	// with their raw payload, and the collector.gap and collector.k8s_gap
	// events that began earlier and ended after the range start.
	Events []*v1.Event
	// Archive is the archive's state.
	Archive ArchiveState
	// Lookbacks are the checks' windows.
	Lookbacks []CheckLookback
	// FindingsAtStart counts the firing findings already true when
	// nats-lens started.
	FindingsAtStart int32
}

// historyGap is a gap before it is written out; End zero while open.
type historyGap struct {
	kind                    v1.HistoryGapKind
	start, end              time.Time
	beforeEpoch, afterEpoch int64
	beforeAt, afterAt       time.Time
	event                   *v1.Event
	connection              string
}

// endOr is the gap's end, or open for a gap that has not ended.
func (g historyGap) endOr(open time.Time) time.Time {
	if g.end.IsZero() {
		return open
	}
	return g.end
}

// FinishDataHistory applies the gap kinds, their order and merge, the
// coverage of the range, the uses table and the 200-row cut of each class.
func FinishDataHistory(f *HistoryFacts) *v1.GetDataHistoryResponse {
	res := &v1.GetDataHistoryResponse{
		HistoryStart: tsOrNil(f.HistoryStart), NewestEpochAt: tsOrNil(f.NewestEpochAt), EventsStart: tsOrNil(f.EventsStart),
		CollectInterval: durationpb.New(f.Interval), Archive: archiveState(f.Archive), FindingsAtStart: f.FindingsAtStart,
	}
	if f.Retention > 0 {
		res.Retention, res.RetentionFrom = durationpb.New(f.Retention), timestamppb.New(f.Now.Add(-f.Retention))
	}
	hatched, listed := historyGaps(f)
	res.Coverage = historyCoverage(f, hatched)
	if before, ok := beforeHistory(f); ok {
		hatched = append(hatched, before)
	}
	// Each class is cut on its own, so that many short events gaps never
	// push a time without collections out.
	newestFirst := func(a, b historyGap) int { return cmp.Or(b.start.Compare(a.start), int(a.kind-b.kind)) }
	slices.SortStableFunc(hatched, newestFirst)
	slices.SortStableFunc(listed, newestFirst)
	res.GapsTotal, res.ListedGapsTotal = int32(len(hatched)), int32(len(listed))
	all := append(hatched[:min(len(hatched), MaxHistoryGaps)], listed[:min(len(listed), MaxHistoryGaps)]...)
	slices.SortStableFunc(all, newestFirst)
	for _, g := range all {
		res.Gaps = append(res.Gaps, g.message())
	}
	res.Uses = HistoryUses(f)
	return res
}

func (g historyGap) message() *v1.HistoryGap {
	m := &v1.HistoryGap{Kind: g.kind, Start: timestamppb.New(g.start), End: tsOrNil(g.end), BeforeEpoch: g.beforeEpoch,
		AfterEpoch: g.afterEpoch, Connection: g.connection, BeforeEpochAt: tsOrNil(g.beforeAt), AfterEpochAt: tsOrNil(g.afterAt)}
	if g.beforeEpoch == 0 {
		m.BeforeEpochAt = nil
	}
	if g.afterEpoch == 0 {
		m.AfterEpochAt = nil
	}
	if g.event != nil {
		m.EventId = g.event.GetId()
	}
	return m
}

// beforeHistory is the gap from the range start to the history start;
// the whole range without history.
func beforeHistory(f *HistoryFacts) (historyGap, bool) {
	switch {
	case f.HistoryStart.IsZero():
		return historyGap{kind: v1.HistoryGapKind_HISTORY_GAP_KIND_BEFORE_HISTORY, start: f.Start}, true
	case f.HistoryStart.After(f.Start):
		g := historyGap{kind: v1.HistoryGapKind_HISTORY_GAP_KIND_BEFORE_HISTORY, start: f.Start, end: f.HistoryStart}
		if !f.HistoryStart.Before(f.End) {
			g.end = time.Time{}
		}
		return g, true
	}
	return historyGap{}, false
}

// historyGaps finds the gaps that overlap the range from the history
// start on: hatched are the times without collections (not running, not
// connected, no collections; overlapping ones merged and labelled by the
// first of those kinds among them), listed the times with collections
// but without some events (events not captured, Kubernetes not watched).
// Each in time order.
func historyGaps(f *HistoryFacts) (hatched, listed []historyGap) {
	if f.HistoryStart.IsZero() || !f.HistoryStart.Before(f.End) {
		return nil, nil
	}
	lo := latest(f.Start, f.HistoryStart)
	var raw []historyGap
	for _, g := range f.Collections {
		// The time before the first collection is before the history.
		if g.BeforeEpoch == 0 {
			continue
		}
		raw = append(raw, historyGap{kind: v1.HistoryGapKind_HISTORY_GAP_KIND_NO_COLLECTIONS, start: g.Start, end: g.End,
			beforeEpoch: g.BeforeEpoch, afterEpoch: g.AfterEpoch, beforeAt: g.BeforeAt, afterAt: g.AfterAt})
	}
	var k8s []historyGap
	var notWatching *v1.Event
	for _, e := range f.Events {
		switch e.GetKind() {
		case "collector.gap":
			raw = append(raw, historyGap{kind: v1.HistoryGapKind_HISTORY_GAP_KIND_NOT_RUNNING, start: e.GetTime().AsTime(),
				end: e.GetReceivedAt().AsTime(), event: e})
		case "collector.k8s_gap":
			k8s = append(k8s, historyGap{kind: v1.HistoryGapKind_HISTORY_GAP_KIND_KUBERNETES_NOT_WATCHED, start: e.GetTime().AsTime(),
				end: e.GetReceivedAt().AsTime(), event: e})
			notWatching = nil
		case "collector.k8s_not_watching":
			if notWatching == nil {
				notWatching = e
			}
		case "collector.started":
			notWatching = nil
		}
	}
	if notWatching != nil {
		k8s = append(k8s, historyGap{kind: v1.HistoryGapKind_HISTORY_GAP_KIND_KUBERNETES_NOT_WATCHED,
			start: notWatching.GetTime().AsTime(), event: notWatching})
	}
	for _, g := range CollectorConnectionGaps(f.Events) {
		raw = append(raw, historyGap{kind: v1.HistoryGapKind_HISTORY_GAP_KIND_NOT_CONNECTED, start: g.Start, end: g.End, event: g.Event})
	}
	for _, g := range EventsConnectionGaps(f.Events) {
		listed = append(listed, historyGap{kind: v1.HistoryGapKind_HISTORY_GAP_KIND_EVENTS_NOT_CAPTURED, start: g.Start, end: g.End,
			event: g.Event, connection: connectionOf(g.Event)})
	}
	listed = append(listed, k8s...)
	inRange := func(g historyGap) bool {
		return g.start.Before(f.End) && g.endOr(f.End.Add(time.Nanosecond)).After(lo)
	}
	hatched = mergeHatched(slices.DeleteFunc(raw, func(g historyGap) bool { return !inRange(g) }))
	for i := range hatched {
		// A time without collections starts no earlier than the history.
		if hatched[i].start.Before(f.HistoryStart) {
			hatched[i].start = f.HistoryStart
		}
	}
	listed = slices.DeleteFunc(listed, func(g historyGap) bool { return !inRange(g) })
	slices.SortStableFunc(listed, func(a, b historyGap) int { return a.start.Compare(b.start) })
	// A gap still open at the range's end has no end, as the events after
	// the range are not read.
	for _, gs := range [][]historyGap{hatched, listed} {
		for i := range gs {
			if gs[i].end.After(f.End) {
				gs[i].end = time.Time{}
			}
		}
	}
	return hatched, listed
}

// mergeHatched merges the times without collections that overlap or
// touch into one, of the first kind among them; its collections either
// side are those of the no-collections gaps it took in.
func mergeHatched(gs []historyGap) []historyGap {
	slices.SortStableFunc(gs, func(a, b historyGap) int { return cmp.Or(a.start.Compare(b.start), int(a.kind-b.kind)) })
	var out []historyGap
	for _, g := range gs {
		n := len(out)
		if n == 0 || (!out[n-1].end.IsZero() && g.start.After(out[n-1].end)) {
			out = append(out, g)
			continue
		}
		m := &out[n-1]
		if m.end.IsZero() || g.end.IsZero() {
			m.end = time.Time{}
		} else if g.end.After(m.end) {
			m.end = g.end
		}
		if g.kind < m.kind {
			m.kind, m.event = g.kind, g.event
		}
		if g.kind == v1.HistoryGapKind_HISTORY_GAP_KIND_NO_COLLECTIONS {
			if m.beforeEpoch == 0 {
				m.beforeEpoch, m.beforeAt = g.beforeEpoch, g.beforeAt
			}
			if g.afterEpoch != 0 {
				m.afterEpoch, m.afterAt = g.afterEpoch, g.afterAt
			}
		}
	}
	return out
}

// connectionOf is the connection an event's raw payload names.
func connectionOf(e *v1.Event) string {
	var raw struct {
		Connection string `json:"connection"`
	}
	_ = json.Unmarshal([]byte(e.GetRaw()), &raw)
	return raw.Connection
}

// CollectorConnectionGaps are the times the collector's own connection
// had no server or was closed, from the connection events in time order:
// each from collector.not_connected or collector.connection_closed to its
// next collector.reconnected, or to nats-lens starting again
// (collector.started); open when neither followed.
func CollectorConnectionGaps(evs []*v1.Event) []ReplayGap {
	var out []ReplayGap
	open := -1
	for _, e := range evs {
		switch e.GetKind() {
		case "collector.started":
			if open >= 0 {
				out[open].End, open = e.GetTime().AsTime(), -1
			}
		case "collector.not_connected", "collector.connection_closed":
			if connectionOf(e) == "collector" && open < 0 {
				open = len(out)
				out = append(out, ReplayGap{Kind: GapCollector, Start: e.GetTime().AsTime(), Event: e})
			}
		case "collector.reconnected":
			if connectionOf(e) == "collector" && open >= 0 {
				out[open].End, open = e.GetTime().AsTime(), -1
			}
		}
	}
	return out
}

// historyCoverage counts the range's collections over the covered span,
// the later of the range start and the history start to the range end,
// and the time without collections in it.
func historyCoverage(f *HistoryFacts, hatched []historyGap) *v1.HistoryCoverage {
	c := &v1.HistoryCoverage{Epochs: f.Epochs, PartialEpochs: f.PartialEpochs}
	if f.HistoryStart.IsZero() || !f.HistoryStart.Before(f.End) {
		return c
	}
	from := latest(f.Start, f.HistoryStart)
	c.CoveredFrom, c.CoveredTo = timestamppb.New(from), timestamppb.New(f.End)
	if f.Interval > 0 {
		c.ExpectedEpochs = int64(f.End.Sub(from) / f.Interval)
	}
	var without time.Duration
	for _, g := range hatched {
		s, e := latest(g.start, from), g.endOr(f.End)
		if e.After(f.End) {
			e = f.End
		}
		if e.After(s) {
			without += e.Sub(s)
		}
	}
	c.WithoutData = durationpb.New(without)
	return c
}

// HistoryUses is the uses table: growth over the covered span of the
// range, the forecasts and the checks still short of their lookback, the
// growth review of acceptances, the 24-hour pages and Replay over the
// history now.
func HistoryUses(f *HistoryFacts) []*v1.HistoryUse {
	var out []*v1.HistoryUse
	// Growth reads the covered span of the range, which grows only while
	// the range follows now.
	growth := &v1.HistoryUse{Id: "growth", Needs: durationpb.New(MinGrowthSpan)}
	if !f.HistoryStart.IsZero() && f.HistoryStart.Before(f.End) {
		from := latest(f.Start, f.HistoryStart)
		has := f.End.Sub(from)
		growth.Has, growth.Enough = durationpb.New(has), has >= MinGrowthSpan
		live := !f.End.Before(f.Now.Add(-f.Interval))
		if !growth.Enough && live && f.End.Sub(f.Start) >= MinGrowthSpan {
			growth.EnoughAt = timestamppb.New(from.Add(MinGrowthSpan))
		}
	}
	out = append(out, growth)
	lookbacks := map[string]time.Duration{}
	for _, l := range f.Lookbacks {
		lookbacks[l.Code] = l.Lookback
	}
	for _, code := range HistoryForecastChecks {
		if lb, ok := lookbacks[code]; ok && lb > 0 {
			out = append(out, historyUse(f, "forecast:"+code, code, lb))
		}
	}
	others := slices.Clone(f.Lookbacks)
	slices.SortStableFunc(others, func(a, b CheckLookback) int {
		return cmp.Or(cmp.Compare(b.Lookback, a.Lookback), cmp.Compare(a.Code, b.Code))
	})
	for _, l := range others {
		if l.Lookback <= 0 || slices.Contains(HistoryForecastChecks, l.Code) {
			continue
		}
		if u := historyUse(f, "lookback:"+l.Code, l.Code, l.Lookback); !u.GetEnough() {
			out = append(out, u)
		}
	}
	out = append(out, historyUse(f, "accept_review", "", AcceptRetention), historyUse(f, "day", "", HistoryDay))
	return append(out, historyUse(f, "replay", "", MaxReplaySpan))
}

// historyUse is a use that reads the history now: it has the time since
// the history start, is enough at the history start plus its need, and
// never with a retention shorter than the need.
func historyUse(f *HistoryFacts, id, code string, needs time.Duration) *v1.HistoryUse {
	u := &v1.HistoryUse{Id: id, CheckCode: code, Needs: durationpb.New(needs)}
	u.Never = f.Retention > 0 && f.Retention < needs
	if f.HistoryStart.IsZero() {
		return u
	}
	has := max(f.Now.Sub(f.HistoryStart), 0)
	u.Has, u.Enough = durationpb.New(has), has >= needs && !u.Never
	if !u.Enough && !u.Never {
		u.EnoughAt = timestamppb.New(f.HistoryStart.Add(needs))
	}
	return u
}

func archiveState(a ArchiveState) *v1.ArchiveState {
	m := &v1.ArchiveState{Enabled: a.Enabled, Location: a.Location, OldestHour: tsOrNil(a.OldestHour), NewestHour: tsOrNil(a.NewestHour),
		Hours: a.Hours, LastExportAt: tsOrNil(a.LastExportAt), NewestFinal: a.NewestFinal, LastError: a.LastError,
		LastErrorAt: tsOrNil(a.LastErrorAt), ListedAt: tsOrNil(a.ListedAt)}
	if a.Retention > 0 {
		m.ArchiveRetention = durationpb.New(a.Retention)
	}
	return m
}
