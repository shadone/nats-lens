package apitest

import (
	"fmt"
	"slices"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// historyMonth is the range the gap rows read: wide enough for a gap days
// back.
const historyMonth = 30 * 24 * time.Hour

// history reads the Data history of the range.
func (q Q) history(r *v1.TimeRange) *v1.GetDataHistoryResponse {
	q.t.Helper()
	return must(q.env.Client.GetDataHistory(ctx, connect.NewRequest(&v1.GetDataHistoryRequest{System: q.System, Range: r}))).of(q.t)
}

// hatched reports whether a gap is a time without collections.
func hatched(g *v1.HistoryGap) bool {
	switch g.GetKind() {
	case v1.HistoryGapKind_HISTORY_GAP_KIND_NOT_RUNNING, v1.HistoryGapKind_HISTORY_GAP_KIND_NOT_CONNECTED,
		v1.HistoryGapKind_HISTORY_GAP_KIND_NO_COLLECTIONS:
		return true
	}
	return false
}

// historyGapOf finds a gap of the kind in the system's last 30 days.
func historyGapOf(kind v1.HistoryGapKind) func(Q) string {
	return func(q Q) string {
		for _, g := range q.history(q.Last(historyMonth)).GetGaps() {
			if g.GetKind() == kind {
				end := "still now"
				if g.GetEnd() != nil {
					end = g.GetEnd().AsTime().Format(time.RFC3339)
				}
				return g.GetStart().AsTime().Format(time.RFC3339) + " to " + end
			}
		}
		return ""
	}
}

// historyUse finds a use of the system's last day that ok accepts.
func historyUse(ok func(*v1.HistoryUse) bool) func(Q) string {
	return func(q Q) string {
		for _, u := range q.history(q.day()).GetUses() {
			if ok(u) {
				return u.GetId()
			}
		}
		return ""
	}
}

func historyRows() []State {
	var rows []State
	for _, k := range []v1.HistoryGapKind{
		v1.HistoryGapKind_HISTORY_GAP_KIND_NOT_RUNNING, v1.HistoryGapKind_HISTORY_GAP_KIND_NOT_CONNECTED,
		v1.HistoryGapKind_HISTORY_GAP_KIND_NO_COLLECTIONS, v1.HistoryGapKind_HISTORY_GAP_KIND_EVENTS_NOT_CAPTURED,
		v1.HistoryGapKind_HISTORY_GAP_KIND_KUBERNETES_NOT_WATCHED, v1.HistoryGapKind_HISTORY_GAP_KIND_BEFORE_HISTORY,
	} {
		rows = append(rows, State{"a data history gap of kind " + k.String(), historyGapOf(k)})
	}
	return append(rows,
		// A range without collections is one gap over all of it, and its
		// coverage counts no collection.
		State{"a data history range without collections", func(q Q) string {
			r := q.Last(30 * time.Minute)
			res := q.history(r)
			if res.GetHistoryStart() == nil || res.GetCoverage().GetEpochs() != 0 {
				return ""
			}
			for _, g := range res.GetGaps() {
				if hatched(g) && !g.GetStart().AsTime().After(r.GetStart().AsTime()) &&
					(g.GetEnd() == nil || !g.GetEnd().AsTime().Before(r.GetEnd().AsTime())) {
					return g.GetKind().String()
				}
			}
			q.t.Errorf("system %q: no gap covers a range without collections: %v", q.System, res.GetGaps())
			return ""
		}},
		// The coverage counts the collections the other pages read.
		State{"data history coverage agrees with the overview's newest epoch", func(q Q) string {
			res, fr := q.history(q.day()), q.freshness(q.day())
			c := res.GetCoverage()
			if (fr.GetEpoch() != 0) != (c.GetEpochs() > 0) || (fr.GetPartial() && c.GetPartialEpochs() == 0) ||
				(fr.GetEpoch() != 0 && res.GetNewestEpochAt().AsTime().Before(fr.GetEpochAt().AsTime())) {
				q.t.Errorf("system %q: coverage %v, newest %v; overview epoch %d at %v partial %v", q.System, c, res.GetNewestEpochAt(),
					fr.GetEpoch(), fr.GetEpochAt(), fr.GetPartial())
				return ""
			}
			if c.GetEpochs() == 0 || c.GetExpectedEpochs() < c.GetEpochs()-1 {
				return ""
			}
			return fmt.Sprintf("%d of %d collections", c.GetEpochs(), c.GetExpectedEpochs())
		}},
		State{"data history starts where GetLensInfo says", func(q Q) string {
			info := must(q.env.Client.GetLensInfo(ctx, connect.NewRequest(&v1.GetLensInfoRequest{System: q.System}))).of(q.t)
			res := q.history(q.day())
			if !res.GetHistoryStart().AsTime().Equal(info.GetHistoryStart().AsTime()) || (res.HistoryStart == nil) != (info.HistoryStart == nil) {
				q.t.Errorf("system %q: data history starts %v, GetLensInfo %v", q.System, res.GetHistoryStart(), info.GetHistoryStart())
				return ""
			}
			if res.GetHistoryStart() == nil || res.GetRetention().AsDuration() != info.GetRetention().AsDuration() {
				return ""
			}
			return res.GetHistoryStart().AsTime().Format(time.RFC3339)
		}},
		// A chart draws no point for a time without collections: no
		// bucket of cluster.connections lies wholly inside one.
		State{"no cluster.connections point inside a time without collections", func(q Q) string {
			checked := 0
			for _, g := range q.history(q.day()).GetGaps() {
				if !hatched(g) || g.GetEnd() == nil {
					continue
				}
				from, to := g.GetStart().AsTime(), g.GetEnd().AsTime()
				s := must(q.env.Client.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{System: q.System, Metric: "cluster.connections",
					Range:     &v1.TimeRange{Start: timestamppb.New(from.Add(-10 * time.Minute)), End: timestamppb.New(to.Add(10 * time.Minute))},
					MaxPoints: 1000}))).of(q.t).GetSeries()
				if len(s.GetPoints()) == 0 {
					continue
				}
				step := s.GetStep().AsDuration()
				for _, p := range s.GetPoints() {
					if at := p.GetTime().AsTime(); at.After(from) && !at.Add(step).After(to) {
						q.t.Errorf("system %q: point at %v inside %v gap %v to %v", q.System, at, g.GetKind(), from, to)
						return ""
					}
				}
				checked++
			}
			if checked == 0 {
				return ""
			}
			return fmt.Sprintf("%d gaps", checked)
		}},
		State{"a history use without enough history yet", historyUse(func(u *v1.HistoryUse) bool {
			// Review reads a bounded window at the newest collection. Its
			// missing span does not support a wall-clock countdown.
			return u.GetId() == "accept_review" && u.GetNeeds().AsDuration() == api.AcceptRetention &&
				u.GetHas() != nil && u.GetHas().AsDuration() > 0 && u.GetHas().AsDuration() < u.GetNeeds().AsDuration() &&
				!u.GetEnough() && !u.GetNever() && u.GetEnoughAt() == nil
		})},
		State{"a history use with enough history", historyUse(func(u *v1.HistoryUse) bool {
			return u.GetId() == "growth" && u.GetNeeds().AsDuration() == api.MinGrowthSpan &&
				u.GetHas() != nil && u.GetHas().AsDuration() >= api.MinGrowthSpan &&
				u.GetEnough() && !u.GetNever() && u.GetEnoughAt() == nil
		})},
		State{"history uses describe global sample availability in every system", everySystem(func(q Q) string {
			res := q.history(q.day())
			for _, u := range res.GetUses() {
				needs, has := u.GetNeeds().AsDuration(), u.GetHas().AsDuration()
				wantNeeds := map[string]time.Duration{"growth": api.MinGrowthSpan, "accept_review": api.AcceptRetention,
					"day": api.HistoryDay, "replay": api.MaxReplaySpan}[u.GetId()]
				never := res.GetRetention().AsDuration() > 0 && res.GetRetention().AsDuration() < needs
				if needs <= 0 || (wantNeeds > 0 && needs != wantNeeds) || has < 0 ||
					u.GetNever() != never || u.GetEnough() != (u.GetHas() != nil && has >= needs && !never) ||
					(u.GetEnoughAt() != nil && (u.GetId() != "growth" || u.GetEnough() || never || u.GetHas() == nil ||
						res.GetNewestEpochAt().AsTime().Before(q.env.Now.Add(-res.GetCollectInterval().AsDuration())))) {
					q.t.Errorf("system %q: inconsistent global availability %v, retention %v", q.System, u, res.GetRetention())
				}
			}
			return fmt.Sprintf("%d global sample spans; not entity eligibility or complete coverage", len(res.GetUses()))
		})},
		State{"a history use limited by retention without a countdown", func(q Q) string {
			res := q.history(q.day())
			for _, u := range res.GetUses() {
				if u.GetNever() && !u.GetEnough() && u.GetEnoughAt() == nil && u.GetHas() != nil &&
					res.GetRetention().AsDuration() > 0 && u.GetNeeds().AsDuration() > res.GetRetention().AsDuration() {
					return u.GetId()
				}
			}
			return ""
		}},
		State{"no growth samples in a range before history in every system", everySystem(func(q Q) string {
			r := q.Last(90 * 24 * time.Hour)
			r.End = timestamppb.New(q.env.Now.Add(-60 * 24 * time.Hour))
			res := q.history(r)
			for _, u := range res.GetUses() {
				if u.GetId() != "growth" {
					continue
				}
				if u.GetNeeds().AsDuration() != api.MinGrowthSpan || u.GetHas() != nil || u.GetEnough() || u.GetEnoughAt() != nil || res.GetCoverage().GetEpochs() != 0 {
					q.t.Errorf("system %q: growth without samples: %v, coverage %v", q.System, u, res.GetCoverage())
				}
				return "no growth samples"
			}
			q.t.Errorf("system %q: missing growth use", q.System)
			return ""
		})},
		State{"an archive with archived hours", func(q Q) string {
			a := q.history(q.day()).GetArchive()
			if a.GetEnabled() && a.GetHours() > 0 && a.GetListedAt() != nil && !a.GetNewestHour().AsTime().Before(a.GetOldestHour().AsTime()) {
				return fmt.Sprintf("%d hours in %s", a.GetHours(), a.GetLocation())
			}
			return ""
		}},
		State{"a system without an archive", func(q Q) string {
			if a := q.history(q.day()).GetArchive(); !a.GetEnabled() && a.GetHours() == 0 {
				return "no archive"
			}
			return ""
		}},
		// Gaps come newest first.
		State{"data history gaps newest first", func(q Q) string {
			gs := q.history(q.Last(historyMonth)).GetGaps()
			if !slices.IsSortedFunc(gs, func(a, b *v1.HistoryGap) int { return b.GetStart().AsTime().Compare(a.GetStart().AsTime()) }) {
				q.t.Errorf("system %q: gaps not newest first: %v", q.System, gs)
				return ""
			}
			if len(gs) < 2 {
				return ""
			}
			return fmt.Sprintf("%d gaps", len(gs))
		}},
	)
}
