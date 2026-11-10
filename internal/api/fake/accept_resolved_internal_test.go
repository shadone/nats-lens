package fake

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The demo refuses an acceptance of a resolved finding as the store does.
// The provider is called directly: the handler refuses every check of the
// demo's resolved findings as not acceptable before it gets here.
func TestDemoRefusesAcceptingAResolvedFinding(t *testing.T) {
	p := New(false)
	f := findingOf(t, p, "stream-003:"+Account+"/order-archive")
	if f.GetState() != resolved {
		t.Fatalf("stream-003 on order-archive is %v, want resolved", f.GetState())
	}
	_, err := p.AcceptFindings(context.Background(), &v1.AcceptFindingsRequest{CheckCode: f.GetCheckCode(), Entities: []*v1.EntityRef{f.GetEntity()},
		Reason: "r", MutedBy: "op", ExpiresAt: timestamppb.New(FixedNow.Add(48 * time.Hour))})
	if !errors.Is(err, api.ErrFailedPrecondition) || !strings.Contains(err.Error(), f.GetEntity().GetKey()) {
		t.Errorf("resolved finding: %v, want failed precondition naming the entity", err)
	}
}
