package api_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

type interruptedSeriesProvider struct{ *fake.Provider }

func (interruptedSeriesProvider) GetTimeSeries(ctx context.Context, _ *v1.GetTimeSeriesRequest) (*v1.GetTimeSeriesResponse, error) {
	<-ctx.Done()
	return nil, errors.New("INTERRUPT Error: Interrupted!")
}

// A series query interrupted at its own 100 ms limit, within the batch's
// 5 s, as DuckDB interrupts it.
func TestBatchSeriesInterruptedAtItsOwnLimit(t *testing.T) {
	defer api.SetBatchTimeouts(5*time.Second, 100*time.Millisecond)()
	_, c := apitest.Serve(t, interruptedSeriesProvider{fake.New(false)})
	_, err := c.GetTimeSeriesBatch(context.Background(), connect.NewRequest(&v1.GetTimeSeriesBatchRequest{Queries: repeatQuery(1)}))
	if got := connect.CodeOf(err); got != connect.CodeDeadlineExceeded {
		t.Errorf("got %s, want deadline_exceeded: %v", got, err)
	}
}

type interruptedShell struct{ *fake.Provider }

func (interruptedShell) ShellStatus(ctx context.Context, _ *v1.GetShellStatusRequest) (*v1.GetShellStatusResponse, error) {
	<-ctx.Done()
	return nil, errors.New("INTERRUPT Error: Interrupted!")
}

// The shell's part interrupted at ShellTimeout reports deadline_exceeded,
// not internal, while the call itself still answers.
func TestShellPartInterruptedAtItsLimit(t *testing.T) {
	h := &api.Handler{Systems: api.One(api.DefaultSystemID, api.DefaultSystemID, interruptedShell{fake.New(false)}),
		ShellTimeout: 100 * time.Millisecond, Log: apitest.Log(t)}
	_, c := apitest.ServeHandler(t, h)
	res, err := c.GetShellStatus(context.Background(), connect.NewRequest(&v1.GetShellStatusRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Msg.GetErrorCode(); got != connect.CodeDeadlineExceeded.String() {
		t.Fatalf("the shell's part reported %q: %s", got, res.Msg.GetError())
	}
}
