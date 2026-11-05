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

// A server at 90% of its usable cores (GOMAXPROCS 4 of 8) in every
// collection for 5 minutes fires; for 4.5 minutes, or just below, it does
// not.
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
	f.fires("server-023", 10)
	r := f.fires("server-023", 11, "nats-0")
	// epoch 11 is 5 minutes after epoch 1; nats-1's run starts at epoch 3.
	want(t, r["nats-0"], "warning", "nats-0 used 3.6 of 4 usable cores (90%) and has been at least 90% busy for 5 minutes (8 cores, GOMAXPROCS 4).")
	if got := r["nats-0"].ConditionSince; got == nil || !got.Equal(at(1)) {
		t.Errorf("condition since %v", got)
	}
	f.fires("server-023", 12, "nats-0")
	r = f.fires("server-023", 13, "nats-0", "nats-1")
	want(t, r["nats-1"], "warning", "for 5 minutes")
}

// A restart starts a new run; a collection without CPU is not read as
// calm; a server that reports no cores is not judged.
func TestCPUSaturatedRestartAndGaps(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 12, three...)
	f.server("A0", "nats-0", 1, 6, at(1), "2.15.0")
	f.server("B0", "nats-0", 7, 12, at(7), "2.15.0")
	f.server("S1", "nats-1", 1, 12, at(1), "2.15.0")
	f.server("S2", "nats-2", 1, 12, at(1), "2.15.0")
	f.cpu(6, "A0", steady(400))
	for e := int64(7); e <= 12; e++ {
		f.stats(e, "B0", m{"cpu_percent": 400, "cores": 8, "gomaxprocs": 4})
	}
	for e := int64(1); e <= 12; e++ {
		if e == 5 {
			f.stats(e, "S1", m{})
			continue
		}
		f.stats(e, "S1", m{"cpu_percent": 400, "cores": 4})
		f.stats(e, "S2", m{"cpu_percent": 800})
	}
	r := f.fires("server-023", 12, "nats-1")
	want(t, r["nats-1"], "warning", "4 of 4 usable cores (100%)")
}

// A firing finding stays while the CPU is at 80% of the busy level and
// resolves below it.
func TestCPUSaturatedResolve(t *testing.T) {
	f := newFx(t)
	f.cluster(13)
	f.cpu(13, "S0", func(e int64) float64 {
		switch {
		case e < 12:
			return 300 // 75%: below 90%, above 72%
		case e == 12:
			return 289 // just above 72%
		}
		return 287
	})
	f.fires("server-023", 12)
	f.row("findings", m{"id": "server-023:nats-0", "check_code": "server-023", "entity_type": "server", "entity_key": "nats-0",
		"severity": "warning", "state": "firing", "last_transition": "firing", "since": at(1), "since_epoch": 1,
		"transitioned_at": at(1), "last_seen_epoch": 11, "updated_epoch": 11, "detail": "x"})
	r := f.fires("server-023", 12, "nats-0")
	want(t, r["nats-0"], "warning", "at least 72% busy for 6 minutes")
	f.exec(`UPDATE findings SET last_seen_epoch = 12`)
	f.fires("server-023", 13)
}
