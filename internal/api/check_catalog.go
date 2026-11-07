package api

import (
	"connectrpc.com/connect"
	"context"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

type checkDefinitions interface {
	CheckDefinitions() (*v1.ListCheckDefinitionsResponse, error)
}

// ListCheckDefinitions reads immutable reference and in-memory configuration.
// It deliberately needs no snapshot, account-name lookup or database access.
func (h *Handler) ListCheckDefinitions(_ context.Context, req *connect.Request[v1.ListCheckDefinitionsRequest]) (*connect.Response[v1.ListCheckDefinitionsResponse], error) {
	sys, err := h.Systems.Resolve(req.Msg.GetSystem())
	if err != nil {
		return nil, h.codeOf("ListCheckDefinitions", err)
	}
	if p, ok := sys.Provider.(checkDefinitions); ok {
		res, err := p.CheckDefinitions()
		if err != nil {
			return nil, h.codeOf("ListCheckDefinitions", err)
		}
		return connect.NewResponse(res), nil
	}
	return connect.NewResponse(&v1.ListCheckDefinitionsResponse{ConfigurationState: "unavailable"}), nil
}
