package api_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/types/descriptorpb"
)

type onCallDeadlineProvider struct {
	api.Provider
	sawDeadline atomic.Bool
}

func (p *onCallDeadlineProvider) OnCall(ctx context.Context, r *v1.GetOnCallRequest) (*api.OnCallFacts, error) {
	deadline, ok := ctx.Deadline()
	p.sawDeadline.Store(ok && time.Until(deadline) <= api.QueryTimeout)
	return p.Provider.OnCall(ctx, r)
}

func TestOnCallNoSideEffectsAndDeadline(t *testing.T) {
	method := v1.File_natslens_v1_service_proto.Services().ByName("LensService").Methods().ByName("GetOnCall")
	if method.Options().(*descriptorpb.MethodOptions).GetIdempotencyLevel() != descriptorpb.MethodOptions_NO_SIDE_EFFECTS {
		t.Fatal("GetOnCall must be declared read-only")
	}
	p := &onCallDeadlineProvider{Provider: fake.New(false)}
	_, c := apitest.Serve(t, p)
	req := &v1.GetOnCallRequest{FindingId: "consumer-010:APP/order-archive/analytics-aggregator"}
	if _, err := c.GetOnCall(context.Background(), connect.NewRequest(req)); err != nil {
		t.Fatal(err)
	}
	if !p.sawDeadline.Load() {
		t.Fatal("missing thirty-second query deadline")
	}
}
