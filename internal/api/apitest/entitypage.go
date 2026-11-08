package apitest

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
	"google.golang.org/protobuf/proto"
)

// RunEntityListContract exercises identical pagination and typed reset semantics
// over HTTP against the real and demo providers.
func RunEntityListContract(t *testing.T, c natslensv1connect.LensServiceClient) {
	t.Helper()
	ctx := context.Background()
	first, err := c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{Limit: 1, IncludeKv: true}))
	if err != nil {
		t.Fatal(err)
	}
	f := first.Msg
	if len(f.Streams) != 1 || f.TotalCount < 2 || f.NextPageToken == "" {
		t.Fatalf("fixture needs multiple streams: %v", f)
	}
	seen := map[string]bool{f.Streams[0].Entity.Key: true}
	token := f.NextPageToken
	for token != "" {
		next, err := c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{Limit: 1, IncludeKv: true, PageToken: token}))
		if err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(next.Msg.Range, f.Range) || !proto.Equal(next.Msg.Totals, f.Totals) || next.Msg.TotalCount != f.TotalCount {
			t.Fatal("continuation changed range or totals")
		}
		for _, s := range next.Msg.Streams {
			if seen[s.Entity.Key] {
				t.Fatal("duplicate", s.Entity.Key)
			}
			seen[s.Entity.Key] = true
		}
		token = next.Msg.NextPageToken
	}
	if len(seen) != int(f.TotalCount) {
		t.Fatal("incomplete traversal")
	}
	if _, err = c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{PageToken: f.NextPageToken, IncludeKv: false})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("changed filter", err)
	}
	if _, err = c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{PageToken: "invalid"})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("invalid token", err)
	}
	if _, err = c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{Limit: -1})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("negative limit", err)
	}
	consumerFirst, err := c.ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{Limit: 1, IncludeEphemeral: true}))
	if err != nil {
		t.Fatal(err)
	}
	cf := consumerFirst.Msg
	if len(cf.Consumers) != 1 || cf.TotalCount < 2 || cf.NextPageToken == "" {
		t.Fatal("consumer count/page missing")
	}
	consumerNext, err := c.ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{Limit: 1, IncludeEphemeral: true, PageToken: cf.NextPageToken}))
	if err != nil {
		t.Fatal(err)
	}
	if consumerNext.Msg.TotalCount != cf.TotalCount || consumerNext.Msg.Consumers[0].Entity.Key == cf.Consumers[0].Entity.Key {
		t.Fatal("consumer continuation")
	}
	// Context storage is bounded. Eviction is explicit, with a typed reset.
	for range 8 {
		if _, err = c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{Limit: 1})); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{IncludeKv: true, PageToken: f.NextPageToken})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("evicted context", err)
	}
}
