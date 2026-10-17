package checks

import "testing"

// run5 builds the scenario and checks stream-001 and consumer-001 fire in
// epochs 8 to 12 when lost is set, and never otherwise; both are quiet in
// epoch 16, when every server is back.
func run5(t *testing.T, name string, holders []int, state func(i int, e int64) string, sl func(e int64) int, lost bool) {
	f := newFx(t)
	g := gen{n: 5, holders: holders, total: 16, state: state, sleader: sl, mleader: func(e int64) int { return 0 }, durable: true}
	g.run(f, nil)
	for _, e := range []int64{8, 9, 10, 12, 16} {
		s, c := f.eval("stream-001", e), f.eval("consumer-001", e)
		t.Logf("%s e%d stream-001: %s", name, e, rowsText(s))
		t.Logf("%s e%d consumer-001: %s", name, e, rowsText(c))
		want := lost && e <= 12
		if (len(s) > 0) != want || (len(c) > 0) != want {
			t.Errorf("%s e%d: stream-001 %d, consumer-001 %d findings, want firing %v", name, e, len(s), len(c), want)
		}
	}
}

func win(from, to int64, e int64) bool { return e >= from && e <= to }

// R5: nats-2 truly down, nats-3 and nats-4 restarting (back, leaderless); nats-0,1 up with no leader.
func TestT3D_R5TwoRestartingOneDown(t *testing.T) {
	run5(t, "A", []int{0, 1, 2, 3, 4}, func(i int, e int64) string {
		switch {
		case i == 2 && e >= 6:
			return "down"
		case (i == 3 || i == 4) && win(6, 7, e):
			return "down"
		case (i == 3 || i == 4) && win(8, 14, e):
			return "rec"
		}
		return "up"
	}, func(e int64) int {
		if e >= 6 && e <= 14 {
			return -1
		}
		return 0
	}, true)
}

// R5: nats-3 and nats-4 restarting, nats-0,1,2 up and agreeing on nats-0: healthy.
func TestT3D_R5TwoRestartingHealthy(t *testing.T) {
	run5(t, "B", []int{0, 1, 2, 3, 4}, func(i int, e int64) string {
		switch {
		case (i == 3 || i == 4) && win(6, 7, e):
			return "down"
		case (i == 3 || i == 4) && win(8, 12, e):
			return "rec"
		}
		return "up"
	}, func(e int64) int { return 0 }, false)
}

// R3 on a 5-node cluster: holders 0,1,2; nats-1 restarting, nats-2 down, nats-0 alone.
func TestT3D_R3On5OneRestartingOneDown(t *testing.T) {
	run5(t, "C", []int{0, 1, 2}, func(i int, e int64) string {
		switch {
		case i == 2 && e >= 6:
			return "down"
		case i == 1 && win(6, 7, e):
			return "down"
		case i == 1 && win(8, 12, e):
			return "rec"
		}
		return "up"
	}, func(e int64) int {
		if e >= 6 && e <= 12 {
			return -1
		}
		return 0
	}, true)
}

// R3 on 5 nodes: nats-1 restarting while nats-0,2 are fine and agree. Leader nats-0.
func TestT3D_R3On5OneRestartingHealthy(t *testing.T) {
	run5(t, "D", []int{0, 1, 2}, func(i int, e int64) string {
		switch {
		case i == 1 && win(6, 7, e):
			return "down"
		case i == 1 && win(8, 12, e):
			return "rec"
		}
		return "up"
	}, func(e int64) int { return 0 }, false)
}
