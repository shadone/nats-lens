package checks

import (
	"context"
	"github.com/shadone/nats-lens/internal/config"
	"strings"
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
				f.fires("server-026", 3)
				f.fires("server-026", 4, "")
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

func TestLeafHealthIdentityAndDirections(t *testing.T) {
	f := leafFixture(t, 5)
	// One name on two accounts remains two identities; only APP is absent.
	site(f, 1, "A", "shared", "APP", 1)
	for e := int64(1); e <= 5; e++ {
		site(f, e, "B", "shared", "OTHER", 2)
	}
	// Uplinks are not remote sites; unnamed rows retain Topology's IP fallback.
	site(f, 1, "A", "uplink", "APP", 3)
	f.exec(`UPDATE leafnodes SET is_spoke=true WHERE name='uplink'`)
	site(f, 1, "A", "", "APP", 4)
	f.exec(`UPDATE leafnodes SET ip='192.0.2.1' WHERE name=''`)
	r := f.fires("server-026", 3, "")[""]
	if r.Value == nil || *r.Value != 2 || !strings.Contains(r.Detail, `"192.0.2.1"`) || strings.Contains(r.Detail, "uplink") || strings.Contains(r.Detail, `account "OTHER"`) {
		t.Fatal(r)
	}
}
func TestLeafHealthRestartAndDuplicateNames(t *testing.T) {
	f := leafFixture(t, 3)
	site(f, 1, "A", "pickup", "APP", 1)
	f.server("A2", "hub-a", 2, 3, at(2), "2.15.0")
	f.exec(`UPDATE epoch_endpoints SET server_id='A2' WHERE server_id='A' AND epoch>=2`)
	f.fires("server-026", 3)
	g := leafFixture(t, 3)
	site(g, 1, "A", "pickup", "APP", 1)
	g.exec(`UPDATE servers SET server_name='hub-a' WHERE server_id='B'`)
	g.fires("server-026", 3)
}
func TestLeafHealthRedundantConnectionsNotReconnects(t *testing.T) {
	f := leafFixture(t, 3)
	for e := int64(1); e <= 3; e++ {
		for lid := int64(1); lid <= 4; lid++ {
			site(f, e, "A", "redundant", "APP", e*10+lid)
		}
	}
	f.fires("server-026", 3)
}
func TestLeafHealthDropAgesOut(t *testing.T) {
	f := leafFixture(t, 5)
	for e := int64(1); e <= 4; e++ {
		site(f, e, "A", "pickup", "APP", e)
	}
	site(f, 5, "A", "pickup", "APP", 4)
	f.exec(`UPDATE epochs SET started_at=started_at + INTERVAL 61 MINUTE WHERE epoch>=4`)
	// The old drops leave the hour, and the gap manufactures no drop.
	f.fires("server-026", 5)
}

func TestLeafHealthScale(t *testing.T) {
	if testing.Short() {
		t.Skip("24h, 1000 sites")
	}
	f := newFx(t)
	// 2.88 million stored leaf observations, inserted in batches.
	f.exec(`INSERT INTO epochs SELECT e, make_timestamp(?::BIGINT)+(e-1)*INTERVAL 30 SECOND,
 make_timestamp(?::BIGINT)+(e-1)*INTERVAL 30 SECOND,0,1,1,['hub'],['hub'],[],0,false,[],NULL FROM range(1,2882) t(e)`, t0.UnixMicro(), t0.UnixMicro())
	f.server("A", "hub", 1, 2881, t0.Add(-time.Hour), "2.15.0")
	f.exec(`INSERT INTO epoch_endpoints(epoch,server_id,endpoint,state) SELECT epoch,'A','LEAFZ','ok' FROM epochs`)
	f.exec(`INSERT INTO leafnodes(epoch,server_id,lid,name,account,is_spoke)
 SELECT e,'A',i,'pickup-'||i,'APP',false FROM range(1,2882) t(e) CROSS JOIN range(1,1001) x(i) WHERE i>1 OR e<2880`)
	start := time.Now()
	r := f.fires("server-026", 2881, "")[""]
	t.Logf("1000 sites, 2881 collections: %s", time.Since(start))
	if r.Value == nil || *r.Value != 1 {
		t.Fatal(r)
	}
}

func TestLeafHealthUnknownHoldsUntilObservedRecovery(t *testing.T) {
	f := leafFixture(t, 7)
	site(f, 1, "A", "pickup", "APP", 1)
	site(f, 6, "B", "pickup", "APP", 2)
	site(f, 7, "B", "pickup", "APP", 2)
	f.exec(`DELETE FROM epoch_endpoints WHERE epoch IN (4,5) AND server_id='B'`)
	set, err := Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Set: set, Interval: 30 * time.Second, only: map[string]bool{LeafHealthCode: true}}
	for epoch := int64(1); epoch <= 7; epoch++ {
		if _, err := e.Run(context.Background(), f.st, epoch); err != nil {
			t.Fatal(err)
		}
		if epoch < 3 {
			continue
		}
		var state string
		if err := f.st.DB().QueryRow(`SELECT state FROM findings WHERE check_code=?`, LeafHealthCode).Scan(&state); err != nil {
			t.Fatal(err)
		}
		want := "firing"
		if epoch == 7 {
			want = "resolved"
		}
		if state != want {
			t.Fatalf("epoch %d state %s want %s", epoch, state, want)
		}
	}
}

func TestLeafHealthGapDoesNotResolveAbsence(t *testing.T) {
	f := leafFixture(t, 7)
	site(f, 1, "A", "pickup", "APP", 1)
	f.exec(`DELETE FROM epoch_endpoints WHERE epoch=4 AND server_id='B'`)
	set, err := Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Set: set, Interval: 30 * time.Second, only: map[string]bool{LeafHealthCode: true}}
	for epoch := int64(1); epoch <= 7; epoch++ {
		if _, err := e.Run(context.Background(), f.st, epoch); err != nil {
			t.Fatal(err)
		}
	}
	var state string
	if err := f.st.DB().QueryRow(`SELECT state FROM findings WHERE check_code=?`, LeafHealthCode).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "firing" {
		t.Fatalf("gap resolved an unobserved recovery: %s", state)
	}
}

func TestLeafHealthConfirmedReconnectDespiteOtherUnknownSite(t *testing.T) {
	f := leafFixture(t, 6)
	site(f, 1, "A", "unknown", "APP", 100)
	f.exec(`DELETE FROM epoch_endpoints WHERE epoch=2 AND server_id='B'`)
	for epoch := int64(1); epoch <= 6; epoch++ {
		site(f, epoch, "A", "reconnect", "APP", epoch)
	}
	r := f.fires("server-026", 6, "")[""]
	if strings.Contains(r.Detail, `"unknown"`) || !strings.Contains(r.Detail, `"reconnect"`) {
		t.Fatal(r)
	}
}
func TestLeafHealthUnknownInterval(t *testing.T) {
	f := leafFixture(t, 3)
	site(f, 1, "A", "pickup", "APP", 1)
	set, err := Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Set: set, only: map[string]bool{LeafHealthCode: true}}
	runs, err := e.Evaluate(context.Background(), f.st.DB(), 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range runs {
		if r.Code == LeafHealthCode && (r.Status != "ok" || len(r.Rows) != 0) {
			t.Fatal(r)
		}
	}
}
