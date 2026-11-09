package fake

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
)

type demoEpisodeTransition struct {
	base *v1.Finding
	tr   *v1.FindingTransition
}

// Demo identities use the existing immutable frame encoding. Raw transitions
// come from seeded per-finding history, never inferred from aggregate row spans.
func (p *Provider) demoEpisodeHistory(s scen) ([]*v1.FindingEpisode, int64, error) {
	entries := []demoEpisodeTransition{}
	bytes := 0
	for _, f := range p.findings(s) {
		for _, tr := range s.transitions(f) {
			if tr.Time == nil || tr.Time.AsTime().After(s.now) {
				continue
			}
			bytes += proto.Size(tr)
			if len(entries) >= api.MaxListRankingRows || bytes > api.MaxListContextBytes {
				return nil, 0, fmt.Errorf("%w: demo history exceeds capture budget", api.ErrResourceExhausted)
			}
			entries = append(entries, demoEpisodeTransition{f, proto.Clone(tr).(*v1.FindingTransition)})
		}
	}
	slices.SortStableFunc(entries, func(a, b demoEpisodeTransition) int {
		if c := a.tr.Time.AsTime().Compare(b.tr.Time.AsTime()); c != 0 {
			return c
		}
		return strings.Compare(a.base.Id, b.base.Id)
	})
	frame := s.now.Unix() * storageFrameScale
	models := []*v1.FindingEpisode{}
	current := map[string]*v1.FindingEpisode{}
	for i, v := range entries {
		tr := v.tr
		tr.Epoch = frame + int64(i+1)
		last := current[v.base.Id]
		newOccurrence := last == nil || tr.State == firing || (last.Finding.State == resolved && tr.State != resolved)
		if newOccurrence {
			f := proto.Clone(v.base).(*v1.Finding)
			f.RelatedConfig = nil
			f.RelatedConfigSource = ""
			f.ConditionSince = nil
			f.Mute = nil
			f.Acknowledgement = nil
			f.Value = nil
			f.Cause = nil
			f.TransitionsLastHour = 0
			f.Since = nil
			f.ResolvedAt = nil
			last = &v1.FindingEpisode{Finding: f, FirstRetainedEpoch: tr.Epoch, FirstRetainedAt: tr.Time,
				Unavailable: []string{"native_collection_history", "historical_related_configuration", "historical_condition_onset", "historical_operator_state", "historical_numeric_value", "historical_cause", "historical_check_definition"},
			}
			if tr.State == firing || (tr.State == flapping && current[v.base.Id] != nil && current[v.base.Id].Finding.State == resolved) {
				last.StartEpoch = proto.Int64(tr.Epoch)
				last.Finding.Since = tr.Time
			} else {
				last.Unavailable = append(last.Unavailable, "occurrence_onset")
			}
			models = append(models, last)
			current[v.base.Id] = last
		}
		last.Finding.State = tr.State
		last.Finding.Severity = tr.Severity
		last.Finding.Detail = tr.Detail
		last.Finding.TransitionedAt = tr.Time
		last.Transitions = append(last.Transitions, tr)
		last.TransitionCount++
		if tr.State == resolved {
			last.ObservedResolutionAt = tr.Time
			last.ResolutionEpoch = tr.Epoch
			last.Finding.ResolvedAt = tr.Time
		}
	}
	for _, m := range models {
		latest := current[m.Finding.Id]
		m.LatestRetainedState = latest.Finding.State
		m.LatestRetainedSeverity = latest.Finding.Severity
		m.LatestStartEpoch = latest.GetStartEpoch()
		m.LatestObservedAt = ts(s.now)
		slices.Reverse(m.Transitions)
	}
	return models, frame + int64(len(entries)), nil
}

