package fake

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// hole is a time without collections inside a scenario's history, in the
// fixed frame: from and to are epochs, and none lies between them. Both
// lie on the scenario's 30-second grid.
type hole struct{ from, to time.Time }

// holes are the scenario's noEpochs placed relative to now.
func (s scen) holes() []hole {
	out := make([]hole, 0, len(s.sc.noEpochs))
	for _, h := range s.sc.noEpochs {
		out = append(out, hole{h.from.Add(s.shift), h.to.Add(s.shift)})
	}
	return out
}

// holeAt is the hole whose inside holds t.
func (s scen) holeAt(t time.Time) (hole, bool) {
	for _, h := range s.holes() {
		if t.After(h.from) && t.Before(h.to) {
			return h, true
		}
	}
	return hole{}, false
}

// withinHole reports whether [b0, b1) lies inside a hole: no epoch
// started in it.
func (s scen) withinHole(b0, b1 time.Time) bool {
	for _, h := range s.holes() {
		if b0.After(h.from) && !b1.After(h.to) {
			return true
		}
	}
	return false
}

// holeGaps are the holes longer than ReplayEpochReach intervals that
// overlap A to B, as the collections rule finds them.
func (s scen) holeGaps(a, b time.Time) []api.ReplayGap {
	var out []api.ReplayGap
	for _, h := range s.holes() {
		if h.to.Sub(h.from) > api.ReplayEpochReach*sampleEvery && h.to.After(a) && h.from.Before(b) {
			out = append(out, api.ReplayGap{Kind: api.GapCollections, Start: h.from, End: h.to, BeforeEpoch: s.epochNumber(h.from),
				BeforeAt: h.from, AfterEpoch: s.epochNumber(h.to), AfterAt: h.to})
		}
	}
	return out
}

// retentionOr is the scenario's retention: its own, else defaultHistory.
func (sc *scenario) retentionOr() time.Duration { return cmp.Or(sc.retention, defaultHistory) }

// firstEpoch is when the scenario's oldest epoch started: the first point
// of the 30-second grid from the newest epoch back at or after dataStart.
func (s scen) firstEpoch() time.Time {
	last := s.lastEpoch()
	return last.Add(-last.Sub(s.dataStart()) / sampleEvery * sampleEvery)
}

// epochFrom is the first epoch that started at or after t; zero when none.
func (s scen) epochFrom(t time.Time) time.Time {
	first, last := s.firstEpoch(), s.lastEpoch()
	at := first
	if t.After(first) {
		at = first.Add((t.Sub(first) + sampleEvery - 1) / sampleEvery * sampleEvery)
	}
	if h, ok := s.holeAt(at); ok {
		at = h.to
	}
	if at.After(last) {
		return time.Time{}
	}
	return at
}

// epochAtOrBefore is the newest epoch that started at or before t; zero
// when none.
func (s scen) epochAtOrBefore(t time.Time) time.Time {
	at, _, ok := s.epochBefore(&v1.TimeRange{Start: ts(s.firstEpoch()), End: ts(t.Add(time.Nanosecond))})
	if !ok {
		return time.Time{}
	}
	return at
}

// epochPairs are the epochs the store's collections read returns for A to
// B: the first of its window (the last epoch before A minus the reach,
// else the first after it), each epoch after a hole with the one before
// the hole, and the last (the first epoch after B, else the last before
// it).
func (s scen) epochPairs(a, b time.Time) []api.EpochPair {
	reach := api.ReplayEpochReach * sampleEvery
	lo := s.epochAtOrBefore(a.Add(-reach - time.Nanosecond))
	if lo.IsZero() {
		lo = s.epochFrom(a.Add(-reach))
	}
	hi := s.epochFrom(b.Add(time.Nanosecond))
	if hi.IsZero() {
		hi = s.epochAtOrBefore(b)
	}
	if lo.IsZero() || hi.IsZero() || hi.Before(lo) {
		return nil
	}
	pair := func(prev, at time.Time) api.EpochPair {
		return api.EpochPair{Prev: s.epochNumber(prev), PrevAt: prev, Epoch: s.epochNumber(at), At: at}
	}
	out := []api.EpochPair{{Epoch: s.epochNumber(lo), At: lo}}
	for _, h := range s.holes() {
		if !h.from.Before(lo) && !h.to.After(hi) {
			out = append(out, pair(h.from, h.to))
		}
	}
	if last := out[len(out)-1].At; hi.After(last) {
		prev := hi.Add(-sampleEvery)
		if h, ok := s.holeAt(prev); ok {
			prev = h.from
		}
		out = append(out, pair(prev, hi))
	}
	return out
}

