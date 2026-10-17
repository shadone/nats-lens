package checks

import (
	"context"
	"fmt"
	"github.com/shadone/nats-lens/internal/config"
	"strings"
	"testing"
	"time"
)

// evalAll runs every check at epoch and returns "code key severity: detail"
// for each finding above info.
func (f *fx) evalAll(epoch int64) map[string]string {
	f.t.Helper()
	cfg := loadCfg()
	set, err := Load(cfg)
	if err != nil {
		f.t.Fatal(err)
	}
	e := &Engine{Set: set}
	runs, err := e.Evaluate(context.Background(), f.st.DB(), epoch)
	if err != nil {
		f.t.Fatal(err)
	}
	out := map[string]string{}
	for _, r := range runs {
		if r.Status != "ok" && r.Status != "disabled" && r.Status != "" {
			f.t.Logf("check %s status %s %s", r.Code, r.Status, r.Error)
		}
		for _, row := range r.Rows {
			out[r.Code+" "+row.EntityKey+" "+row.Severity] = row.Detail
		}
	}
	return out
}

// roll builds a rolling restart of nats-0..2, one after another.
// down: epochs a server is missing; rec: epochs after it is back with
// JetStream not ready, no stream leader and catching up; gap: epochs between
// a server being fully up and the next one's lame duck.
type roll struct {
	down, rec, gap int64
	startAt        int64
	total          int64
}

func (r roll) downAt(i int, e int64) bool {
	a := r.startAt + int64(i)*(r.down+r.rec+r.gap)
	return e >= a && e < a+r.down
}
func (r roll) recAt(i int, e int64) bool {
	a := r.startAt + int64(i)*(r.down+r.rec+r.gap) + r.down
	return e >= a && e < a+r.rec
}
func (r roll) backEpoch(i int) int64 { return r.startAt + int64(i)*(r.down+r.rec+r.gap) + r.down }
func (r roll) pid(i int, e int64) string {
	if e >= r.backEpoch(i) {
		return fmt.Sprintf("N%d", i)
	}
	return fmt.Sprintf("S%d", i)
}

func (f *fx) buildRoll(r roll, skipEpoch func(int64) bool, after func(e int64)) {
	names := three
	for i := range 3 {
		f.server(fmt.Sprintf("S%d", i), names[i], 1, 1, t0.Add(-time.Hour), "2.15.0")
		for _, id := range []string{fmt.Sprintf("S%d", i), fmt.Sprintf("N%d", i)} {
			f.streamConfig(id, "jobs", 1, r.total, nil)
			f.consumerConfig(id, "jobs", "worker", 1, r.total, nil)
		}
		f.event(int64(100+i), at(r.backEpoch(i)-r.down).Add(-20*time.Second), "server.lameduck", m{"source": "sys", "server_name": names[i]})
	}
	slead, mlead := 0, 0
	made := map[string]bool{}
	var ev int64 = 200
	for e := int64(1); e <= r.total; e++ {
		if skipEpoch != nil && skipEpoch(e) {
			continue
		}
		var missing []string
		for i := range 3 {
			if r.downAt(i, e) {
				missing = append(missing, names[i])
			}
		}
		f.epoch(e, three, missing...)
		for i := range 3 {
			if r.downAt(i, e) {
				continue
			}
			id := r.pid(i, e)
			if id[0] == 'N' && !made[id] {
				made[id] = true
				f.server(id, names[i], e, e, at(r.backEpoch(i)).Add(-5*time.Second), "2.15.0")
			} else {
				f.exec(`UPDATE servers SET last_epoch = ?, last_seen_at = ? WHERE server_id = ?`, e, at(e), id)
			}
		}
		ok := func(i int) bool { return !r.downAt(i, e) && !r.recAt(i, e) }
		if !ok(slead) {
			for i := range 3 {
				if ok(i) {
					slead = i
					break
				}
			}
		}
		if !ok(mlead) {
			old := mlead
			for i := range 3 {
				if ok(i) {
					mlead = i
					break
				}
			}
			ev++
			f.event(ev, at(e).Add(-10*time.Second), "meta.leader_elected", m{"source": "sys", "server_name": names[mlead], "summary": "from " + names[old]})
		}
		for i := range 3 {
			if r.downAt(i, e) {
				continue
			}
			id := r.pid(i, e)
			f.row("epoch_endpoints", m{"epoch": e, "server_id": id, "endpoint": "JSZ", "state": "ok"})
			cols := m{"meta_cluster_size": 3, "meta_leader": names[mlead]}
			if id[0] == 'N' {
				cols["start_time"] = at(r.backEpoch(i)).Add(-5 * time.Second)
			}
			if r.recAt(i, e) {
				cols["meta_leader"] = ""
			}
			f.stats(e, id, cols)
			if r.recAt(i, e) {
				f.row("health", m{"epoch": e, "server_id": id, "status": "unavailable", "status_code": 503, "error_count": 1,
					"errors": `[{"type":"JETSTREAM","error":"JetStream is not current with the meta leader"}]`})
				f.row("raft_stats", m{"epoch": e, "server_id": id, "group_id": "g", "catching_up": true})
				f.streamStats(e, id, "jobs", m{"leader": ""})
				f.consumerStats(e, id, "jobs", "worker", m{"leader": ""})
				continue
			}
			ndown := int64(len(missing))
			f.streamStats(e, id, "jobs", m{"leader": names[slead], "is_leader": i == slead,
				"replicas_reported": 2, "replicas_offline": ndown})
			for j := range 3 {
				if j != i {
					f.streamReplica(e, id, "jobs", names[j], m{"offline": r.downAt(j, e), "current": !r.downAt(j, e) && !r.recAt(j, e)})
				}
			}
			f.consumerStats(e, id, "jobs", "worker", m{"is_leader": i == slead, "leader": names[slead], "replicas_reported": 2, "replicas_offline": ndown})
		}
		if after != nil {
			after(e)
		}
	}
}

