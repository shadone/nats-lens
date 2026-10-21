package api_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// capacityStub answers Capacity with fn and the rest as the demo; it names
// the account APP.
type capacityStub struct {
	*fake.Provider
	calls int
	fn    func(ctx context.Context, req *v1.GetCapacityRequest) (*api.CapacityFacts, error)
}

func (s *capacityStub) Capacity(ctx context.Context, req *v1.GetCapacityRequest) (*api.CapacityFacts, error) {
	s.calls++
	return s.fn(ctx, req)
}

func (s *capacityStub) AccountNames(context.Context) (map[string]string, error) {
	return map[string]string{"APP": "shop"}, nil
}

// The handler fills the range, calls the provider within QueryTimeout,
// finishes the facts and names the accounts; without an epoch the
// response is empty.
func TestCapacityHandler(t *testing.T) {
	var left time.Duration
	var got *v1.GetCapacityRequest
	p := &capacityStub{Provider: fake.New(false)}
	p.fn = func(ctx context.Context, req *v1.GetCapacityRequest) (*api.CapacityFacts, error) {
		d, _ := ctx.Deadline()
		left, got = time.Until(d), req
		return &api.CapacityFacts{Epoch: 100, EpochAt: capAt, Streams: []api.CapacityStreamFact{{Account: "APP", Stream: "orders"}}}, nil
	}
	_, c := apitest.Serve(t, p)
	res, err := c.GetCapacity(context.Background(), connect.NewRequest(&v1.GetCapacityRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if left > api.QueryTimeout || left < api.QueryTimeout-time.Second {
		t.Errorf("provider had %s, want %s", left, api.QueryTimeout)
	}
	if r := got.GetRange(); r.GetStart() == nil || r.GetEnd() == nil || r.GetEnd().AsTime().Sub(r.GetStart().AsTime()) != api.DefaultRange {
		t.Errorf("range = %v, want the default span with both ends", r)
	}
	if m := res.Msg; m.GetEpoch() != 100 || len(m.GetStreams()) != 1 || m.GetStreams()[0].GetAccountName() != "shop" {
		t.Errorf("response = %v", m)
	}
	p.fn = func(context.Context, *v1.GetCapacityRequest) (*api.CapacityFacts, error) {
		return &api.CapacityFacts{}, nil
	}
	res, err = c.GetCapacity(context.Background(), connect.NewRequest(&v1.GetCapacityRequest{}))
	if err != nil || res.Msg.GetEpoch() != 0 || res.Msg.GetEpochAt() != nil || len(res.Msg.GetStreams()) != 0 || len(res.Msg.GetServers()) != 0 {
		t.Errorf("no epoch: %v, %v", res, err)
	}
}

// An unknown system or an inverted range is invalid and reaches no
// provider; a provider's errors map to their codes without their text.
func TestCapacityHandlerErrors(t *testing.T) {
	p := &capacityStub{Provider: fake.New(false)}
	p.fn = func(context.Context, *v1.GetCapacityRequest) (*api.CapacityFacts, error) { return nil, api.ErrNotFound }
	_, c := apitest.Serve(t, p)
	for name, req := range map[string]*v1.GetCapacityRequest{
		"unknown system": {System: "nope"},
		"inverted range": {Range: &v1.TimeRange{Start: timestamppb.New(fake.FixedNow), End: timestamppb.New(fake.FixedNow.Add(-time.Hour))}},
	} {
		if _, err := c.GetCapacity(context.Background(), connect.NewRequest(req)); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: %v, want invalid_argument", name, err)
		}
	}
	if p.calls != 0 {
		t.Errorf("%d provider calls for invalid requests", p.calls)
	}
	if _, err := c.GetCapacity(context.Background(), connect.NewRequest(&v1.GetCapacityRequest{})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("not found: %v", err)
	}
	p.fn = func(context.Context, *v1.GetCapacityRequest) (*api.CapacityFacts, error) {
		return nil, errors.New("SELECT failed at /var/lib/natslens")
	}
	_, err := c.GetCapacity(context.Background(), connect.NewRequest(&v1.GetCapacityRequest{}))
	if connect.CodeOf(err) != connect.CodeInternal || err.Error() != "internal: internal error; see the server log" {
		t.Errorf("internal: %v", err)
	}
}
