package storeprov

import (
	"context"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// SystemSummary is empty: the handler adds the system reference.
func (p *Provider) SystemSummary(context.Context, *v1.ListSystemsRequest) (*v1.SystemSummary, error) {
	return &v1.SystemSummary{}, nil
}
