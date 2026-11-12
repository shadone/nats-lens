package api_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// outOfSpill answers the way DuckDB does when a query needs more memory
// than its limit and more spill than duckdb_max_temp_size.
type outOfSpill struct{ *fake.Provider }

func (outOfSpill) ListServers(context.Context, *v1.ListServersRequest) (*v1.ListServersResponse, error) {
	return nil, fmt.Errorf("list servers: %w", errors.New("Out of Memory Error: failed to offload data block of size 256.0 KiB (2.0 GiB/2.0 GiB used).\nThis limit was set by the 'max_temp_directory_size' setting."))
}

// A query that ran out of memory and spill answers resource_exhausted, as
// an overloaded query budget does, so that the client tries again; the
// driver's text stays in the server log.
func TestOutOfMemoryAnswersResourceExhausted(t *testing.T) {
	h := &api.Handler{Systems: api.One(api.DefaultSystemID, api.DefaultSystemID, outOfSpill{fake.New(false)}), Log: apitest.Log(t)}
	_, err := h.ListServers(context.Background(), connect.NewRequest(&v1.ListServersRequest{}))
	if got := connect.CodeOf(err); got != connect.CodeResourceExhausted {
		t.Fatalf("answered %s: %v", got, err)
	}
	if strings.Contains(err.Error(), "max_temp_directory_size") {
		t.Errorf("the answer carries the driver's text: %v", err)
	}
	if !strings.Contains(err.Error(), "ran out of memory and spill space; try a shorter range or try again shortly") {
		t.Errorf("the answer does not say what ran out: %v", err)
	}
	var ce *connect.Error
	if !errors.As(err, &ce) || ce.Meta().Get(api.BusyHeader) != api.BusyOutOfMemory {
		t.Errorf("no %s metadata: %v", api.BusyHeader, err)
	}
}

// notFoundMentioningMemory is a sentinel error whose text happens to name
// DuckDB's out of memory error.
type notFoundMentioningMemory struct{ *fake.Provider }

func (notFoundMentioningMemory) ListServers(context.Context, *v1.ListServersRequest) (*v1.ListServersResponse, error) {
	return nil, fmt.Errorf("%w: server \"Out of Memory Error\"", api.ErrNotFound)
}

// A sentinel error keeps its code even when its text reads like running
// out of memory.
func TestSentinelBeatsOutOfMemoryText(t *testing.T) {
	h := &api.Handler{Systems: api.One(api.DefaultSystemID, api.DefaultSystemID, notFoundMentioningMemory{fake.New(false)}), Log: apitest.Log(t)}
	_, err := h.ListServers(context.Background(), connect.NewRequest(&v1.ListServersRequest{}))
	if got := connect.CodeOf(err); got != connect.CodeNotFound {
		t.Fatalf("answered %s: %v", got, err)
	}
}

// The metadata reaches a client over HTTP, where the UI reads it.
func TestOutOfMemoryMetadataOverHTTP(t *testing.T) {
	_, c := apitest.Serve(t, outOfSpill{fake.New(false)})
	_, err := c.ListServers(context.Background(), connect.NewRequest(&v1.ListServersRequest{}))
	var ce *connect.Error
	if !errors.As(err, &ce) || ce.Code() != connect.CodeResourceExhausted || ce.Meta().Get(api.BusyHeader) != api.BusyOutOfMemory {
		t.Fatalf("client got %v, metadata %v", err, ce.Meta())
	}
}
