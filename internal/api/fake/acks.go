package fake

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// storedAck is an acknowledgement kept in memory. since is the episode it
// belongs to, the finding's since in the fixed frame, so that it still
// matches the finding when the scenario moves in live mode. The times of
// a seeded acknowledgement are in the fixed frame and move with the
// scenario; those of one made through the API are absolute.
type storedAck struct {
	a        *v1.Acknowledgement
	since    time.Time
	relative bool
}

// at is a time of the acknowledgement as the scenario s places it.
func (sa *storedAck) at(s scen, t *timestamppb.Timestamp) *timestamppb.Timestamp {
	if t == nil || !sa.relative {
		return t
	}
	return ts(t.AsTime().Add(s.shift))
}

// end ends the acknowledgement at s.now. A seeded one first takes its
// times as the scenario places them now, so that the end, made through
// the API, does not move with the scenario.
func (sa *storedAck) end(s scen, by, reason string) {
	if sa.relative {
		sa.a.CreatedAt, sa.a.QuietUntil, sa.a.EndedAt = sa.at(s, sa.a.GetCreatedAt()), sa.at(s, sa.a.GetQuietUntil()), sa.at(s, sa.a.GetEndedAt())
		sa.relative = false
	}
	sa.a.EndedAt, sa.a.EndedBy, sa.a.EndReason = ts(s.now), by, reason
}

// view returns a copy of the acknowledgement as of the scenario s, with
// quiet set for s.now.
func (sa *storedAck) view(s scen) *v1.Acknowledgement {
	out := proto.Clone(sa.a).(*v1.Acknowledgement)
	out.CreatedAt, out.QuietUntil, out.EndedAt = sa.at(s, sa.a.GetCreatedAt()), sa.at(s, sa.a.GetQuietUntil()), sa.at(s, sa.a.GetEndedAt())
	out.Quiet = sa.inForce(s) && (out.GetQuietUntil() == nil || out.GetQuietUntil().AsTime().After(s.now))
	return out
}

// inForce reports whether the acknowledgement was made by s.now and not
// ended then; whether its episode still runs is the finding's to say.
func (sa *storedAck) inForce(s scen) bool {
	created, ended := sa.at(s, sa.a.GetCreatedAt()), sa.at(s, sa.a.GetEndedAt())
	return !created.AsTime().After(s.now) && (ended == nil || ended.AsTime().After(s.now))
}

// of reports whether the acknowledgement is of f's current episode.
func (sa *storedAck) of(s scen, f *v1.Finding) bool {
	return f.GetState() != resolved && sa.a.GetFindingId() == f.GetId() && s.clock(f.GetSince().AsTime()).Equal(sa.since)
}

// ackFor returns the newest acknowledgement in force of f's current
// episode, or nil. The caller holds p.mu.
func (p *Provider) ackFor(s scen, f *v1.Finding) *v1.Acknowledgement {
	for i := len(p.acks) - 1; i >= 0; i-- {
		if sa := p.acks[i]; sa.of(s, f) && sa.inForce(s) {
			return sa.view(s)
		}
	}
	return nil
}

// AcknowledgeFinding keeps a new acknowledgement in memory; one of the
// same episode in force ends as replaced.
func (p *Provider) AcknowledgeFinding(_ context.Context, req *v1.AcknowledgeFindingRequest) (*v1.Acknowledgement, error) {
	s := p.scen()
	var f *v1.Finding
	for _, x := range p.findings(s) {
		if x.GetId() == req.GetFindingId() {
			f = x
		}
	}
	switch {
	case f == nil:
		return nil, fmt.Errorf("finding %q: %w", req.GetFindingId(), api.ErrNotFound)
	case f.GetState() == resolved:
		return nil, fmt.Errorf("finding %q does not fire: %w", req.GetFindingId(), api.ErrFailedPrecondition)
	}
	a := &v1.Acknowledgement{FindingId: f.GetId(), Note: req.GetNote(), AckedBy: req.GetAckedBy(), CreatedAt: ts(s.now)}
	if !req.GetUntilResolved() {
		a.QuietUntil = ts(s.now.Add(req.GetQuietFor().AsDuration()))
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, sa := range p.acks {
		if sa.of(s, f) && sa.a.GetEndedAt() == nil {
			sa.end(s, req.GetAckedBy(), "replaced")
		}
	}
	a.Id = strconv.Itoa(p.nextAckID)
	p.nextAckID++
	sa := &storedAck{a: a, since: s.clock(f.GetSince().AsTime())}
	p.acks = append(p.acks, sa)
	return sa.view(s), nil
}

// EndAcknowledgement hands back an acknowledgement in force.
func (p *Provider) EndAcknowledgement(_ context.Context, req *v1.EndAcknowledgementRequest) (*v1.Acknowledgement, error) {
	s := p.scen()
	fs := p.findings(s)
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, sa := range p.acks {
		if sa.a.GetId() != req.GetId() {
			continue
		}
		current := false
		for _, f := range fs {
			current = current || sa.of(s, f)
		}
		if !current || !sa.inForce(s) {
			return nil, fmt.Errorf("acknowledgement %s is no longer in force: %w", req.GetId(), api.ErrFailedPrecondition)
		}
		sa.end(s, req.GetEndedBy(), "released")
		return sa.view(s), nil
	}
	return nil, fmt.Errorf("acknowledgement %q: %w", req.GetId(), api.ErrNotFound)
}
