package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// addFinding stores a finding of the check for the entity, in the given state.
func addFinding(t *testing.T, s *Store, code, typ, key, state string) {
	t.Helper()
	last := map[string]string{"firing": "firing", "resolved": "resolved"}[state]
	if _, err := s.DB().Exec(`INSERT INTO findings (id, check_code, entity_type, entity_key, severity, state, last_transition, since, since_epoch,
  transitioned_at, last_seen_epoch, updated_epoch, detail) VALUES (?, ?, ?, ?, 'info', ?, ?, ?, 1, ?, 1, 1, '')`,
		code+":"+key, code, typ, key, state, last, t0, t0); err != nil {
		t.Fatal(err)
	}
}

func TestCreateAcceptancesStoresAllOrNone(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	addFinding(t, s, "consumer-016", "consumer", "APP/orders/a", "firing")
	addFinding(t, s, "consumer-016", "consumer", "APP/orders/b", "firing")
	addFinding(t, s, "consumer-016", "consumer", "APP/orders/gone", "resolved")
	ok := Mute{CheckCode: "consumer-016", EntityType: "consumer", EntityKey: "APP/orders/a", Reason: "r", MutedBy: "op",
		CreatedAt: t0, ExpiresAt: t0.Add(30 * 24 * time.Hour), Accepted: true}
	bad := ok
	bad.EntityKey, bad.ExpiresAt = "APP/orders/b", t0 // expires when created: the table refuses it
	if _, err := s.CreateAcceptances(ctx, []Mute{ok, bad}, t0); err == nil {
		t.Fatal("a batch with a refused row was stored")
	}
	none := ok
	none.EntityKey = "APP/orders/typo"
	if _, err := s.CreateAcceptances(ctx, []Mute{ok, none}, t0); !errors.Is(err, ErrNoFinding) || !strings.Contains(err.Error(), "APP/orders/typo") {
		t.Fatalf("an entity without a finding: %v", err)
	}
	// A resolved finding is not accepted: nothing fires to accept.
	gone := ok
	gone.EntityKey = "APP/orders/gone"
	if _, err := s.CreateAcceptances(ctx, []Mute{ok, gone}, t0); !errors.Is(err, ErrFindingNotFiring) || !strings.Contains(err.Error(), "APP/orders/gone") {
		t.Fatalf("a resolved finding: %v", err)
	}
	if all, n, err := s.ListMutes(ctx, t0, true, 10, 0); err != nil || n != 0 || len(all) != 0 {
		t.Fatalf("after refused batches: %d mutes, %v", n, err)
	}
	second := ok
	second.EntityKey = "APP/orders/b"
	got, err := s.CreateAcceptances(ctx, []Mute{ok, second}, t0)
	if err != nil || len(got) != 2 || got[0].EntityKey != "APP/orders/a" || got[1].EntityKey != "APP/orders/b" || got[0].ID == got[1].ID || !got[1].Accepted {
		t.Fatalf("batch: %+v, %v", got, err)
	}
	// An acceptance in force refuses another of the same check and entity.
	if _, err := s.CreateAcceptances(ctx, []Mute{ok}, t0.Add(time.Hour)); !errors.Is(err, ErrAcceptanceInForce) || !strings.Contains(err.Error(), "APP/orders/a") {
		t.Fatalf("second acceptance: %v", err)
	}
	// Once it ended, a new one is allowed.
	if _, err := s.UnmuteMute(ctx, got[0].ID, "op", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAcceptances(ctx, []Mute{ok}, t0.Add(2*time.Hour)); err != nil {
		t.Fatalf("after the end: %v", err)
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

// A plain mute of an entity with an acceptance of the check in force is
// refused; a mute of another check, of every entity, or after the
// acceptance ended is stored.
func TestCreateMuteRefusesAnAcceptedEntity(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	addFinding(t, s, "consumer-016", "consumer", "APP/orders/a", "firing")
	acc := Mute{CheckCode: "consumer-016", EntityType: "consumer", EntityKey: "APP/orders/a", Reason: "r", MutedBy: "op",
		CreatedAt: t0, ExpiresAt: t0.Add(30 * 24 * time.Hour), Accepted: true}
	got, err := s.CreateAcceptances(ctx, []Mute{acc}, t0)
	if err != nil {
		t.Fatal(err)
	}
	plain := acc
	plain.Accepted, plain.CreatedAt, plain.ExpiresAt = false, t0.Add(time.Hour), t0.Add(2*time.Hour)
	if _, err := s.CreateMute(ctx, plain); !errors.Is(err, ErrAccepted) || !strings.Contains(err.Error(), "APP/orders/a") ||
		!strings.Contains(err.Error(), t0.Add(30*24*time.Hour).Format("2006-01-02 15:04 UTC")) {
		t.Fatalf("plain mute of an accepted entity: %v", err)
	}
	other, every := plain, plain
	other.CheckCode = "consumer-017"
	every.EntityType, every.EntityKey = "", ""
	for _, m := range []Mute{other, every} {
		if _, err := s.CreateMute(ctx, m); err != nil {
			t.Fatalf("mute %s %q: %v", m.CheckCode, m.EntityKey, err)
		}
	}
	if _, err := s.UnmuteMute(ctx, got[0].ID, "op", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMute(ctx, plain); err != nil {
		t.Fatalf("after the acceptance ended: %v", err)
	}
}
