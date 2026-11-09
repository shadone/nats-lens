package collect

import (
	"context"
	"testing"
	"time"
)

// The store keeps times in microseconds. A snapshot's times have no finer
// part, so that the started_at the runner hands on equals the stored one
// on a clock with nanoseconds (Linux) as well as on one without (macOS).
func TestSnapshotTimesHaveMicrosecondPrecision(t *testing.T) {
	at := time.Date(2026, 10, 17, 14, 30, 0, 123456789, time.UTC)
	c := microCollector(50*time.Millisecond, 0, &at)
	snap := c.Collect(context.Background())
	want := time.Date(2026, 10, 17, 14, 30, 0, 123456000, time.UTC)
	if !snap.StartedAt.Equal(want) || !snap.FinishedAt.Equal(want) {
		t.Fatalf("snapshot times %v and %v, want %v", snap.StartedAt, snap.FinishedAt, want)
	}
}
