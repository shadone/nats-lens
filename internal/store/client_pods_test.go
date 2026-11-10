package store

import (
	"context"
	"testing"
	"time"
)

type podRow struct {
	uid, ip, node, kind, workload string
	host                          bool
	first, last                   time.Time
	ended                         *time.Time
}

func clientPodRows(t *testing.T, s *Store) map[string]podRow {
	t.Helper()
	rows, err := s.DB().Query(`SELECT uid, ip, node, workload_kind, workload, host_network, first_seen, last_seen, ended_at FROM client_pods`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]podRow{}
	for rows.Next() {
		var r podRow
		if err := rows.Scan(&r.uid, &r.ip, &r.node, &r.kind, &r.workload, &r.host, &r.first, &r.last, &r.ended); err != nil {
			t.Fatal(err)
		}
		r.first, r.last = r.first.UTC(), r.last.UTC()
		if r.ended != nil {
			e := r.ended.UTC()
			r.ended = &e
		}
		out[r.uid+" "+r.ip] = r
	}
	return out
}

// Client pods are written by address: a pod seen again keeps its first
// sighting and comes back to life, a relist ends the live pods it does not
// list at their last sighting, an end is kept, and the touch refreshes
// only live addresses. Run with a non-UTC TZ as well
// (TZ=Pacific/Honolulu).
func TestWriteClientPods(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	t0 := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	a := ClientPod{UID: "a", Namespace: "shop", Name: "cart-1", Node: "n1", WorkloadKind: "Deployment", Workload: "cart",
		IPs: []string{"10.4.0.1", "fd00::1", "10.4.0.1"}, StartedAt: t0.Add(-time.Hour)}
	b := ClientPod{UID: "b", Namespace: "shop", Name: "cart-2", IPs: []string{"10.4.0.2"}}
	c := ClientPod{UID: "c", Namespace: "payments", Name: "ledger-0", IPs: []string{"10.5.0.3"}, HostNetwork: true}
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: t0, Seen: []ClientPod{a, b, c}}); err != nil {
		t.Fatal(err)
	}
	r := clientPodRows(t, s)
	if len(r) != 4 || !r["a 10.4.0.1"].first.Equal(t0.Add(-time.Hour)) || !r["b 10.4.0.2"].first.Equal(t0) || !r["c 10.5.0.3"].host ||
		r["a fd00::1"].kind != "Deployment" {
		t.Fatalf("first write: %+v", r)
	}
	// Ten minutes on: a moves to another node, b is no longer listed in
	// shop, c's namespace is not relisted.
	t1 := t0.Add(10 * time.Minute)
	a.Node = "n2"
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: t1, Seen: []ClientPod{a}, Relists: []ClientPodRelist{{Namespace: "shop", Listed: []string{"a"}}}}); err != nil {
		t.Fatal(err)
	}
	r = clientPodRows(t, s)
	if x := r["a 10.4.0.1"]; x.node != "n2" || !x.first.Equal(t0.Add(-time.Hour)) || !x.last.Equal(t1) || x.ended != nil {
		t.Fatalf("a: %+v", x)
	}
	if x := r["b 10.4.0.2"]; x.ended == nil || !x.ended.Equal(t0) {
		t.Fatalf("b not ended at its last sighting: %+v", x)
	}
	if r["c 10.5.0.3"].ended != nil {
		t.Fatal("c ended by another namespace's relist")
	}
	// c ends; the touch moves a's last sighting, not b's or c's.
	t2 := t1.Add(5 * time.Minute)
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: t2, Ended: map[string]time.Time{"c": t2.Add(-time.Minute)}, Touch: []string{""}}); err != nil {
		t.Fatal(err)
	}
	r = clientPodRows(t, s)
	if x := r["c 10.5.0.3"]; x.ended == nil || !x.ended.Equal(t2.Add(-time.Minute)) {
		t.Fatalf("c: %+v", x)
	}
	if !r["a fd00::1"].last.Equal(t2) || !r["b 10.4.0.2"].last.Equal(t0) {
		t.Fatalf("touch: %+v", r)
	}
	// b listed again lives again, from its first sighting.
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: t2, Seen: []ClientPod{b}}); err != nil {
		t.Fatal(err)
	}
	if x := clientPodRows(t, s)["b 10.4.0.2"]; x.ended != nil || !x.first.Equal(t0) {
		t.Fatalf("b again: %+v", x)
	}
	// An all-namespace relist that lists nothing ends every live pod.
	if err := s.WriteClientPods(ctx, ClientPodBatch{At: t2, Relists: []ClientPodRelist{{}}}); err != nil {
		t.Fatal(err)
	}
	for k, x := range clientPodRows(t, s) {
		if x.ended == nil {
			t.Errorf("%s live after an empty relist", k)
		}
	}
	// The sweep removes the addresses last seen before the window.
	if _, err := s.Sweep(ctx, t2.Add(14*24*time.Hour).Add(-30*time.Second), 14*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	r = clientPodRows(t, s)
	if _, ok := r["c 10.5.0.3"]; ok || len(r) != 3 {
		t.Fatalf("after the sweep: %+v", r)
	}
}
