package api_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// reviewShell lets a test fail, panic or slow down ShellStatus and
// SystemSummary.
type reviewShell struct {
	renamed
	shellErr   error
	shellPanic bool
	sumDelay   time.Duration
}

func (r reviewShell) ShellStatus(ctx context.Context, req *v1.GetShellStatusRequest) (*v1.GetShellStatusResponse, error) {
	if r.shellPanic {
		panic("boom")
	}
	if r.shellErr != nil {
		return nil, r.shellErr
	}
	return r.renamed.ShellStatus(ctx, req)
}

func (r reviewShell) SystemSummary(ctx context.Context, req *v1.ListSystemsRequest) (*v1.SystemSummary, error) {
	if r.sumDelay > 0 {
		select {
		case <-time.After(r.sumDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return r.renamed.SystemSummary(ctx, req)
}

func reviewSystems(a, b reviewShell) api.Systems {
	return api.Systems{
		{Ref: &v1.SystemRef{Id: "a"}, Provider: a},
		{Ref: &v1.SystemRef{Id: "b"}, Provider: b},
	}
}

// A failing ShellStatus of the page's system still returns every system
// and its badge, as ListSystems lists them.
func TestShellStatusErrorKeepsSystemsAndBadges(t *testing.T) {
	a := reviewShell{renamed: newRenamed("a"), shellErr: errors.New("database is gone")}
	b := reviewShell{renamed: newRenamed("b")}
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: reviewSystems(a, b)})
	res, err := c.GetShellStatus(context.Background(), connect.NewRequest(&v1.GetShellStatusRequest{System: "a"}))
	t.Logf("code %v err %v", codeOf(err), err)
	if err != nil || len(res.Msg.GetSystems()) != 2 {
		t.Errorf("a failing system's shell call loses every system's badge and the list: err=%v", err)
	}
	ls, err := c.ListSystems(context.Background(), connect.NewRequest(&v1.ListSystemsRequest{}))
	if err != nil || len(ls.Msg.GetSystems()) != 2 {
		t.Fatalf("ListSystems: %v", err)
	}
}

// A panic in ShellStatus is recovered as internal, like any call.
func TestShellStatusPanicIsInternal(t *testing.T) {
	a := reviewShell{renamed: newRenamed("a"), shellPanic: true}
	b := reviewShell{renamed: newRenamed("b")}
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: reviewSystems(a, b)})
	_, err := c.GetShellStatus(context.Background(), connect.NewRequest(&v1.GetShellStatusRequest{System: "a"}))
	if codeOf(err) != connect.CodeInternal {
		t.Errorf("panic: %v", err)
	}
	_, err = c.GetOverview(context.Background(), connect.NewRequest(&v1.GetOverviewRequest{System: "b"}))
	if err != nil {
		t.Errorf("server died after a panic: %v", err)
	}
}

// A slow badge summary of another system does not hold up the page
// system's header.
func TestShellStatusSlowOtherBadgeDoesNotDelay(t *testing.T) {
	a := reviewShell{renamed: newRenamed("a")}
	b := reviewShell{renamed: newRenamed("b"), sumDelay: 5 * time.Second}
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: reviewSystems(a, b)})
	start := time.Now()
	if _, err := c.GetShellStatus(context.Background(), connect.NewRequest(&v1.GetShellStatusRequest{System: "a"})); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("system a's shell took %v, held by system b's badge", d)
	}
}

// A client deadline shorter than the badge read: the call ends with
// deadline_exceeded, as other calls do, and does not wait for the read.
func TestShellStatusClientDeadlineExceeded(t *testing.T) {
	a := reviewShell{renamed: newRenamed("a")}
	b := reviewShell{renamed: newRenamed("b"), sumDelay: 3 * time.Second}
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: reviewSystems(a, b)})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.GetShellStatus(ctx, connect.NewRequest(&v1.GetShellStatusRequest{System: "a"}))
	took := time.Since(start)
	t.Logf("after %v: code %v", took, codeOf(err))
	// Below b's 3 s read, with room for a loaded machine.
	if codeOf(err) != connect.CodeDeadlineExceeded || took > 2*time.Second {
		t.Errorf("deadline: %v after %v", err, took)
	}
	// The canceled badge is not kept: a next call reads it again.
	res, err := c.GetShellStatus(context.Background(), connect.NewRequest(&v1.GetShellStatusRequest{System: "a"}))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("next badge b: %v", res.Msg.GetSystems()[1])
}

// The server's side of a deadline that ends while the badges wait: the
// handler returns deadline_exceeded, not a response with pending badges.
// Over HTTP the client's own deadline usually ends the call first, so the
// handler is called directly.
func TestShellStatusPastDeadlineIsDeadlineExceeded(t *testing.T) {
	a := reviewShell{renamed: newRenamed("a")}
	b := reviewShell{renamed: newRenamed("b"), sumDelay: 3 * time.Second}
	h := &api.Handler{Systems: reviewSystems(a, b), Log: apitest.Log(t)}
	// The badges wait for the deadline, not their own limit, and system
	// a's part has a second to answer before it.
	api.SetBadgeClock(h, nil, time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	res, err := h.GetShellStatus(ctx, connect.NewRequest(&v1.GetShellStatusRequest{System: "a"}))
	if codeOf(err) != connect.CodeDeadlineExceeded {
		t.Fatalf("code %v err %v, response %v", codeOf(err), err, res)
	}
}

// Parallel shells on both systems while the runtime status flips.
func TestShellStatusParallelWhileStatusFlips(t *testing.T) {
	a := reviewShell{renamed: newRenamed("a")}
	b := reviewShell{renamed: newRenamed("b")}
	systems := reviewSystems(a, b)
	var flip atomicBool
	systems[0].Status = func() api.CollectionStatus {
		if flip.toggle() {
			return api.CollectionStatus{State: api.CollectionNotConnected, LastError: "x", Since: time.Now()}
		}
		return api.CollectionStatus{State: api.CollectionCollecting}
	}
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: systems})
	done := make(chan error, 64)
	for i := range 64 {
		go func() {
			sys := []string{"a", "b", "", "nope"}[i%4]
			_, err := c.GetShellStatus(context.Background(), connect.NewRequest(&v1.GetShellStatusRequest{System: sys}))
			done <- err
		}()
	}
	for range 64 {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
}
