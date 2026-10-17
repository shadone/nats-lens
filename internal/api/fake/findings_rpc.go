package fake

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// ListFindings filters the scenario's findings and pages through them.
func (p *Provider) ListFindings(_ context.Context, req *v1.ListFindingsRequest) (*v1.ListFindingsResponse, error) {
	s := p.scen()
	var out []*v1.Finding
	for _, f := range p.findings(s) {
		if keepFinding(f, req) {
			out = append(out, f)
		}
	}
	page, next, err := api.Page(out, req.GetPageToken(), req.GetPageSize())
	if err != nil {
		return nil, err
	}
	return &v1.ListFindingsResponse{Findings: page, NextPageToken: next, TotalCount: int32(len(out))}, nil
}

func keepFinding(f *v1.Finding, req *v1.ListFindingsRequest) bool {
	r := req.GetRange()
	end := f.GetResolvedAt().AsTime()
	if f.GetResolvedAt() == nil {
		end = r.GetEnd().AsTime()
	}
	if !overlaps(f.GetSince().AsTime(), end, r) {
		return false
	}
	states := req.GetStates()
	if len(states) == 0 {
		if f.GetState() == resolved {
			return false
		}
	} else if !slices.Contains(states, f.GetState()) {
		return false
	}
	if len(req.GetSeverities()) > 0 && !slices.Contains(req.GetSeverities(), f.GetSeverity()) {
		return false
	}
	if f.GetMute() != nil && !req.GetIncludeMuted() {
		return false
	}
	if req.GetCheckCode() != "" && f.GetCheckCode() != req.GetCheckCode() {
		return false
	}
	return matchesEntity(f.GetEntity(), req.GetEntity())
}

// GetFinding returns one finding and its transitions.
func (p *Provider) GetFinding(_ context.Context, req *v1.GetFindingRequest) (*v1.GetFindingResponse, error) {
	s := p.scen()
	for _, f := range p.findings(s) {
		if f.GetId() == req.GetId() {
			return &v1.GetFindingResponse{Finding: f, Transitions: s.transitions(f)}, nil
		}
	}
	return nil, fmt.Errorf("finding %q: %w", req.GetId(), api.ErrNotFound)
}

// transitions are the finding's state changes, newest first.
func (s scen) transitions(f *v1.Finding) []*v1.FindingTransition {
	tr := func(at time.Time, st v1.FindingState) *v1.FindingTransition {
		return &v1.FindingTransition{FindingId: f.GetId(), Time: ts(at), State: st, Severity: f.GetSeverity(), Detail: f.GetDetail()}
	}
	var out []*v1.FindingTransition
	if f.GetCheckCode() == "stream-020" {
		out = append(out,
			tr(s.at(14, 12, 0), flapping), tr(s.at(14, 5, 0), resolved), tr(s.at(13, 48, 0), firing),
			tr(s.at(13, 41, 0), resolved), tr(s.at(11, 0, 0), firing), tr(s.at(8, 30, 0), resolved),
			tr(s.at(8, 15, 0), firing), tr(s.at(7, 45, 0), resolved), tr(s.at(7, 30, 0), firing))
		return out
	}
	if f.GetResolvedAt() != nil {
		out = append(out, tr(f.GetResolvedAt().AsTime(), resolved))
	}
	return append(out, tr(f.GetSince().AsTime(), firing))
}

// storedMute is a mute kept in memory. The seeded mute is relative to the
// scenario and moves with it in live mode; mutes created through the API
// are absolute.
type storedMute struct {
	m        *v1.Mute
	relative bool
}

func seedMute() *storedMute {
	s := scen{now: FixedNow}
	return &storedMute{relative: true, m: &v1.Mute{
		Id: "1", CheckCode: "stream-031", Entity: streamRef("audit-archive"),
		Reason:    "Retention cut to 90d is in review; growth after the backfill is expected.",
		MutedBy:   "denis",
		CreatedAt: ts(s.at(12, 0, 0)), ExpiresAt: ts(s.on(24, 12, 0)),
	}}
}

