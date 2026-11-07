package api

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// finish turns the drafts into steps: evidence and entities without
// repeats and capped, an ID each, in time order with the outside-range
// steps last, at most maxReplaySteps in the range and maxOutsideSteps
// after it. It returns the steps and how many were left out.
func (b *replayBuilder) finish() ([]*v1.ReplayStep, int32) {
	var in, out []*v1.ReplayStep
	ids := map[string]int{}
	for _, d := range b.steps {
		s := &v1.ReplayStep{Time: tsOrNil(d.time), End: tsOrNil(d.end), OpenAtB: d.openAtB, Kind: d.kind, Severity: d.severity,
			Title: d.title, Body: d.body, OutsideRange: d.outside, GapKind: d.gapKind,
			LeadersTaken: leaderCounts(d.leaders)}
		ev := distinctLabels(uniqueEvidence(d.evidence))
		s.EvidenceCount, s.Evidence = int32(len(ev)), capEvidence(ev, maxStepEvidence)
		ents := uniqueEntities(d.entities)
		s.EntityCount, s.Entities = int32(len(ents)), ents[:min(len(ents), maxStepEntities)]
		s.Id = stepID(s, ids)
		if d.outside {
			out = append(out, s)
		} else {
			in = append(in, s)
		}
	}
	omitted := 0
	in, omitted = capSteps(in, maxReplaySteps)
	sortSteps(out)
	if len(out) > maxOutsideSteps {
		omitted += len(out) - maxOutsideSteps
		out = out[:maxOutsideSteps]
	}
	return append(in, out...), int32(omitted)
}

func sortSteps(ss []*v1.ReplayStep) {
	slices.SortStableFunc(ss, func(x, y *v1.ReplayStep) int {
		return cmp.Or(x.GetTime().AsTime().Compare(y.GetTime().AsTime()), cmp.Compare(x.GetKind(), y.GetKind()), cmp.Compare(x.GetId(), y.GetId()))
	})
}

// kept reports whether a step is kept first when the steps are capped:
// warning and critical steps, restarts, gone assets and gaps.
func kept(s *v1.ReplayStep) bool {
	switch s.GetKind() {
	case v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART, v1.ReplayStepKind_REPLAY_STEP_KIND_GONE, v1.ReplayStepKind_REPLAY_STEP_KIND_GAP:
		return true
	}
	return s.GetSeverity() >= v1.Severity_SEVERITY_WARNING
}

// capSteps keeps at most n steps, those kept first, earliest first, and
// returns them in order with how many were left out.
func capSteps(ss []*v1.ReplayStep, n int) ([]*v1.ReplayStep, int) {
	sortSteps(ss)
	if len(ss) <= n {
		return ss, 0
	}
	var first, rest []*v1.ReplayStep
	for _, s := range ss {
		if kept(s) {
			first = append(first, s)
		} else {
			rest = append(rest, s)
		}
	}
	out := append(first[:min(len(first), n)], rest[:max(0, min(len(rest), n-len(first)))]...)
	sortSteps(out)
	return out, len(ss) - len(out)
}

// uniqueEvidence drops nil and repeated evidence and orders it by time.
func uniqueEvidence(ev []*v1.ReplayEvidence) []*v1.ReplayEvidence {
	seen := map[string]bool{}
	var out []*v1.ReplayEvidence
	for _, e := range ev {
		if e == nil {
			continue
		}
		k := e.GetKind().String() + "|" + e.GetId() + "|" + e.GetTime().AsTime().String()
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, e)
	}
	slices.SortStableFunc(out, func(x, y *v1.ReplayEvidence) int {
		return cmp.Or(x.GetTime().AsTime().Compare(y.GetTime().AsTime()), cmp.Compare(x.GetKind(), y.GetKind()), cmp.Compare(x.GetId(), y.GetId()))
	})
	return out
}

// capEvidence keeps at most n of a step's evidence, in time order: the
// step's own evidence first (collections, findings, announcements, pod
// events), then elections, which leaders_taken counts in full.
func capEvidence(ev []*v1.ReplayEvidence, n int) []*v1.ReplayEvidence {
	if len(ev) <= n {
		return ev
	}
	keep := map[*v1.ReplayEvidence]bool{}
	for _, elections := range []bool{false, true} {
		for _, e := range ev {
			if len(keep) < n && (e.GetElected() != "") == elections {
				keep[e] = true
			}
		}
	}
	out := make([]*v1.ReplayEvidence, 0, n)
	for _, e := range ev {
		if keep[e] {
			out = append(out, e)
		}
	}
	return out
}

