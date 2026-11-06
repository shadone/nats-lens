package storeprov_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/fake"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The demo Go counter and stored SQL keep the page's handoff count, while
// the real check excludes the handoff and the API exposes a system warning.
func TestLeafCheckAPIAndDemoDropRule(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := st.DB().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"A", "B"} {
		r.add("servers", map[string]any{"server_id": id, "server_name": id, "first_epoch": 1, "last_epoch": 5, "first_seen_at": t0, "last_seen_at": t0, "start_time": t0.Add(-time.Hour)})
	}
	var history []api.TopologyCollection
	for e := int64(1); e <= 5; e++ {
		at := t0.Add(time.Duration(e-1) * 30 * time.Second)
		exec(`INSERT INTO epochs VALUES (?,?,?,0,2,2,['A','B'],['A','B'],[],0,false,[],NULL)`, e, at, at)
		c := api.TopologyCollection{Epoch: e, At: at, Leafz: map[string]bool{"A": true, "B": true}}
		for _, id := range []string{"A", "B"} {
			r.add("epoch_endpoints", map[string]any{"epoch": e, "server_id": id, "endpoint": "LEAFZ", "state": "ok"})
		}
		hub := "A"
		if e%2 == 0 {
			hub = "B"
		}
		for _, l := range []api.TopologyLeafRow{{ServerID: hub, Server: hub, LID: e, Name: "handoff", Account: "APP"}, {ServerID: "A", Server: "A", LID: e, Name: "reconnect", Account: "APP"}} {
			r.add("leafnodes", map[string]any{"epoch": e, "server_id": l.ServerID, "lid": l.LID, "name": l.Name, "account": l.Account})
			c.Leafnodes = append(c.Leafnodes, l)
		}
		history = append(history, c)
	}
	set, err := checks.Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	var disabled []string
	for _, c := range set.Checks() {
		if c.Code != checks.LeafHealthCode && c.Code != checks.CheckFailedCode {
			disabled = append(disabled, c.Code)
		}
	}
	set, err = checks.Load(config.Checks{Disabled: disabled})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&checks.Engine{Set: set, Interval: 30 * time.Second}).Run(context.Background(), st, 5); err != nil {
		t.Fatal(err)
	}
	end := t0.Add(150 * time.Second)
	p := &storeprov.Provider{St: st, Checks: set, Interval: 30 * time.Second, Clock: func() time.Time { return end }}
	f, err := p.GetFinding(ctx, &v1.GetFindingRequest{Id: checks.LeafHealthCode + ":"})
	if err != nil {
		t.Fatal(err)
	}
	finding := f.GetFinding()
	if finding.GetEntity().GetType() != v1.EntityType_ENTITY_TYPE_SYSTEM || finding.GetEntity().GetKey() != "" || !strings.Contains(finding.GetDetail(), `"reconnect"`) || strings.Contains(finding.GetDetail(), `"handoff"`) || finding.GetValue().GetNumber() != 1 {
		t.Fatal(finding)
	}
	facts, err := p.Topology(ctx, &v1.GetTopologyRequest{Range: &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(end)}})
	if err != nil {
		t.Fatal(err)
	}
	goDrops := api.CountTopologyDrops(history, 30*time.Second, 5, history[4].At)
	for _, d := range goDrops.Leafs {
		found := false
		for _, s := range facts.Drops.Leafs {
			if d == s {
				found = true
			}
		}
		if !found {
			t.Fatalf("Go/demo %v differs from stored %v", goDrops.Leafs, facts.Drops.Leafs)
		}
	}
	if len(goDrops.Leafs) != 2 || len(facts.Drops.Leafs) != 2 {
		t.Fatal(goDrops.Leafs, facts.Drops.Leafs)
	}
}

// The fixed global story has a missing expected hub. A page can show a
// disconnected site, but the conservative check must not invent coverage.
func TestLeafCheckFixedDemoCoverage(t *testing.T) {
	p := fake.Demo(false)[1].Provider
	end := fake.FixedNow
	r := &v1.TimeRange{Start: timestamppb.New(end.Add(-time.Hour)), End: timestamppb.New(end)}
	facts, err := p.Topology(ctx, &v1.GetTopologyRequest{Range: r})
	if err != nil {
		t.Fatal(err)
	}
	missing := false
	for _, s := range facts.Servers {
		missing = missing || s.Missing
	}
	if !missing {
		t.Fatal("fixture needs missing expected hub")
	}
	findings, err := p.ListFindings(ctx, &v1.ListFindingsRequest{Range: r})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings.GetFindings() {
		if f.GetCheckCode() == checks.LeafHealthCode {
			t.Fatalf("incomplete demo invents leaf warning: %v", f)
		}
	}
}
