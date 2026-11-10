package store

import (
	"context"
	"strconv"
	"testing"
	"time"
)

// From the review: exact times survive a non-UTC session (run with TZ set), via
// insert, end, touch and relist.
func TestReviewClientPodsTimesExact(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	t0 := time.Date(2026, 11, 10, 23, 30, 0, 0, time.UTC)
	start := t0.Add(-90 * time.Minute)
	p := ClientPod{UID: "x", Namespace: "shop", Name: "x-1", IPs: []string{"10.0.0.1"}, StartedAt: start}
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: t0, Seen: []ClientPod{p}}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: t0.Add(5 * time.Minute), Touch: []string{""}}); err != nil {
		t.Fatal(err)
	}
	end := t0.Add(7 * time.Minute)
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: end, Ended: map[string]time.Time{"x": end}}); err != nil {
		t.Fatal(err)
	}
	r := clientPodRows(t, s)["x 10.0.0.1"]
	if !r.first.Equal(start) || !r.last.Equal(end) || r.ended == nil || !r.ended.Equal(end) {
		t.Fatalf("times shifted: first %v (want %v) last %v ended %v (want %v)", r.first, start, r.last, r.ended, end)
	}
	var n int
	if err := s.DB().QueryRow(`SELECT count(*) FROM client_pods WHERE last_seen < ?`, end.Add(time.Second)).Scan(&n); err != nil || n != 1 {
		t.Fatalf("direct compare: %d %v", n, err)
	}
}

// From the review: a namespace dropped from client_pod_watch (or one whose watch
// is failing) is never relisted, yet the touch refreshes its rows forever:
// they stay live, are never swept, and keep matching reused addresses.
func TestReviewTouchKeepsUnwatchedNamespaceLive(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	t0 := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	shop := ClientPod{UID: "s", Namespace: "shop", Name: "s-1", IPs: []string{"10.0.0.1"}, StartedAt: t0}
	pay := ClientPod{UID: "p", Namespace: "payments", Name: "p-1", IPs: []string{"10.0.0.2"}, StartedAt: t0}
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: t0, Seen: []ClientPod{shop, pay}}); err != nil {
		t.Fatal(err)
	}
	// The payments watch fails from here on: its touches leave it out.
	mid := t0.Add(time.Hour)
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: mid, Touch: []string{"shop"}}); err != nil {
		t.Fatal(err)
	}
	if r := clientPodRows(t, s)["p 10.0.0.2"]; !r.last.Equal(t0) || !clientPodRows(t, s)["s 10.0.0.1"].last.Equal(mid) {
		t.Fatalf("touch of shop only: payments last %v", r.last)
	}
	// Restart with client_pod_watch.namespaces: [shop] only; a week of
	// touches. The start ends every live address; the list of shop brings
	// its pod back.
	if err := s.EndLiveClientPods(ctx); err != nil {
		t.Fatal(err)
	}
	later := t0.Add(7 * 24 * time.Hour)
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: later, Seen: []ClientPod{shop}, Relists: []ClientPodRelist{{Namespace: "shop", Listed: []string{"s"}}}, Touch: []string{"shop"}}); err != nil {
		t.Fatal(err)
	}
	if r := clientPodRows(t, s)["s 10.0.0.1"]; r.ended != nil || !r.first.Equal(t0) {
		t.Errorf("shop pod after the restart: %+v", r)
	}
	r := clientPodRows(t, s)["p 10.0.0.2"]
	if r.ended == nil || !r.last.Equal(t0) {
		t.Errorf("payments pod, no longer watched, is live with last_seen %v (ended %v)", r.last, r.ended)
	}
}

// From the review: a pod whose sandbox is recreated (node reboot) keeps its UID
// and gets a new address; its old address must end.
func TestReviewPodNewAddressEndsOld(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	t0 := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	p := ClientPod{UID: "p", Namespace: "shop", Name: "p-1", IPs: []string{"10.0.0.1"}, StartedAt: t0}
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: t0, Seen: []ClientPod{p}}); err != nil {
		t.Fatal(err)
	}
	p.IPs = []string{"10.0.0.9"}
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: t0.Add(time.Hour), Seen: []ClientPod{p}, Touch: []string{""}}); err != nil {
		t.Fatal(err)
	}
	if r := clientPodRows(t, s)["p 10.0.0.1"]; r.ended == nil {
		t.Errorf("old address still live, last_seen %v", r.last)
	}
	if r := clientPodRows(t, s)["p 10.0.0.9"]; r.ended != nil || !r.first.Equal(t0.Add(time.Hour)) {
		t.Errorf("new address: first %v ended %v, want from the write", r.first, r.ended)
	}
	// An IPv4-mapped address is written as plain IPv4, as CONNZ writes it.
	q := ClientPod{UID: "q", Namespace: "shop", Name: "q-1", IPs: []string{"::ffff:10.0.0.5"}}
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: t0, Seen: []ClientPod{q}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := clientPodRows(t, s)["q 10.0.0.5"]; !ok {
		t.Errorf("mapped address not canonical: %v", clientPodRows(t, s))
	}
}

// From the review: how long a relist of a large namespace holds the store lock.
func TestReviewRelistCost(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	t0 := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	const n = 10_000
	var seen []ClientPod
	var uids []string
	for i := range n {
		uid := "uid-" + strconv.Itoa(i)
		seen = append(seen, ClientPod{UID: uid, Namespace: "shop", Name: uid, IPs: []string{"10.0." + strconv.Itoa(i/256) + "." + strconv.Itoa(i%256)}})
		uids = append(uids, uid)
	}
	start := time.Now()
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: t0, Seen: seen}); err != nil {
		t.Fatal(err)
	}
	t.Logf("first write of %d pods: %v", n, time.Since(start))
	start = time.Now()
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: t0.Add(time.Minute), Relists: []ClientPodRelist{{Namespace: "shop", Listed: uids[1:]}}}); err != nil {
		t.Fatal(err)
	}
	t.Logf("relist of %d: %v", n, time.Since(start))
	start = time.Now()
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: t0.Add(5 * time.Minute), Touch: []string{""}}); err != nil {
		t.Fatal(err)
	}
	t.Logf("touch: %v", time.Since(start))
	start = time.Now()
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: t0.Add(6 * time.Minute), Seen: seen}); err != nil {
		t.Fatal(err)
	}
	t.Logf("second write of %d pods, all stored: %v", n, time.Since(start))
}
