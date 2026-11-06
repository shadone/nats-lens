package checks

import (
	"context"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/routehealth"
	"github.com/shadone/nats-lens/internal/store"
)

// Both collections report one slow link, with ordinary servers as a
// nonfailing control. Findings name both ends rather than claiming blame.
func TestSlowRouteFinding(t *testing.T) {
	f := newFx(t)
	names := []string{"nats-0", "nats-1", "nats-2", "nats-3", "nats-4"}
	f.epochs(1, 2, names...)
	for i, name := range names {
		id := fmt.Sprintf("S%d", i)
		f.server(id, name, 1, 2, t0.Add(-time.Hour), "2.15.0")
	}
	for e := int64(1); e <= 2; e++ {
		for i := range names {
			for j := i + 1; j < len(names); j++ {
				d := 400_000
				if i == 0 && j == 1 {
					d = 4_000_000
				}
				f.row("routes", m{"epoch": e, "server_id": fmt.Sprintf("S%d", i), "rid": j, "remote_name": names[j], "rtt_ns": d})
			}
		}
	}
	f.fires("server-025", 1)
	r := f.fires("server-025", 2, "nats-0", "nats-1")
	want(t, r["nats-0"], "warning", "nats-1", "4ms", "two eligible collections")
}

// Every scenario compares the actual check output with FinishTopology
// using the same stored facts, and independently asserts expected entities.
func TestSlowRouteAgreement(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    int
		edit func(*fx)
		want []string
	}{
		{name: "slow server", n: 5, want: []string{"nats-0", "nats-1", "nats-2", "nats-3", "nats-4"}},
		{name: "normal", n: 5, edit: func(f *fx) { f.exec(`UPDATE routes SET rtt_ns=400000`) }},
		{name: "two servers", n: 2},
		{name: "even zones", n: 6, edit: func(f *fx) {
			f.exec(`UPDATE routes SET rtt_ns=CASE WHEN (server_id IN ('S0','S1','S2')) = (remote_name IN ('nats-0','nats-1','nats-2')) THEN 400000 ELSE 10000000 END`)
		}},
		{name: "jitter", n: 5, edit: func(f *fx) { f.exec(`UPDATE routes SET rtt_ns=rtt_ns+epoch*1000+rid*2000`) }, want: []string{"nats-0", "nats-1", "nats-2", "nats-3", "nats-4"}},
		{name: "unknown RTT", n: 5, edit: func(f *fx) { f.exec(`UPDATE routes SET rtt_ns=NULL WHERE remote_name='nats-4'`) }},
		{name: "zero RTT", n: 5, edit: func(f *fx) { f.exec(`UPDATE routes SET rtt_ns=0 WHERE remote_name='nats-4'`) }},
		{name: "current missing report", n: 5, edit: func(f *fx) { f.exec(`DELETE FROM routes WHERE epoch=2 AND remote_name='nats-4'`) }},
		{name: "previous missing report", n: 5, edit: func(f *fx) { f.exec(`DELETE FROM routes WHERE epoch=1 AND remote_name='nats-4'`) }},
		{name: "previous normal", n: 5, edit: func(f *fx) { f.exec(`UPDATE routes SET rtt_ns=400000 WHERE epoch=1`) }},
		{name: "current normal", n: 5, edit: func(f *fx) { f.exec(`UPDATE routes SET rtt_ns=400000 WHERE epoch=2`) }},
		{name: "previous judged independently", n: 5, edit: func(f *fx) {
			f.exec(`DELETE FROM routes WHERE epoch=1 AND NOT (server_id='S0' AND remote_name='nats-4')`)
		}},
		{name: "missing ROUTEZ", n: 5, edit: func(f *fx) {
			f.exec(`DELETE FROM routes WHERE epoch=2`)
			f.row("epoch_endpoints", m{"epoch": 2, "server_id": "S0", "endpoint": "ROUTEZ", "state": "timeout"})
		}},
		// Names are the identity of RTT pairs; new process/RIDs are fresh
		// observations, not counter differences or proof of dropped connections.
		{name: "new process same name", n: 5, edit: func(f *fx) {
			f.exec(`UPDATE servers SET last_epoch=1 WHERE server_id='S0'`)
			f.server("NEW", "nats-0", 2, 2, at(2), "2.15.0")
			f.exec(`UPDATE routes SET server_id='NEW',rid=rid+100 WHERE epoch=2 AND server_id='S0'`)
		}, want: []string{"nats-0", "nats-1", "nats-2", "nats-3", "nats-4"}},
		{name: "peer process ID resolves to stable name", n: 5, edit: func(f *fx) {
			f.exec(`UPDATE routes SET remote_id='S' || right(remote_name,1), remote_name=''`)
		}, want: []string{"nats-0", "nats-1", "nats-2", "nats-3", "nats-4"}},
		{name: "different peer name", n: 5, edit: func(f *fx) {
			f.exec(`UPDATE routes SET remote_name='replacement' WHERE epoch=2 AND remote_name='nats-4'`)
		}},
		{name: "unknown cluster placement", n: 5, edit: func(f *fx) { f.exec(`UPDATE servers SET cluster=NULL WHERE server_id='S1'`) }, want: []string{"nats-0", "nats-1", "nats-2", "nats-3", "nats-4"}},
		{name: "independent cluster", n: 5, edit: func(f *fx) {
			f.server("X", "other-0", 1, 2, t0.Add(-time.Hour), "2.15.0")
			f.exec(`UPDATE servers SET cluster='other' WHERE server_id='X'`)
			for e := int64(1); e <= 2; e++ {
				f.row("routes", m{"epoch": e, "server_id": "X", "rid": 1, "remote_name": "other-1", "rtt_ns": 100000000})
			}
		}, want: []string{"nats-0", "nats-1", "nats-2", "nats-3", "nats-4"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFx(t)
			var names []string
			for i := 0; i < tc.n; i++ {
				names = append(names, fmt.Sprintf("nats-%d", i))
			}
			f.epochs(1, 2, names...)
			for i, name := range names {
				f.server(fmt.Sprintf("S%d", i), name, 1, 2, t0.Add(-time.Hour), "2.15.0")
			}
			f.exec(`UPDATE servers SET cluster='shop'`)
			for e := int64(1); e <= 2; e++ {
				for i := range names {
					for j := i + 1; j < len(names); j++ {
						d := 400000
						if j == len(names)-1 {
							d = 4000000
						}
						f.row("routes", m{"epoch": e, "server_id": fmt.Sprintf("S%d", i), "rid": j, "remote_name": names[j], "rtt_ns": d})
					}
				}
			}
			if tc.edit != nil {
				tc.edit(f)
			}
			f.fires("server-025", 2, tc.want...)
			assertSlowAgreement(t, f, 2, 30*time.Second)
		})
	}
}

