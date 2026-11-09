package storeprov

import (
	"context"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func (p *Provider) GetEventBatch(ctx context.Context, req *v1.GetEventBatchRequest) (*v1.GetEventBatchResponse, error) {
	return &v1.GetEventBatchResponse{}, nil
}
