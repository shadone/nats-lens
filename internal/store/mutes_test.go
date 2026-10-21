package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMuteLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	whole, err := s.CreateMute(ctx, Mute{CheckCode: "stream-025", Reason: "known", MutedBy: "op", CreatedAt: t0, ExpiresAt: t0.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	one, err := s.CreateMute(ctx, Mute{CheckCode: "stream-031", EntityType: "stream", EntityKey: "APP/audit-archive",
		Reason: "retention in review", MutedBy: "op", CreatedAt: t0, ExpiresAt: t0.Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if whole.ID == 0 || one.ID == whole.ID || whole.EntityType != "" || one.EntityKey != "APP/audit-archive" {
		t.Fatalf("created %+v and %+v", whole, one)
	}
	active, n, err := s.ListMutes(ctx, t0.Add(2*time.Hour), false, 10, 0)
	if err != nil || len(active) != 1 || n != 1 || active[0].ID != one.ID {
		t.Fatalf("active after the first expired: %+v, %v", active, err)
	}
	if _, err := s.UnmuteMute(ctx, whole.ID, "op", t0.Add(2*time.Hour)); !errors.Is(err, ErrMuteInactive) {
		t.Fatalf("unmute expired: %v", err)
	}
	if _, err := s.UnmuteMute(ctx, 999, "op", t0); !errors.Is(err, ErrMuteNotFound) {
		t.Fatalf("unmute unknown: %v", err)
	}
	un, err := s.UnmuteMute(ctx, one.ID, "lead", t0.Add(3*time.Hour))
	if err != nil || un.UnmutedBy != "lead" || !un.UnmutedAt.Equal(t0.Add(3*time.Hour)) {
		t.Fatalf("unmute: %+v, %v", un, err)
	}
	all, n, err := s.ListMutes(ctx, t0.Add(3*time.Hour), true, 10, 0)
	if err != nil || len(all) != 2 || n != 2 || all[0].ID != one.ID || all[0].UnmutedAt.IsZero() {
		t.Fatalf("all: %+v, %v", all, err)
	}
	if page, n, err := s.ListMutes(ctx, t0.Add(3*time.Hour), true, 1, one.ID); err != nil || len(page) != 1 || n != 2 || page[0].ID != whole.ID {
		t.Fatalf("second page: %+v, %d, %v", page, n, err)
	}
	if _, err := s.CreateMute(ctx, Mute{CheckCode: "x", Reason: "r", MutedBy: "op", CreatedAt: t0, ExpiresAt: t0}); err == nil {
		t.Fatal("a mute that expires when it is created was accepted")
	}
}

func TestMuteLockGivesUpWithTheContext(t *testing.T) {
	s := openMem(t)
	s.mu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := s.CreateMute(ctx, Mute{CheckCode: "x-001", Reason: "r", MutedBy: "op", CreatedAt: t0, ExpiresAt: t0.Add(time.Hour)})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("create while locked: %v", err)
	}
	s.mu.Unlock()
	// The abandoned lock is released, so the next write goes through.
	if _, err := s.CreateMute(context.Background(), Mute{CheckCode: "x-001", Reason: "r", MutedBy: "op", CreatedAt: t0, ExpiresAt: t0.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
}

func TestAcceptanceRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	g := -3.0 * (1 << 20)
	a, err := s.CreateMute(ctx, Mute{CheckCode: "stream-005", EntityType: "stream", EntityKey: "SHOP/order-archive",
		Reason: "kept for the account's life", MutedBy: "ops@example.com", CreatedAt: t0, ExpiresAt: t0.Add(180 * 24 * time.Hour),
		Accepted: true, GrowthPerDay: &g})
	if err != nil || !a.Accepted || a.GrowthPerDay == nil || *a.GrowthPerDay != g {
		t.Fatalf("created %+v, %v", a, err)
	}
	if _, err := s.CreateMute(ctx, Mute{CheckCode: "stream-005", Reason: "r", MutedBy: "op", CreatedAt: t0,
		ExpiresAt: t0.Add(time.Hour), Accepted: true}); err == nil {
		t.Fatal("an acceptance without an entity was stored")
	}
	// A person lifts it: no end reason. EndAcceptance then ends nothing.
	un, err := s.UnmuteMute(ctx, a.ID, "lead", t0.Add(time.Hour))
	if err != nil || !un.Accepted || un.EndReason != "" {
		t.Fatalf("unmute = %+v, %v", un, err)
	}
	if ok, err := s.EndAcceptance(ctx, a.ID, t0.Add(2*time.Hour), "nats-lens", "x"); err != nil || ok {
		t.Fatalf("end of a lifted acceptance = %v, %v", ok, err)
	}
	// EndAcceptance leaves a plain mute alone.
	m, err := s.CreateMute(ctx, Mute{CheckCode: "stream-005", EntityType: "stream", EntityKey: "SHOP/x", Reason: "r", MutedBy: "op",
		CreatedAt: t0, ExpiresAt: t0.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := s.EndAcceptance(ctx, m.ID, t0.Add(time.Minute), "nats-lens", "x"); err != nil || ok {
		t.Fatalf("end of a plain mute = %v, %v", ok, err)
	}
}
