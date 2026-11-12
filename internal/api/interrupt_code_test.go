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

// interrupted answers the way DuckDB does when a query is interrupted at
// the call's deadline or cancel: an error that does not wrap the
// context's.
type interrupted struct{ *fake.Provider }

func (interrupted) ListServers(ctx context.Context, _ *v1.ListServersRequest) (*v1.ListServersResponse, error) {
	<-ctx.Done()
	return nil, errors.New("INTERRUPT Error: Interrupted!")
}

// A call whose context ended answers with the context's code, not
// internal, whatever error the interrupted query returned.
func TestInterruptedCallAnswersTheContextsCode(t *testing.T) {
	h := &api.Handler{Systems: api.One(api.DefaultSystemID, api.DefaultSystemID, interrupted{fake.New(false)}), Log: apitest.Log(t)}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := h.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{}))
	if got := connect.CodeOf(err); got != connect.CodeDeadlineExceeded {
		t.Fatalf("a deadline answered %s: %v", got, err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	_, err = h.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{}))
	if got := connect.CodeOf(err); got != connect.CodeCanceled {
		t.Fatalf("a cancel answered %s: %v", got, err)
	}
}
