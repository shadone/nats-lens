package store

import (
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/collect"
)

// FixtureSnapshot exposes the fixture epoch to the external test package
// store_test, which can import archive (archive imports store).
func FixtureSnapshot(t *testing.T, start time.Time) *collect.Snapshot {
	return fixtureSnapshot(t, start)
}
