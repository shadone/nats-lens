package api

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/admit"
	"github.com/shadone/nats-lens/internal/timing"
)

// fullQueueFlow returns a flow that a full queue refused at once, as a call's
// flow is after one of its queries was refused for the query budget.
func fullQueueFlow(t *testing.T) *admit.Flow {
	t.Helper()
	g := admit.New(1, nil)
	hold, err := g.Enter(context.Background(), admit.Request{Source: timing.SourceAPI})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hold)
	qctx, qcancel := context.WithCancel(context.Background())
	t.Cleanup(qcancel)
	for range g.QueueLimit() {
		go func() { _, _ = g.Enter(qctx, admit.Request{Source: timing.SourceAPI}) }()
	}
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(time.Millisecond) {
		if _, q := g.Load(); q == g.QueueLimit() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the queue did not fill")
		}
	}
	ctx := admit.WithFlow(context.Background())
	_, err = g.Enter(ctx, admit.Request{Source: timing.SourceAPI})
	var oe *admit.OverloadError
	if !errors.As(err, &oe) || !oe.QueueFull {
		t.Fatalf("the full queue answered %v", err)
	}
	f := admit.FlowOf(ctx)
	if !f.Overloaded() {
		t.Fatal("the refused flow is not marked")
	}
	return f
}

// Every rule that turns a provider's failure into a call's code, applied
// in the order a call applies them: endedBy and codeOf in the handler,
// then overloadCode in the timing interceptor. The context's state and the
// flow are set by the test, not by timers, so the outcome does not depend
// on scheduling.
func TestAnswerCodes(t *testing.T) {
	live := context.Background()
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	refusedFlow := fullQueueFlow(t)
	queueFull := &admit.OverloadError{QueueFull: true, Limit: 1, Queued: 64}
	deadlineInQueue := fmt.Errorf("%w: waited 1s for one of 1 query slots: %w", admit.ErrOverloaded, context.DeadlineExceeded)
	interrupt := errors.New("INTERRUPT Error: Interrupted!")

	cases := []struct {
		name string
		ctx  context.Context
		flow *admit.Flow
		err  error
		want connect.Code
	}{
		{"queue full at once", live, refusedFlow, queueFull, connect.CodeResourceExhausted},
		{"queue full, returned after the deadline", expired, refusedFlow, queueFull, connect.CodeResourceExhausted},
		{"queue full, then an interrupted query at the deadline", expired, refusedFlow, interrupt, connect.CodeResourceExhausted},
		{"queue full, then the provider returned the deadline", expired, refusedFlow, context.DeadlineExceeded, connect.CodeResourceExhausted},
		{"deadline passed in the queue", expired, refusedFlow, deadlineInQueue, connect.CodeResourceExhausted},
		{"interrupted at the deadline, no refusal", expired, nil, interrupt, connect.CodeDeadlineExceeded},
		{"interrupted when the caller left", canceled, nil, interrupt, connect.CodeCanceled},
		{"interrupted when the caller left after a refusal", canceled, refusedFlow, interrupt, connect.CodeCanceled},
		{"interrupt text before the deadline stays internal", live, nil, interrupt, connect.CodeInternal},
		{"real internal error before the deadline", live, nil, errors.New("boom"), connect.CodeInternal},
		{"real internal error after the deadline", expired, nil, errors.New("constraint violated"), connect.CodeInternal},
		{"real internal error after a refusal and the deadline", expired, refusedFlow, errors.New("constraint violated"), connect.CodeInternal},
		{"closed store after the deadline stays unavailable", expired, refusedFlow, admit.ErrClosed, connect.CodeUnavailable},
	}
	h := &Handler{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := overloadCode(c.flow, h.answer(c.ctx, "ListServers", c.err))
			if connect.CodeOf(got) != c.want {
				t.Errorf("answered %s (%v), want %s", connect.CodeOf(got), got, c.want)
			}
		})
	}
}
