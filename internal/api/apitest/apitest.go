// Package apitest starts the API over HTTP for tests and returns a Connect
// client for it.
package apitest

import (
	"log/slog"
	"net/http/httptest"
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
	if h.Log == nil {
		h.Log = slog.New(slog.NewTextHandler(t.Output(), nil))
	}
	srv := httptest.NewServer(api.NewMux(h, webui.FS()))
	t.Cleanup(srv.Close)
	return srv, natslensv1connect.NewLensServiceClient(srv.Client(), srv.URL+api.PathPrefix, opts...)
}
