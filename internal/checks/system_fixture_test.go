package checks

import "fmt"

// firingSystem stores code's system finding as firing, last seen in epoch.
func (f *fx) firingSystem(code string, epoch int64) {
	f.t.Helper()
	f.row("findings", m{"id": code + ":", "check_code": code, "entity_type": "system", "entity_key": "",
		"severity": "warning", "state": "firing", "last_transition": "firing", "since": at(1), "since_epoch": 1,
		"transitioned_at": at(1), "last_seen_epoch": epoch, "updated_epoch": epoch, "detail": "x"})
}

// metaCluster stores epochs 1 to n of the three servers of cluster, each
// reporting a meta group of 3 led by nats-0 with nothing pending; over
// adds to or overrides the columns of server i in epoch e.
func (f *fx) metaCluster(n int64, over func(e int64, i int) m) {
	f.t.Helper()
	f.cluster(n)
	for e := int64(1); e <= n; e++ {
		for i := range 3 {
			cols := m{"meta_cluster_size": 3, "meta_quorum_needed": 2, "meta_leader": "nats-0", "meta_pending": 0,
				"meta_peers_offline": 0, "meta_peers_not_current": 0, "js_api_level": 1}
			if over != nil {
				for k, v := range over(e, i) {
					cols[k] = v
				}
			}
			f.stats(e, fmt.Sprintf("S%d", i), cols)
		}
	}
}
