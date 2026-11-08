package storeprov

import (
	"context"
	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func (p *Provider) ListMetricContributors(ctx context.Context, req *v1.ListMetricContributorsRequest) (*v1.ListMetricContributorsResponse, error) {
	return nil, api.ErrFailedPrecondition
}
