package storeprov

import (
	"context"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func (p *Provider) ListEventSelection(ctx context.Context, r *v1.ListEventSelectionRequest) (*v1.ListEventSelectionResponse, error) {
	return &v1.ListEventSelectionResponse{}, nil
}
