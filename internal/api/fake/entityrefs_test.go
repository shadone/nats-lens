package fake_test

import (
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// walkRefs calls f on every EntityRef in m, with its path.
func walkRefs(path string, m protoreflect.Message, f func(string, *v1.EntityRef)) {
	if !m.IsValid() {
		return
	}
	if e, ok := m.Interface().(*v1.EntityRef); ok {
		f(path, e)
		return
	}
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Kind() != protoreflect.MessageKind {
			return true
		}
		p := path + "." + string(fd.Name())
		switch {
		case fd.IsList():
			for i := range v.List().Len() {
				walkRefs(fmt.Sprintf("%s[%d]", p, i), v.List().Get(i).Message(), f)
			}
		case fd.IsMap():
		default:
			walkRefs(p, v.Message(), f)
		}
		return true
	})
}

// Every EntityRef the demo returns for example-global, after a mute of
// SUPPLIERS sent as the UI sends it (with name and account_name), has the
// parts its type implies, and an account entity carries the account's
// name. (A reviewer's case.)
func TestDemoEntityRefs(t *testing.T) {
	c := demo(t)
	sys := fake.GlobalSystem
	ui := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_ACCOUNT, Key: fake.SuppliersAccount, Name: fake.SuppliersAccount,
		Account: fake.SuppliersAccount, AccountName: "SUPPLIERS"}
	mres := must(c.MuteFinding(ctx, connect.NewRequest(&v1.MuteFindingRequest{System: sys, CheckCode: "account-003", Entity: ui,
		Reason: "renewing", MutedBy: "ops", ExpiresAt: timestamppb.New(fake.FixedNow.Add(24 * time.Hour))})))
	r := &v1.TimeRange{Start: timestamppb.New(fake.FixedNow.Add(-24 * time.Hour)), End: timestamppb.New(fake.FixedNow)}
	res := []proto.Message{
		mres,
		must(c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{System: sys, Range: r, IncludeMuted: true, PageSize: 200}))),
		must(c.ListMutes(ctx, connect.NewRequest(&v1.ListMutesRequest{System: sys, IncludeInactive: true}))),
		must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{System: sys, Range: r}))),
		must(c.ListSystems(ctx, connect.NewRequest(&v1.ListSystemsRequest{Range: r, StripBuckets: 3}))),
		must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{System: sys, Range: r}))),
		must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{System: sys, Range: r,
			Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_ACCOUNT, Key: fake.SuppliersAccount}}))),
		must(c.GetReplay(ctx, connect.NewRequest(&v1.GetReplayRequest{System: sys, A: timestamppb.New(fake.FixedNow.Add(-6 * time.Hour)), B: timestamppb.New(fake.FixedNow)}))),
		must(c.GetCapacity(ctx, connect.NewRequest(&v1.GetCapacityRequest{System: sys, Range: r}))),
	}
	accounts := 0
	for i, m := range res {
		walkRefs(fmt.Sprintf("res%d", i), m.ProtoReflect(), func(p string, e *v1.EntityRef) {
			switch e.GetType() {
			case v1.EntityType_ENTITY_TYPE_ACCOUNT:
				accounts++
				if e.GetAccount() != e.GetKey() {
					t.Errorf("%s: account entity without account: %v", p, e)
				}
				if e.GetKey() == fake.SuppliersAccount && e.GetAccountName() != "SUPPLIERS" {
					t.Errorf("%s: SUPPLIERS unnamed: %v", p, e)
				}
			case v1.EntityType_ENTITY_TYPE_SERVER:
				if e.GetServer() != e.GetKey() || e.GetAccount() != "" || e.GetAccountName() != "" {
					t.Errorf("%s: server entity %v", p, e)
				}
			case v1.EntityType_ENTITY_TYPE_STREAM, v1.EntityType_ENTITY_TYPE_CONSUMER:
				if e.GetAccount() == "" || e.GetStream() == "" {
					t.Errorf("%s: asset entity %v", p, e)
				}
			case v1.EntityType_ENTITY_TYPE_SYSTEM:
				if e.GetKey() != "" || e.GetAccount() != "" {
					t.Errorf("%s: system entity %v", p, e)
				}
			}
		})
	}
	if accounts < 5 {
		t.Errorf("only %d account entities seen", accounts)
	}
}
