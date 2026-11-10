package apitest

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// replayRange is the A and B the Replay rows read: the system's newest
// Kubernetes window of the last day, else the last 2 hours.
func (q Q) replayRange() (time.Time, time.Time) {
	q.t.Helper()
	ws := must(q.env.Client.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{System: q.System, Range: q.day()}))).of(q.t).GetWindows()
	if len(ws) == 0 {
		return q.env.Now.Add(-2 * time.Hour), q.env.Now
	}
	w := slices.MaxFunc(ws, func(x, y *v1.TimeWindow) int { return x.GetEnd().AsTime().Compare(y.GetEnd().AsTime()) })
	return w.GetStart().AsTime(), w.GetEnd().AsTime()
}

// replay reads the Replay of the system's replayRange.
func (q Q) replay() *v1.GetReplayResponse {
	q.t.Helper()
	a, b := q.replayRange()
	return must(q.env.Client.GetReplay(ctx, connect.NewRequest(&v1.GetReplayRequest{System: q.System, A: timestamppb.New(a),
		B: timestamppb.New(b)}))).of(q.t)
}

// replayStep finds a step of the system's Replay that ok accepts.
func replayStep(ok func(*v1.GetReplayResponse, *v1.ReplayStep) bool) func(Q) string {
	return func(q Q) string {
		res := q.replay()
		for _, s := range res.GetSteps() {
			if ok(res, s) {
				return s.GetTitle()
			}
		}
		return ""
	}
}

// replayGone finds a gone asset of the system's Replay that ok accepts.
func replayGone(ok func(*v1.GoneAsset) bool) func(Q) string {
	return func(q Q) string {
		for _, g := range q.replay().GetGone() {
			if ok(g) {
				return g.GetEntity().GetKey()
			}
		}
		return ""
	}
}

// hasEvidence reports whether a step has evidence of the kind.
func hasEvidence(s *v1.ReplayStep, kind v1.ReplayEvidenceKind) bool {
	return slices.ContainsFunc(s.GetEvidence(), func(e *v1.ReplayEvidence) bool { return e.GetKind() == kind })
}

// replayCountsDiff says where a Replay's counts disagree with its steps,
// their evidence and its gone list; "" when they agree. A count that is
// unset is not compared.
func replayCountsDiff(res *v1.GetReplayResponse, event func(id string) *v1.Event) string {
	if res.GetStepsTruncated() || res.GetGoneTruncated() {
		return ""
	}
	// Elections and quorum advisories among the steps' evidence; every
	// election between A and B is evidence of a restart, stop or leaders
	// step, a quorum advisory only when a quorum step takes it in.
	elections, quorum, capped := 0, 0, false
	for _, s := range res.GetSteps() {
		capped = capped || int(s.GetEvidenceCount()) > len(s.GetEvidence())
		for _, e := range s.GetEvidence() {
			if e.GetKind() != v1.ReplayEvidenceKind_REPLAY_EVIDENCE_KIND_EVENT {
				continue
			}
			at := e.GetTime().AsTime()
			if at.Before(res.GetA().AsTime()) || at.After(res.GetB().AsTime()) {
				continue
			}
			switch event(e.GetId()).GetKind() {
			case "stream.leader_elected", "consumer.leader_elected":
				elections++
			case "stream.quorum_lost", "consumer.quorum_lost":
				quorum++
			}
		}
	}
	restarts, stops := 0, 0
	stepped := map[string]bool{}
	for _, s := range res.GetSteps() {
		if s.GetOutsideRange() {
			continue
		}
		switch s.GetKind() {
		case v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART:
			if strings.HasSuffix(s.GetTitle(), " stops answering") {
				stops++
			} else {
				restarts++
			}
		case v1.ReplayStepKind_REPLAY_STEP_KIND_GONE:
			for _, e := range s.GetEntities() {
				stepped[e.GetKey()] = true
			}
		}
	}
	c := res.GetCounts()
	if c.LeaderElections != nil && !capped && int(c.GetLeaderElections()) != elections {
		return fmt.Sprintf("%d leader elections, %d among the evidence", c.GetLeaderElections(), elections)
	}
	if c.QuorumLostReports != nil && !capped && int(c.GetQuorumLostReports()) < quorum {
		return fmt.Sprintf("%d quorum reports, %d among the evidence", c.GetQuorumLostReports(), quorum)
	}
	if c.ServersRestarted != nil && int(c.GetServersRestarted()) != restarts {
		return fmt.Sprintf("%d servers restarted, %d restart steps", c.GetServersRestarted(), restarts)
	}
	if c.ServersStopped != nil && int(c.GetServersStopped()) != stops {
		return fmt.Sprintf("%d servers stopped, %d stop steps", c.GetServersStopped(), stops)
	}
	var streams, consumers, deleted, unknown int32
	for _, g := range res.GetGone() {
		if !stepped[g.GetEntity().GetKey()] {
			return g.GetEntity().GetKey() + " gone without a step"
		}
		delete(stepped, g.GetEntity().GetKey())
		switch {
		case g.GetDeleteEvent() != nil:
			deleted++
		case len(g.GetNotAnsweringAtB()) > 0:
			unknown++
		case g.GetEntity().GetType() == v1.EntityType_ENTITY_TYPE_STREAM:
			streams++
		default:
			consumers++
		}
	}
	if len(stepped) > 0 {
		return fmt.Sprintf("gone steps name %v, which the gone list does not", slices.Sorted(maps.Keys(stepped)))
	}
	if c.AssetsGoneWithDelete != nil && (c.GetStreamsGoneWithoutDelete() != streams || c.GetConsumersGoneWithoutDelete() != consumers ||
		c.GetAssetsGoneWithDelete() != deleted || c.GetAssetsUnknownAtB() != unknown) {
		return fmt.Sprintf("gone counts %v, gone list %d streams, %d consumers, %d deleted, %d unknown", c, streams, consumers, deleted, unknown)
	}
	return ""
}

