package store

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"
)

// A relist that lists nothing ends every live pod of its namespace.
func TestClientPodsEmptyRelistEndsNamespace(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	t0 := time.Date(2026, 11, 10, 23, 30, 0, 0, time.UTC)
	a := ClientPod{UID: "a", Namespace: "shop", Name: "a", IPs: []string{"10.0.0.1"}}
	b := ClientPod{UID: "b", Namespace: "pay", Name: "b", IPs: []string{"10.0.0.2"}}
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: t0, Seen: []ClientPod{a, b}}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: t0.Add(time.Minute), Relists: []ClientPodRelist{{Namespace: "shop"}}}); err != nil {
		t.Fatal(err)
	}
	r := clientPodRows(t, s)
	if r["a 10.0.0.1"].ended == nil || r["b 10.0.0.2"].ended != nil {
		t.Fatalf("empty relist of shop: a ended %v, b ended %v", r["a 10.0.0.1"].ended, r["b 10.0.0.2"].ended)
	}
}

// A pod deleted (watch event) and then left out of a relist in
// the same batch ends at its deletion, not at its last touch.
func TestClientPodEndedAndRelistSameBatch(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	t0 := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	a := ClientPod{UID: "a", Namespace: "shop", Name: "a", IPs: []string{"10.0.0.1"}, StartedAt: t0}
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: t0, Seen: []ClientPod{a}}); err != nil {
		t.Fatal(err)
	}
	gone := t0.Add(4 * time.Minute)
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: t0.Add(5 * time.Minute), Ended: map[string]time.Time{"a": gone},
		Relists: []ClientPodRelist{{Namespace: "shop"}}}); err != nil {
		t.Fatal(err)
	}
	if r := clientPodRows(t, s)["a 10.0.0.1"]; r.ended == nil || !r.ended.Equal(gone) {
		t.Errorf("ended at %v, want the deletion %v", r.ended, gone)
	}
}

// A pod present across a restart keeps one row, its first
// sighting, and is matched for a time inside the restart window.
func TestClientPodRestartReopens(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	t0 := time.Date(2026, 11, 10, 23, 30, 0, 0, time.UTC)
	a := ClientPod{UID: "a", Namespace: "shop", Name: "a", IPs: []string{"10.0.0.1"}, StartedAt: t0}
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: t0, Seen: []ClientPod{a}}); err != nil {
		t.Fatal(err)
	}
	if err := s.EndLiveClientPods(ctx); err != nil {
		t.Fatal(err)
	}
	back := t0.Add(2 * time.Hour)
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: back, Seen: []ClientPod{a}, Relists: []ClientPodRelist{{Namespace: "shop", Listed: []string{"a"}}}}); err != nil {
		t.Fatal(err)
	}
	rows := clientPodRows(t, s)
	if len(rows) != 1 {
		t.Fatalf("rows %v", rows)
	}
	r := rows["a 10.0.0.1"]
	if r.ended != nil || !r.first.Equal(t0) || !r.last.Equal(back) {
		t.Fatalf("row %+v", r)
	}
	var n int
	mid := t0.Add(time.Hour).UnixMicro()
	if err := s.DB().QueryRow(`SELECT count(*) FROM client_pods WHERE first_seen <= make_timestamp(?::BIGINT) AND (ended_at IS NULL OR ended_at >= make_timestamp(?::BIGINT))`, mid, mid).Scan(&n); err != nil || n != 1 {
		t.Fatalf("match in restart window: %d %v", n, err)
	}
}

// Concurrent writers (the temp table is per connection; the lock
// serialises) do not collide.
func TestClientPodsConcurrentWrites(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	t0 := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for g := range 8 {
		wg.Go(func() {
			for i := range 20 {
				uid := strconv.Itoa(g) + "-" + strconv.Itoa(i)
				p := ClientPod{UID: uid, Namespace: "ns", Name: uid, IPs: []string{"10." + strconv.Itoa(g) + ".0." + strconv.Itoa(i)}}
				if err := s.WriteClientPods(ctx, ClientPodBatch{At: t0.Add(time.Duration(i) * time.Second), Seen: []ClientPod{p}}); err != nil {
					errs <- err
					return
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if n := len(clientPodRows(t, s)); n != 160 {
		t.Fatalf("rows %d", n)
	}
}

// A pod seen and ended in one batch, as one that lived less than a flush,
// is written ended.
func TestClientPodSeenAndEndedSameBatch(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	t0 := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	a := ClientPod{UID: "a", Namespace: "shop", Name: "job-a", IPs: []string{"10.0.0.1"}, StartedAt: t0}
	end := t0.Add(10 * time.Second)
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: t0.Add(15 * time.Second), Seen: []ClientPod{a}, Ended: map[string]time.Time{"a": end}}); err != nil {
		t.Fatal(err)
	}
	if r := clientPodRows(t, s)["a 10.0.0.1"]; r.ended == nil || !r.ended.Equal(end) || !r.first.Equal(t0) {
		t.Fatalf("row %+v", r)
	}
}
