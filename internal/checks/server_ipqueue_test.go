package checks

import (
	"testing"
)

// ipq stores IPQUEUESZ ok for servers S0..S2 in epochs 1 to n, and the
// queue of server id with pending items per epoch (0 leaves it out, as
// IPQUEUESZ lists only queues that are not empty).
func (f *fx) ipq(n int64, id, queue string, pending func(e int64) int64) {
	for e := int64(1); e <= n; e++ {
		if p := pending(e); p > 0 {
			f.row("ipqueues", m{"epoch": e, "server_id": id, "queue": queue, "pending": p, "in_progress": 1})
		}
	}
}

func (f *fx) ipqOK(n int64) {
	for e := int64(1); e <= n; e++ {
		for _, id := range []string{"S0", "S1", "S2"} {
			f.row("epoch_endpoints", m{"epoch": e, "server_id": id, "endpoint": "IPQUEUESZ", "state": "ok"})
		}
	}
}

// A queue at or above 10000 items in three collections in a row fires; one
// that emptied in between, or stays just below, does not.
func TestIpqueueBacklog(t *testing.T) {
	f := newFx(t)
	f.cluster(5)
	f.ipqOK(5)
	f.ipq(5, "S0", "JS API Queue", func(e int64) int64 { return 10000 * (e - 2) })
	f.ipq(5, "S1", "[ACC:APP] stream 'jobs' send", func(e int64) int64 {
		if e == 4 {
			return 0
		}
		return 50000
	})
	f.ipq(5, "S2", "Routed JS API Requests", func(int64) int64 { return 9999 })
	f.fires("server-022", 4)
	r := f.fires("server-022", 5, "nats-0")
	want(t, r["nats-0"], "warning", `nats-0 is behind on internal queues: "JS API Queue" holds 30000 items, up from 10000.`)
	if v := r["nats-0"].Value; v == nil || *v != 30000 {
		t.Errorf("value %v", v)
	}
}

// A collection whose IPQUEUESZ did not answer is not read as empty: it
// breaks the run, and does not count.
func TestIpqueueBacklogNotAnswered(t *testing.T) {
	f := newFx(t)
	f.cluster(4)
	f.ipqOK(4)
	f.ipq(4, "S0", "JS API Queue", func(int64) int64 { return 20000 })
	f.exec(`DELETE FROM ipqueues WHERE epoch = 3`)
	f.exec(`UPDATE epoch_endpoints SET state = 'missing' WHERE epoch = 3 AND endpoint = 'IPQUEUESZ'`)
	f.fires("server-022", 4)
	f.exec(`DELETE FROM epoch_endpoints WHERE epoch = 4 AND server_id = 'S0'`)
	f.fires("server-022", 4)
}

// A firing finding stays while a queue holds half of min_pending and
// resolves below.
func TestIpqueueBacklogResolve(t *testing.T) {
	f := newFx(t)
	f.cluster(4)
	f.ipqOK(4)
	f.ipq(4, "S0", "JS API Queue", func(e int64) int64 { return map[int64]int64{3: 5000, 4: 4999}[e] })
	f.fires("server-022", 3)
	f.row("findings", m{"id": "server-022:nats-0", "check_code": "server-022", "entity_type": "server", "entity_key": "nats-0",
		"severity": "warning", "state": "firing", "last_transition": "firing", "since": at(1), "since_epoch": 1,
		"transitioned_at": at(1), "last_seen_epoch": 2, "updated_epoch": 2, "detail": "x"})
	r := f.fires("server-022", 3, "nats-0")
	want(t, r["nats-0"], "warning", `"JS API Queue" holds 5000 items.`)
	f.exec(`UPDATE findings SET last_seen_epoch = 3`)
	f.fires("server-022", 4)
}

// A process that restarted fills its queues while it catches up: it is not
// judged while restarting, nor while its raft groups catch up.
func TestIpqueueBacklogAfterRestart(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 8, three...)
	f.server("S0", "nats-0", 1, 3, at(1), "2.15.0")
	f.server("S9", "nats-0", 4, 8, at(4), "2.15.0")
	f.server("S1", "nats-1", 1, 8, at(1), "2.15.0")
	f.server("S2", "nats-2", 1, 8, at(1), "2.15.0")
	for e := int64(1); e <= 8; e++ {
		for _, id := range []string{"S0", "S9", "S1", "S2"} {
			if (id == "S0" && e > 3) || (id == "S9" && e < 4) {
				continue
			}
			f.row("epoch_endpoints", m{"epoch": e, "server_id": id, "endpoint": "IPQUEUESZ", "state": "ok"})
			f.stats(e, id, m{})
		}
		if e >= 4 && e <= 6 {
			f.row("ipqueues", m{"epoch": e, "server_id": "S9", "queue": "RAFT apply", "pending": 30000, "in_progress": 0})
		}
		// nats-1 did not restart, but its raft groups catch up.
		f.row("ipqueues", m{"epoch": e, "server_id": "S1", "queue": "RAFT apply", "pending": 30000, "in_progress": 0})
		f.row("raft_stats", m{"epoch": e, "server_id": "S1", "group_id": "G1", "catching_up": true})
	}
	f.fires("server-022", 6)
}
