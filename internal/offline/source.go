// Package offline composes captured data with the authoritative read-only API.
package offline

import (
	"context"
	"errors"
	"github.com/shadone/nats-lens/internal/investigation"
)

func Open(ctx context.Context, selection investigation.OfflineSelection) (*investigation.Session, error) {
	return nil, errors.New("offline source not implemented")
}
