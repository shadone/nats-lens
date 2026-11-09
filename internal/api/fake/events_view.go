// The fake applies the store's event rules to scenario events, which carry
// no payload. Where it differs from the store:
//   - tooling is a client name with the natslens- prefix, not the
//     configured tooling names;
//   - a loopback client counts as tooling in every epoch: the scenarios
//     store no loopback decisions;
//   - a Kubernetes event counts 1: there is no stored count;
//   - an API audit record's subject is its summary up to " by ", and a
//     Kubernetes event's object is the second word of its summary.

package fake

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// routineEvent applies the routine rule to a scenario event: a routine
// kind, an API audit record of a successful call that changes nothing, or
// a connect or disconnect of a tooling client.
func routineEvent(e *v1.Event) bool {
	switch {
	case slices.Contains(api.RoutineKinds, e.GetKind()):
		return true
	case e.GetKind() == api.AuditKind:
		subject, _, _ := strings.Cut(e.GetSummary(), " by ")
		return api.RoutineAudit(subject, e.GetReason())
	}
	return slices.Contains(api.ToolingKinds, e.GetKind()) &&
		(strings.HasPrefix(e.GetClientName(), "natslens-") || e.GetClientHost() == "127.0.0.1" || e.GetClientHost() == "::1")
}

// markRoutine sets the routine flag; a routine event is not coloured.
func markRoutine(e *v1.Event) {
	e.Routine = routineEvent(e)
	if e.Routine {
		e.Severity = v1.Severity_SEVERITY_UNSPECIFIED
	}
}

// subject is what a folded row shares beyond the event's fields: a
// finding event's finding, a Kubernetes event's object.
func subject(e *v1.Event) string {
	switch e.GetSource() {
	case srcFinding:
		return e.GetTransition().GetFindingId()
	case srcK8s:
		if w := strings.Fields(e.GetSummary()); len(w) > 1 {
			return strings.TrimSuffix(w[1], ":")
		}
	}
	return ""
}

// richRequest reports whether a request asks for routine events, folding,
// facets or a histogram; other requests take the old path unchanged.
func richRequest(req *v1.ListEventsRequest) bool {
	return req.GetHideRoutine() || req.GetFoldRepeats() || req.GetHistogramBuckets() > 0 || req.GetIncludeFacets()
}

// listEventsRich answers a request with routine events, folding, facets
// or a histogram, by the same rules as storeprov.
func (p *Provider) listEventsRich(req *v1.ListEventsRequest, cur api.Cursor, all []*v1.Event) *v1.ListEventsResponse {
	hide := req.GetHideRoutine()
	// A folded list keeps to the events of its first page; IDs grow with
	// time, so the newest has the highest.
	var upto int64
	if req.GetFoldRepeats() {
		upto = cur.Upto
		if upto == 0 && len(all) > 0 {
			upto = num(all[0].GetId())
		}
		all = slices.DeleteFunc(all, func(e *v1.Event) bool { return num(e.GetId()) > upto })
	}
	res := &v1.ListEventsResponse{}
	var shown []*v1.Event
	for _, e := range all {
		markRoutine(e)
		if !keepEvent(e, req) {
			continue
		}
		if hide && e.GetRoutine() {
			res.RoutineHidden++
			continue
		}
		res.TotalCount++
		shown = append(shown, e)
	}
	rows := shown
	if req.GetFoldRepeats() {
		rows = fold(shown)
	}
	for _, e := range rows {
		if cur.ID != 0 && !olderThan(e, cur) {
			continue
		}
		if len(res.Events) == int(req.GetPageSize()) {
			l := res.Events[len(res.Events)-1]
			res.NextPageToken = api.EncodeCursor(api.Cursor{Time: l.GetTime().AsTime(), ID: num(l.GetId()), Upto: upto})
			break
		}
		res.Events = append(res.Events, e)
	}
	if req.GetIncludeFacets() {
		res.Facets = facets(all, req)
		for _, e := range all {
			if inRange(e.GetTime().AsTime(), req.GetRange()) {
				res.TotalUnfiltered++
			}
		}
	}
	if hide {
		res.RoutineGroups = routineGroups(all, req)
	}
	if n := req.GetHistogramBuckets(); n > 0 {
		res.Histogram = histogram(all, req, int(n))
	}
	return res
}

// foldKey is what the events of one folded row share.
type foldKey struct {
	source                                                           v1.EventSource
	kind, server, account, stream, consumer, client, reason, subject string
}

