package checks

import (
	"context"
	"strings"
	"testing"
)

func metaGroupScopes(t *testing.T, f *fx) map[string]string {
	t.Helper()
	conn, err := f.st.DB().Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := prepareMetaGroups(context.Background(), conn, 1, 8); err != nil {
		t.Fatal(err)
	}
	rows, err := conn.QueryContext(context.Background(), `SELECT server_id, group_key FROM meta_check_groups`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	scopes := map[string]string{}
	for rows.Next() {
		var id, key string
		if err := rows.Scan(&id, &key); err != nil {
			t.Fatal(err)
		}
		scopes[id] = key
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return scopes
}

func TestMetaGroupsUnobservedPeerAloneIsNotProof(t *testing.T) {
	f := metaGroupsFixture(t, 8, func(g string, e int64) string { return "" })
	// Even a name seen on only one peer entry has no observed process identity.
	f.exec(`UPDATE raft_stats SET peers = json_object(server_id || '-hash', json_object('name', server_id || '-offline'))`)
	for id, key := range metaGroupScopes(t, f) {
		if key != "" {
			t.Errorf("%s certified by an unobserved name: %s", id, key)
		}
	}
	want(t, f.eval("system-002", 8)[""], "critical", "identity unknown")
}

func TestMetaGroupsSharedDownPeerWithinKnownGroup(t *testing.T) {
	f := metaGroupsFixture(t, 8, func(g string, e int64) string { return g + "-0" })
	f.exec(`DELETE FROM server_stats WHERE server_id = 'orders-2'`)
	f.exec(`DELETE FROM raft_stats WHERE server_id = 'orders-2'`)
	// RAFTZ excludes the reporter: two responding members still identify one
	// another, while their common third member is down.
	f.exec(`UPDATE raft_stats SET peers = '{"down":{"name":"offline"},"one":{"name":"orders-1"}}' WHERE server_id = 'orders-0'`)
	f.exec(`UPDATE raft_stats SET peers = '{"down":{"name":"offline"},"zero":{"name":"orders-0"}}' WHERE server_id = 'orders-1'`)
	scopes := metaGroupScopes(t, f)
	if scopes["orders-0"] == "" || scopes["orders-0"] != scopes["orders-1"] || scopes["orders-0"] == scopes["shipping-0"] {
		t.Fatalf("known group lost: %v", scopes)
	}
	if r := f.eval("system-002", 8); len(r) != 0 {
		t.Fatal(rowsText(r))
	}
}

func TestMetaGroupsOnlySharedDownPeerRemainsUnknown(t *testing.T) {
	f := metaGroupsFixture(t, 8, func(g string, e int64) string {
		if g == "orders" {
			return ""
		}
		return "shipping-0"
	})
	f.exec(`UPDATE raft_stats SET peers = '{"down":{"name":"offline"}}' WHERE server_id LIKE 'orders-%'`)
	scopes := metaGroupScopes(t, f)
	if scopes["orders-0"] != "" || scopes["shipping-0"] == "" {
		t.Fatalf("scope uncertainty leaked: %v", scopes)
	}
	r := f.eval("system-002", 8)
	want(t, r[""], "critical", "identity unknown", "orders-0")
	if strings.Contains(r[""].Detail, "Meta group [orders") {
		t.Fatal(rowsText(r))
	}
}

func TestMetaGroupsTransitiveUnresolvedBridges(t *testing.T) {
	f := metaGroupsFixture(t, 8, func(g string, e int64) string { return "" })
	// Two unresolved hashes link orders -> shipping -> an otherwise isolated
	// reporter. Neither edge can certify physical membership, even transitively.
	f.exec(`UPDATE raft_stats SET peers = json_merge_patch(peers, '{"bridge-a":{"name":"offline-a"}}')`)
	f.exec(`UPDATE raft_stats SET peers = json_merge_patch(peers, '{"bridge-b":{"name":"offline-b"}}') WHERE server_id = 'shipping-0'`)
	f.exec(`UPDATE raft_stats SET peers = '{"bridge-b":{"name":"offline-b"}}' WHERE server_id = 'shipping-4'`)
	// Remove shipping-4 from observed peer resolution (the fixture lists 0..2).
	for id, key := range metaGroupScopes(t, f) {
		if key != "" {
			t.Errorf("transitive bridge certified %s: %s", id, key)
		}
	}
}

func TestMetaGroupsConflictingHashDoesNotMergeKnownGroups(t *testing.T) {
	f := metaGroupsFixture(t, 8, func(g string, e int64) string { return g + "-0" })
	f.exec(`UPDATE raft_stats SET peers = json_merge_patch(peers, json_object('collision', json_object('name', split_part(server_id,'-',1) || '-0')))`)
	scopes := metaGroupScopes(t, f)
	if scopes["orders-0"] == "" || scopes["shipping-0"] == "" || scopes["orders-0"] == scopes["shipping-0"] {
		t.Fatalf("conflicting hash merged groups: %v", scopes)
	}
}
