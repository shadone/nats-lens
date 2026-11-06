package checks

import (
	"context"
	"fmt"
	"github.com/shadone/nats-lens/internal/config"
	"strings"
	"testing"
	"time"
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

func TestMetaGroupsLabelsDoNotSplitMembership(t *testing.T) {
	f := metaGroupsFixture(t, 8, func(g string, e int64) string { return "orders-0" })
	// Relabel every server differently, then make their RAFT membership overlap.
	// Neither labels nor the leader's cluster name establish another group.
	f.exec(`UPDATE servers SET domain = server_id, cluster = server_id`)
	f.exec(`UPDATE raft_stats SET peers = '{"shared":{"name":"orders-0"}}'`)
	if r := f.eval("system-003", 8, map[string]string{"max_changes": "0"}); len(r) != 0 {
		t.Fatalf("one physical group split by labels: %s", rowsText(r))
	}
	if r := f.eval("system-002", 8); len(r) != 0 {
		t.Fatalf("one healthy physical group: %s", rowsText(r))
	}
}

func TestMetaGroupsQuorumEvidenceAndUnknown(t *testing.T) {
	f := metaGroupsFixture(t, 2, func(g string, e int64) string { return g + "-0" })
	f.exec(`UPDATE server_stats SET meta_peers_offline = meta_cluster_size WHERE server_id IN ('orders-0','shipping-0') AND epoch = 1`)
	r := f.eval("system-002", 1)
	want(t, r[""], "critical", "meta leader orders-0 sees 3", "meta leader shipping-0 sees 5")
	f.exec(`UPDATE server_stats SET meta_peers_offline = NULL, meta_leader = NULL WHERE epoch = 2`)
	if r := f.eval("system-002", 2); len(r) != 0 {
		t.Fatalf("missing facts treated as leaderless: %s", rowsText(r))
	}
}

func TestMetaGroupsHistoryAndValue(t *testing.T) {
	f := metaGroupsFixture(t, 12, func(g string, e int64) string { return fmt.Sprintf("%s-%d", g, e%2) })
	r := f.eval("system-003", 12)
	want(t, r[""], "warning", "Meta group [orders-0", "Meta group [shipping-0")
	if v := r[""].Value; v == nil || *v != 11 {
		t.Fatalf("value must be largest group count, not sum: %v", v)
	}
	if r := f.eval("system-003", 4); len(r) != 0 {
		t.Fatalf("history borrowed future changes: %s", rowsText(r))
	}
	// Facts first obtained after the historical evaluation must not scope it.
	f.exec(`DELETE FROM raft_stats WHERE epoch < 5`)
	r = f.eval("system-003", 4, map[string]string{"max_changes": "0"})
	if len(r) > 0 && !strings.Contains(r[""].Detail, "identity unknown") {
		t.Fatalf("future group identity leaked: %s", rowsText(r))
	}
}

func TestMetaGroupsJSZAttachmentIsUnambiguous(t *testing.T) {
	f := metaGroupsFixture(t, 8, func(g string, e int64) string {
		if g == "orders" {
			return fmt.Sprintf("orders-%d", e%2)
		}
		return g + "-0"
	})
	// Orders-2 has only JSZ, and is absent from the other reporters' peer lists.
	f.exec(`DELETE FROM raft_stats WHERE server_id = 'orders-2'`)
	f.exec(`UPDATE raft_stats SET peers = '{"orders-peer-0":{"name":"orders-0"},"orders-peer-1":{"name":"orders-1"}}' WHERE server_id LIKE 'orders-%'`)
	r := f.eval("system-003", 8)
	want(t, r[""], "warning", "Meta group [orders-0, orders-1, orders-2]", "changed 7 times")
}

func TestMetaGroupsUnscopedAdvisoriesDoNotInventAGroup(t *testing.T) {
	for _, flap := range []bool{false, true} {
		f := metaGroupsFixture(t, 8, func(g string, e int64) string {
			if flap && g == "orders" {
				return fmt.Sprintf("orders-%d", e%2)
			}
			return g + "-0"
		})
		for i := int64(1); i <= 6; i++ {
			f.event(i, at(i+1), "meta.leader_elected", m{"server_name": fmt.Sprintf("unresolved-%d", i), "source": "sys"})
		}
		r := f.eval("system-003", 8)
		if !flap && len(r) != 0 {
			t.Fatalf("unscoped advisories combined: %s", rowsText(r))
		}
		if flap {
			want(t, r[""], "warning", "changed 7 times", "unresolved group identity were not counted")
		}
	}
}

func TestMetaGroupsAmbiguousPeerNamesStayUnscoped(t *testing.T) {
	f := metaGroupsFixture(t, 1, func(g string, e int64) string { return "" })
	f.exec(`UPDATE servers SET server_name = 'same-name'`)
	f.exec(`UPDATE raft_stats SET peers = '{"same-hash":{"name":"same-name"}}'`)
	r := f.eval("system-002", 1)
	want(t, r[""], "critical", "meta group identity unknown")
}

func TestMetaGroupsSuccessfulRunState(t *testing.T) {
	f := metaGroupsFixture(t, 3, func(g string, e int64) string {
		if g == "orders" && e == 1 {
			return ""
		}
		return g + "-0"
	})
	set, err := Load(config.Checks{Timeout: config.Duration(10 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	engine := &Engine{Set: set, only: map[string]bool{"system-002": true, "system-003": true}}
	if _, err := engine.Run(context.Background(), f.st, 1); err != nil {
		t.Fatal(err)
	}
	// A malformed stored membership is a failed run, not healthy zero data.
	f.exec(`UPDATE raft_stats SET peers = '[]' WHERE epoch = 2`)
	if _, err := engine.Run(context.Background(), f.st, 2); err != nil {
		t.Fatal(err)
	}
	var state string
	var seen int64
	if err := f.st.DB().QueryRow(`SELECT state, last_seen_epoch FROM findings WHERE id = 'system-002:'`).Scan(&state, &seen); err != nil {
		t.Fatal(err)
	}
	if state != "firing" || seen != 1 {
		t.Fatalf("failed run erased last successful state: %s %d", state, seen)
	}
	if _, err := engine.Run(context.Background(), f.st, 3); err != nil {
		t.Fatal(err)
	}
	if err := f.st.DB().QueryRow(`SELECT state FROM findings WHERE id = 'system-002:'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "resolved" {
		t.Fatalf("healthy group did not resolve stable public identity: %s", state)
	}
}

func TestMetaGroupsBoundedWindow(t *testing.T) {
	f := metaGroupsFixture(t, 31, func(g string, e int64) string { return fmt.Sprintf("%s-%d", g, e%2) })
	// 80 servers in 20 independent groups, 31 snapshots, plus 24,800 old
	// observations outside the window. Only the 2,480 window rows may scope it.
	f.exec(`INSERT INTO servers SELECT servers.* REPLACE ('copy' || i || '-' || server_id AS server_id,
  'copy' || i || '-' || server_name AS server_name) FROM servers CROSS JOIN range(1,10) copies(i)`)
	f.exec(`INSERT INTO server_stats SELECT server_stats.* REPLACE ('copy' || i || '-' || server_id AS server_id,
  'copy' || i || '-' || meta_leader AS meta_leader) FROM server_stats CROSS JOIN range(1,10) copies(i)`)
	f.exec(`INSERT INTO raft_stats SELECT raft_stats.* REPLACE ('copy' || i || '-' || server_id AS server_id,
  replace(replace(peers::VARCHAR, 'orders', 'copy' || i || '-orders'), 'shipping', 'copy' || i || '-shipping')::JSON AS peers)
  FROM raft_stats CROSS JOIN range(1,10) copies(i)`)
	f.exec(`INSERT INTO server_stats SELECT server_stats.* REPLACE (epoch - i * 1000 AS epoch)
  FROM server_stats CROSS JOIN range(1,11) history(i)`)
	start := time.Now()
	r := f.eval("system-003", 31)
	t.Logf("80 servers, 20 groups, 2480 window observations + 24800 older observations: %s", time.Since(start))
	if len(r) != 1 || r[""].Value == nil || *r[""].Value != 30 {
		t.Fatalf("bounded group counts: %s", rowsText(r))
	}
}
