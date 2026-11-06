package api

import (
	"cmp"
	"slices"
	"strings"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// OnCallFacts are the present finding, its episode's transitions and the
// newest collection's facts. Providers read each kind in batches.
type OnCallFacts struct {
	At          time.Time
	Finding     *v1.Finding
	Transitions []*v1.FindingTransition
	Firing      []*v1.Finding
	Assets      map[string]*v1.OnCallAsset
	Now         *v1.OnCallNow
	Windows     []*v1.TimeWindow
	PlaybookURL string
}

// OnCallSpan bounds the episode's story, independently of the present's facts.
func OnCallSpan(f *v1.Finding, now time.Time) (time.Time, time.Time) {
	b := now
	if f.GetState() == v1.FindingState_FINDING_STATE_RESOLVED && f.GetResolvedAt() != nil {
		b = f.ResolvedAt.AsTime()
	}
	start := OnCallConditionStart(f)
	a := start.Add(-15 * time.Minute)
	if a.Before(b.Add(-MaxReplaySpan)) {
		a = b.Add(-MaxReplaySpan)
	}
	if !a.Before(b) {
		a = b.Add(-time.Second)
	}
	return a, b
}

// OnCallConditionStart prefers the observed condition start over first sighting.
func OnCallConditionStart(f *v1.Finding) time.Time {
	if f.GetConditionSince() != nil {
		return f.ConditionSince.AsTime()
	}
	return f.GetSince().AsTime()
}

// OnCallAffected is the headline's firing, unmuted group, worst first.
// A resolved or muted finding keeps its own entity available to open.
func OnCallAffected(f *v1.Finding, firing []*v1.Finding) []*v1.Finding {
	var out []*v1.Finding
	for _, other := range firing {
		if other.GetState() != v1.FindingState_FINDING_STATE_RESOLVED && other.GetMute() == nil && other.GetCheckCode() == f.GetCheckCode() && other.GetEntity().GetType() == f.GetEntity().GetType() {
			out = append(out, other)
		}
	}
	slices.SortFunc(out, func(a, b *v1.Finding) int {
		return cmp.Or(cmp.Compare(severityOrder(a.GetSeverity()), severityOrder(b.GetSeverity())), cmp.Compare(rankOf(expiryWarning(a)), rankOf(expiryWarning(b))), strings.Compare(a.GetEntity().GetKey(), b.GetEntity().GetKey()), strings.Compare(a.GetId(), b.GetId()))
	})
	return out
}

// FinishOnCall applies the shared context, episode, ordering and cut rules.
// Replay has already worded its steps with FinishReplay.
func FinishOnCall(f *OnCallFacts, replay *v1.GetReplayResponse, note string) *v1.GetOnCallResponse {
	a, b := OnCallSpan(f.Finding, f.At)
	out := &v1.GetOnCallResponse{Finding: f.Finding, Now: f.Now, StoryA: timestamppb.New(a), StoryB: timestamppb.New(b), StepsNote: note, PlaybookUrl: f.PlaybookURL}
	if out.Now == nil {
		out.Now = &v1.OnCallNow{}
	} else {
		out.Now = proto.Clone(out.Now).(*v1.OnCallNow)
		out.Now.OpenWindow = nil
	}
	for _, tr := range f.Transitions {
		if !tr.GetTime().AsTime().Before(f.Finding.GetSince().AsTime()) && !tr.GetTime().AsTime().After(b) {
			out.Transitions = append(out.Transitions, tr)
		}
	}
	if len(out.Transitions) > 100 {
		out.Transitions = out.Transitions[:100]
	}
	affected := OnCallAffected(f.Finding, f.Firing)
	out.SameCheckFiring = int32(len(affected))
	if len(affected) == 0 {
		affected = []*v1.Finding{f.Finding}
	}
	out.AssetsTotal = int32(len(affected))
	for _, finding := range affected[:min(10, len(affected))] {
		asset := &v1.OnCallAsset{Entity: finding.GetEntity(), FindingId: finding.GetId(), Severity: finding.GetSeverity()}
		if known := f.Assets[OnCallEntityKey(finding.GetEntity())]; known != nil {
			asset.Replicas, asset.Online, asset.Leader, asset.Answering = known.Replicas, known.Online, known.Leader, known.Answering
		}
		out.Assets = append(out.Assets, asset)
	}
	contextStart := OnCallConditionStart(f.Finding).Add(-15 * time.Minute)
	for _, w := range f.Windows {
		if w.GetStart() == nil || w.Start.AsTime().After(f.At) {
			continue
		}
		end := f.At
		if w.GetEnd() != nil {
			end = w.End.AsTime()
		}
		if !end.Before(contextStart) && (out.ContextWindow == nil || w.Start.AsTime().After(out.ContextWindow.Start.AsTime())) {
			out.ContextWindow = w
		}
		if end.After(f.At.Add(-K8sUpgradeGap)) && (out.Now.OpenWindow == nil || w.Start.AsTime().After(out.Now.OpenWindow.Start.AsTime())) {
			out.Now.OpenWindow = w
		}
	}
	if replay != nil {
		steps := slices.Clone(replay.GetSteps())
		out.StepsTotal = int32(len(steps)) + replay.GetStepsOmitted()
		start := OnCallConditionStart(f.Finding)
		distance := func(s *v1.ReplayStep) time.Duration {
			d := s.GetTime().AsTime().Sub(start)
			if d < 0 {
				return -d
			}
			return d
		}
		slices.SortFunc(steps, func(a, b *v1.ReplayStep) int {
			return cmp.Or(cmp.Compare(b.GetSeverity(), a.GetSeverity()), cmp.Compare(distance(a), distance(b)), strings.Compare(a.GetId(), b.GetId()))
		})
		steps = steps[:min(12, len(steps))]
		slices.SortFunc(steps, func(a, b *v1.ReplayStep) int {
			return cmp.Or(a.GetTime().AsTime().Compare(b.GetTime().AsTime()), strings.Compare(a.GetId(), b.GetId()))
		})
		out.Steps = steps
		if replay.GetStepsTruncated() {
			out.StepsNote = "Replay's read limit left some steps out."
		}
	}
	return out
}

// OnCallEntityKey includes type because entity keys alone are not unique.
func OnCallEntityKey(e *v1.EntityRef) string { return e.GetType().String() + ":" + e.GetKey() }

// OnCallOnline counts reported placement squares except missing and offline.
// No reports means unknown, even when the configured count is known.
func OnCallOnline(ps []*v1.ReplicaPlacement) *int32 {
	var n int32
	reported := false
	for _, p := range ps {
		if p.GetServer() == "" {
			continue
		}
		switch p.GetState() {
		case v1.PlacementState_PLACEMENT_STATE_MISSING:
			continue
		case v1.PlacementState_PLACEMENT_STATE_OFFLINE:
			reported = true
		default:
			reported = true
			n++
		}
	}
	if !reported {
		return nil
	}
	return &n
}

// OnCallRates sums the reported rates of streams affected by the whole
// headline group, including assets beyond the ten displayed. Delivery
// includes the other consumers of a missing consumer's parent stream.
func OnCallRates(f *OnCallFacts, delivered, stored map[string]float64) {
	affected := OnCallAffected(f.Finding, f.Firing)
	if len(affected) == 0 {
		affected = []*v1.Finding{f.Finding}
	}
	streams := map[string]bool{}
	for _, finding := range affected {
		e := finding.GetEntity()
		if e.GetType() == v1.EntityType_ENTITY_TYPE_CONSUMER || e.GetType() == v1.EntityType_ENTITY_TYPE_STREAM {
			streams[e.GetAccount()+"/"+e.GetStream()] = true
		}
	}
	var delivery, rate float64
	hasDelivery, hasRate := false, false
	for key, v := range delivered {
		i := strings.LastIndexByte(key, '/')
		if i >= 0 && streams[key[:i]] {
			delivery += v
			hasDelivery = true
		}
	}
	for key, v := range stored {
		if streams[key] {
			rate += v
			hasRate = true
		}
	}
	if f.Now == nil {
		f.Now = &v1.OnCallNow{}
	}
	if hasDelivery {
		f.Now.DeliveredPerSecond = &delivery
	}
	if hasRate {
		f.Now.StreamMsgsPerSecond = &rate
	}
}
