package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// DefaultSystemID is the ID of the one system of a configuration that
// does not list systems.
const DefaultSystemID = "default"

// CollectionState is what a system's collector is doing.
type CollectionState string

// Collection states.
const (
	CollectionStarting         CollectionState = "starting"
	CollectionCollecting       CollectionState = "collecting"
	CollectionNotConnected     CollectionState = "not_connected"
	CollectionConnectionClosed CollectionState = "connection_closed"
)

// CollectionStatus is the state of a system's collector as the runtime
// sees it.
type CollectionStatus struct {
	State CollectionState
	// LastError is the newest connection error; empty when there is none.
	LastError string
	// Since is when State began.
	Since time.Time
	// LastEpochAt is when the newest epoch started; zero before the first.
	LastEpochAt time.Time
}

// System is one NATS system the API serves.
type System struct {
	// Ref names the system; its ID is the system field of a request.
	Ref *v1.SystemRef
	// Provider answers the calls for the system.
	Provider Provider
	// Status reports the system's collector; nil when there is no
	// collector, as in the demo.
	Status func() CollectionStatus
}

// Systems are the systems the API serves, in configuration order. The
// first is the default: a request with an empty system reads it.
type Systems []System

// One returns the registry of a single system.
func One(id, name string, p Provider) Systems {
	return Systems{{Ref: &v1.SystemRef{Id: id, Name: name}, Provider: p}}
}

// Default is the first system; ok is false when there is none.
func (s Systems) Default() (System, bool) {
	if len(s) == 0 {
		return System{}, false
	}
	return s[0], true
}

// IDs lists the system IDs in order.
func (s Systems) IDs() []string {
	out := make([]string, len(s))
	for i, sys := range s {
		out[i] = sys.Ref.GetId()
	}
	return out
}

// Resolve returns the system with the ID, or the default system for an
// empty ID. An unknown ID is ErrInvalid, naming the known IDs.
func (s Systems) Resolve(id string) (System, error) {
	if id == "" {
		if d, ok := s.Default(); ok {
			return d, nil
		}
		return System{}, fmt.Errorf("%w: no system is configured", ErrFailedPrecondition)
	}
	for _, sys := range s {
		if sys.Ref.GetId() == id {
			return sys, nil
		}
	}
	return System{}, fmt.Errorf("%w: unknown system %q; known systems: %s", ErrInvalid, id, strings.Join(s.IDs(), ", "))
}

// ListSystems lists the systems in order with a summary from each
// provider. A provider whose summary fails is listed by its reference
// alone.
func (h *Handler) ListSystems(ctx context.Context, req *connect.Request[v1.ListSystemsRequest]) (*connect.Response[v1.ListSystemsResponse], error) {
	d, ok := h.Systems.Default()
	if !ok {
		return nil, h.codeOf("ListSystems", fmt.Errorf("%w: no system is configured", ErrFailedPrecondition))
	}
	m := req.Msg
	if err := withRange(d.Provider, &m.Range); err != nil {
		return nil, h.codeOf("ListSystems", err)
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	res := &v1.ListSystemsResponse{Range: m.GetRange(), DefaultSystem: d.Ref.GetId()}
	for _, sys := range h.Systems {
		sum, err := sys.Provider.SystemSummary(ctx, m)
		if err != nil || sum == nil {
			if err != nil && h.Log != nil {
				h.Log.Warn("system summary failed", "system", sys.Ref.GetId(), "err", err)
			}
			sum = &v1.SystemSummary{}
		}
		sum.System = proto.CloneOf(sys.Ref)
		res.Systems = append(res.Systems, sum)
	}
	return connect.NewResponse(res), nil
}