func (p *Provider) ListFindingEpisodes(ctx context.Context, in *v1.ListFindingEpisodesRequest) (*v1.ListFindingEpisodesResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r := proto.Clone(in).(*v1.ListFindingEpisodesRequest)
	cursor, err := p.episodePages.Prepare(r, p.Now())
	if err != nil {
		return nil, err
	}
	s := p.scen()
	frame := int64(0)
	for _, pin := range []int64{r.FirstRetainedEpoch, cursor.ReadEpoch} {
		if pin == 0 {
			continue
		}
		f := pin / storageFrameScale
		if f <= 0 || frame != 0 && frame != f {
			return nil, api.ErrInvalid
		}
		frame = f
	}
	if frame != 0 {
		at := time.Unix(frame, 0).UTC()
		if at.After(s.now) {
			return nil, api.ErrInvalid
		}
		if at.Before(s.dataStart()) {
			return nil, fmt.Errorf("%w: demo reference frame expired", api.ErrFailedPrecondition)
		}
		s = scen{now: at, shift: at.Sub(FixedNow), sc: p.sc}
	}
	models, high, err := p.demoEpisodeHistory(s)
	if err != nil {
		return nil, err
	}
	if cursor.Pinned && cursor.ReadEpoch > high {
		return nil, api.ErrFailedPrecondition
	}
	if !cursor.Pinned {
		cursor.ReadEpoch = high
	}
	out := &v1.ListFindingEpisodesResponse{Range: r.Range, ReadEpoch: cursor.ReadEpoch, ReadAt: ts(p.Now())}
	matches := []*v1.FindingEpisode{}
	for _, model := range models {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if model.FirstRetainedEpoch > cursor.ReadEpoch || r.FindingId != "" && model.Finding.Id != r.FindingId || r.FirstRetainedEpoch > 0 && model.FirstRetainedEpoch != r.FirstRetainedEpoch || r.CheckCode != "" && model.Finding.CheckCode != r.CheckCode || !matchesEntity(model.Finding.Entity, r.Entity) {
			continue
		}
		e := proto.Clone(model).(*v1.FindingEpisode)
		kept := []*v1.FindingTransition{}
		var before *v1.FindingTransition
		for _, tr := range e.Transitions {
			if tr.Epoch <= cursor.ReadEpoch {
				kept = append(kept, tr)
				if before == nil && tr.Time.AsTime().Before(r.Range.End.AsTime()) {
					before = tr
				}
			}
		}
		if before == nil || !e.FirstRetainedAt.AsTime().Before(r.Range.End.AsTime()) {
			continue
		}
		e.ObservedResolutionAt = nil
		e.ResolutionEpoch = 0
		for _, tr := range kept {
			if tr.State == resolved {
				e.ObservedResolutionAt = tr.Time
				e.ResolutionEpoch = tr.Epoch
				break
			}
		}
		if e.ObservedResolutionAt != nil && !e.ObservedResolutionAt.AsTime().After(r.Range.Start.AsTime()) {
			continue
		}
		e.Finding.State = before.State
		e.Finding.Severity = before.Severity
		e.Finding.Detail = before.Detail
		e.Finding.TransitionedAt = before.Time
		e.Finding.ResolvedAt = nil
		if before.State == resolved {
			e.Finding.ResolvedAt = before.Time
		}
		e.Transitions = kept
		e.TransitionCount = int32(len(kept))
		if !s.now.Equal(p.Now()) {
			e.Unavailable = append(e.Unavailable, "current_state_outside_demo_frame")
		}
		matches = append(matches, e)
	}
	slices.SortStableFunc(matches, func(a, b *v1.FindingEpisode) int {
		if a.FirstRetainedEpoch > b.FirstRetainedEpoch {
			return -1
		}
		if a.FirstRetainedEpoch < b.FirstRetainedEpoch {
			return 1
		}
		return strings.Compare(a.Finding.Id, b.Finding.Id)
	})
	raw, _ := proto.MarshalOptions{Deterministic: true}.Marshal(&v1.ListFindingEpisodesResponse{Episodes: matches})
	sum := sha256.Sum256(raw)
	membership := strconv.FormatUint(binary.BigEndian.Uint64(sum[:8]), 10) + ":" + strconv.FormatUint(binary.BigEndian.Uint64(sum[8:16]), 10)
	if err := cursor.CheckTotal(int32(len(matches)), membership); err != nil {
		return nil, err
	}
	cursor.Membership = membership
	out.TotalCount = int32(len(matches))
	if len(matches) == 0 {
		out.Unavailable = []string{"matching_occurrence_history"}
	}
	for i := cursor.Offset; i < len(matches) && len(out.Episodes) < int(r.PageSize); i++ {
		e := proto.Clone(matches[i]).(*v1.FindingEpisode)
		all := e.Transitions
		e.Transitions = nil
		if r.IncludeTransitions {
			start := min(int(r.TransitionOffset), len(all))
			e.Transitions = all[start:min(start+100, len(all))]
		}
		out.Episodes = append(out.Episodes, e)
		for !api.FitsEntityPage(out) && len(e.Transitions) > 1 {
			e.Transitions = e.Transitions[:len(e.Transitions)-1]
		}
		if !api.FitsEntityPage(out) {
			out.Episodes = out.Episodes[:len(out.Episodes)-1]
			if len(out.Episodes) == 0 {
				return nil, api.ErrResourceExhausted
			}
			break
		}
		if r.IncludeTransitions && int(r.TransitionOffset)+len(e.Transitions) < len(all) {
			e.NextTransitionOffset = r.TransitionOffset + int32(len(e.Transitions))
			e.TransitionPageToken = cursor.Next(out.ReadEpoch, out.TotalCount, 0)
		}
	}
	if next := cursor.Offset + len(out.Episodes); next < len(matches) {
		out.NextPageToken = cursor.Next(out.ReadEpoch, out.TotalCount, next)
	}
	return out, nil
}
