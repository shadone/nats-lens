package checks

import (
	"testing"
)

// cpu stores the CPU of server id from epoch 1 to n, with 8 cores and
// GOMAXPROCS 4.
func (f *fx) cpu(n int64, id string, percent func(e int64) float64) {
	for e := int64(1); e <= n; e++ {
		f.stats(e, id, m{"cpu_percent": percent(e), "cores": 8, "gomaxprocs": 4})
	}
}

func steady(p float64) func(int64) float64 { return func(int64) float64 { return p } }

// A server at 90% of its usable cores (GOMAXPROCS 4 of 8) in at least 80%
// of the collections of 5 minutes fires, once it has answered over all of
// them; one calm collection does not stop it; CPU just below never fires.
func TestCPUSaturated(t *testing.T) {
	f := newFx(t)
	f.cluster(13)
	f.cpu(13, "S0", steady(360))
	f.cpu(13, "S1", func(e int64) float64 {
		if e == 2 {
			return 100
		}
		return 380
	})
	f.cpu(13, "S2", steady(359.9))
	// At epoch 10 the 5 minutes begin before the first collection.
	f.fires("server-023", 10)
	r := f.fires("server-023", 11, "nats-0", "nats-1")
	want(t, r["nats-0"], "warning", "nats-0 used 3.6 of 4 usable cores (90%); 11 of the 11 collections in the last 5 minutes were at least 90% busy (8 cores, GOMAXPROCS 4).")
	want(t, r["nats-1"], "warning", "10 of the 11 collections")
	if v := r["nats-0"].Value; v == nil || *v != 0.9 {
		t.Errorf("value %v", v)
	}
}

// The share of busy collections is the threshold: 9 of 11 (82%) fire, 8 of
// 11 (73%) do not; fewer than min_samples collections never fire.
func TestCPUSaturatedShare(t *testing.T) {
	for _, c := range []struct {
		calm  int64
		fires bool
	}{{2, true}, {3, false}} {
		f := newFx(t)
		f.cluster(11)
		f.cpu(11, "S0", func(e int64) float64 {
			if e <= c.calm {
				return 100
			}
			return 400
		})
		if c.fires {
			f.fires("server-023", 11, "nats-0")
		} else {
			f.fires("server-023", 11)
		}
	}
	f := newFx(t)
	f.cluster(11)
	f.cpu(11, "S0", steady(400))
	if got := f.eval("server-023", 11, map[string]string{"min_samples": "12"}); len(got) != 0 {
		t.Errorf("min_samples 12: %s", rowsText(got))
	}
}

// A restarting process is not judged; a collection without a CPU value is
// skipped, not read as calm; a server that reports no cores is not judged.
func TestCPUSaturatedRestartAndGaps(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 30, three...)
	f.server("A0", "nats-0", 1, 6, at(1), "2.15.0")
	f.server("B0", "nats-0", 7, 30, at(7), "2.15.0")
	f.server("S1", "nats-1", 1, 30, at(1), "2.15.0")
	f.server("S2", "nats-2", 1, 30, at(1), "2.15.0")
	f.cpu(6, "A0", steady(400))
	for e := int64(7); e <= 30; e++ {
		f.stats(e, "B0", m{"cpu_percent": 400, "cores": 8, "gomaxprocs": 4})
	}
	for e := int64(1); e <= 30; e++ {
		if e == 5 {
			f.stats(e, "S1", m{})
			continue
		}
		f.stats(e, "S1", m{"cpu_percent": 400, "cores": 4})
		f.stats(e, "S2", m{"cpu_percent": 800})
	}
	r := f.fires("server-023", 12, "nats-1")
	want(t, r["nats-1"], "warning", "4 of 4 usable cores (100%); 10 of the 10 collections")
	// at(27) is 10 minutes after B0 started: no longer restarting.
	f.fires("server-023", 26, "nats-1")
	f.fires("server-023", 27, "nats-0", "nats-1")
}

// A gap of more than three collect intervals ends a run: the collections
// after nats-lens was down are judged alone; a shorter gap does not.
func TestCPUSaturatedGap(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 100, three...)
	f.server("S0", "nats-0", 1, 100, at(1), "2.15.0")
	for _, e := range []int64{1, 2, 3, 100} {
		f.stats(e, "S0", m{"cpu_percent": 790, "cores": 8, "gomaxprocs": 8})
	}
	f.fires("server-023", 100)
	g := newFx(t)
	g.cluster(13)
	g.cpu(13, "S0", steady(400))
	g.exec(`DELETE FROM server_stats WHERE epoch BETWEEN 5 AND 8`)
	g.fires("server-023", 13)
	h := newFx(t)
	h.cluster(13)
	h.cpu(13, "S0", steady(400))
	h.exec(`DELETE FROM server_stats WHERE epoch BETWEEN 5 AND 6`)
	h.fires("server-023", 13, "nats-0")
}

// A firing finding stays while 80% of the collections are at 72% of the
// cores (0.8 of the busy level) and resolves once fewer are.
func TestCPUSaturatedResolve(t *testing.T) {
	f := newFx(t)
	f.cluster(14)
	f.cpu(14, "S0", func(e int64) float64 {
		if e < 12 {
			return 300 // 75%: below 90%, above 72%
		}
		return 280
	})
	f.fires("server-023", 12)
	f.row("findings", m{"id": "server-023:nats-0", "check_code": "server-023", "entity_type": "server", "entity_key": "nats-0",
		"severity": "warning", "state": "firing", "last_transition": "firing", "since": at(1), "since_epoch": 1,
		"transitioned_at": at(1), "last_seen_epoch": 12, "updated_epoch": 12, "detail": "x"})
	r := f.fires("server-023", 13, "nats-0")
	want(t, r["nats-0"], "warning", "9 of the 11 collections in the last 5 minutes were at least 72% busy")
	f.exec(`UPDATE findings SET last_seen_epoch = 13`)
	f.fires("server-023", 14)
}
