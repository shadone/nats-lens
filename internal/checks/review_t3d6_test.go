package checks

import "testing"

// Server nats-1 is replaced by a pod named nats-9 (new name, same peer id):
// the new name has no earlier process, so it is not "restarting".
func TestT3D_ServerRenamed(t *testing.T) {
	f := newFx(t)
	g := gen{n: 3, holders: []int{0, 1, 2}, total: 20, durable: true, mleader: func(e int64) int { return 0 },
		sleader: func(e int64) int { return 0 },
		state: func(i int, e int64) string {
			switch {
			case i == 1 && e >= 6 && e <= 7:
				return "down"
			case i == 1 && e >= 8 && e <= 11:
				return "rec"
			}
			return "up"
		}}
	g.run(f, nil)
	f.exec(`UPDATE servers SET server_name = 'nats-9' WHERE server_id = 'P1_1'`)
	for e := int64(8); e <= 14; e++ {
		var out []string
		for k, d := range f.evalAll(e) {
			out = append(out, k+": "+d)
		}
		t.Logf("e%d: %v", e, out)
	}
}
