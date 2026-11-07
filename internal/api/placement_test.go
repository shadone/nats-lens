package api_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// placementStub answers GetPlacement with fn and the rest as the demo.
type placementStub struct {
	*fake.Provider
	calls int
	fn    func(ctx context.Context, req *v1.GetPlacementRequest) (*v1.GetPlacementResponse, error)
}

func (s *placementStub) GetPlacement(ctx context.Context, req *v1.GetPlacementRequest) (*v1.GetPlacementResponse, error) {
	s.calls++
	return s.fn(ctx, req)
}

func cell(server string, st v1.PlacementState) *v1.PlacementCell {
	return &v1.PlacementCell{Server: server, State: st}
}

func streamAsset(name string, replicas int32, cells ...*v1.PlacementCell) *v1.PlacementAsset {
	return &v1.PlacementAsset{Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "APP/" + name, Account: "APP", Stream: name, Name: name},
		Replicas: &replicas, Cells: cells}
}

func consumerAsset(stream, name string, cells ...*v1.PlacementCell) *v1.PlacementAsset {
	return &v1.PlacementAsset{Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: "APP/" + stream + "/" + name, Account: "APP",
		Stream: stream, Consumer: name, Name: name}, Durable: true, Cells: cells}
}

// unsorted is a provider's answer: in no order, unfiltered.
func unsorted() *v1.GetPlacementResponse {
	const (
		L = v1.PlacementState_PLACEMENT_STATE_LEADER
		S = v1.PlacementState_PLACEMENT_STATE_IN_SYNC
		B = v1.PlacementState_PLACEMENT_STATE_BEHIND
		D = v1.PlacementState_PLACEMENT_STATE_DISAGREES
	)
	orders := streamAsset("orders", 3, cell("nats-0", L), cell("nats-1", S), cell("nats-2", B))
	orders.Consumers = []*v1.PlacementAsset{
		consumerAsset("orders", "shipper", cell("nats-0", L), cell("nats-1", S)),
		consumerAsset("orders", "billing", cell("nats-0", L), cell("nats-1", D)),
	}
	kv := streamAsset("KV_sessions", 1, cell("nats-2", L))
	returns := streamAsset("returns", 3, cell("nats-1", L), cell("nats-2", S))
	returns.Unplaced = 1
	return &v1.GetPlacementResponse{Streams: []*v1.PlacementAsset{returns, orders, kv}}
}

