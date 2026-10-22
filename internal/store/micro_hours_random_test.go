package store

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sort"
	"testing"
	"time"

	"github.com/nats-io/nats.go/micro"

	"github.com/shadone/nats-lens/internal/collect"
)

// The hourly sums match the documented rule computed in Go from the raw
// rows, over random histories: restarts, counters that fall, instances
// that miss epochs, gaps over an hour, a clock stepped back.
func TestMicroHoursRandomHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("writes 1,800 epochs")
	}
	for seed := range uint64(6) {
		t.Run(fmt.Sprint(seed), func(t *testing.T) { hoursRandom(t, seed, seed%2 == 1) })
	}
}

type hoursRow struct {
	epoch           int
	inst, ep        string
	req, errs, proc int64
	started         time.Time
}

func hoursRandom(t *testing.T, seed uint64, stepBack bool) {
	r := rand.New(rand.NewPCG(seed, 7))
	s := openMem(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 16, 21, 17, 3, 0, time.UTC)
	type inst struct {
		id      string
		started time.Time
		c       map[string][3]int64
	}
	var insts []*inst
	newInst := func(n int, now time.Time) *inst {
		return &inst{id: fmt.Sprintf("I%d", n), started: now.Add(-time.Duration(r.IntN(3600)) * time.Second), c: map[string][3]int64{}}
	}
	nid := 0
	for range 3 {
		insts = append(insts, newInst(nid, at.Add(-24*time.Hour)))
		nid++
	}
	eps := []string{"get", "create"}
	var starts []time.Time
	var rows []hoursRow
	for e := 1; e <= 300; e++ {
		step := time.Duration(10+r.IntN(50)) * time.Second
		switch x := r.IntN(100); {
		case x < 3:
			step = time.Duration(61+r.IntN(90)) * time.Minute
		case x < 5 && stepBack:
			step = -time.Duration(1+r.IntN(40)) * time.Minute
		}
		at = at.Add(step)
		// evolve
		for i, in := range insts {
			switch x := r.IntN(100); {
			case x < 3: // restart with a new ID
				nid++
				insts[i] = &inst{id: fmt.Sprintf("I%d", nid), started: at.Add(-time.Duration(r.IntN(30)) * time.Second), c: map[string][3]int64{}}
				continue
			case x < 5: // counter falls, same start
				for _, ep := range eps {
					in.c[ep] = [3]int64{int64(r.IntN(3)), 0, int64(r.IntN(5))}
				}
				continue
			}
			for _, ep := range eps {
				v := in.c[ep]
				n := int64(r.IntN(20))
				v[0] += n
				v[1] += int64(r.IntN(int(n) + 1))
				v[2] += n * int64(1+r.IntN(1000))
				in.c[ep] = v
			}
		}
		if r.IntN(100) < 4 {
			nid++
			insts = append(insts, &inst{id: fmt.Sprintf("I%d", nid), started: at.Add(-time.Duration(r.IntN(120)) * time.Second), c: map[string][3]int64{}})
		}
		snap := collect.NewSnapshot(at)
		acc := collect.MicroAccount{Label: "APP", Account: "APP",
			Asked: []collect.MicroAsked{{Request: collect.MicroStats, State: collect.MicroOK}}}
		for _, in := range insts {
			if r.IntN(100) < 10 { // missed this epoch
				continue
			}
			st := micro.Stats{ServiceIdentity: micro.ServiceIdentity{Name: "orders", ID: in.id},
				Type: micro.StatsResponseType, Started: in.started}
			for _, ep := range eps {
				v := in.c[ep]
				st.Endpoints = append(st.Endpoints, &micro.EndpointStats{Name: ep, NumRequests: int(v[0]),
					NumErrors: int(v[1]), ProcessingTime: time.Duration(v[2])})
				rows = append(rows, hoursRow{e, in.id, ep, v[0], v[1], v[2], in.started})
			}
			acc.Stats = append(acc.Stats, st)
		}
		snap.Micro = []collect.MicroAccount{acc}
		ep, err := s.WriteSnapshot(ctx, snap)
		if err != nil {
			t.Fatal(err)
		}
		if int(ep) != e {
			t.Fatalf("epoch %d numbered %d", e, ep)
		}
		starts = append(starts, at)
	}
	// expected, by the documented rule
	type key struct {
		hour time.Time
		ep   string
	}
	want := map[key][4]int64{}
	byEpoch := map[int][]hoursRow{}
	for _, rw := range rows {
		byEpoch[rw.epoch] = append(byEpoch[rw.epoch], rw)
	}
	for e := 1; e <= len(starts); e++ {
		cur := byEpoch[e]
		if len(cur) == 0 {
			continue
		}
		start := starts[e-1]
		lo := e
		for x := 1; x < e; x++ {
			if !starts[x-1].Before(start.Add(-time.Hour)) {
				lo = x
				break
			}
		}
		var prevStart time.Time
		if e > 1 {
			prevStart = starts[e-2]
		}
		touched := map[string]bool{}
		for _, c := range cur {
			var p *hoursRow
			for x := e - 1; x >= lo; x-- {
				for i := range byEpoch[x] {
					q := byEpoch[x][i]
					if q.inst == c.inst && q.ep == c.ep {
						p = &q
						break
					}
				}
				if p != nil {
					break
				}
			}
			var d [3]int64
			switch {
			case p == nil:
				if e > 1 && c.started.After(prevStart) {
					d = [3]int64{c.req, c.errs, c.proc}
				}
			case c.req < p.req || c.errs < p.errs || c.proc < p.proc || !c.started.Equal(p.started):
				d = [3]int64{c.req, c.errs, c.proc}
			default:
				d = [3]int64{c.req - p.req, c.errs - p.errs, c.proc - p.proc}
			}
			k := key{start.Truncate(time.Hour), c.ep}
			v := want[k]
			v[0] += d[0]
			v[1] += d[1]
			v[2] += d[2]
			if !touched[c.ep] {
				v[3]++
				touched[c.ep] = true
			}
			want[k] = v
		}
	}
	var wantS []string
	for k, v := range want {
		wantS = append(wantS, fmt.Sprintf("%s|%s|%d|%d|%d|%d", k.hour.Format("2006-01-02 15"), k.ep, v[0], v[1], v[2], v[3]))
	}
	sort.Strings(wantS)
	got := rowStrings(t, s.DB(), `SELECT strftime(hour, '%Y-%m-%d %H'), endpoint, requests, errors, processing_time_ns, epochs
FROM micro_endpoint_hours ORDER BY 1, 2`)
	sort.Strings(got)
	wantRows(t, "hours", got, wantS)
	t.Logf("%d hours rows, %d raw rows", len(got), len(rows))
}

// Should one instance arrive twice in an epoch under one account, its
// increase is counted once: 100 then 110 is 10 over two epochs.
func TestMicroHoursOneRowPerInstance(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	h := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)
	old := h.Add(-24 * time.Hour)
	for i, n := range []int{100, 110} {
		snap := hourSnapshot(t, h.Add(time.Duration(i)*30*time.Second), hourInstance{"A", old, n, 0, time.Millisecond})
		second := snap.Micro[0]
		second.Label = "APP-2"
		snap.Micro = append(snap.Micro, second)
		if _, err := s.WriteSnapshot(ctx, snap); err != nil {
			t.Fatal(err)
		}
	}
	got := rowStrings(t, s.DB(), `SELECT requests, epochs FROM micro_endpoint_hours`)
	if len(got) != 1 || got[0] != "10|2" {
		t.Errorf("an increase of 10 over two epochs: %q", got)
	}
}