// leaderCounts are a step's elections by new leader, most first, then by
// name.
func leaderCounts(by map[string]int) []*v1.ReplayLeaderCount {
	var out []*v1.ReplayLeaderCount
	for l, n := range by {
		out = append(out, &v1.ReplayLeaderCount{Leader: l, Count: int32(n)})
	}
	slices.SortFunc(out, func(x, y *v1.ReplayLeaderCount) int {
		return cmp.Or(cmp.Compare(y.GetCount(), x.GetCount()), cmp.Compare(x.GetLeader(), y.GetLeader()))
	})
	return out
}

// distinctLabels tells apart evidence of one step whose labels are the
// same, as a folded step's often are: such labels begin with their time,
// and a finding's that still repeat end with its entity.
func distinctLabels(ev []*v1.ReplayEvidence) []*v1.ReplayEvidence {
	relabel := func(ev []*v1.ReplayEvidence, f func(e *v1.ReplayEvidence) string) []*v1.ReplayEvidence {
		n := map[string]int{}
		for _, e := range ev {
			n[e.GetLabel()]++
		}
		out := make([]*v1.ReplayEvidence, len(ev))
		for i, e := range ev {
			out[i] = e
			if n[e.GetLabel()] > 1 {
				c := proto.Clone(e).(*v1.ReplayEvidence)
				c.Label = f(e)
				out[i] = c
			}
		}
		return out
	}
	ev = relabel(ev, func(e *v1.ReplayEvidence) string {
		if e.GetTime() == nil {
			return e.GetLabel()
		}
		return clockText(e.GetTime().AsTime()) + " " + e.GetLabel()
	})
	return relabel(ev, func(e *v1.ReplayEvidence) string {
		if _, key, ok := strings.Cut(e.GetId(), ":"); ok && e.GetKind() == v1.ReplayEvidenceKind_REPLAY_EVIDENCE_KIND_FINDING {
			return e.GetLabel() + ", " + key
		}
		return e.GetLabel()
	})
}

func uniqueEntities(es []*v1.EntityRef) []*v1.EntityRef {
	seen := map[string]bool{}
	var out []*v1.EntityRef
	for _, e := range es {
		if e == nil {
			continue
		}
		k := e.GetType().String() + "|" + e.GetKey()
		if !seen[k] {
			seen[k] = true
			out = append(out, e)
		}
	}
	return out
}

// stepID is the step's kind and its first evidence, for example
// "restart:collection:812"; a repeat gets a suffix.
func stepID(s *v1.ReplayStep, ids map[string]int) string {
	kind := strings.ToLower(strings.TrimPrefix(s.GetKind().String(), "REPLAY_STEP_KIND_"))
	id := kind + ":" + strconv.FormatInt(s.GetTime().AsTime().Unix(), 10)
	if ev := s.GetEvidence(); len(ev) > 0 {
		id = kind + ":" + strings.ToLower(strings.TrimPrefix(ev[0].GetKind().String(), "REPLAY_EVIDENCE_KIND_")) + ":" + ev[0].GetId()
	}
	if s.GetOutsideRange() {
		id += ":after"
	}
	ids[id]++
	if n := ids[id]; n > 1 {
		id += ":" + strconv.Itoa(n)
	}
	return id
}

// why is a cause of a count staying unset; nil when there is none.
type why struct {
	reason   v1.ReplayUnobservedReason
	from, to time.Time
}

// first is the first cause that applies.
func first(ws ...*why) *why {
	for _, w := range ws {
		if w != nil {
			return w
		}
	}
	return nil
}

// cause is a cause without a span when cond holds.
func cause(cond bool, r v1.ReplayUnobservedReason) *why {
	if !cond {
		return nil
	}
	return &why{reason: r}
}

// gapCause is the earliest gap of the kind that overlaps A to B, by the
// rule of gapOverlaps, as a cause.
func (b *replayBuilder) gapCause(kind string, r v1.ReplayUnobservedReason) *why {
	var out *why
	for _, g := range b.f.Gaps {
		if g.Kind == kind && !g.Start.After(b.f.B) && (g.End.IsZero() || g.End.After(b.f.A)) && (out == nil || g.Start.Before(out.from)) {
			out = &why{reason: r, from: g.Start, to: g.End}
		}
	}
	return out
}

// cutCause is the read limit as a cause when it cut the rows at or
// before B: a read that reached past B read every row of A to B.
func (b *replayBuilder) cutCause(cut bool, until time.Time) *why {
	if !cut || until.After(b.f.B) {
		return nil
	}
	return &why{reason: v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_READ_LIMIT, from: until}
}

