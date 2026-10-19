package api

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

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
			Title: d.title, Body: d.body, OutsideRange: d.outside}
		ev := uniqueEvidence(d.evidence)
		s.EvidenceCount, s.Evidence = int32(len(ev)), ev[:min(len(ev), maxStepEvidence)]
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

// counts are the response's numbers; a count stays unset when what it
// counts was not observed.
func (b *replayBuilder) counts() *v1.ReplayCounts {
	f, c := b.f, &v1.ReplayCounts{}
	n := func(v int) *int32 { x := int32(v); return &x }
	if f.Collected {
		c.ServersRestarted = n(len(f.Restarts))
		// A stop needs the collection at B to be told.
		if f.EpochB != 0 {
			c.ServersStopped = n(len(f.Stops))
		}
		raised := map[string]bool{}
		for _, t := range f.Transitions {
			if t.State == v1.FindingState_FINDING_STATE_FIRING && t.Severity >= v1.Severity_SEVERITY_WARNING && b.inRange(t.Time) {
				raised[t.FindingID] = true
			}
		}
		if !f.TransitionsTruncated {
			c.FindingsRaised = n(len(raised))
		}
	}
	if f.GoneRead && f.EpochA != 0 && f.EpochB != 0 {
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
		c.StreamsGoneWithoutDelete, c.ConsumersGoneWithoutDelete, c.AssetsGoneWithDelete, c.AssetsUnknownAtB =
			n(streams), n(consumers), n(deleted), n(unknown)
	}
	if f.EventsCaptured {
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
		if !f.EventsTruncated {
			c.LeadersMoved, c.LeaderElections, c.MetaLeaderChanges, c.QuorumLostReports = n(m), n(elections), n(meta), n(quorum)
		}
		c.ClientReconnects = n(int(f.ClientReconnects))
	}
	if f.Kubernetes {
		c.NodesUpgraded, c.NodesDrained = n(int(f.Nodes.Upgraded)), n(int(f.Nodes.Drained))
		if f.Nodes.PoolLabels {
			c.NodePoolsUpgraded = n(int(f.Nodes.Pools))
		}
	}
	return c
}