// The handler fills the range, calls the provider within QueryTimeout,
// sorts, counts and filters what it returns.
func TestPlacementHandler(t *testing.T) {
	var left time.Duration
	var got *v1.GetPlacementRequest
	p := &placementStub{Provider: fake.New(false)}
	p.fn = func(ctx context.Context, req *v1.GetPlacementRequest) (*v1.GetPlacementResponse, error) {
		d, _ := ctx.Deadline()
		left, got = time.Until(d), req
		return unsorted(), nil
	}
	_, c := apitest.Serve(t, p)
	res, err := c.GetPlacement(context.Background(), connect.NewRequest(&v1.GetPlacementRequest{Filter: v1.PlacementFilter_PLACEMENT_FILTER_SERVERS_DISAGREE}))
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
	if n := m.GetCounts(); n.GetAll() != 5 || n.GetServersDisagree() != 1 || n.GetReplicaBehind() != 1 || n.GetMissing() != 1 || n.GetSingleReplica() != 1 {
		t.Errorf("counts = %v", n)
	}
	if len(m.GetStreams()) != 1 || m.GetStreams()[0].GetMatches() || len(m.GetStreams()[0].GetConsumers()) != 1 ||
		m.GetStreams()[0].GetConsumers()[0].GetEntity().GetConsumer() != "billing" || !m.GetStreams()[0].GetConsumers()[0].GetMatches() {
		t.Errorf("servers disagree lists %v, want orders for billing only", m.GetStreams())
	}
	all, err := c.GetPlacement(context.Background(), connect.NewRequest(&v1.GetPlacementRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, s := range all.Msg.GetStreams() {
		keys = append(keys, s.GetEntity().GetKey())
		for _, c := range s.GetConsumers() {
			keys = append(keys, c.GetEntity().GetKey())
		}
	}
	if want := []string{"APP/KV_sessions", "APP/orders", "APP/orders/billing", "APP/orders/shipper", "APP/returns"}; !slices.Equal(keys, want) {
		t.Errorf("all = %v, want %v", keys, want)
	}
}

// An unknown system or filter is invalid and reaches no provider; a
// provider's not found is not found.
func TestPlacementHandlerErrors(t *testing.T) {
	p := &placementStub{Provider: fake.New(false)}
	p.fn = func(context.Context, *v1.GetPlacementRequest) (*v1.GetPlacementResponse, error) {
		return nil, api.ErrNotFound
	}
	_, c := apitest.Serve(t, p)
	for name, req := range map[string]*v1.GetPlacementRequest{
		"unknown system":           {System: "nope"},
		"unknown filter":           {Filter: v1.PlacementFilter(42)},
		"selection without server": {SelectedAsset: "APP/orders"},
		"selection without asset":  {SelectedServer: "nats-0"},
		"selection empty part":     {SelectedAsset: "APP//orders", SelectedServer: "nats-0"},
		"selection invalid shape":  {SelectedAsset: "APP/orders/shipper/extra", SelectedServer: "nats-0"},
		"inverted range":           {Range: &v1.TimeRange{Start: timestamppb.New(fake.FixedNow), End: timestamppb.New(fake.FixedNow.Add(-time.Hour))}},
	} {
		if _, err := c.GetPlacement(context.Background(), connect.NewRequest(req)); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: %v, want invalid_argument", name, err)
		}
	}
	if p.calls != 0 {
		t.Errorf("%d provider calls for invalid requests", p.calls)
	}
	_, err := c.GetPlacement(context.Background(), connect.NewRequest(&v1.GetPlacementRequest{}))
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("not found: %v", err)
	}
	p.fn = func(context.Context, *v1.GetPlacementRequest) (*v1.GetPlacementResponse, error) {
		return nil, errors.New("SELECT failed at /var/lib/natslens")
	}
	if _, err := c.GetPlacement(context.Background(), connect.NewRequest(&v1.GetPlacementRequest{})); connect.CodeOf(err) != connect.CodeInternal {
		t.Errorf("internal: %v", err)
	}
}

func created(s string) *timestamppb.Timestamp {
	t, _ := time.Parse(time.RFC3339, s)
	return timestamppb.New(t)
}

// A cell differs from the value most cells report; on a tie the
// leader's value is the reference; a cell without a value never differs.
func TestSetDiffers(t *testing.T) {
	const a, b = "2026-10-01T08:00:00Z", "2026-10-15T09:30:00Z"
	for _, tc := range []struct {
		name    string
		leader  string
		created []string
		hashes  []string
		want    [][]string
	}{
		{"one server apart from two", "nats-0", []string{b, a, a}, []string{"h", "h", "h"}, [][]string{{"created"}, nil, nil}},
		{"the leader apart from the others", "nats-0", []string{a, b, b}, []string{"x", "h", "h"}, [][]string{{"created", "config_hash"}, nil, nil}},
		{"a tie goes to the leader", "nats-1", []string{a, b, ""}, []string{"h", "h", ""}, [][]string{{"created"}, nil, nil}},
		{"a tie without a leader goes to the smallest", "", []string{b, a, ""}, []string{"", "", ""}, [][]string{{"created"}, nil, nil}},
		{"all agree", "nats-0", []string{a, a, a}, []string{"h", "h", "h"}, [][]string{nil, nil, nil}},
	} {
		var cells []*v1.PlacementCell
		for i := range tc.created {
			c := &v1.PlacementCell{Server: []string{"nats-0", "nats-1", "nats-2"}[i], ConfigHash: tc.hashes[i]}
			if tc.created[i] != "" {
				c.Created = created(tc.created[i])
			}
			cells = append(cells, c)
		}
		api.SetDiffers(cells, tc.leader)
		for i, c := range cells {
			if !slices.Equal(c.GetDiffers(), tc.want[i]) {
				t.Errorf("%s: %s differs %v, want %v", tc.name, c.GetServer(), c.GetDiffers(), tc.want[i])
			}
		}
	}
}
