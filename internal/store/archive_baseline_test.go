package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/store"
)

// An acceptance archived in h0 without a growth gets its baseline before
// h1 and h2 are exported. Every later hour holds the baselined copy, but a
// rebuild that ordered the unlifted copies by unmuted_at alone saw a tie
// and kept either; it keeps the copy with the growth. Half the runs
// rebuild on one thread, where the tie fell to the oldest file.
func TestRebuildKeepsTheBaselinedCopyOfAnAcceptance(t *testing.T) {
	ctx := context.Background()
	lost := 0
	const runs = 20
	for i := 0; i < runs; i++ {
		st, dir := openMem(t), t.TempDir()
		a := newArchiver(st, dir)
		write(t, st, event(h0.Add(time.Minute), "a"))
		m, err := st.CreateMute(ctx, store.Mute{CheckCode: "stream-005", EntityType: "stream", EntityKey: "SHOP/orders",
			Reason: "r", MutedBy: "ops@example.com", CreatedAt: h0, ExpiresAt: h0.Add(90 * 24 * time.Hour), Accepted: true})
		if err != nil {
			t.Fatal(err)
		}
		if n := exported(t, a, h1.Add(30*time.Minute)); n < 1 {
			t.Fatalf("first export = %d", n)
		}
		if ok, err := st.SetAcceptanceBaseline(ctx, m.ID, h1, 10<<20); err != nil || !ok {
			t.Fatalf("baseline %v %v", ok, err)
		}
		write(t, st, event(h1.Add(time.Minute), "b"), event(h2.Add(time.Minute), "c"))
		if n := exported(t, a, now.Add(time.Hour)); n < 1 {
			t.Fatalf("second export = %d", n)
		}
		// One thread on even runs: DuckDB then breaks the tie in the order
		// the files are read, h0 first, the copy without the growth.
		rb := openMem(t)
		if i%2 == 0 {
			if err := rb.SetThreads(ctx, 1); err != nil {
				t.Fatal(err)
			}
		}
		if err := newArchiver(rb, dir).Rebuild(ctx, now.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		all, _, err := rb.ListMutes(ctx, now, true, 10, 0)
		if err != nil || len(all) != 1 {
			t.Fatalf("mutes %v %v", all, err)
		}
		if all[0].GrowthPerDay == nil {
			lost++
		}
	}
	if lost > 0 {
		t.Errorf("the rebuild lost the baseline in %d of %d runs, though later hours hold it", lost, runs)
	}
}

// A baseline the review writes counts as a mute change: the newest hour is
// exported again and carries it.
func TestBaselineReexportsNewestHour(t *testing.T) {
	ctx := context.Background()
	st, dir := openMem(t), t.TempDir()
	a := newArchiver(st, dir)
	write(t, st, event(h0.Add(time.Minute), "a"))
	m, err := st.CreateMute(ctx, store.Mute{CheckCode: "stream-005", EntityType: "stream", EntityKey: "SHOP/orders",
		Reason: "r", MutedBy: "ops@example.com", CreatedAt: h0, ExpiresAt: h0.Add(90 * 24 * time.Hour), Accepted: true})
	if err != nil {
		t.Fatal(err)
	}
	if n := exported(t, a, now); n != 1 {
		t.Fatalf("first export = %d hours", n)
	}
	if n := exported(t, a, now.Add(time.Minute)); n != 0 {
		t.Fatalf("export with no change = %d hours", n)
	}
	if ok, err := st.SetAcceptanceBaseline(ctx, m.ID, now, 10<<20); err != nil || !ok {
		t.Fatalf("baseline %v %v", ok, err)
	}
	if n := exported(t, a, now.Add(2*time.Minute)); n != 1 {
		t.Fatalf("export after the baseline = %d hours, want the newest one", n)
	}
	all, _, err := rebuilt(t, dir, now.Add(2*time.Minute)).ListMutes(ctx, now, true, 10, 0)
	if err != nil || len(all) != 1 || all[0].GrowthPerDay == nil {
		t.Fatalf("rebuilt mutes %+v, %v; want the baseline", all, err)
	}
}
