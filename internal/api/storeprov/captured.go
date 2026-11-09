package storeprov

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/shadone/nats-lens/internal/store"
)

// NewCaptured borrows an already validated closed read-only dataset. The caller
// owns DB.Close. No collector, configuration, runtime status, or archiver callback
// is constructed. A positive captured/asserted cadence and dataset clock are required.
func NewCaptured(ctx context.Context, db *sql.DB, interval time.Duration, reference time.Time, fingerprint string) (*Provider, error) {
	if interval <= 0 || reference.IsZero() {
		return nil, errors.New("captured provider requires cadence and reference clock")
	}
	st, err := store.NewReadView(ctx, db)
	if err != nil {
		return nil, err
	}
	p := &Provider{St: st, Interval: interval, Clock: func() time.Time { return reference.UTC() }, CapturedOnly: true}
	if err = p.listPages.InitImmutable(fingerprint); err != nil {
		return nil, err
	}
	if err = p.episodePages.InitImmutable(fingerprint); err != nil {
		return nil, err
	}
	return p, nil
}
