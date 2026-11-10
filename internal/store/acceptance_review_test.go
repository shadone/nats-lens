package store

import (
	"context"
	"testing"
	"time"
)

// Further cases: a review two hours after now must be in force whatever
// the host's zone; ties in the review date order by ID; the account filter
// keeps a stream and consumer by the part before the first "/", the same
// stream name in another account goes to others, and a server entity is
// kept.
func TestReviewAcceptancesZoneTiesAccounts(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	add := func(typ, key string, review time.Duration) Mute {
		t.Helper()
		m, err := s.CreateMute(ctx, Mute{CheckCode: "stream-005", EntityType: typ, EntityKey: key, Reason: "r", MutedBy: "op",
			CreatedAt: t0, ExpiresAt: t0.Add(review), Accepted: true})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	day := 24 * time.Hour
	a := add("stream", "APP/orders", 30*day)
	b := add("stream", "OTHER/orders", 30*day)
	c := add("consumer", "APP/orders/w", 30*day)
	d := add("server", "nats-0", 30*day)
	near := add("stream", "APP/near", 2*time.Hour)
	now := t0.Add(time.Hour)

	ms, total, _, err := s.ListAcceptances(ctx, AcceptanceQuery{Now: now, Limit: 10})
	if err != nil || total != 5 || len(ms) != 5 || ms[0].ID != near.ID || ms[1].ID != a.ID || ms[2].ID != b.ID || ms[3].ID != c.ID || ms[4].ID != d.ID {
		t.Fatalf("order: %+v total %d err %v", ms, total, err)
	}
	// Past the near review by 1 h: out, in any zone.
	ms, total, _, _ = s.ListAcceptances(ctx, AcceptanceQuery{Now: t0.Add(3 * time.Hour), Limit: 10})
	if total != 4 || ms[0].ID != a.ID {
		t.Fatalf("after near review: total %d first %d", total, ms[0].ID)
	}
	ms, total, others, _ := s.ListAcceptances(ctx, AcceptanceQuery{Now: now, Account: "APP", Limit: 10})
	if total != 4 || others != 1 || len(ms) != 4 {
		t.Fatalf("APP: total %d others %d %+v", total, others, ms)
	}
	for _, m := range ms {
		if m.ID == b.ID {
			t.Fatal("OTHER/orders listed under APP")
		}
	}
	// Paging by one over ties visits each once.
	seen := map[int64]bool{}
	for off := 0; off < 5; off++ {
		p, _, _, _ := s.ListAcceptances(ctx, AcceptanceQuery{Now: now, Limit: 1, Offset: off})
		if len(p) != 1 || seen[p[0].ID] {
			t.Fatalf("page %d: %+v", off, p)
		}
		seen[p[0].ID] = true
	}
	// CreateMute does not check for an acceptance in force (CreateAcceptances
	// does); a second row of the same entity is listed as its own.
	add("stream", "APP/orders", 40*day)
	if _, total, _, _ = s.ListAcceptances(ctx, AcceptanceQuery{Now: now, Limit: 10}); total != 6 {
		t.Fatalf("after a second row of APP/orders: total %d", total)
	}
}
