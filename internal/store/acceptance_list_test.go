package store

import (
	"context"
	"testing"
	"time"
)

func TestCreateMutesStoresAllOrNone(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	ok := Mute{CheckCode: "consumer-016", EntityType: "consumer", EntityKey: "APP/orders/a", Reason: "r", MutedBy: "op",
		CreatedAt: t0, ExpiresAt: t0.Add(30 * 24 * time.Hour), Accepted: true}
	bad := ok
	bad.EntityKey, bad.ExpiresAt = "APP/orders/b", t0 // expires when created: the table refuses it
	if _, err := s.CreateMutes(ctx, []Mute{ok, bad}); err == nil {
		t.Fatal("a batch with a refused row was stored")
	}
	if all, n, err := s.ListMutes(ctx, t0, true, 10, 0); err != nil || n != 0 || len(all) != 0 {
		t.Fatalf("after a refused batch: %d mutes, %v", n, err)
	}
	second := ok
	second.EntityKey = "APP/orders/b"
	got, err := s.CreateMutes(ctx, []Mute{ok, second})
	if err != nil || len(got) != 2 || got[0].EntityKey != "APP/orders/a" || got[1].EntityKey != "APP/orders/b" || got[0].ID == got[1].ID || !got[1].Accepted {
		t.Fatalf("batch: %+v, %v", got, err)
	}
	if _, err := s.CreateMutes(ctx, []Mute{{CheckCode: "stream-005", Accepted: true, Reason: "r", MutedBy: "op", CreatedAt: t0, ExpiresAt: t0.Add(48 * time.Hour)}}); err == nil {
		t.Fatal("an acceptance without an entity was stored")
	}
}

func TestListAcceptancesOrderAndAccount(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	day := 24 * time.Hour
	add := func(code, typ, key string, review time.Duration, accepted bool) Mute {
		t.Helper()
		m, err := s.CreateMute(ctx, Mute{CheckCode: code, EntityType: typ, EntityKey: key, Reason: "r", MutedBy: "op",
			CreatedAt: t0, ExpiresAt: t0.Add(review), Accepted: accepted})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	late := add("stream-005", "stream", "APP/archive", 90*day, true)
	soon := add("consumer-017", "consumer", "APP/orders/paused", 10*day, true)
	other := add("stream-018", "stream", "SUPPLIERS/feed", 30*day, true)
	add("stream-006", "stream", "APP/orders", 5*day, false) // a plain mute is not listed
	ran := add("stream-019", "stream", "APP/KV_cfg", 2*day, true)
	ended := add("stream-020", "stream", "APP/idle", 60*day, true)
	now := t0.Add(3 * day)
	if _, err := s.UnmuteMute(ctx, ended.ID, "lead", t0.Add(day)); err != nil {
		t.Fatal(err)
	}
	ids := func(ms []Mute) []int64 {
		var out []int64
		for _, m := range ms {
			out = append(out, m.ID)
		}
		return out
	}
	eq := func(a, b []int64) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	ms, total, others, err := s.ListAcceptances(ctx, AcceptanceQuery{Now: now, Limit: 10})
	if err != nil || total != 3 || others != 0 || !eq(ids(ms), []int64{soon.ID, other.ID, late.ID}) {
		t.Fatalf("in force: %v, total %d, others %d, %v", ids(ms), total, others, err)
	}
	// Ended ones follow, the latest end first: ran out at day 2, ended by a person at day 1.
	ms, total, _, err = s.ListAcceptances(ctx, AcceptanceQuery{Now: now, Ended: true, Limit: 10})
	if err != nil || total != 5 || !eq(ids(ms), []int64{soon.ID, other.ID, late.ID, ran.ID, ended.ID}) {
		t.Fatalf("with ended: %v, total %d, %v", ids(ms), total, err)
	}
	ms, total, others, err = s.ListAcceptances(ctx, AcceptanceQuery{Now: now, Account: "APP", Limit: 10})
	if err != nil || total != 2 || others != 1 || !eq(ids(ms), []int64{soon.ID, late.ID}) {
		t.Fatalf("APP: %v, total %d, others %d, %v", ids(ms), total, others, err)
	}
	ms, _, _, err = s.ListAcceptances(ctx, AcceptanceQuery{Now: now, Limit: 1, Offset: 1})
	if err != nil || !eq(ids(ms), []int64{other.ID}) {
		t.Fatalf("second page: %v, %v", ids(ms), err)
	}
	// One second before the soonest review date it is in force, at it not.
	ms, _, _, err = s.ListAcceptances(ctx, AcceptanceQuery{Now: t0.Add(10 * day), Limit: 10})
	if err != nil || !eq(ids(ms), []int64{other.ID, late.ID}) {
		t.Fatalf("at the review date: %v, %v", ids(ms), err)
	}
	ms, _, _, err = s.ListAcceptances(ctx, AcceptanceQuery{Now: t0.Add(10*day - time.Second), Limit: 10})
	if err != nil || len(ms) != 3 || ms[0].ID != soon.ID {
		t.Fatalf("a second before the review date: %v, %v", ids(ms), err)
	}
}
