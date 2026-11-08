package dataset

import (
	"context"
	"github.com/shadone/nats-lens/internal/store"
)

type Options struct {
	MaxBytes     int64
	MinFreeBytes int64
}

func Acquire(ctx context.Context, st *store.Store, destination string, system System, applicationVersion string, o Options) (Manifest, error) {
	return Manifest{}, ErrInvalid
}
