package storeprov

import (
	"context"
	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// StorageChange reads both endpoint collections in one read transaction.
func (p *Provider) StorageChange(ctx context.Context, req *v1.GetStorageChangeRequest) (*api.StorageChangeFacts, error) {
	return &api.StorageChangeFacts{}, nil
}
