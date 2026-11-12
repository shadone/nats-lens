package api_test

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/admit"
	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/timing"
)

type codesProvider struct {
	*fake.Provider
	fn func(ctx context.Context) error
}

func (p codesProvider) ListServers(ctx context.Context, _ *v1.ListServersRequest) (*v1.ListServersResponse, error) {
	return nil, p.fn(ctx)
}

func TestAnswerCodesEndToEnd(t *testing.T) {
	// A gate whose one slot is held and whose queue is full.
	full := admit.New(1, nil)
	hold, _ := full.Enter(context.Background(), admit.Request{Source: timing.SourceAPI})
	defer hold()
	qctx, qcancel := context.WithCancel(context.Background())
	defer qcancel()
	for range full.QueueLimit() {
		go full.Enter(qctx, admit.Request{Source: timing.SourceAPI})
	}
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(time.Millisecond) {
		if _, q := full.Load(); q == full.QueueLimit() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the queue did not fill")
		}
	}
	// A gate whose slot is held, with room to queue.
	busy := admit.New(1, nil)
	hold2, _ := busy.Enter(context.Background(), admit.Request{Source: timing.SourceAPI})
	defer hold2()

	interrupt := errors.New("INTERRUPT Error: Interrupted!")
	// enterFull asks the full gate for a slot and records whether it was
	// refused for the full queue, as the queue-full cases rely on.
	enterFull := func(ctx context.Context, refused *atomic.Bool) error {
		_, err := full.Enter(ctx, admit.Request{Source: timing.SourceAPI})
		var oe *admit.OverloadError
		refused.Store(errors.As(err, &oe))
		return err
	}
	cases := []struct {
		name string
		// queueFull marks a case whose call must be refused for the full
		// queue; it gets a longer server deadline, so that the call
		// reaches the gate before it passes on a loaded host.
		queueFull bool
		fn        func(ctx context.Context, refused *atomic.Bool) error
		want      connect.Code
	}{
		{"queue full at once", true, func(ctx context.Context, refused *atomic.Bool) error {
			return enterFull(ctx, refused)
		}, connect.CodeResourceExhausted},
		{"queue full, returned after the deadline", true, func(ctx context.Context, refused *atomic.Bool) error {
			err := enterFull(ctx, refused)
			<-ctx.Done()
			return err
		}, connect.CodeResourceExhausted},
		{"queue full, then an interrupted query at the deadline", true, func(ctx context.Context, refused *atomic.Bool) error {
			_ = enterFull(ctx, refused)
			<-ctx.Done()
			return interrupt
		}, connect.CodeResourceExhausted},
		{"deadline passed in the queue", false, func(ctx context.Context, _ *atomic.Bool) error {
			_, err := busy.Enter(ctx, admit.Request{Source: timing.SourceAPI})
			return err
		}, connect.CodeResourceExhausted},
		{"interrupted at the deadline, no refusal", false, func(ctx context.Context, _ *atomic.Bool) error {
			<-ctx.Done()
			return interrupt
		}, connect.CodeDeadlineExceeded},
		{"real internal error before the deadline", false, func(ctx context.Context, _ *atomic.Bool) error {
			return errors.New("boom")
		}, connect.CodeInternal},
		{"real internal error after the deadline", false, func(ctx context.Context, _ *atomic.Bool) error {
			<-ctx.Done()
			return errors.New("constraint violated")
		}, connect.CodeInternal},
		{"closed store after the deadline stays unavailable", false, func(ctx context.Context, _ *atomic.Bool) error {
			<-ctx.Done()
			return admit.ErrClosed
		}, connect.CodeUnavailable},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id := "codes-" + string(rune('a'+i))
			var refused atomic.Bool
			fn := func(ctx context.Context) error { return c.fn(ctx, &refused) }
			h := &api.Handler{Systems: api.One(id, id, codesProvider{fake.New(false), fn}), Log: apitest.Log(t)}
			_, cl := apitest.ServeHandler(t, h)
			// The client allows more than the server: the header carries
			// 1 s, 3 s for a queue-full case, and the client waits 5 s
			// longer for the server's answer. A shorter deadline can pass
			// before a call reaches the gate on a loaded host, and then
			// nothing was refused (the gate checks the context first).
			serverMs := 1000
			if c.queueFull {
				serverMs = 3000
			}
			req := connect.NewRequest(&v1.ListServersRequest{System: id})
			req.Header().Set("Connect-Timeout-Ms", strconv.Itoa(serverMs))
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(serverMs)*time.Millisecond+5*time.Second)
			defer cancel()
			// The counter is the process's, so a run with -count=N counts
			// from what earlier runs left.
			before := timing.APIDuration.Count(id, "ListServers", c.want.String())
			_, err := cl.ListServers(ctx, req)
			if c.queueFull && !refused.Load() {
				t.Fatalf("the gate did not refuse the call for its full queue; client got %s: %v", connect.CodeOf(err), err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for timing.APIDuration.Count(id, "ListServers", c.want.String()) != before+1 {
				if time.Now().After(deadline) {
					t.Errorf("server did not record %s; client got %s: %v", c.want, connect.CodeOf(err), err)
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
			t.Logf("client got %s", connect.CodeOf(err))
		})
	}
}
