package fake

import (
	"context"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Runtime RED boundary; authoritative occurrence projection follows tests.
func (p *Provider) ListFindingEpisodes(ctx context.Context, r *v1.ListFindingEpisodesRequest) (*v1.ListFindingEpisodesResponse, error) {
	return &v1.ListFindingEpisodesResponse{}, nil
}
