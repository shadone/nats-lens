package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/store"
)

// A mute lifted after the newest hour was exported makes that hour pending
// again, so a rebuild sees the lift; older hours are not exported again.
func TestMuteChangeReexportsNewestHour(t *testing.T) {
	ctx := context.Background()
	st, dir := openMem(t), t.TempDir()
	a := newArchiver(st, dir)
	write(t, st, event(h0.Add(time.Minute), "a"), event(h1.Add(time.Minute), "b"))
	m, err := st.CreateMute(ctx, store.Mute{CheckCode: "stream-025", Reason: "known", MutedBy: "op",
		CreatedAt: h0, ExpiresAt: h0.Add(48 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if n := exported(t, a, now); n != 2 {
		t.Fatalf("first export = %d hours, want h0 and h1", n)
	}
	if n := exported(t, a, now); n != 0 {
		t.Fatalf("second export = %d hours, want none", n)
	}
	if _, err := st.UnmuteMute(ctx, m.ID, "lead", now); err != nil {
		t.Fatal(err)
	}
	if n := exported(t, a, now.Add(time.Minute)); n != 1 {
		t.Fatalf("export after the lift = %d hours, want the newest one", n)
	}
	if got := scalar[time.Time](t, st, `SELECT max(exported_at) FROM archive_hours WHERE hour = ?`, h0); !got.Equal(now) {
		t.Errorf("h0 exported at %v, want only the first export at %v", got, now)
	}
	if n := exported(t, a, now.Add(2*time.Minute)); n != 0 {
		t.Fatalf("export with no change = %d hours, want none", n)
	}
	// A new mute counts as a change too.
	if _, err := st.CreateMute(ctx, store.Mute{CheckCode: "server-018", Reason: "noisy", MutedBy: "op",
		CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if n := exported(t, a, now.Add(3*time.Minute)); n != 1 {
		t.Fatalf("export after a new mute = %d hours, want the newest one", n)
	}
	rb := rebuilt(t, dir, now.Add(3*time.Minute))
	if n := scalar[int](t, rb, `SELECT count(*) FROM mutes WHERE unmuted_by = 'lead'`); n != 1 {
		t.Errorf("lifted mutes after rebuild = %d, want 1", n)
	}
	if n := scalar[int](t, rb, `SELECT count(*) FROM mutes`); n != 2 {
		t.Errorf("mutes after rebuild = %d, want 2", n)
	}
}