func assertSlowAgreement(t *testing.T, f *fx, epoch int64, interval time.Duration) {
	t.Helper()
	ctx := context.Background()
	rows, err := routehealth.ReadRows(ctx, f.st.DB(), epoch)
	if err != nil {
		t.Fatal(err)
	}
	earlier, err := routehealth.ReadEarlier(ctx, f.st.DB(), epoch, at(epoch), interval)
	if err != nil {
		t.Fatal(err)
	}
	facts := &api.TopologyFacts{Epoch: epoch, EpochAt: at(epoch), Interval: interval, Routes: rows, Earlier: earlier}
	// Include reporting and remote names as the API's expected-server list.
	clusters := map[string]string{}
	for _, r := range rows {
		clusters[r.Server] = r.Cluster
		if _, ok := clusters[r.Remote]; !ok {
			clusters[r.Remote] = ""
		}
	}
	for n, c := range clusters {
		facts.Servers = append(facts.Servers, api.TopologyServerFact{Name: n, Cluster: c})
	}
	expected := map[string]bool{}
	for _, r := range api.FinishTopology(facts).GetRoutes() {
		if r.GetSlow() {
			expected[r.GetServerA()] = true
			expected[r.GetServerB()] = true
		}
	}
	set, err := Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	runs, err := (&Engine{Set: set, Interval: interval, only: map[string]bool{SlowRouteCode: true}}).Evaluate(ctx, f.st.DB(), epoch)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range runs {
		if r.Code == SlowRouteCode {
			if r.Status != store.RunOK {
				t.Fatalf("check status %s: %s", r.Status, r.Error)
			}
			got := map[string]bool{}
			for _, v := range r.Rows {
				got[v.EntityKey] = true
			}
			if !maps.Equal(got, expected) {
				t.Fatalf("check %v differs from API %v", got, expected)
			}
		}
	}
}

