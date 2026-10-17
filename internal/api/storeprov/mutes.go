package storeprov

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// entityTypeName is the mutes.entity_type value of an entity type: the
// enum name without its prefix, in lower case.
func entityTypeName(t v1.EntityType) string {
	return strings.ToLower(strings.TrimPrefix(t.String(), "ENTITY_TYPE_"))
}

func entityTypeOf(name string) v1.EntityType {
	return v1.EntityType(v1.EntityType_value["ENTITY_TYPE_"+strings.ToUpper(name)])
}

func muteProto(m store.Mute) *v1.Mute {
	out := &v1.Mute{
		Id: strconv.FormatInt(m.ID, 10), CheckCode: m.CheckCode, Reason: m.Reason, MutedBy: m.MutedBy,
		CreatedAt: ts(m.CreatedAt), ExpiresAt: ts(m.ExpiresAt), UnmutedAt: ts(m.UnmutedAt), UnmutedBy: m.UnmutedBy,
	}
	if m.EntityType != "" {
		out.Entity = &v1.EntityRef{Type: entityTypeOf(m.EntityType), Key: m.EntityKey}
	}
	return out
}

// MuteFinding stores the mute; the handler has validated it.
func (p *Provider) MuteFinding(ctx context.Context, req *v1.MuteFindingRequest) (*v1.Mute, error) {
	m := store.Mute{
		CheckCode: req.GetCheckCode(), Reason: req.GetReason(), MutedBy: req.GetMutedBy(),
		CreatedAt: p.Now(), ExpiresAt: req.GetExpiresAt().AsTime(),
	}
	if e := req.GetEntity(); e != nil {
		m.EntityType, m.EntityKey = entityTypeName(e.GetType()), e.GetKey()
	}
	// The handler has checked the request, so a row the table rejects is
	// a fault of this server, reported as internal.
	out, err := p.St.CreateMute(ctx, m)
	if err != nil {
		return nil, err
	}
	return muteProto(out), nil
}

// UnmuteFinding lifts an active mute.
func (p *Provider) UnmuteFinding(ctx context.Context, req *v1.UnmuteFindingRequest) (*v1.Mute, error) {
	id, err := parseID(req.GetMuteId())
	if err != nil {
		return nil, fmt.Errorf("%w: mute_id %q is not a mute ID", api.ErrInvalid, req.GetMuteId())
	}
	m, err := p.St.UnmuteMute(ctx, id, req.GetUnmutedBy(), p.Now())
	switch {
	case errors.Is(err, store.ErrMuteNotFound):
		return nil, fmt.Errorf("mute %d: %w", id, api.ErrNotFound)
	case errors.Is(err, store.ErrMuteInactive):
		return nil, fmt.Errorf("mute %d no longer applies: %w", id, api.ErrFailedPrecondition)
	case err != nil:
		return nil, err
	}
	return muteProto(m), nil
}

// ListMutes returns the stored mutes, newest first, a page at a time.
func (p *Provider) ListMutes(ctx context.Context, req *v1.ListMutesRequest) (*v1.ListMutesResponse, error) {
	cur, err := api.DecodeCursor(req.GetPageToken())
	if err != nil {
		return nil, err
	}
	// One mute more than the page tells whether another page follows.
	ms, total, err := p.St.ListMutes(ctx, p.Now(), req.GetIncludeInactive(), int(req.GetPageSize())+1, cur.ID)
	if err != nil {
		return nil, err
	}
	res := &v1.ListMutesResponse{TotalCount: int32(total)}
	if n := int(req.GetPageSize()); len(ms) > n {
		ms = ms[:n]
		res.NextPageToken = api.EncodeCursor(api.Cursor{ID: ms[n-1].ID})
	}
	for _, m := range ms {
		res.Mutes = append(res.Mutes, muteProto(m))
	}
	return res, nil
}

// parseID reads a database ID given as a string: a positive decimal
// number in canonical form, so that "007" and "+7" do not name mute 7.
func parseID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != s {
		return 0, fmt.Errorf("%q is not an ID", s)
	}
	return id, nil
}
