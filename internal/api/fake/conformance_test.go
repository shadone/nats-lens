package fake_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The demo shows every state of the conformance table that the store
// provider shows; storeprov runs the same table over its store fixture.
func TestDemoConformance(t *testing.T) {
	c := demo(t)
	// A mute of an account finding, as the store's table has one.
	must(c.MuteFinding(ctx, connect.NewRequest(&v1.MuteFindingRequest{System: fake.GlobalSystem, CheckCode: "account-003",
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_ACCOUNT, Key: fake.SuppliersAccount}, Reason: "Renewal booked.", MutedBy: "ops",
		ExpiresAt: timestamppb.New(fake.FixedNow.Add(24 * time.Hour))})))
	apitest.RunConformance(t, apitest.Env{Client: c, Systems: []string{fake.Cluster, fake.GlobalSystem, fake.StagingSystem}, Now: fake.FixedNow})
}
