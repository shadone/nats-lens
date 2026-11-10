package api_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// In example-global, SUPPLIERS' scope keeps its account JWT finding and the
// server findings, which concern every account, and leaves out APP's
// stream findings, which it counts.
func TestListFindingsAccountScope(t *testing.T) {
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: fake.Demo(false)})
	ctx := context.Background()
	list := func(account string) *v1.ListFindingsResponse {
		res, err := c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{System: fake.GlobalSystem, Account: account, PageSize: 500, IncludeMuted: true}))
		if err != nil {
			t.Fatal(err)
		}
		return res.Msg
	}
	all := list("")
	if all.GetOtherAccountCount() != 0 {
		t.Errorf("unscoped: %d of other accounts, want 0", all.GetOtherAccountCount())
	}
	sup := list(fake.SuppliersAccount)
	var codes []string
	for _, f := range sup.GetFindings() {
		codes = append(codes, f.GetCheckCode()+" "+f.GetEntity().GetType().String())
		if a := api.FindingAccount(f.GetEntity()); a != "" && a != fake.SuppliersAccount {
			t.Errorf("SUPPLIERS' scope holds %s on %s", f.GetCheckCode(), f.GetEntity().GetKey())
		}
	}
	if !slices.Contains(codes, "account-003 ENTITY_TYPE_ACCOUNT") || !slices.Contains(codes, "server-001 ENTITY_TYPE_SERVER") {
		t.Errorf("SUPPLIERS' scope: %v; want its account-003 and the server findings", codes)
	}
	if slices.Contains(codes, "stream-002 ENTITY_TYPE_STREAM") || slices.Contains(codes, "account-004 ENTITY_TYPE_ACCOUNT") {
		t.Errorf("SUPPLIERS' scope holds APP's findings: %v", codes)
	}
	if got := int(sup.GetTotalCount() + sup.GetOtherAccountCount()); got != len(all.GetFindings()) || sup.GetOtherAccountCount() == 0 {
		t.Errorf("SUPPLIERS: %d kept and %d of other accounts; want %d in all, some left out", sup.GetTotalCount(), sup.GetOtherAccountCount(), len(all.GetFindings()))
	}
}

func TestListFindingsAccountTooLong(t *testing.T) {
	_, c := apitest.Serve(t, fake.New(false))
	_, err := c.ListFindings(context.Background(), connect.NewRequest(&v1.ListFindingsRequest{Account: strings.Repeat("A", api.MaxNameLength+1)}))
	if codeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("got %v, want invalid_argument", err)
	}
}

func stream(key string) *v1.EntityRef {
	return &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: key}
}

// The two streams with a replica on nats-us-4 name it offline; a repeated
// entity is answered once and an unknown one is not reported.
func TestListAssetPlacement(t *testing.T) {
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: fake.Demo(false)})
	res, err := c.ListAssetPlacement(context.Background(), connect.NewRequest(&v1.ListAssetPlacementRequest{System: fake.GlobalSystem,
		Entities: []*v1.EntityRef{stream("APP/price-changes"), stream("APP/shipping-updates"), stream("APP/price-changes"), stream("APP/no-such-stream")}}))
	if err != nil {
		t.Fatal(err)
	}
	as := res.Msg.GetAssets()
	if len(as) != 3 || res.Msg.GetEpochAt() == nil {
		t.Fatalf("%d assets, epoch %v; want 3 and an epoch", len(as), res.Msg.GetEpochAt())
	}
	for _, a := range as[:2] {
		offline := slices.ContainsFunc(a.GetReplicas(), func(r *v1.ReplicaPlacement) bool {
			return r.GetServer() == "nats-us-4" && r.GetState() == v1.PlacementState_PLACEMENT_STATE_OFFLINE
		})
		if !a.GetReported() || a.GetLeader() == "" || !offline || a.GetEntity().GetStream() == "" {
			t.Errorf("%s: %v", a.GetEntity().GetKey(), a)
		}
	}
	if as[2].GetReported() || len(as[2].GetReplicas()) != 0 || as[2].GetEntity().GetKey() != "APP/no-such-stream" {
		t.Errorf("unknown stream: %v", as[2])
	}
}

func TestListAssetPlacementRefused(t *testing.T) {
	_, c := apitest.Serve(t, fake.New(false))
	many := make([]*v1.EntityRef, api.MaxAssetPlacement+1)
	for i := range many {
		many[i] = stream("APP/orders")
	}
	for name, es := range map[string][]*v1.EntityRef{
		"too many":    many,
		"a server":    {{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: "nats-0"}},
		"a bad key":   {stream("orders")},
		"no type":     {{Key: "APP/orders"}},
		"a consumer?": {{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: "APP/orders"}},
	} {
		_, err := c.ListAssetPlacement(context.Background(), connect.NewRequest(&v1.ListAssetPlacementRequest{Entities: es}))
		if codeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: got %v, want invalid_argument", name, err)
		}
	}
}
