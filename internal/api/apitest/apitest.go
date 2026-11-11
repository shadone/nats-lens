// Package apitest starts the API over HTTP for tests and returns a Connect
// client for it.
package apitest

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"sync"
	"testing"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
	"github.com/shadone/nats-lens/internal/webui"
)

// Serve serves p as the one system "default" on a test server and returns
// the server and a client that speaks the given options' protocol (Connect
// when none is given). The handler logs to the test output.
func Serve(t *testing.T, p api.Provider, opts ...connect.ClientOption) (*httptest.Server, natslensv1connect.LensServiceClient) {
	t.Helper()
	return ServeHandler(t, &api.Handler{Systems: api.One(api.DefaultSystemID, api.DefaultSystemID, p)}, opts...)
}

// ServeHandler is Serve with a handler configured by the caller; a nil Log
// is set to the test output.
func ServeHandler(t *testing.T, h *api.Handler, opts ...connect.ClientOption) (*httptest.Server, natslensv1connect.LensServiceClient) {
	t.Helper()
	if err := h.Systems.Validate(); err != nil {
		t.Fatal(err)
	}
	if h.Log == nil {
		h.Log = Log(t)
	}
	srv := httptest.NewServer(api.NewMux(h, webui.FS()))
	t.Cleanup(srv.Close)
	return srv, natslensv1connect.NewLensServiceClient(srv.Client(), srv.URL+api.PathPrefix, opts...)
}

// Log returns a logger that writes to the test output until the test ends
// and drops what comes later. A read the handler runs in the background,
// such as a slow badge, can log its timeout after the test has returned,
// and the test output panics on a write after the test has completed.
func Log(t *testing.T) *slog.Logger {
	w := &untilDone{w: t.Output()}
	t.Cleanup(func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.done = true
	})
	return slog.New(slog.NewTextHandler(w, nil))
}

type untilDone struct {
	mu   sync.Mutex
	w    io.Writer
	done bool
}

func (u *untilDone) Write(p []byte) (int, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.done {
		return len(p), nil
	}
	return u.w.Write(p)
}
