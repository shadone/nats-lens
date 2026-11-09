package fake

import (
	"context"
	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func (p *Provider) ListEventSelection(ctx context.Context, r *v1.ListEventSelectionRequest) (*v1.ListEventSelectionResponse, error) {
	return api.ReadEventSelection(ctx, p, r)
}
