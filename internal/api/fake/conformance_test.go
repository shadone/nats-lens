package fake_test

import (
	"testing"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
)

// The demo shows every state of the conformance table that the store
// provider shows; storeprov runs the same table over its store fixture.
func TestDemoConformance(t *testing.T) {
	apitest.RunConformance(t, apitest.Env{Client: demo(t), Systems: []string{fake.Cluster, fake.GlobalSystem, fake.StagingSystem}, Now: fake.FixedNow})
}
