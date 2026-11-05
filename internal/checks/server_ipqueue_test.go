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

// A queue at or above 1000 items in three collections in a row fires; one
// that emptied in between, or stays just below, does not.
func TestIpqueueBacklog(t *testing.T) {
	f := newFx(t)
	f.cluster(5)
	f.ipqOK(5)
	f.ipq(5, "S0", "JS API Queue", func(e int64) int64 { return 1000 * (e - 2) })
	f.ipq(5, "S1", "[ACC:APP] stream 'jobs' send", func(e int64) int64 {
		if e == 4 {
			return 0
		}
		return 5000
	})
	f.ipq(5, "S2", "Routed JS API Requests", func(int64) int64 { return 999 })
	f.fires("server-022", 4)
	r := f.fires("server-022", 5, "nats-0")
	want(t, r["nats-0"], "warning", `nats-0 is behind on internal queues: "JS API Queue" holds 3000 items, up from 1000.`)
	if v := r["nats-0"].Value; v == nil || *v != 3000 {
		t.Errorf("value %v", v)
	}
}

// A collection whose IPQUEUESZ did not answer is not read as empty: it
// breaks the run, and does not count.
func TestIpqueueBacklogNotAnswered(t *testing.T) {
	f := newFx(t)
	f.cluster(4)
	f.ipqOK(4)
	f.ipq(4, "S0", "JS API Queue", func(int64) int64 { return 2000 })
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
	f.ipq(4, "S0", "JS API Queue", func(e int64) int64 { return map[int64]int64{3: 500, 4: 499}[e] })
	f.fires("server-022", 3)
	f.row("findings", m{"id": "server-022:nats-0", "check_code": "server-022", "entity_type": "server", "entity_key": "nats-0",
		"severity": "warning", "state": "firing", "last_transition": "firing", "since": at(1), "since_epoch": 1,
		"transitioned_at": at(1), "last_seen_epoch": 2, "updated_epoch": 2, "detail": "x"})
	r := f.fires("server-022", 3, "nats-0")
	want(t, r["nats-0"], "warning", `"JS API Queue" holds 500 items.`)
	f.exec(`UPDATE findings SET last_seen_epoch = 3`)
	f.fires("server-022", 4)
}
