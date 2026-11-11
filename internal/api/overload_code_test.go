package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/admit"
	"github.com/shadone/nats-lens/internal/timing"
)

// overloadedFlow returns a flow one of whose queries was refused for the
// budget.
func overloadedFlow(t *testing.T) *admit.Flow {
	t.Helper()
	g := admit.New(1, nil)
	hold, _ := g.Enter(context.Background(), admit.Request{Source: timing.SourceAPI})
	defer hold()
	ctx, cancel := context.WithTimeout(admit.WithFlow(context.Background()), 10*time.Millisecond)
	defer cancel()
	if _, err := g.Enter(ctx, admit.Request{Source: timing.SourceAPI}); !errors.Is(err, admit.ErrOverloaded) {
		t.Fatalf("setup: %v", err)
	}
	return admit.FlowOf(ctx)
}

func TestOverloadCodeMapsOnlyTheDeadline(t *testing.T) {
	f := overloadedFlow(t)
	deadline := connect.NewError(connect.CodeDeadlineExceeded, context.DeadlineExceeded)
	if got := connect.CodeOf(overloadCode(f, deadline)); got != connect.CodeResourceExhausted {
		t.Fatalf("a deadline after a refusal answered %s", got)
	}
	for _, code := range []connect.Code{connect.CodeInternal, connect.CodeUnknown, connect.CodeInvalidArgument} {
		err := connect.NewError(code, errors.New("boom"))
		if got := connect.CodeOf(overloadCode(f, err)); got != code {
			t.Errorf("%s after a refusal answered %s", code, got)
		}
	}
	if got := connect.CodeOf(overloadCode(&admit.Flow{}, deadline)); got != connect.CodeDeadlineExceeded {
		t.Errorf("a deadline without a refusal answered %s", got)
	}
}