// fold turns newest-first events into rows: each run of events with the
// same key, each at most api.FoldGap after the previous one, becomes its
// newest event with repeats and first_time set. Rows come newest first.
func fold(evs []*v1.Event) []*v1.Event {
	type run struct {
		newest, oldest *v1.Event
		n              int32
	}
	open := map[foldKey]*run{}
	var runs []*run
	for _, e := range slices.Backward(evs) {
		k := foldKey{e.GetSource(), e.GetKind(), e.GetServer(), e.GetAccount(), e.GetStream(), e.GetConsumer(), e.GetClientName(), e.GetReason(), subject(e)}
		r := open[k]
		if r == nil || e.GetTime().AsTime().Sub(r.newest.GetTime().AsTime()) > api.FoldGap {
			r = &run{oldest: e}
			open[k] = r
			runs = append(runs, r)
		}
		r.newest = e
		r.n++
	}
	out := make([]*v1.Event, 0, len(runs))
	for _, r := range runs {
		r.newest.Repeats, r.newest.FirstTime = r.n, r.oldest.GetTime()
		out = append(out, r.newest)
	}
	slices.SortStableFunc(out, func(a, b *v1.Event) int {
		return cmp.Or(b.GetTime().AsTime().Compare(a.GetTime().AsTime()), cmp.Compare(num(b.GetId()), num(a.GetId())))
	})
	return out
}

// facets counts the events per source and per server, each without its
// own dimension's filter.
func facets(all []*v1.Event, req *v1.ListEventsRequest) []*v1.EventFacet {
	count := func(r *v1.ListEventsRequest, value func(*v1.Event) string) map[string]int32 {
		out := map[string]int32{}
		for _, e := range all {
			if keepEvent(e, r) && !(r.GetHideRoutine() && e.GetRoutine()) && value(e) != "" {
				out[value(e)]++
			}
		}
		return out
	}
	bySource := proto.Clone(req).(*v1.ListEventsRequest)
	bySource.Sources = nil
	byServer := proto.Clone(req).(*v1.ListEventsRequest)
	if byServer.GetEntity().GetType() == v1.EntityType_ENTITY_TYPE_SERVER {
		byServer.Entity = nil
	}
	return append(sortedFacets("source", count(bySource, func(e *v1.Event) string { return e.GetSource().String() })),
		sortedFacets("server", count(byServer, func(e *v1.Event) string { return e.GetServer() }))...)
}

// sortedFacets lists a dimension's counts, largest first.
func sortedFacets(dim string, counts map[string]int32) []*v1.EventFacet {
	out := make([]*v1.EventFacet, 0, len(counts))
	for v, n := range counts {
		out = append(out, &v1.EventFacet{Dimension: dim, Value: v, Count: n})
	}
	slices.SortFunc(out, func(a, b *v1.EventFacet) int {
		return cmp.Or(cmp.Compare(b.GetCount(), a.GetCount()), cmp.Compare(a.GetValue(), b.GetValue()))
	})
	return out
}

// routineGroups counts the routine events the request matches by kind and
// client, largest first.
func routineGroups(all []*v1.Event, req *v1.ListEventsRequest) []*v1.RoutineGroup {
	counts := map[[2]string]int32{}
	for _, e := range all {
		if e.GetRoutine() && keepEvent(e, req) {
			counts[[2]string{e.GetKind(), e.GetClientName()}]++
		}
	}
	out := make([]*v1.RoutineGroup, 0, len(counts))
	for k, n := range counts {
		out = append(out, &v1.RoutineGroup{Label: api.RoutineLabel(k[0], k[1]), Kind: k[0], ClientName: k[1], Count: n})
	}
	slices.SortFunc(out, func(a, b *v1.RoutineGroup) int {
		return cmp.Or(cmp.Compare(b.GetCount(), a.GetCount()), cmp.Compare(a.GetKind(), b.GetKind()),
			cmp.Compare(a.GetClientName(), b.GetClientName()))
	})
	return out[:min(len(out), api.MaxRoutineGroups)]
}

// histogram counts the events the request matches, routine ones
// included, in n buckets over the range.
func histogram(all []*v1.Event, req *v1.ListEventsRequest, n int) []*v1.EventBucket {
	start, end := req.GetRange().GetStart().AsTime(), req.GetRange().GetEnd().AsTime()
	width := api.HistogramWidth(end.Sub(start), n)
	out := make([]*v1.EventBucket, api.BucketCount(end.Sub(start), width))
	for i := range out {
		out[i] = &v1.EventBucket{Start: ts(start.Add(time.Duration(i) * width))}
	}
	for _, e := range all {
		if !keepEvent(e, req) {
			continue
		}
		b := out[int(e.GetTime().AsTime().Sub(start)/width)]
		switch {
		case e.GetRoutine():
			b.Routine++
		case e.GetSeverity() >= v1.Severity_SEVERITY_WARNING:
			b.Warning++
		default:
			b.Notable++
		}
	}
	return out
}
