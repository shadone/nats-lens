package fake_test

import (
	"connectrpc.com/connect"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
	"strings"
	"testing"
)

func TestOnCallDemo(t *testing.T) {
	c := demo(t)
	id := "consumer-010:APP/order-archive/analytics-aggregator"
	got := must(c.GetOnCall(ctx, connect.NewRequest(&v1.GetOnCallRequest{FindingId: id})))
	detail := must(c.GetFinding(ctx, connect.NewRequest(&v1.GetFindingRequest{Id: id})))
	if !proto.Equal(got.Finding, detail.Finding) || got.Finding.Acknowledgement == nil || got.ContextWindow == nil || got.PlaybookUrl != "https://runbooks.example.com/nats/durable-missing" || got.Now.DeliveredPerSecond == nil || got.Now.StreamMsgsPerSecond == nil || len(got.Steps) == 0 || !strings.Contains(got.Finding.Detail, "last reported by") {
		t.Fatalf("card: %v", got)
	}
	if len(got.Assets) != 1 || got.Assets[0].Online != nil || got.Assets[0].GetReplicas() != 3 {
		t.Fatalf("missing durable: %v", got.Assets)
	}
	fs := must(c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{System: fake.StagingSystem})))
	if len(fs.Findings) == 0 {
		t.Fatal("no staging findings")
	}
	missing := must(c.GetOnCall(ctx, connect.NewRequest(&v1.GetOnCallRequest{System: fake.StagingSystem, FindingId: fs.Findings[0].Id})))
	if missing.Now.EpochAt != nil || missing.Now.ServersAnswering != nil || missing.Now.DeliveredPerSecond != nil {
		t.Fatalf("stale collection reported as now: %v", missing.Now)
	}
}

func TestOnCallRequestErrors(t *testing.T) {
	c := demo(t)
	for _, tc := range []struct {
		system, id string
		code       connect.Code
	}{
		{"", "", connect.CodeInvalidArgument}, {"", "unknown", connect.CodeNotFound}, {"unknown", "unknown", connect.CodeInvalidArgument}, {"", strings.Repeat("x", 4097), connect.CodeInvalidArgument},
	} {
		_, err := c.GetOnCall(ctx, connect.NewRequest(&v1.GetOnCallRequest{System: tc.system, FindingId: tc.id}))
		if codeOf(err) != tc.code {
			t.Fatalf("%q: %v", tc.system, err)
		}
	}
}

func TestOnCallEveryDemoFinding(t *testing.T) {
	c := demo(t)
	for _, system := range []string{fake.Cluster, fake.GlobalSystem, fake.StagingSystem} {
		fs := must(c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{System: system, IncludeMuted: true, States: []v1.FindingState{v1.FindingState_FINDING_STATE_FIRING, v1.FindingState_FINDING_STATE_FLAPPING, v1.FindingState_FINDING_STATE_CHANGED, v1.FindingState_FINDING_STATE_RESOLVED}, PageSize: 500})))
		for _, f := range fs.Findings {
			card := must(c.GetOnCall(ctx, connect.NewRequest(&v1.GetOnCallRequest{System: system, FindingId: f.Id})))
			if !proto.Equal(card.Finding, f) || len(card.Assets) > 10 || len(card.Steps) > 12 || card.AssetsTotal < int32(len(card.Assets)) {
				t.Fatalf("%s/%s: %v", system, f.Id, card)
			}
			if f.State == v1.FindingState_FINDING_STATE_RESOLVED && !proto.Equal(card.StoryB, f.ResolvedAt) {
				t.Fatal(card)
			}
		}
	}
}
