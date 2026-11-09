package storeprov

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
	"google.golang.org/protobuf/proto"
)

func (p *Provider) ListFindingEpisodes(ctx context.Context, in *v1.ListFindingEpisodesRequest) (*v1.ListFindingEpisodesResponse, error) {
	r := proto.Clone(in).(*v1.ListFindingEpisodesRequest)
	cursor, err := p.episodePages.Prepare(r, p.RangeReference())
	if err != nil {
		return nil, err
	}
	typ, key := "", ""
	if e := r.GetEntity(); e != nil && e.GetType() != v1.EntityType_ENTITY_TYPE_SYSTEM {
		typ, key = entityTypeName(e.GetType()), e.GetKey()
	}
	page, err := p.St.FindingEpisodes(ctx, store.FindingEpisodeQuery{
		From: r.Range.Start.AsTime(), To: r.Range.End.AsTime(), FindingID: r.FindingId, FirstRetainedEpoch: r.FirstRetainedEpoch,
		CheckCode: r.CheckCode, EntityType: typ, EntityKey: key, ReadEpoch: cursor.ReadEpoch, ReadEpochKnown: cursor.Pinned, Limit: int(r.PageSize), Offset: cursor.Offset,
	})
	if errors.Is(err, store.ErrFindingEpisodeReadLimit) {
		return nil, fmt.Errorf("%w: occurrence read budget; narrow scope or page size", api.ErrResourceExhausted)
	}
	if errors.Is(err, store.ErrInvalidFindingEpisodeQuery) {
		return nil, fmt.Errorf("%w: occurrence read boundary unavailable; restart the list", api.ErrFailedPrecondition)
	}
	if err != nil {
		return nil, err
	}
	if page.Total > math.MaxInt32 {
		return nil, fmt.Errorf("%w: too many occurrences; narrow scope", api.ErrResourceExhausted)
	}
	if err = cursor.CheckTotal(int32(page.Total), page.Signature); err != nil {
		return nil, err
	}
	cursor.Membership = page.Signature
	fs := make([]store.Finding, len(page.Items))
	for i, e := range page.Items {
		fs[i] = e.Finding
		if !e.StartKnown {
			fs[i].Since = time.Time{}
		}
	}
	findings, err := p.findingProtos(ctx, fs)
	if err != nil {
		return nil, err
	}
	out := &v1.ListFindingEpisodesResponse{Range: r.Range, ReadEpoch: page.ReadEpoch, ReadAt: ts(p.Now()), TotalCount: int32(page.Total)}
	if page.Total == 0 {
		out.Unavailable = []string{"matching_occurrence_history"}
	}
	for i, e := range page.Items {
		item := &v1.FindingEpisode{Finding: findings[i], FirstRetainedEpoch: e.FirstRetainedEpoch, FirstRetainedAt: ts(e.Finding.Since),
			ObservedResolutionAt: ts(e.ResolutionAt), ResolutionEpoch: e.ResolutionEpoch, LatestRetainedSeverity: severityOfText[e.LatestSeverity],
			LatestStartEpoch: e.LatestStartEpoch, LatestObservedAt: ts(e.LatestObservedAt), TransitionCount: int32(e.TransitionCount), CauseEventId: e.Finding.CauseEventID,
			Unavailable: []string{"historical_related_configuration", "historical_condition_onset", "historical_operator_state", "historical_flapping", "historical_check_definition"},
		}
		if e.EndingUnknown {
			item.Unavailable = append(item.Unavailable, "occurrence_resolution")
		}
		if !e.StartKnown {
			item.Unavailable = append(item.Unavailable, "occurrence_onset")
		}
		if e.LatestState == "" {
			item.Unavailable = append(item.Unavailable, "latest_retained_state")
		}
		if e.StartKnown {
			v := e.Finding.SinceEpoch
			item.StartEpoch = &v
		}
		if e.LatestState != "" {
			item.LatestRetainedState = stateOf(store.Finding{State: e.LatestState, LastTransition: e.LatestTransition, Flapping: e.LatestFlapping})
		}
		if item.CauseEventId > 0 && item.Finding.Cause == nil {
			item.Unavailable = append(item.Unavailable, "cause_event")
		}
		if r.IncludeTransitions {
			rows, err := p.St.FindingTransitions(ctx, store.TransitionQuery{FindingID: e.Finding.ID, MinEpoch: e.FirstRetainedEpoch, MaxEpoch: e.LastRetainedEpoch, Limit: 100, Offset: int(r.TransitionOffset)})
			if err != nil {
				return nil, err
			}
			size := 0
			for _, tr := range rows {
				size += len(tr.Detail) + len(tr.FindingID) + len(tr.CheckCode) + len(tr.EntityType) + len(tr.EntityKey)
				if size > 8<<20 {
					return nil, fmt.Errorf("%w: occurrence history exceeds read budget", api.ErrResourceExhausted)
				}
				item.Transitions = append(item.Transitions, &v1.FindingTransition{FindingId: tr.FindingID, Epoch: tr.Epoch, Time: ts(tr.Time), State: transitionState[tr.Kind], Severity: severityOfText[tr.Severity], Detail: tr.Detail})
			}
		}
		out.Episodes = append(out.Episodes, item)
		// Raw history can be shortened with an explicit offset/token. A single
		// oversized transition/occurrence is rejected, never returned half-written.
		for !api.FitsEntityPage(out) && len(item.Transitions) > 1 {
			item.Transitions = item.Transitions[:len(item.Transitions)-1]
		}
		if !api.FitsEntityPage(out) {
			out.Episodes = out.Episodes[:len(out.Episodes)-1]
			if len(out.Episodes) == 0 {
				return nil, fmt.Errorf("%w: occurrence exceeds page budget", api.ErrResourceExhausted)
			}
			break
		}
		if r.IncludeTransitions && int(r.TransitionOffset)+len(item.Transitions) < e.TransitionCount {
			item.NextTransitionOffset = r.TransitionOffset + int32(len(item.Transitions))
			item.TransitionPageToken = cursor.Next(page.ReadEpoch, int32(page.Total), 0)
		}
	}
	next := cursor.Offset + len(out.Episodes)
	if next < page.Total {
		out.NextPageToken = cursor.Next(page.ReadEpoch, int32(page.Total), next)
	}
	return out, nil
}
