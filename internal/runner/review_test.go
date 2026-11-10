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

// growthStore holds 433 epochs, every 30 minutes over 9 days, in which
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
	exec(`INSERT INTO servers (server_id, server_name, first_epoch, last_epoch, first_seen_at, last_seen_at) VALUES ('NSERVER0', 'nats-0', 1, 433, ?, ?)`,
		reviewStart, reviewStart)
	var epoch int64
	for i := 0; i <= 432; i++ {
		epoch = int64(i + 1)
		at := reviewStart.Add(time.Duration(i) * 30 * time.Minute)
		days := at.Sub(reviewStart).Hours() / 24
		exec(`INSERT INTO epochs (epoch, started_at, finished_at, duration_ms, expected_servers, observed_servers,
  expected_names, observed_names, missing_names, endpoint_failures, partial, errors)
VALUES (?, ?, ?, 0, 1, 1, ['nats-0'], ['nats-0'], [], 0, false, [])`, epoch, at, at)
		exec(`INSERT INTO server_stats (epoch, server_id, js_storage) VALUES (?, 'NSERVER0', ?)`,
			epoch, int64(1<<30+serverPerDay*days))
		for name, rate := range perDay {
			if h, ok := lastHours[name]; ok && at.Before(reviewStart.Add(time.Duration(216-h)*time.Hour)) {
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

// withServerReview gives server-005 the growth review for the test: no
// built-in server check has it, but the review of a server's growth stays.
func withServerReview(t *testing.T) {
	t.Helper()
	old := growthReview
	growthReview = func(code string) bool { return code == "server-005" || old(code) }
	t.Cleanup(func() { growthReview = old })
}

// An acceptance of a check without the growth review, such as one stored
// before its check lost the review, is neither ended nor baselined however
// its entity grows: it ends at its review date.
func TestReviewSkipsChecksWithoutGrowthReview(t *testing.T) {
	ctx := context.Background()
	st, last := growthStore(t, map[string]float64{"grew": 16 * mib, "new": 16 * mib}, nil, 0)
	ended, err := st.CreateMute(ctx, store.Mute{CheckCode: "stream-018", EntityType: "stream", EntityKey: "SHOP/grew",
		Reason: "r", MutedBy: "op", CreatedAt: reviewStart, ExpiresAt: reviewStart.Add(180 * 24 * time.Hour), Accepted: true, GrowthPerDay: f(1 * mib)})
	if err != nil {
		t.Fatal(err)
	}
	unset, err := st.CreateMute(ctx, store.Mute{CheckCode: "stream-002", EntityType: "stream", EntityKey: "SHOP/new",
		Reason: "r", MutedBy: "op", CreatedAt: reviewStart, ExpiresAt: reviewStart.Add(180 * 24 * time.Hour), Accepted: true})
	if err != nil {
		t.Fatal(err)
	}
	rev, err := ReviewAcceptances(ctx, st, last, 30*time.Second)
	if err != nil || len(rev.Ended) != 0 || len(rev.Baselined) != 0 {
		t.Fatalf("ended %+v, baselined %+v, %v", rev.Ended, rev.Baselined, err)
	}
	got := mutesByID(t, st)
	if !got[ended.ID].UnmutedAt.IsZero() || got[unset.ID].GrowthPerDay != nil {
		t.Fatalf("acceptances changed: %+v, %+v", got[ended.ID], got[unset.ID])
	}
}

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
	withServerReview(t)
	ctx := context.Background()
	st, last := growthStore(t, map[string]float64{
		"grew-60":     16 * mib,
		"grew-40":     14 * mib,
		"small":       200 << 10,
		"short":       50 * mib,
		"no-recorded": 50 * mib,
		"young":       50 * mib,
		"muted":       50 * mib,
	}, map[string]int{"short": 20, "young": 5 * 24}, 30*mib)
	grew60 := accept(t, st, "stream", "SHOP/grew-60", f(10*mib))
	grew40 := accept(t, st, "stream", "SHOP/grew-40", f(10*mib))
	// Under the floor: +100 KiB to +200 KiB/day is a change of 100%, but
	// less than half of 1 MiB.
	small := accept(t, st, "stream", "SHOP/small", f(100<<10))
	// Under 8 days of data now: no rate, so nothing to compare.
	short := accept(t, st, "stream", "SHOP/short", f(1*mib))
	// The stream is gone: no data at all.
	gone := accept(t, st, "stream", "SHOP/gone", f(1*mib))
	// Accepted without a rate, as the stream was young then: the review
	// records the rate now as the baseline and does not end it.
	noRecorded := accept(t, st, "stream", "SHOP/no-recorded", nil)
	// Accepted without a rate and still under 8 days old: nothing yet.
	young := accept(t, st, "stream", "SHOP/young", nil)
	server := accept(t, st, "server", "nats-0", f(10*mib))
	// A plain mute, even with a growth, is never ended by the review.
	plain, err := st.CreateMute(ctx, store.Mute{CheckCode: "stream-005", EntityType: "stream", EntityKey: "SHOP/muted",
		Reason: "in review", MutedBy: "op", CreatedAt: reviewStart, ExpiresAt: reviewStart.Add(30 * 24 * time.Hour), GrowthPerDay: f(1)})
	if err != nil {
		t.Fatal(err)
	}

	rev, err := ReviewAcceptances(ctx, st, last, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(rev.Ended) != 2 {
		t.Fatalf("ended %+v, want grew-60 and the server", rev.Ended)
	}
	if len(rev.Baselined) != 1 || rev.Baselined[0].ID != noRecorded.ID {
		t.Fatalf("baselined %+v, want no-recorded", rev.Baselined)
	}
	at := reviewStart.Add(216 * time.Hour)
	ms := mutesByID(t, st)
	for _, tc := range []struct {
		m      store.Mute
		reason string
	}{
		{grew60, "growth changed from +10 MiB/day to +16 MiB/day over the last 7 days (+16 MiB in the last day)"},
		{server, "growth changed from +10 MiB/day to +30 MiB/day over the last 7 days (+30 MiB in the last day)"},
	} {
		got := ms[tc.m.ID]
		if !got.UnmutedAt.Equal(at) || got.UnmutedBy != api.AcceptEndedBy || got.EndReason != tc.reason {
			t.Errorf("%s: unmuted %v by %q, reason %q; want %v by nats-lens, %q", tc.m.EntityKey, got.UnmutedAt, got.UnmutedBy, got.EndReason, at, tc.reason)
		}
	}
	for _, m := range []store.Mute{grew40, small, short, gone, noRecorded, young, plain} {
		if got := ms[m.ID]; !got.UnmutedAt.IsZero() || got.EndReason != "" {
			t.Errorf("%s (accepted %v) was ended: %+v", m.EntityKey, m.Accepted, got)
		}
	}
	if g := ms[noRecorded.ID].GrowthPerDay; g == nil || *g != 50*mib {
		t.Errorf("no-recorded baseline = %v, want 50 MiB/day", g)
	}
	if g := ms[young.ID].GrowthPerDay; g != nil {
		t.Errorf("young stream got a baseline of %v", *g)
	}

	// A second review ends nothing more and leaves the ended rows as they
	// were.
	again, err := ReviewAcceptances(ctx, st, last, 30*time.Second)
	if err != nil || len(again.Ended) != 0 || len(again.Baselined) != 0 {
		t.Fatalf("second review %+v, %v", again, err)
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
		Reason: "r", MutedBy: "op", CreatedAt: reviewStart.Add(216*time.Hour + time.Second), ExpiresAt: reviewStart.Add(240 * time.Hour),
		Accepted: true, GrowthPerDay: f(1 * mib)})
	if err != nil {
		t.Fatal(err)
	}
	if rev, err := ReviewAcceptances(ctx, st, last, 30*time.Second); err != nil || len(rev.Ended) != 0 {
		t.Fatalf("ended %+v, %v", rev.Ended, err)
	}
	if got := mutesByID(t, st)[m.ID]; !got.UnmutedAt.IsZero() {
		t.Fatalf("acceptance newer than the epoch was ended: %+v", got)
	}
}

// The review reads the acceptances and the growth of all their entities
// in one query each. With 200 acceptances of streams collected every 30
// seconds for 8 days and a half (6.1 million stream_stats rows), it reads
// the two days it needs and stays well under a collect interval.
func TestReviewManyAcceptances(t *testing.T) {
	if testing.Short() {
		t.Skip("writes 6 million rows")
	}
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	const epochs = 8*2880 + 1440 // 8.5 days at 30 seconds
	for _, q := range []string{
		`INSERT INTO epochs (epoch, started_at, finished_at, duration_ms, expected_servers, observed_servers,
  expected_names, observed_names, missing_names, endpoint_failures, partial, errors)
SELECT i + 1, TIMESTAMP '2026-10-21 00:00:00' + to_seconds(i * 30), TIMESTAMP '2026-10-21 00:00:00' + to_seconds(i * 30),
  0, 1, 1, ['nats-0'], ['nats-0'], [], 0, false, [] FROM range(` + fmt.Sprint(epochs) + `) r(i)`,
		`INSERT INTO stream_stats (epoch, server_id, account, stream, bytes, is_leader)
SELECT e.i + 1, 'NSERVER0', 'SHOP', printf('s%03d', s.j), (100 << 20) + e.i * s.j * 364, true
FROM range(` + fmt.Sprint(epochs) + `) e(i), range(200) s(j)`,
	} {
		if _, err := st.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for j := range 200 {
		// 364 bytes per epoch and per j: j * 364 * 2880 bytes a day.
		rec := float64(j) * 364 * 2880
		if j == 199 {
			rec = 1 << 20 // wrong on purpose: the one that ends, so the rates were measured
		}
		accept(t, st, "stream", fmt.Sprintf("SHOP/s%03d", j), f(rec))
	}
	start := time.Now()
	rev, err := ReviewAcceptances(ctx, st, epochs, 30*time.Second)
	took := time.Since(start)
	if err != nil || len(rev.Ended) != 1 || rev.Ended[0].EntityKey != "SHOP/s199" || len(rev.Baselined) != 0 {
		t.Fatalf("ended %+v, baselined %d, %v; want only s199", rev.Ended, len(rev.Baselined), err)
	}
	t.Logf("reviewed 200 acceptances over %d epochs in %v", epochs, took)
	if took > 10*time.Second {
		t.Errorf("review took %v", took)
	}
}
