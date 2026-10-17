package fake

import (
	"context"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// ListKeyValueStores returns no stores yet.
func (p *Provider) ListKeyValueStores(context.Context, *v1.ListKeyValueStoresRequest) (*v1.ListKeyValueStoresResponse, error) {
	return &v1.ListKeyValueStoresResponse{}, nil
}

// ListObjectStores returns no stores yet.
func (p *Provider) ListObjectStores(context.Context, *v1.ListObjectStoresRequest) (*v1.ListObjectStoresResponse, error) {
	return &v1.ListObjectStoresResponse{}, nil
}
