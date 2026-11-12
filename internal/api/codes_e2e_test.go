package api_test

import (
	"context"
	"errors"
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
	cases := []struct {
		name string
		fn   func(ctx context.Context) error
		want connect.Code
	}{
		{"queue full at once", func(ctx context.Context) error {
			_, err := full.Enter(ctx, admit.Request{Source: timing.SourceAPI})
			return err
		}, connect.CodeResourceExhausted},
		{"queue full, returned after the deadline", func(ctx context.Context) error {
			_, err := full.Enter(ctx, admit.Request{Source: timing.SourceAPI})
			<-ctx.Done()
			return err
		}, connect.CodeResourceExhausted},
		{"queue full, then an interrupted query at the deadline", func(ctx context.Context) error {
			_, _ = full.Enter(ctx, admit.Request{Source: timing.SourceAPI})
			<-ctx.Done()
			return interrupt
		}, connect.CodeResourceExhausted},
		{"deadline passed in the queue", func(ctx context.Context) error {
			_, err := busy.Enter(ctx, admit.Request{Source: timing.SourceAPI})
			return err
		}, connect.CodeResourceExhausted},
		{"interrupted at the deadline, no refusal", func(ctx context.Context) error {
			<-ctx.Done()
			return interrupt
		}, connect.CodeDeadlineExceeded},
		{"real internal error before the deadline", func(ctx context.Context) error {
			return errors.New("boom")
		}, connect.CodeInternal},
		{"real internal error after the deadline", func(ctx context.Context) error {
			<-ctx.Done()
			return errors.New("constraint violated")
		}, connect.CodeInternal},
		{"closed store after the deadline stays unavailable", func(ctx context.Context) error {
			<-ctx.Done()
			return admit.ErrClosed
		}, connect.CodeUnavailable},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id := "codes-" + string(rune('a'+i))
			h := &api.Handler{Systems: api.One(id, id, codesProvider{fake.New(false), c.fn}), Log: apitest.Log(t)}
			_, cl := apitest.ServeHandler(t, h)
			// The client allows more than the server: the header carries
			// 1 s, the client waits up to 5 s for the server's answer. A
			// shorter deadline can pass before a call reaches the gate on a
			// loaded host, and then nothing waited in the queue.
			req := connect.NewRequest(&v1.ListServersRequest{System: id})
			req.Header().Set("Connect-Timeout-Ms", "1000")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := cl.ListServers(ctx, req)
			deadline := time.Now().Add(5 * time.Second)
			for timing.APIDuration.Count(id, "ListServers", c.want.String()) != 1 {
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