// collect records every finding above info, except replicas behind at
// warning (a replica offline because its server restarts is info), and server-003 at warning when slow is set (recovery
// longer than server-003's min_duration of 3 minutes).
func collect(f *fx, from int64, bad *[]string, seen map[string]bool, slow ...bool) func(e int64) {
	return func(e int64) {
		if e < from {
			return
		}
		for k, d := range f.evalAll(e) {
			if strings.HasSuffix(k, " info") {
				continue
			}
			code := strings.Fields(k)[0]
			sev := k[strings.LastIndex(k, " ")+1:]
			if code == "stream-003" && sev == "warning" {
				continue
			}
			if code == "server-003" && sev == "warning" && len(slow) > 0 && slow[0] {
				continue
			}
			if !seen[k] {
				seen[k] = true
				*bad = append(*bad, fmt.Sprintf("epoch %d %s: %s", e, k, d))
			}
		}
	}
}

// N1: each server takes 2-3 minutes to recover.
func TestT3D_SlowRollingRestart(t *testing.T) {
	for _, rec := range []int64{4, 6, 7} {
		t.Run(fmt.Sprint("rec", rec), func(t *testing.T) {
			f := newFx(t)
			r := roll{down: 2, rec: rec, gap: 3, startAt: 4, total: 4 + 3*(5+rec) + 6}
			var bad []string
			f.buildRoll(r, nil, collect(f, 5, &bad, map[string]bool{}, time.Duration(rec)*30*time.Second > 3*time.Minute))
			for _, b := range bad {
				t.Errorf("%s", b)
			}
		})
	}
}

// N3: collector restarted in the middle of a rolling restart: no epochs for
// 4 minutes (8 epochs), numbering continues.
func TestT3D_CollectorGapDuringRoll(t *testing.T) {
	f := newFx(t)
	r := roll{down: 2, rec: 4, gap: 3, startAt: 4, total: 40}
	var bad []string
	f.buildRoll(r, func(e int64) bool { return e >= 14 && e <= 21 }, collect(f, 5, &bad, map[string]bool{}))
	for _, b := range bad {
		t.Errorf("%s", b)
	}
}

func loadCfg() config.Checks { return config.Checks{Timeout: config.Duration(10 * time.Second)} }
