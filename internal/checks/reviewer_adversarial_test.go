package checks

import (
 "fmt"
 "strings"
 "testing"
)

func TestReviewerSameDomainAndMissingMembership(t *testing.T) {
 f := metaGroupsFixture(t, 12, func(g string, e int64) string { if g == "orders" { return fmt.Sprintf("orders-%d",e%2) }; return g+"-0" })
 f.exec(`UPDATE servers SET domain = 'same', cluster = 'same'`)
 want(t, f.eval("system-003",12)[""], "warning", "changed 11 times", "Meta group [orders-0")
 f.exec(`DELETE FROM raft_stats`)
 r:=f.eval("system-003",12,map[string]string{"max_changes":"0"})
 if len(r)>0 && !strings.Contains(r[""].Detail,"identity unknown") { t.Fatal(rowsText(r)) }
}

func TestReviewerMembershipArrivesAndChurns(t *testing.T) {
 f := metaGroupsFixture(t,12,func(g string,e int64) string { if g=="orders" { return fmt.Sprintf("orders-%d",e%2) }; return g+"-0" })
 f.exec(`DELETE FROM raft_stats WHERE epoch < 5`)
 f.exec(`UPDATE raft_stats SET peers = '{"new-peer":{"name":"new-peer"},"orders-peer-1":{"name":"orders-1"}}' WHERE server_id LIKE 'orders-%' AND epoch >= 8`)
 want(t,f.eval("system-003",12)[""],"warning","changed 11 times","Meta group [orders-0, orders-1, orders-2]")
 r:=f.eval("system-003",4,map[string]string{"max_changes":"0"})
 if len(r)>0 && !strings.Contains(r[""].Detail,"identity unknown") { t.Fatal(rowsText(r)) }
}

// Distinct physical groups can reuse a down server's name: RAFT IDs hash
// names, and the down processes have no server_stats rows to reveal the duplicate.
func TestReviewerSharedDownPeerDoesNotCertifyUnion(t *testing.T) {
 f := metaGroupsFixture(t,8,func(g string,e int64) string { if g=="orders" { return "" }; return "shipping-0" })
 // Each three-member group has two unique observed members and one down
 // member named offline-common. RAFTZ excludes its own reporter.
 f.exec(`DELETE FROM server_stats WHERE server_id NOT IN ('orders-0','orders-1','shipping-0','shipping-1')`)
 f.exec(`DELETE FROM raft_stats WHERE server_id NOT IN ('orders-0','orders-1','shipping-0','shipping-1')`)
 f.exec(`UPDATE server_stats SET meta_cluster_size=3, meta_quorum_needed=2, meta_peers_offline=1`)
 f.exec(`UPDATE raft_stats SET peers = ?::JSON WHERE server_id = ?`, `{"syRCe8oa":{"name":"offline-common"},"ooMa6tJN":{"name":"orders-1"}}`, "orders-0")
 f.exec(`UPDATE raft_stats SET peers = ?::JSON WHERE server_id = ?`, `{"syRCe8oa":{"name":"offline-common"},"9Ojv00T0":{"name":"orders-0"}}`, "orders-1")
 f.exec(`UPDATE raft_stats SET peers = ?::JSON WHERE server_id = ?`, `{"syRCe8oa":{"name":"offline-common"},"m5gZHCMp":{"name":"shipping-1"}}`, "shipping-0")
 f.exec(`UPDATE raft_stats SET peers = ?::JSON WHERE server_id = ?`, `{"syRCe8oa":{"name":"offline-common"},"L6pesJkC":{"name":"shipping-0"}}`, "shipping-1")
 r:=f.eval("system-002",8)
 t.Logf("shared down peer result: %s",rowsText(r))
 if len(r)==0 { t.Errorf("healthy shipping masks explicitly leaderless orders via an unobserved, name-hashed peer") }
 f.exec(`UPDATE server_stats SET meta_leader = '' WHERE server_id LIKE 'shipping-%'`)
 r=f.eval("system-002",8)
 if strings.Contains(r[""].Detail,"Meta group [orders-0, orders-1, shipping-0, shipping-1]") { t.Errorf("unobserved duplicate name certifies a physical union: %s",rowsText(r)) }
 f.exec(`UPDATE server_stats SET meta_leader = split_part(server_id,'-',1) || '-0'`)
 for i:=int64(1);i<=6;i++ { g:="orders"; if i%2==0 { g="shipping" }; f.event(i,at(i+1),"meta.leader_elected",m{"server_name":g+"-0","source":"sys"}) }
 r=f.eval("system-003",8)
 if len(r)>0 && !strings.Contains(r[""].Detail,"identity unknown") { t.Errorf("independent advisories became a certified group count: %s",rowsText(r)) }

}
