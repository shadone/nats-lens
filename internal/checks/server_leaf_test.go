package checks

import (
	"testing"
	"time"
)

func leafFixture(t *testing.T, n int64) *fx {
	t.Helper()
	f := newFx(t)
	f.epochs(1, n, "hub-a", "hub-b")
	for i, id := range []string{"A", "B"} {
		f.server(id, []string{"hub-a", "hub-b"}[i], 1, n, t0.Add(-time.Hour), "2.15.0")
		for e := int64(1); e <= n; e++ {
			f.row("epoch_endpoints", m{"epoch": e, "server_id": id, "endpoint": "LEAFZ", "state": "ok"})
		}
	}
	return f
}
func site(f *fx, e int64, hub, name, account string, lid int64) {
	f.row("leafnodes", m{"epoch": e, "server_id": hub, "name": name, "account": account, "lid": lid, "is_spoke": false})
}
func TestLeafHealthDisconnectRecovery(t *testing.T) {
	f := leafFixture(t, 5)
	site(f, 1, "A", "pickup-1", "APP", 1)
	for e := int64(1); e <= 5; e++ {
		site(f, e, "B", "healthy", "APP", 2)
	}
	f.fires("server-026", 2)
	r := f.fires("server-026", 3, "")[""]
	if r.Severity != "warning" {
		t.Fatal(r)
	}
	site(f, 4, "B", "pickup-1", "APP", 3)
	f.fires("server-026", 4)
	f.fires("server-026", 5)
}
func TestLeafHealthReconnectAndHandoff(t *testing.T) {
	for _, handoff := range []bool{false, true} {
		t.Run(map[bool]string{false: "reconnect", true: "handoff"}[handoff], func(t *testing.T) {
			f := leafFixture(t, 5)
			for e := int64(1); e <= 5; e++ {
				hub := "A"
				if handoff && e%2 == 0 {
					hub = "B"
				}
				site(f, e, hub, "pickup-1", "APP", e)
			}
			if handoff {
				f.fires("server-026", 5)
			} else {
				f.fires("server-026", 5, "")
			}
		})
	}
}
func TestLeafHealthUnknownBreaksAbsence(t *testing.T) {
	for _, mutation := range []string{
		`DELETE FROM epoch_endpoints WHERE epoch=2 AND server_id='B'`,
		`UPDATE epoch_endpoints SET state='timeout' WHERE epoch=2 AND server_id='A'`,
		`UPDATE epochs SET started_at=started_at + INTERVAL 10 MINUTE WHERE epoch>=2`,
		`UPDATE epochs SET observed_names=['hub-a'], observed_servers=1 WHERE epoch=2`,
	} {
		t.Run(mutation, func(t *testing.T) {
			f := leafFixture(t, 3)
			site(f, 1, "A", "pickup-1", "APP", 1)
			f.exec(mutation)
			f.fires("server-026", 3)
		})
	}
}