// view returns a copy of the mute as of the scenario s.
func (sm *storedMute) view(s scen) *v1.Mute {
	if !sm.relative {
		return proto.Clone(sm.m).(*v1.Mute)
	}
	out := &v1.Mute{
		Id: sm.m.GetId(), CheckCode: sm.m.GetCheckCode(), Entity: sm.m.GetEntity(), Reason: sm.m.GetReason(),
		MutedBy: sm.m.GetMutedBy(), UnmutedBy: sm.m.GetUnmutedBy(),
		CreatedAt: ts(sm.m.GetCreatedAt().AsTime().Add(s.shift)), ExpiresAt: ts(sm.m.GetExpiresAt().AsTime().Add(s.shift)),
	}
	if sm.m.GetUnmutedAt() != nil {
		out.UnmutedAt = sm.m.GetUnmutedAt()
	}
	return out
}

func (sm *storedMute) active(s scen) bool {
	m := sm.view(s)
	return m.GetUnmutedAt() == nil && m.GetExpiresAt().AsTime().After(s.now)
}

func (sm *storedMute) covers(f *v1.Finding) bool {
	if sm.m.GetCheckCode() != f.GetCheckCode() {
		return false
	}
	e := sm.m.GetEntity()
	return e == nil || (e.GetType() == f.GetEntity().GetType() && e.GetKey() == f.GetEntity().GetKey())
}

// MuteFinding keeps a new mute in memory.
func (p *Provider) MuteFinding(_ context.Context, req *v1.MuteFindingRequest) (*v1.Mute, error) {
	now := p.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	m := &v1.Mute{
		Id: strconv.Itoa(p.nextID), CheckCode: req.GetCheckCode(), Entity: req.GetEntity(), Reason: req.GetReason(),
		MutedBy: req.GetMutedBy(), CreatedAt: ts(now), ExpiresAt: req.GetExpiresAt(),
	}
	p.nextID++
	p.mutes = append(p.mutes, &storedMute{m: m})
	return proto.Clone(m).(*v1.Mute), nil
}

// UnmuteFinding lifts an active mute.
func (p *Provider) UnmuteFinding(_ context.Context, req *v1.UnmuteFindingRequest) (*v1.Mute, error) {
	s := p.scen()
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, sm := range p.mutes {
		if sm.m.GetId() != req.GetMuteId() {
			continue
		}
		if !sm.active(s) {
			return nil, fmt.Errorf("mute %s no longer applies: %w", req.GetMuteId(), api.ErrFailedPrecondition)
		}
		sm.m.UnmutedAt, sm.m.UnmutedBy = ts(s.now), req.GetUnmutedBy()
		return sm.view(s), nil
	}
	return nil, fmt.Errorf("mute %q: %w", req.GetMuteId(), api.ErrNotFound)
}

// ListMutes returns the mutes, newest first.
func (p *Provider) ListMutes(_ context.Context, req *v1.ListMutesRequest) (*v1.ListMutesResponse, error) {
	s := p.scen()
	p.mu.Lock()
	defer p.mu.Unlock()
	cur, err := api.DecodeCursor(req.GetPageToken())
	if err != nil {
		return nil, err
	}
	res := &v1.ListMutesResponse{}
	// Mutes are kept oldest first with growing IDs; pages go newest first
	// and continue below the last ID of the previous page.
	for i := len(p.mutes) - 1; i >= 0; i-- {
		m := p.mutes[i]
		if !req.GetIncludeInactive() && !m.active(s) {
			continue
		}
		res.TotalCount++
		if cur.ID != 0 && num(m.m.GetId()) >= cur.ID {
			continue
		}
		if len(res.Mutes) == int(req.GetPageSize()) {
			res.NextPageToken = api.EncodeCursor(api.Cursor{ID: num(res.Mutes[len(res.Mutes)-1].GetId())})
			continue
		}
		res.Mutes = append(res.Mutes, m.view(s))
	}
	return res, nil
}
