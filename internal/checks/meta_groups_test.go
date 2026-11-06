package checks

import (
	"fmt"
	"strings"
	"testing"
)

// Both domains deliberately have the same cluster label. Only disjoint RAFT
// membership establishes separate physical meta groups.
func metaGroupsFixture(t *testing.T, n int64, leader func(string, int64) string) *fx {
	t.Helper()
	f := newFx(t)
	f.epochs(1, n)
	for _, group := range []string{"orders", "shipping"} {
		count := 3
		if group == "shipping" {
			count = 5
		}
		for i := 0; i < count; i++ {
			id := fmt.Sprintf("%s-%d", group, i)
			f.server(id, id, 1, n, t0, "2.15.0")
			f.exec(`UPDATE servers SET cluster = 'shop', domain = ? WHERE server_id = ?`, group, id)
			peers := fmt.Sprintf(`{"%s-peer-0":{"name":"%s-0"},"%s-peer-1":{"name":"%s-1"},"%s-peer-2":{"name":"%s-2"}}`, group, group, group, group, group, group)
			for e := int64(1); e <= n; e++ {
				f.stats(e, id, m{"meta_cluster": "shop", "meta_cluster_size": count, "meta_quorum_needed": count/2 + 1, "meta_leader": leader(group, e), "meta_peers_offline": 0})
				f.row("raft_stats", m{"epoch": e, "server_id": id, "group_id": "_meta_", "peers": peers})
			}
		}
	}
	return f
}

func TestMetaGroupsLeaderlessNotMasked(t *testing.T) {
	f := metaGroupsFixture(t, 8, func(g string, e int64) string {
		if g == "orders" && e >= 3 {
			return ""
		}
		return g + "-0"
	})
	for _, e := range []int64{3, 8} {
		r := f.eval("system-002", e)
		if len(r) != 1 || !strings.Contains(r[""].Detail, "orders-0") {
			t.Errorf("epoch %d: leaderless orders group masked: %s", e, rowsText(r))
		}
	}
	if r := f.eval("system-002", 2); len(r) != 0 {
		t.Fatalf("healthy independent groups: %s", rowsText(r))
	}
}

func TestMetaGroupsMinorityFlappingNotMasked(t *testing.T) {
	f := metaGroupsFixture(t, 12, func(g string, e int64) string {
		if g == "orders" {
			return fmt.Sprintf("orders-%d", e%2)
		}
		return g + "-0"
	})
	r := f.eval("system-003", 12)
	if len(r) != 1 || !strings.Contains(r[""].Detail, "changed 11 times") || !strings.Contains(r[""].Detail, "orders-1 to orders-0") {
		t.Fatalf("minority group flapping masked: %s", rowsText(r))
	}
	if r := f.eval("system-003", 4); len(r) != 0 {
		t.Fatalf("below threshold: %s", rowsText(r))
	}
}

func TestMetaGroupsIndependentAdvisoriesNotCombined(t *testing.T) {
	f := metaGroupsFixture(t, 12, func(g string, e int64) string { return g + "-0" })
	for i := int64(1); i <= 6; i++ {
		g := "orders"
		if i%2 == 0 {
			g = "shipping"
		}
		f.event(i, at(i+1), "meta.leader_elected", m{"server_name": g + "-0", "source": "sys"})
	}
	if r := f.eval("system-003", 12); len(r) != 0 {
		t.Fatalf("three advisories per group combined: %s", rowsText(r))
	}
}
