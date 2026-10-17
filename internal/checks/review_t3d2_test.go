package checks

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// gen builds a cluster of n servers epoch by epoch. state returns "up",
// "down" or "rec" (back, JetStream not ready, no leader) per server and
// epoch; the stream "jobs" (replicas len(holders)) lives on holders.
type gen struct {
	n        int
	holders  []int
	total    int64
	state    func(i int, e int64) string
	sleader  func(e int64) int // -1: none
	mleader  func(e int64) int
	lameduck map[int64][]int // epoch of last answer -> servers announcing after it
	durable  bool
}

func (g gen) name(i int) string { return fmt.Sprintf("nats-%d", i) }

// run builds the epochs and calls after(e) when each is stored.
func (g gen) run(f *fx, after func(e int64)) {
	names := make([]string, g.n)
	for i := range names {
		names[i] = g.name(i)
	}
	pid := make([]int, g.n)
	started := make([]bool, g.n)
	prev := make([]string, g.n)
	var evid int64 = 1000
	cur := func(i int) string { return fmt.Sprintf("P%d_%d", i, pid[i]) }
	for e := int64(1); e <= g.total; e++ {
		var missing []string
		for i := range g.n {
			if g.state(i, e) == "down" {
				missing = append(missing, names[i])
			}
		}
		f.epoch(e, names, missing...)
		for _, i := range g.lameduck[e-1] {
			evid++
			f.event(evid, at(e).Add(-10*time.Second), "server.lameduck", m{"source": "sys", "server_name": names[i]})
		}
		ndown := 0
		for i := range g.n {
			if g.state(i, e) == "down" {
				ndown++
			}
		}
		for i := range g.n {
			st := g.state(i, e)
			if st == "down" {
				prev[i] = st
				continue
			}
			if prev[i] == "down" {
				pid[i]++
				started[i] = false
			}
			prev[i] = st
			id := cur(i)
			start := t0.Add(-time.Hour)
			if pid[i] > 0 {
				start = at(e).Add(-5 * time.Second)
				if !started[i] {
					// remember the restart's real start: the first epoch of this process
					f.exec(`CREATE TABLE IF NOT EXISTS t3d_start (server_id VARCHAR, t TIMESTAMP)`)
					f.exec(`INSERT INTO t3d_start VALUES (?, ?)`, id, start)
				} else {
					var s time.Time
					if err := f.st.DB().QueryRow(`SELECT t FROM t3d_start WHERE server_id = ?`, id).Scan(&s); err == nil {
						start = s
					}
				}
			}
			if !started[i] {
				started[i] = true
				f.server(id, names[i], e, e, start, "2.15.0")
				f.streamConfig(id, "jobs", e, g.total, m{"replicas": len(g.holders)})
				if g.durable {
					f.consumerConfig(id, "jobs", "worker", e, g.total, nil)
				}
			} else {
				f.exec(`UPDATE servers SET last_epoch = ?, last_seen_at = ? WHERE server_id = ?`, e, at(e), id)
			}
			f.row("epoch_endpoints", m{"epoch": e, "server_id": id, "endpoint": "JSZ", "state": "ok"})
			ml := ""
			if st == "up" && g.mleader(e) >= 0 {
				ml = names[g.mleader(e)]
			}
			f.stats(e, id, m{"start_time": start, "meta_cluster_size": g.n, "meta_leader": ml})
			held := false
			for _, h := range g.holders {
				if h == i {
					held = true
				}
			}
			if st == "rec" {
				f.row("health", m{"epoch": e, "server_id": id, "status": "unavailable", "status_code": 503, "error_count": 1,
					"errors": `[{"type":"JETSTREAM","error":"JetStream is not current with the meta leader"}]`})
				f.row("raft_stats", m{"epoch": e, "server_id": id, "group_id": "g", "catching_up": true})
			}
			if !held {
				continue
			}
			sl := g.sleader(e)
			lead := ""
			if st == "up" && sl >= 0 {
				lead = names[sl]
			}
			off := 0
			for _, h := range g.holders {
				if g.state(h, e) == "down" {
					off++
				}
			}
			f.streamStats(e, id, "jobs", m{"leader": lead, "is_leader": sl == i && st == "up",
				"replicas_reported": len(g.holders) - 1, "replicas_offline": off})
			if st == "up" {
				for _, h := range g.holders {
					if h != i {
						f.streamReplica(e, id, "jobs", names[h], m{"offline": g.state(h, e) == "down"})
					}
				}
			}
			if g.durable {
				f.consumerStats(e, id, "jobs", "worker", m{"leader": lead, "is_leader": sl == i && st == "up",
					"replicas_reported": len(g.holders) - 1, "replicas_offline": off})
			}
		}
		if after != nil {
			after(e)
		}
	}
}

