package acquisition

import (
	"context"
	"errors"
	"github.com/shadone/nats-lens/internal/dataset"
	"github.com/shadone/nats-lens/internal/store"
)

type Source struct {
	Store  *store.Store
	System dataset.System
}
type Options struct {
	Socket, Directory, ApplicationVersion string
	Budget                                dataset.Options
}
type Result struct {
	Path     string           `json:"path"`
	Manifest dataset.Manifest `json:"manifest"`
}

func Start(context.Context, Options, []Source) (func(), error) {
	return nil, errors.New("local admin acquisition not implemented")
}
func Request(context.Context, string, string) (Result, error) {
	return Result{}, errors.New("local admin acquisition not implemented")
}
