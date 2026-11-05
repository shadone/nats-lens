package storeprov

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// ackProto converts a stored acknowledgement; quiet says whether its quiet
// holds at at.
func ackProto(a store.Ack, at time.Time) *v1.Acknowledgement {
	return &v1.Acknowledgement{
		Id: strconv.FormatInt(a.ID, 10), FindingId: a.FindingID, Note: a.Note, AckedBy: a.AckedBy,
		CreatedAt: ts(a.CreatedAt), QuietUntil: ts(a.QuietUntil), Quiet: a.QuietAt(at),
		EndedAt: ts(a.EndedAt), EndedBy: a.EndedBy, EndReason: a.EndReason,
	}
}

// AcknowledgeFinding stores the acknowledgement; the handler has validated
// it.
func (p *Provider) AcknowledgeFinding(ctx context.Context, req *v1.AcknowledgeFindingRequest) (*v1.Acknowledgement, error) {
	now := p.Now()
	a := store.Ack{FindingID: req.GetFindingId(), Note: req.GetNote(), AckedBy: req.GetAckedBy(), CreatedAt: now}
	if !req.GetUntilResolved() {
		a.QuietUntil = now.Add(req.GetQuietFor().AsDuration())
	}
	out, err := p.St.Acknowledge(ctx, a)
	switch {
	case errors.Is(err, store.ErrFindingNotFound):
		return nil, fmt.Errorf("finding %q: %w", req.GetFindingId(), api.ErrNotFound)
	case errors.Is(err, store.ErrFindingNotFiring):
		return nil, fmt.Errorf("finding %q does not fire: %w", req.GetFindingId(), api.ErrFailedPrecondition)
	case err != nil:
		return nil, err
	}
	return ackProto(out, now), nil
}

// EndAcknowledgement hands back an acknowledgement in force.
func (p *Provider) EndAcknowledgement(ctx context.Context, req *v1.EndAcknowledgementRequest) (*v1.Acknowledgement, error) {
	id, err := parseID(req.GetId())
	if err != nil {
		return nil, fmt.Errorf("%w: id %q is not an acknowledgement ID", api.ErrInvalid, req.GetId())
	}
	now := p.Now()
	a, err := p.St.EndAck(ctx, id, req.GetEndedBy(), now)
	switch {
	case errors.Is(err, store.ErrAckNotFound):
		return nil, fmt.Errorf("acknowledgement %d: %w", id, api.ErrNotFound)
	case errors.Is(err, store.ErrAckEnded):
		return nil, fmt.Errorf("acknowledgement %d is no longer in force: %w", id, api.ErrFailedPrecondition)
	case err != nil:
		return nil, err
	}
	return ackProto(a, now), nil
}