// timeline records, per epoch, the findings above info of the watched codes.
func timeline(f *fx, watch ...string) (func(e int64), func() string) {
	rows := map[int64][]string{}
	var eps []int64
	return func(e int64) {
			for k := range f.evalAll(e) {
				code := strings.Fields(k)[0]
				for _, w := range watch {
					if w == code || w == "*" {
						rows[e] = append(rows[e], k)
					}
				}
			}
			sort.Strings(rows[e])
			eps = append(eps, e)
		}, func() string {
			var b strings.Builder
			for _, e := range eps {
				fmt.Fprintf(&b, "e%d: %s\n", e, strings.Join(rows[e], " | "))
			}
			return b.String()
		}
}

func leaderSticky(g *gen, srv func(e int64) []int) func(e int64) int {
	cur := -1
	return func(e int64) int {
		cands := srv(e)
		for _, c := range cands {
			if c == cur {
				return cur
			}
		}
		if len(cands) == 0 {
			cur = -1
			return -1
		}
		cur = cands[0]
		return cur
	}
}

// Q1: GKE drains two of three nodes 30 s apart; the third stays up. The
// outage lasts under a minute for the drained pods but they need minutes
// to come back.
func TestT3D_NodeUpgradeTwoDrained(t *testing.T) {
	g := gen{n: 3, holders: []int{0, 1, 2}, total: 40, durable: true}
	// nats-0 down e10-11, nats-1 down e11-12 (30 s later), both rec 4 epochs.
	g.state = func(i int, e int64) string {
		switch i {
		case 0:
			if e >= 10 && e <= 11 {
				return "down"
			}
			if e >= 12 && e <= 15 {
				return "rec"
			}
		case 1:
			if e >= 11 && e <= 12 {
				return "down"
			}
			if e >= 13 && e <= 16 {
				return "rec"
			}
		}
		return "up"
	}
	// leader nats-0 until it goes down, then none while quorum is missing, back at e17.
	g.sleader = func(e int64) int {
		switch {
		case e < 10:
			return 0
		case e <= 16:
			return -1
		}
		return 2
	}
	g.mleader = g.sleader
	g.lameduck = map[int64][]int{9: {0}, 10: {1}}
	f := newFx(t)
	after, dump := timeline(f, "stream-001", "consumer-001", "system-003", "server-001", "server-003", "server-004", "stream-009", "*")
	g.run(f, after)
	t.Log("\n" + dump())
	// Two of three replicas away: the quorum is lost while they recover,
	// and back once the leader is elected again.
	for e := int64(12); e <= 16; e++ {
		got := f.evalAll(e)
		for _, k := range []string{"stream-001 APP/jobs critical", "consumer-001 APP/jobs/worker critical"} {
			if _, ok := got[k]; !ok {
				t.Errorf("epoch %d: no %s", e, k)
			}
		}
	}
	for k := range f.evalAll(17) {
		if strings.HasPrefix(k, "stream-001") || strings.HasPrefix(k, "consumer-001") {
			t.Errorf("epoch 17, leader back: %s", k)
		}
	}
}
