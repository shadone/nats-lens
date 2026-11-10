package fake

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// AcceptFindings keeps one acceptance per entity, as MuteFinding with
// accept keeps one, all at once.
func (p *Provider) AcceptFindings(_ context.Context, req *v1.AcceptFindingsRequest) ([]*v1.Mute, error) {
	// Build every acceptance first, growth included, then store them under
	// one lock, so a reader sees all of them or none.
	ms := make([]*v1.Mute, 0, len(req.GetEntities()))
	for _, e := range req.GetEntities() {
		ms = append(ms, p.newMute(&v1.MuteFindingRequest{CheckCode: req.GetCheckCode(), Entity: e, Reason: req.GetReason(),
			ExpiresAt: req.GetExpiresAt(), MutedBy: req.GetMutedBy(), Accept: true}))
	}
	s := p.scen()
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkAcceptances(s, ms); err != nil {
		return nil, err
	}
	out := make([]*v1.Mute, 0, len(ms))
	for _, m := range ms {
		m.Id = strconv.Itoa(p.nextID)
		p.nextID++
		p.mutes = keepNewest(append(p.mutes, &storedMute{m: m}), p.seededMutes)
		c := proto.Clone(m).(*v1.Mute)
		api.MarkMute(c)
		out = append(out, c)
	}
	return out, nil
}

// ListAcceptances returns the acceptances in force, the soonest review
// first, then with include_ended the ended ones, the latest end first.
func (p *Provider) ListAcceptances(_ context.Context, req *v1.ListAcceptancesRequest) (*v1.ListAcceptancesResponse, error) {
	s := p.scen()
	p.mu.Lock()
	type row struct {
		m      *v1.Mute
		active bool
		id     int64
	}
	var rows []row
	other := 0
	for _, sm := range p.mutes {
		if !sm.m.GetAccepted() {
			continue
		}
		active := sm.active(s)
		if !active && !req.GetIncludeEnded() {
			continue
		}
		v := sm.view(s)
		if a := req.GetAccount(); a != "" {
			if own := api.FindingAccount(v.GetEntity()); own != "" && own != a {
				other++
				continue
			}
		}
		rows = append(rows, row{v, active, num(v.GetId())})
	}
	p.mu.Unlock()
	end := func(m *v1.Mute) int64 {
		if m.GetUnmutedAt() != nil {
			return m.GetUnmutedAt().AsTime().UnixMicro()
		}
		return m.GetExpiresAt().AsTime().UnixMicro()
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		switch {
		case a.active != b.active:
			return a.active
		case a.active && !a.m.GetExpiresAt().AsTime().Equal(b.m.GetExpiresAt().AsTime()):
			return a.m.GetExpiresAt().AsTime().Before(b.m.GetExpiresAt().AsTime())
		case !a.active && end(a.m) != end(b.m):
			return end(a.m) > end(b.m)
		}
		return a.id < b.id
	})
	ms := make([]*v1.Mute, 0, len(rows))
	for _, r := range rows {
		ms = append(ms, r.m)
	}
	page, next, err := api.Page(ms, req.GetPageToken(), req.GetPageSize())
	if err != nil {
		return nil, err
	}
	return &v1.ListAcceptancesResponse{Acceptances: page, NextPageToken: next, TotalCount: int32(len(ms)), OtherAccountCount: int32(other)}, nil
}

// checkAcceptances refuses acceptances as the store does: an entity
// without a finding of the check in the scenario, in any state, is
// not_found; one with an acceptance of the check in force is
// already_exists. p.mu is held.
func (p *Provider) checkAcceptances(s scen, ms []*v1.Mute) error {
	all := s.sc.findings(s)
	for _, m := range ms {
		e := m.GetEntity()
		what := fmt.Sprintf("%s for %s %s", m.GetCheckCode(), strings.ToLower(strings.TrimPrefix(e.GetType().String(), "ENTITY_TYPE_")), e.GetKey())
		if !slices.ContainsFunc(all, func(f *v1.Finding) bool {
			return f.GetCheckCode() == m.GetCheckCode() && f.GetEntity().GetType() == e.GetType() && f.GetEntity().GetKey() == e.GetKey()
		}) {
			return fmt.Errorf("%w: %s: no finding of the check for the entity; accept only an entity the check has a finding for", api.ErrNotFound, what)
		}
		for _, sm := range p.mutes {
			if sm.m.GetAccepted() && sm.covers(&v1.Finding{CheckCode: m.GetCheckCode(), Entity: e}) && sm.m.GetEntity() != nil && sm.active(s) {
				return fmt.Errorf("%w: %s: an acceptance of the check for the entity is in force; end it before accepting again", api.ErrAlreadyExists, what)
			}
		}
	}
	return nil
}