// epochCounts counts the epochs that started in [start, end) and those
// that missed a server.
func (s scen) epochCounts(start, end time.Time) (epochs, partial int64) {
	lo, hi := s.epochFrom(start), s.epochAtOrBefore(end.Add(-time.Nanosecond))
	if lo.IsZero() || hi.IsZero() || hi.Before(lo) {
		return 0, 0
	}
	for at := lo; !at.After(hi); at = at.Add(sampleEvery) {
		if h, ok := s.holeAt(at); ok {
			at = h.to.Add(-sampleEvery)
			continue
		}
		epochs++
		if s.sc.serverDown != nil && s.anyDown(at) {
			partial++
		}
	}
	return epochs, partial
}

// checkLookbacks are the built-in checks' lookbacks, read once from
// checks/*.sql.
var checkLookbacks = sync.OnceValue(func() []api.CheckLookback {
	set, err := checks.Load(config.Checks{})
	if err != nil {
		return nil
	}
	var out []api.CheckLookback
	for _, c := range set.Checks() {
		if lb := set.Lookback(c.Code); lb > 0 {
			out = append(out, api.CheckLookback{Code: c.Code, Lookback: lb})
		}
	}
	return out
})

// DataHistory reads the scenario's history by the store's rules: the
// epochs on their grid with the scenario's holes, the collector events
// around the range, the scenario's archive and the built-in checks'
// lookbacks.
func (p *Provider) DataHistory(_ context.Context, req *v1.GetDataHistoryRequest) (*api.HistoryFacts, error) {
	s := p.scen()
	start, end := req.GetRange().GetStart().AsTime().UTC(), req.GetRange().GetEnd().AsTime().UTC()
	f := &api.HistoryFacts{Start: start, End: end, Now: s.now, Interval: sampleEvery, Retention: s.sc.retentionOr(),
		HistoryStart: s.dataStart(), NewestEpochAt: s.lastEpoch(), Lookbacks: checkLookbacks()}
	f.Epochs, f.PartialEpochs = s.epochCounts(start, end)
	f.Collections = api.CollectionGaps(start, end, api.ReplayEpochReach*sampleEvery, s.epochPairs(start, end))
	if s.sc.archive != nil {
		f.Archive = s.sc.archive(s)
	}
	all := p.events(s)
	for _, e := range all {
		if at := e.GetTime().AsTime(); f.EventsStart.IsZero() || at.Before(f.EventsStart) {
			f.EventsStart = at
		}
		at, kind := e.GetTime().AsTime(), e.GetKind()
		gap := kind == "collector.gap" || kind == "collector.k8s_gap"
		if e.GetSource() != v1.EventSource_EVENT_SOURCE_COLLECTOR || !slices.Contains(api.HistoryEventKinds, kind) || at.After(end) ||
			(at.Before(start.Add(-api.HistoryEventsLookback)) && !(gap && !e.GetReceivedAt().AsTime().Before(start))) {
			continue
		}
		f.Events = append(f.Events, e)
	}
	slices.SortStableFunc(f.Events, func(a, b *v1.Event) int { return a.GetTime().AsTime().Compare(b.GetTime().AsTime()) })
	hist := s.dataStart()
	for _, fd := range p.findings(s) {
		if fd.GetState() != firing {
			continue
		}
		since := fd.GetSince().AsTime()
		if (fd.GetConditionSince() != nil && fd.GetConditionSince().AsTime().Before(hist)) ||
			(!since.Before(hist) && since.Before(hist.Add(api.HistoryStartSlack))) {
			f.FindingsAtStart++
		}
	}
	return f, nil
}

// demoArchive is an archive kept for archive_retention days that holds
// the hours of the last held days to the open hour, exported
// provisionally three minutes ago.
func demoArchive(days, held int) func(s scen) api.ArchiveState {
	return func(s scen) api.ArchiveState {
		newest := s.now.Truncate(time.Hour)
		oldest := s.now.Add(-time.Duration(held) * 24 * time.Hour).Truncate(time.Hour)
		return api.ArchiveState{Enabled: true, Location: "/var/lib/natslens/archive", Retention: time.Duration(days) * 24 * time.Hour,
			OldestHour: oldest, NewestHour: newest, Hours: int32(newest.Sub(oldest)/time.Hour) + 1, LastExportAt: s.now.Add(-3 * time.Minute),
			ListedAt: newest.Add(5 * time.Minute)}
	}
}
