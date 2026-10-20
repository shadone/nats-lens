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

// topologyStub answers Topology with fn and the rest as the demo; it
// names the account APP.
type topologyStub struct {
	*fake.Provider
	calls int
	fn    func(ctx context.Context, req *v1.GetTopologyRequest) (*api.TopologyFacts, error)
}

func (s *topologyStub) Topology(ctx context.Context, req *v1.GetTopologyRequest) (*api.TopologyFacts, error) {
	s.calls++
	return s.fn(ctx, req)
}

func (s *topologyStub) AccountNames(context.Context) (map[string]string, error) {
	return map[string]string{"APP": "shop"}, nil
}

// The handler fills the range, calls the provider within QueryTimeout,
// finishes the facts and names the leaf nodes' accounts.
func TestTopologyHandler(t *testing.T) {
	var left time.Duration
	var got *v1.GetTopologyRequest
	p := &topologyStub{Provider: fake.New(false)}
	p.fn = func(ctx context.Context, req *v1.GetTopologyRequest) (*api.TopologyFacts, error) {
		d, _ := ctx.Deadline()
		left, got = time.Until(d), req
		f := topoFacts()
		f.Leafnodes = []api.TopologyLeafRow{leaf("E0", "e0", "store-a", 1)}
		return f, nil
	}
	_, c := apitest.Serve(t, p)
	res, err := c.GetTopology(context.Background(), connect.NewRequest(&v1.GetTopologyRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if left > api.QueryTimeout || left < api.QueryTimeout-time.Second {
		t.Errorf("provider had %s, want %s", left, api.QueryTimeout)
	}
	if r := got.GetRange(); r.GetStart() == nil || r.GetEnd() == nil || r.GetEnd().AsTime().Sub(r.GetStart().AsTime()) != api.DefaultRange {
		t.Errorf("range = %v, want the default span with both ends", r)
	}
	m := res.Msg
	if m.GetEpoch() != 100 || len(m.GetClusters()) != 2 || len(m.GetLeafnodes()) != 1 || m.GetLeafnodes()[0].GetAccountName() != "shop" {
		t.Errorf("response = %v", m)
	}
	// No epoch in the range: the empty response.
	p.fn = func(context.Context, *v1.GetTopologyRequest) (*api.TopologyFacts, error) {
		return &api.TopologyFacts{}, nil
	}
	res, err = c.GetTopology(context.Background(), connect.NewRequest(&v1.GetTopologyRequest{}))
	if err != nil || res.Msg.GetEpoch() != 0 || res.Msg.GetEpochAt() != nil || len(res.Msg.GetClusters()) != 0 {
		t.Errorf("no epoch: %v, %v", res, err)
	}
}

// An unknown system or an inverted range is invalid and reaches no
// provider; a provider's errors map to their codes without their text.
func TestTopologyHandlerErrors(t *testing.T) {
	p := &topologyStub{Provider: fake.New(false)}
	p.fn = func(context.Context, *v1.GetTopologyRequest) (*api.TopologyFacts, error) { return nil, api.ErrNotFound }
	_, c := apitest.Serve(t, p)
	for name, req := range map[string]*v1.GetTopologyRequest{
		"unknown system": {System: "nope"},
		"inverted range": {Range: &v1.TimeRange{Start: timestamppb.New(fake.FixedNow), End: timestamppb.New(fake.FixedNow.Add(-time.Hour))}},
	} {
		if _, err := c.GetTopology(context.Background(), connect.NewRequest(req)); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: %v, want invalid_argument", name, err)
		}
	}
	if p.calls != 0 {
		t.Errorf("%d provider calls for invalid requests", p.calls)
	}
	if _, err := c.GetTopology(context.Background(), connect.NewRequest(&v1.GetTopologyRequest{})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("not found: %v", err)
	}
	p.fn = func(context.Context, *v1.GetTopologyRequest) (*api.TopologyFacts, error) {
		return nil, errors.New("SELECT failed at /var/lib/natslens")
	}
	_, err := c.GetTopology(context.Background(), connect.NewRequest(&v1.GetTopologyRequest{}))
	if connect.CodeOf(err) != connect.CodeInternal || err.Error() != "internal: internal error; see the server log" {
		t.Errorf("internal: %v", err)
	}
}