// replayRows are the Replay states both providers must show.
func replayRows() []State {
	return []State{
		{"a Replay restart step with event and collection evidence", replayStep(func(_ *v1.GetReplayResponse, s *v1.ReplayStep) bool {
			return s.GetKind() == v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART && !strings.HasSuffix(s.GetTitle(), " stops answering") &&
				hasEvidence(s, v1.ReplayEvidenceKind_REPLAY_EVIDENCE_KIND_EVENT) && hasEvidence(s, v1.ReplayEvidenceKind_REPLAY_EVIDENCE_KIND_COLLECTION)
		})},
		{"a Replay quorum loss as one span step", replayStep(func(_ *v1.GetReplayResponse, s *v1.ReplayStep) bool {
			return s.GetKind() == v1.ReplayStepKind_REPLAY_STEP_KIND_QUORUM && (s.GetEnd() != nil || s.GetOpenAtB()) &&
				hasEvidence(s, v1.ReplayEvidenceKind_REPLAY_EVIDENCE_KIND_FINDING)
		})},
		{"a Replay asset gone without a delete event, with its servers at A", replayGone(func(g *v1.GoneAsset) bool {
			return g.GetDeleteEvent() == nil && len(g.GetServersAtA()) > 0 && len(g.GetNotAnsweringAtB()) == 0
		})},
		{"a Replay gone asset with a delete event naming the client", replayGone(func(g *v1.GoneAsset) bool {
			return g.GetDeleteEvent() != nil && g.GetDeleteClient() != ""
		})},
		{"a Replay step after B that closes one in the range", replayStep(func(res *v1.GetReplayResponse, s *v1.ReplayStep) bool {
			return s.GetOutsideRange() && s.GetTime().AsTime().After(res.GetB().AsTime())
		})},
		{"a Replay catch-up step with Placement evidence", replayStep(func(_ *v1.GetReplayResponse, s *v1.ReplayStep) bool {
			return s.GetKind() == v1.ReplayStepKind_REPLAY_STEP_KIND_CATCH_UP && (s.GetEnd() != nil || s.GetOpenAtB()) &&
				slices.ContainsFunc(s.GetEvidence(), func(e *v1.ReplayEvidence) bool {
					return e.GetKind() == v1.ReplayEvidenceKind_REPLAY_EVIDENCE_KIND_COLLECTION && e.GetEntity() == nil
				})
		})},
		{"a Replay gap step, with its gap kind", replayStep(func(_ *v1.GetReplayResponse, s *v1.ReplayStep) bool {
			return s.GetKind() == v1.ReplayStepKind_REPLAY_STEP_KIND_GAP && s.GetGapKind() != v1.ReplayGapKind_REPLAY_GAP_KIND_UNSPECIFIED
		})},
		{"the Replay counts agree with its steps and gone list, in every system", everySystem(func(q Q) string {
			res := q.replay()
			event := func(id string) *v1.Event {
				return must(q.env.Client.GetEvent(ctx, connect.NewRequest(&v1.GetEventRequest{System: q.System, Id: id}))).of(q.t).GetEvent()
			}
			if d := replayCountsDiff(res, event); d != "" {
				q.t.Errorf("system %q: %s", q.System, d)
				return ""
			}
			if res.GetCounts().ServersRestarted == nil && res.GetCounts().AssetsGoneWithDelete == nil {
				return ""
			}
			return fmt.Sprintf("%d steps, %d gone", len(res.GetSteps()), len(res.GetGone()))
		})},
		{"every unset Replay count says why, and only those, in every system", everySystem(func(q Q) string {
			res := q.replay()
			c := res.GetCounts().ProtoReflect()
			unset := map[string]bool{}
			fs := c.Descriptor().Fields()
			for i := range fs.Len() {
				if !c.Has(fs.Get(i)) {
					unset[string(fs.Get(i).Name())] = true
				}
			}
			listed := map[string]bool{}
			for _, u := range res.GetUnobserved() {
				if !unset[u.GetCount()] || listed[u.GetCount()] || u.GetReason() == v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_UNSPECIFIED {
					q.t.Errorf("system %q: %v names a set count, a count twice or no reason", q.System, u)
					return ""
				}
				listed[u.GetCount()] = true
			}
			if len(listed) != len(unset) {
				q.t.Errorf("system %q: unset %v, listed %v", q.System, unset, listed)
				return ""
			}
			if len(unset) == 0 {
				return ""
			}
			return fmt.Sprintf("%d unset", len(unset))
		})},
		{"a Replay with A older than the retention is invalid_argument", func(q Q) string {
			ret := must(q.env.Client.GetLensInfo(ctx, connect.NewRequest(&v1.GetLensInfoRequest{System: q.System}))).of(q.t).GetRetention()
			if ret == nil || ret.AsDuration() <= 0 {
				return ""
			}
			a := q.env.Now.Add(-ret.AsDuration() - time.Hour)
			_, err := q.env.Client.GetReplay(ctx, connect.NewRequest(&v1.GetReplayRequest{System: q.System, A: timestamppb.New(a),
				B: timestamppb.New(a.Add(30 * time.Minute))}))
			if connect.CodeOf(err) != connect.CodeInvalidArgument {
				q.t.Errorf("system %q: A older than the retention gave %v", q.System, err)
				return ""
			}
			return ret.AsDuration().String()
		}},
	}
}
