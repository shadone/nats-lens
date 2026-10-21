package runner

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/store"
)

const mib = 1 << 20

var reviewStart = time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)

// growthStore holds 49 epochs, every 30 minutes over 24 hours, in which
// each stream of perDay grows steadily at its rate (bytes per day) and the
// server nats-0's JetStream storage at serverPerDay. A stream listed in
// lastHours reports only in the epochs of its last so many hours. It
// returns the store and the last epoch.
func growthStore(t *testing.T, perDay map[string]float64, lastHours map[string]int, serverPerDay float64) (*store.Store, int64) {
	t.Helper()
	st, err := store.OpenPath(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := st.DB().Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO servers (server_id, server_name, first_epoch, last_epoch, first_seen_at, last_seen_at) VALUES ('NSERVER0', 'nats-0', 1, 49, ?, ?)`,
		reviewStart, reviewStart)
	var epoch int64
	for i := 0; i <= 48; i++ {
		epoch = int64(i + 1)
		at := reviewStart.Add(time.Duration(i) * 30 * time.Minute)
		days := at.Sub(reviewStart).Hours() / 24
		exec(`INSERT INTO epochs (epoch, started_at, finished_at, duration_ms, expected_servers, observed_servers,
  expected_names, observed_names, missing_names, endpoint_failures, partial, errors)
VALUES (?, ?, ?, 0, 1, 1, ['nats-0'], ['nats-0'], [], 0, false, [])`, epoch, at, at)
		exec(`INSERT INTO server_stats (epoch, server_id, js_storage) VALUES (?, 'NSERVER0', ?)`,
			epoch, int64(1<<30+serverPerDay*days))
		for name, rate := range perDay {
			if h, ok := lastHours[name]; ok && at.Before(reviewStart.Add(time.Duration(24-h)*time.Hour)) {
				continue
			}
			exec(`INSERT INTO stream_stats (epoch, server_id, account, stream, bytes, is_leader) VALUES (?, 'NSERVER0', 'SHOP', ?, ?, true)`,
				epoch, name, int64(100*mib+rate*days))
		}
	}
	return st, epoch
}

func accept(t *testing.T, st *store.Store, typ, key string, growth *float64) store.Mute {
	t.Helper()
	m, err := st.CreateMute(context.Background(), store.Mute{CheckCode: typ + "-005", EntityType: typ, EntityKey: key,
		Reason: "by design", MutedBy: "ops@example.com", CreatedAt: reviewStart, ExpiresAt: reviewStart.Add(180 * 24 * time.Hour),
		Accepted: true, GrowthPerDay: growth})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func f(v float64) *float64 { return &v }

func mutesByID(t *testing.T, st *store.Store) map[int64]store.Mute {
	t.Helper()
	all, _, err := st.ListMutes(context.Background(), reviewStart, true, 1000, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64]store.Mute{}
	for _, m := range all {
		out[m.ID] = m
	}
	return out
}

func TestReviewEndsAcceptanceOnGrowthChange(t *testing.T) {
	ctx := context.Background()
	st, last := growthStore(t, map[string]float64{
		"grew-60":     16 * mib,
		"grew-40":     14 * mib,
		"small":       200 << 10,
		"short":       50 * mib,
		"no-recorded": 50 * mib,
		"muted":       50 * mib,
	}, map[string]int{"short": 5}, 30*mib)
	grew60 := accept(t, st, "stream", "SHOP/grew-60", f(10*mib))
	grew40 := accept(t, st, "stream", "SHOP/grew-40", f(10*mib))
	// Under the floor: +100 KiB to +200 KiB/day is a change of 100%, but
	// less than half of 1 MiB.
	small := accept(t, st, "stream", "SHOP/small", f(100<<10))
	// Under 6 hours of data now: no rate, so nothing to compare.
	short := accept(t, st, "stream", "SHOP/short", f(1*mib))
	// The stream is gone: no data at all.
	gone := accept(t, st, "stream", "SHOP/gone", f(1*mib))
	// Accepted without a rate: never reviewed.
	noRecorded := accept(t, st, "stream", "SHOP/no-recorded", nil)
	server := accept(t, st, "server", "nats-0", f(10*mib))
	// A plain mute, even with a growth, is never ended by the review.
	plain, err := st.CreateMute(ctx, store.Mute{CheckCode: "stream-005", EntityType: "stream", EntityKey: "SHOP/muted",
		Reason: "in review", MutedBy: "op", CreatedAt: reviewStart, ExpiresAt: reviewStart.Add(30 * 24 * time.Hour), GrowthPerDay: f(1)})
	if err != nil {
		t.Fatal(err)
	}

	ended, err := ReviewAcceptances(ctx, st, last, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(ended) != 2 {
		t.Fatalf("ended %+v, want grew-60 and the server", ended)
	}
	at := reviewStart.Add(24 * time.Hour)
	ms := mutesByID(t, st)
	for _, tc := range []struct {
		m      store.Mute
		reason string
	}{
		{grew60, "growth changed from +10 MiB/day to +16 MiB/day"},
		{server, "growth changed from +10 MiB/day to +30 MiB/day"},
	} {
		got := ms[tc.m.ID]
		if !got.UnmutedAt.Equal(at) || got.UnmutedBy != api.AcceptEndedBy || got.EndReason != tc.reason {
			t.Errorf("%s: unmuted %v by %q, reason %q; want %v by nats-lens, %q", tc.m.EntityKey, got.UnmutedAt, got.UnmutedBy, got.EndReason, at, tc.reason)
		}
	}
	for _, m := range []store.Mute{grew40, small, short, gone, noRecorded, plain} {
		if got := ms[m.ID]; !got.UnmutedAt.IsZero() || got.EndReason != "" {
			t.Errorf("%s (accepted %v) was ended: %+v", m.EntityKey, m.Accepted, got)
		}
	}

	// A second review ends nothing more and leaves the ended rows as they
	// were.
	again, err := ReviewAcceptances(ctx, st, last, 30*time.Second)
	if err != nil || len(again) != 0 {
		t.Fatalf("second review ended %+v, %v", again, err)
	}
	if got, was := mutesByID(t, st)[grew60.ID], ms[grew60.ID]; !got.UnmutedAt.Equal(was.UnmutedAt) || got.EndReason != was.EndReason {
		t.Errorf("grew-60 changed by the second review: %+v", got)
	}
}

// An acceptance created after the epoch started is reviewed from the next
// epoch on, so it is never ended at a time before it was created.
func TestReviewSkipsAcceptanceNewerThanTheEpoch(t *testing.T) {
	ctx := context.Background()
	st, last := growthStore(t, map[string]float64{"grew": 16 * mib}, nil, 0)
	m, err := st.CreateMute(ctx, store.Mute{CheckCode: "stream-005", EntityType: "stream", EntityKey: "SHOP/grew",
		Reason: "r", MutedBy: "op", CreatedAt: reviewStart.Add(24*time.Hour + time.Second), ExpiresAt: reviewStart.Add(48 * time.Hour),
		Accepted: true, GrowthPerDay: f(1 * mib)})
	if err != nil {
		t.Fatal(err)
	}
	if ended, err := ReviewAcceptances(ctx, st, last, 30*time.Second); err != nil || len(ended) != 0 {
		t.Fatalf("ended %+v, %v", ended, err)
	}
	if got := mutesByID(t, st)[m.ID]; !got.UnmutedAt.IsZero() {
		t.Fatalf("acceptance newer than the epoch was ended: %+v", got)
	}
}

// The review reads the acceptances and the growth of all their entities
// in one query each; many acceptances take about as long as a few.
func TestReviewManyAcceptances(t *testing.T) {
	ctx := context.Background()
	rates := map[string]float64{}
	for i := range 200 {
		rates[fmt.Sprintf("s%03d", i)] = float64(i) * mib
	}
	st, last := growthStore(t, rates, nil, 0)
	for name := range rates {
		accept(t, st, "stream", "SHOP/"+name, f(rates[name]))
	}
	start := time.Now()
	ended, err := ReviewAcceptances(ctx, st, last, 30*time.Second)
	if err != nil || len(ended) != 0 {
		t.Fatalf("ended %d, %v; every rate is unchanged", len(ended), err)
	}
	t.Logf("reviewed 200 acceptances in %v", time.Since(start))
}