func TestSlowRouteCollectionEligibility(t *testing.T) {
	for _, tc := range []struct {
		name     string
		current  int64
		interval time.Duration
		want     bool
	}{
		{"boundary", 4, 30 * time.Second, true},
		{"gap", 5, 30 * time.Second, false},
		{"unknown interval", 2, 0, false},
		{"negative interval", 2, -time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFx(t)
			names := []string{"nats-0", "nats-1", "nats-2"}
			f.epochs(1, tc.current, names...)
			for i, n := range names {
				f.server(fmt.Sprintf("S%d", i), n, 1, tc.current, t0.Add(-time.Hour), "2.15.0")
			}
			// Empty collections between the two do not replace the previous
			// eligible route-bearing collection.
			for _, e := range []int64{1, tc.current} {
				for i := range names {
					for j := i + 1; j < len(names); j++ {
						d := 400000
						if j == 2 {
							d = 4000000
						}
						f.row("routes", m{"epoch": e, "server_id": fmt.Sprintf("S%d", i), "rid": j, "remote_name": names[j], "rtt_ns": d})
					}
				}
			}
			assertSlowAgreement(t, f, tc.current, tc.interval)
			set, err := Load(config.Checks{})
			if err != nil {
				t.Fatal(err)
			}
			runs, err := (&Engine{Set: set, Interval: tc.interval, only: map[string]bool{SlowRouteCode: true}}).Evaluate(context.Background(), f.st.DB(), tc.current)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range runs {
				if r.Code == SlowRouteCode && (len(r.Rows) > 0) != tc.want {
					t.Fatalf("rows %v, want firing %v", r.Rows, tc.want)
				}
			}
		})
	}
}

func TestSlowRouteEngineControls(t *testing.T) {
	f := newFx(t)
	f.epoch(1, []string{"nats-0"})
	for _, tc := range []struct {
		name            string
		disabled        bool
		budget, timeout time.Duration
		status          string
	}{
		{name: "disabled", disabled: true, status: store.RunDisabled},
		{name: "budget", budget: time.Nanosecond, status: store.RunError},
		{name: "timeout", timeout: time.Nanosecond, status: store.RunError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Checks{Timeout: config.Duration(tc.timeout)}
			if tc.disabled {
				cfg.Disabled = []string{SlowRouteCode}
			}
			set, err := Load(cfg)
			if err != nil {
				t.Fatal(err)
			}
			runs, err := (&Engine{Set: set, Interval: 30 * time.Second, Budget: tc.budget, only: map[string]bool{SlowRouteCode: true}}).Evaluate(context.Background(), f.st.DB(), 1)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, r := range runs {
				if r.Code == SlowRouteCode {
					found = true
					if r.Status != tc.status {
						t.Fatalf("status %s, want %s: %s", r.Status, tc.status, r.Error)
					}
				}
			}
			if !found {
				t.Fatal("missing slow-route check run")
			}
		})
	}
}
