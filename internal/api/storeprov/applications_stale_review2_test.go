package storeprov_test

import (
	"testing"
	"time"
)

// Review case: a client pod row whose namespace watch failed two days ago
// stays live (ended_at NULL, last_seen two days old). A connection now
// from that address must not be matched to it.
func TestReview2StaleLiveClientPodRowMatches(t *testing.T) {
	p := appsProvider(t, appsLast)
	seen := appsLast.Add(-48 * time.Hour).UnixMicro()
	if _, err := p.St.DB().ExecContext(ctx, `INSERT INTO client_pods (uid, ip, namespace, pod, node, workload_kind, workload, app_name,
  host_network, first_seen, last_seen, ended_at) VALUES ('uid-gone', '10.1.0.7', 'billing', 'gone-pod', 'node-9', 'Deployment', 'gone',
  '', false, make_timestamp(?::BIGINT), make_timestamp(?::BIGINT), NULL)`, appsLast.Add(-72*time.Hour).UnixMicro(), seen); err != nil {
		t.Fatal(err)
	}
	res := appsPage(t, appsClient(t, p), appsLast.Add(time.Minute), "", false)
	cd := appNamed(res, "callback-dispatcher").GetRunsOn()
	if cd.GetMatched() != 1 {
		t.Errorf("callback-dispatcher matched %d connections, want 1: 10.1.0.7 matched to a pod last seen 48 hours ago (%v)", cd.GetMatched(), cd)
	}
}
