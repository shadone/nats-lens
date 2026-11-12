package storeprov

import (
	"context"

	"github.com/shadone/nats-lens/internal/store"
)

// SetSpillBytes replaces the read of a store's spill for a test and
// returns the function that puts it back.
func SetSpillBytes(f func(*store.Store, context.Context) (int64, error)) func() {
	old := spillBytes
	spillBytes = f
	return func() { spillBytes = old }
}

// SetReplayRowLimit lowers the Replay read limit for a test and returns
// the function that puts it back.
func SetReplayRowLimit(n int) func() {
	old := replayRowLimit
	replayRowLimit = n
	return func() { replayRowLimit = old }
}
