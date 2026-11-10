package runner

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/store"
)

// Acceptances in force of checks that are no longer acceptable are counted
// and named; acceptable checks, plain mutes and ended acceptances are not.
func TestUnacceptableAcceptances(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	add := func(code, key string, accepted bool, expires time.Time) int64 {
		t.Helper()
		m, err := st.CreateMute(ctx, store.Mute{CheckCode: code, EntityType: "server", EntityKey: key, Reason: "r", MutedBy: "op",
			CreatedAt: now.Add(-time.Hour), ExpiresAt: expires, Accepted: accepted})
		if err != nil {
			t.Fatal(err)
		}
		return m.ID
	}
	later := now.Add(30 * 24 * time.Hour)
	add("server-014", "nats-0", true, later)
	add("server-014", "nats-1", true, later)
	add("stream-002", "nats-2", true, later)
	add("stream-005", "nats-3", true, later)
	add("server-009", "nats-4", false, later)
	add("server-007", "nats-5", true, now.Add(-time.Minute))
	// A lifted acceptance is no longer in force.
	if _, err := st.UnmuteMute(ctx, add("client-007", "nats-6", true, later), "op", now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	n, codes, err := UnacceptableAcceptances(ctx, st, now)
	if err != nil || n != 3 || !slices.Equal(codes, []string{"server-014", "stream-002"}) {
		t.Fatalf("UnacceptableAcceptances = %d, %v, %v; want 3 of server-014 and stream-002", n, codes, err)
	}
}
