package storeprov

import (
	"context"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/checkaccept"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// AcceptFindings stores one acceptance per entity in one transaction; the
// handler has validated the request. The growth of every entity is read
// in one query, as MuteFinding reads one entity's.
func (p *Provider) AcceptFindings(ctx context.Context, req *v1.AcceptFindingsRequest) ([]*v1.Mute, error) {
	now := p.Now()
	ms := make([]store.Mute, 0, len(req.GetEntities()))
	ents := make([]store.EntityKey, 0, len(req.GetEntities()))
	for _, e := range req.GetEntities() {
		m := store.Mute{
			CheckCode: req.GetCheckCode(), Reason: req.GetReason(), MutedBy: req.GetMutedBy(),
			CreatedAt: now, ExpiresAt: req.GetExpiresAt().AsTime(), Accepted: true,
			EntityType: entityTypeName(e.GetType()), EntityKey: e.GetKey(),
		}
		ms = append(ms, m)
		ents = append(ents, store.EntityKey{Type: m.EntityType, Key: m.EntityKey})
	}
	if checkaccept.GrowthReview(req.GetCheckCode()) {
		g, err := p.acceptGrowths(ctx, now, ents)
		if err != nil {
			return nil, err
		}
		for i := range ms {
			ms[i].GrowthPerDay = g[ents[i]]
		}
	}
	stored, err := p.St.CreateMutes(ctx, ms)
	if err != nil {
		return nil, err
	}
	out := make([]*v1.Mute, 0, len(stored))
	for _, m := range stored {
		out = append(out, muteProto(m))
	}
	return out, nil
}

// ListAcceptances returns a page of the acceptances, in force first.
func (p *Provider) ListAcceptances(ctx context.Context, req *v1.ListAcceptancesRequest) (*v1.ListAcceptancesResponse, error) {
	cur, err := api.DecodeCursor(req.GetPageToken())
	if err != nil {
		return nil, err
	}
	n := int(req.GetPageSize())
	// One acceptance more than the page tells whether another page follows.
	ms, total, others, err := p.St.ListAcceptances(ctx, store.AcceptanceQuery{
		Now: p.Now(), Ended: req.GetIncludeEnded(), Account: req.GetAccount(), Offset: cur.Offset, Limit: n + 1,
	})
	if err != nil {
		return nil, err
	}
	res := &v1.ListAcceptancesResponse{TotalCount: int32(total), OtherAccountCount: int32(others)}
	if len(ms) > n {
		ms = ms[:n]
		res.NextPageToken = api.EncodeCursor(api.Cursor{Offset: cur.Offset + n})
	}
	for _, m := range ms {
		res.Acceptances = append(res.Acceptances, muteProto(m))
	}
	return res, nil
}
