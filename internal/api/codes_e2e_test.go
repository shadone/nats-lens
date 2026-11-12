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

// The answer codes through HTTP, the interceptor and the handler. The
// mapping rules themselves are TestAnswerCodes, without timers; these
// cases check that a call goes through them. The client's context has no
// deadline: connect-go writes a context deadline into Connect-Timeout-Ms,
// in place of the test's, so the client's and the server's deadlines ended
// together, and when the client's cancel reached the server first its
// context ended as canceled, not as past its deadline, and the call was
// recorded as canceled. A watchdog cancels only a call that hangs.
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
	interrupt := errors.New("INTERRUPT Error: Interrupted!")
	// enterFull asks the full gate for a slot and records whether it was
	// refused for the full queue.
	enterFull := func(ctx context.Context, refused *atomic.Bool) error {
		_, err := full.Enter(ctx, admit.Request{Source: timing.SourceAPI})
		var oe *admit.OverloadError
		refused.Store(errors.As(err, &oe) && oe.QueueFull)
		return err
	}
	cases := []struct {
		name string
		// serverMs is the call's deadline on the server. A case that waits
		// for it gets 3 s, so that the call reaches the gate before it
		// passes; the gate checks the context first.
		serverMs  int
		queueFull bool
		fn        func(ctx context.Context, refused *atomic.Bool) error
		want      connect.Code
	}{
		{"queue full at once", 30000, true, func(ctx context.Context, refused *atomic.Bool) error {
			return enterFull(ctx, refused)
		}, connect.CodeResourceExhausted},
		{"queue full, then an interrupted query at the deadline", 3000, true, func(ctx context.Context, refused *atomic.Bool) error {
			_ = enterFull(ctx, refused)
			<-ctx.Done()
			return interrupt
		}, connect.CodeResourceExhausted},
		{"real internal error", 30000, false, func(ctx context.Context, _ *atomic.Bool) error {
			return errors.New("boom")
		}, connect.CodeInternal},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id := "codes-" + string(rune('a'+i))
			var refused atomic.Bool
			fn := func(ctx context.Context) error { return c.fn(ctx, &refused) }
			h := &api.Handler{Systems: api.One(id, id, codesProvider{fake.New(false), fn}), Log: apitest.Log(t)}
			_, cl := apitest.ServeHandler(t, h)
			req := connect.NewRequest(&v1.ListServersRequest{System: id})
			req.Header().Set("Connect-Timeout-Ms", strconv.Itoa(c.serverMs))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			watchdog := time.AfterFunc(time.Minute, cancel)
			defer watchdog.Stop()
			// The counter is the process's, so a run with -count=N counts
			// from what earlier runs left.
			before := timing.APIDuration.Count(id, "ListServers", c.want.String())
			_, err := cl.ListServers(ctx, req)
			if c.queueFull && !refused.Load() {
				t.Fatalf("the gate did not refuse the call for its full queue; client got %s: %v", connect.CodeOf(err), err)
			}
			if got := connect.CodeOf(err); got != c.want {
				t.Errorf("client got %s: %v; want %s", got, err, c.want)
			}
			// The interceptor records the call before the response is
			// written, so the count is final once the client has it.
			if n := timing.APIDuration.Count(id, "ListServers", c.want.String()); n != before+1 {
				t.Errorf("server recorded %d calls as %s, want %d", n, c.want, before+1)
			}
		})
	}
}
