package storeprov_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// A mute of the system entity, which the mute dialog sends for a system
// finding by default, is stored and listed with its entity.
func TestMuteOfTheSystemEntity(t *testing.T) {
	c, _ := findingsClient(t)
	sys := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SYSTEM}
	res, err := c.MuteFinding(ctx, connect.NewRequest(&v1.MuteFindingRequest{CheckCode: "system-002", Entity: sys,
		Reason: "meta group maintenance", MutedBy: "ops@example.com", ExpiresAt: timestamppb.New(now.Add(time.Hour))}))
	if err != nil {
		t.Fatalf("mute of the system entity: %v", err)
	}
	if e := res.Msg.GetMute().GetEntity(); e.GetType() != v1.EntityType_ENTITY_TYPE_SYSTEM || e.GetKey() != "" {
		t.Fatalf("mute entity = %v, want the system", e)
	}
	list := must(c.ListMutes(ctx, connect.NewRequest(&v1.ListMutesRequest{PageSize: 10})))
	if len(list.GetMutes()) != 1 || list.GetMutes()[0].GetEntity().GetType() != v1.EntityType_ENTITY_TYPE_SYSTEM {
		t.Fatalf("mutes = %v", list.GetMutes())
	}
}