// counts are the response's numbers and why each unset one is unset: a
// count stays unset when what it counts was not observed, and then has
// one entry naming the first cause that applies, in the order checked
// here.
func (b *replayBuilder) counts() (*v1.ReplayCounts, []*v1.ReplayUnobserved) {
	f, c := b.f, &v1.ReplayCounts{}
	var unobserved []*v1.ReplayUnobserved
	// set sets a count, or records why it stays unset.
	set := func(name string, dst **int32, v int, w *why) {
		if w == nil {
			x := int32(v)
			*dst = &x
			return
		}
		unobserved = append(unobserved, &v1.ReplayUnobserved{Count: name, Reason: w.reason, From: tsOrNil(w.from), To: tsOrNil(w.to)})
	}
	R := func(r v1.ReplayUnobservedReason) v1.ReplayUnobservedReason { return r }
	noCollections := cause(!f.Collected, R(v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_NO_COLLECTIONS))
	noA := cause(f.EpochA == 0, R(v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_NO_COLLECTION_AT_A))
	noB := cause(f.EpochB == 0, R(v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_NO_COLLECTION_AT_B))

	set("servers_restarted", &c.ServersRestarted, len(f.Restarts), noCollections)
	// A stop needs the collection at B to be told.
	set("servers_stopped", &c.ServersStopped, len(f.Stops), first(noCollections, noB))
	raised := map[string]bool{}
	for _, t := range f.Transitions {
		if t.State == v1.FindingState_FINDING_STATE_FIRING && t.Severity >= v1.Severity_SEVERITY_WARNING && b.inRange(t.Time) {
			raised[t.FindingID] = true
		}
	}
	set("findings_raised", &c.FindingsRaised, len(raised), first(noCollections, b.cutCause(f.TransitionsTruncated, f.TransitionsReadUntil)))

	streams, consumers, deleted, unknown := 0, 0, 0, 0
	for _, g := range f.Gone {
		switch goneClass(g) {
		case goneDeleted:
			deleted++
		case goneUnknown:
			unknown++
		default:
			if g.Entity.GetType() == v1.EntityType_ENTITY_TYPE_STREAM {
				streams++
			} else {
				consumers++
			}
		}
	}
	gone := first(noA, noB, cause(!f.GoneRead, R(v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_GONE_NOT_READ)))
	set("streams_gone_without_delete", &c.StreamsGoneWithoutDelete, streams, gone)
	set("consumers_gone_without_delete", &c.ConsumersGoneWithoutDelete, consumers, gone)
	set("assets_gone_with_delete", &c.AssetsGoneWithDelete, deleted, gone)
	set("assets_unknown_at_b", &c.AssetsUnknownAtB, unknown, gone)

	// Event capture over A to B: events stored up to B, nats-lens running
	// and every connection that captures events connected (collections do
	// not tell, as events come on connections of their own); the counts
	// from the event rows also need the read limit not to have cut them
	// before B, the reconnects (read by a query of their own) do not.
	events := first(cause(!f.EventsCaptured, R(v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_EVENTS_NOT_CAPTURED)),
		b.gapCause(GapCollector, v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_COLLECTOR_GAP),
		b.gapCause(GapEvents, v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_EVENTS_CONNECTION_LOST))
	rows := first(events, b.cutCause(f.EventsTruncated, f.EventsReadUntil))
	moved, _, elections := b.elections()
	m := 0
	for _, v := range moved {
		if v {
			m++
		}
	}
	meta := 0
	for id, prev := range b.metaElections() {
		if i := slices.IndexFunc(f.Events, func(e *v1.Event) bool { return e.GetId() == id }); i >= 0 &&
			prev != "" && prev != f.Events[i].GetServer() {
			meta++
		}
	}
	quorum := 0
	for _, e := range f.Events {
		if (e.GetKind() == "stream.quorum_lost" || e.GetKind() == "consumer.quorum_lost") && b.inRange(e.GetTime().AsTime()) {
			quorum++
		}
	}
	set("leaders_moved", &c.LeadersMoved, m, rows)
	set("leader_elections", &c.LeaderElections, elections, rows)
	set("meta_leader_changes", &c.MetaLeaderChanges, meta, rows)
	set("quorum_lost_reports", &c.QuorumLostReports, quorum, rows)
	set("client_reconnects", &c.ClientReconnects, int(f.ClientReconnects), events)

	// The node counts need Kubernetes watched over all of A to B, and
	// every node event to name its node; the pool count a pool label.
	nodes := first(cause(!f.Kubernetes, R(v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_KUBERNETES_NOT_WATCHED)),
		b.gapCause(GapCollector, v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_COLLECTOR_GAP),
		b.gapCause(GapKubernetes, v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_KUBERNETES_GAP),
		cause(f.Nodes.Unnamed, R(v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_NODE_UNNAMED)))
	set("nodes_upgraded", &c.NodesUpgraded, int(f.Nodes.Upgraded), nodes)
	set("nodes_drained", &c.NodesDrained, int(f.Nodes.Drained), nodes)
	set("node_pools_upgraded", &c.NodePoolsUpgraded, int(f.Nodes.Pools),
		first(nodes, cause(!f.Nodes.PoolLabels, R(v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_NO_POOL_LABELS))))
	return c, unobserved
}
