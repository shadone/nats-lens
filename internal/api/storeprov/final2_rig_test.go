package storeprov_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/events"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
	"github.com/shadone/nats-lens/internal/inventory"
	"github.com/shadone/nats-lens/internal/runner"
	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/testcluster"
)

type rig struct {
	t    *testing.T
	ctx  context.Context
	c    *testcluster.Cluster
	st   *store.Store
	r    *runner.Runner
	cl   natslensv1connect.LensServiceClient
	app  *nats.Conn
	js   jetstream.JetStream
	last map[string]string
	n    int
}

func (g *rig) dial(user, pass, name string) *nats.Conn {
	urls := strings.Join([]string{g.c.URL(0), g.c.URL(1), g.c.URL(2)}, ",")
	nc, err := nats.Connect(urls, nats.UserInfo(user, pass), nats.Name(name), nats.MaxReconnects(-1), nats.ReconnectWait(100*time.Millisecond))
	if err != nil {
		g.t.Fatal(err)
	}
	g.t.Cleanup(nc.Close)
	return nc
}

func newRig(t *testing.T, inv string, accountEvents bool, cfg config.Checks) *rig {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	g := &rig{t: t, ctx: ctx, c: testcluster.Start(t, 3), last: map[string]string{}}
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	g.st = st
	if inv != "" {
		p := filepath.Join(t.TempDir(), "inv.yaml")
		if err := os.WriteFile(p, []byte(inv), 0o600); err != nil {
			t.Fatal(err)
		}
		rows, err := inventory.Load(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.ReplaceInventory(ctx, rows, p, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	capture := events.New(events.Options{Writer: st, FlushInterval: 100 * time.Millisecond, Log: log})
	if err := capture.SubscribeSys(g.dial(testcluster.SysUser, testcluster.SysPass, "lens-events")); err != nil {
		t.Fatal(err)
	}
	g.app = g.dial(testcluster.AppUser, testcluster.AppPass, "lens-account")
	if accountEvents {
		if err := capture.SubscribeAccount("APP", g.app); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan struct{})
	go func() { _ = capture.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	g.js, err = jetstream.New(g.dial(testcluster.AppUser, testcluster.AppPass, "setup"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Timeout = config.Duration(10 * time.Second)
	set, err := checks.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	g.r = &runner.Runner{
		Collector: collect.New(g.dial(testcluster.SysUser, testcluster.SysPass, "lens"), collect.Options{
			Window: time.Second, ConnzLimit: 100, Tracker: collect.NewTracker(time.Hour), Log: log}),
		Store: st, Interval: 10 * time.Second, Retention: time.Hour, Log: log,
		Checks: &checks.Engine{Set: set, Log: log},
	}
	_, g.cl = apitest.Serve(t, &storeprov.Provider{St: st, Interval: 10 * time.Second, Live: true, Checks: set})
	return g
}

// epoch takes one epoch and logs the changes of the firing set.
func (g *rig) epoch(label string) {
	g.t.Helper()
	time.Sleep(300 * time.Millisecond)
	e, err := g.r.Once(g.ctx)
	if err != nil {
		g.t.Fatal(err)
	}
	g.n++
	fs, err := g.st.Findings(g.ctx, time.Now(), store.FindingQuery{FiringOnly: true})
	if err != nil {
		g.t.Fatal(err)
	}
	cur := map[string]string{}
	for _, f := range fs {
		cur[f.ID] = f.Severity + " " + f.Detail
	}
	var lines []string
	for id, v := range cur {
		if g.last[id] != v {
			lines = append(lines, fmt.Sprintf("    + %s [%s]", id, v))
		}
	}
	for id := range g.last {
		if _, ok := cur[id]; !ok {
			lines = append(lines, fmt.Sprintf("    - %s resolved", id))
		}
	}
	sort.Strings(lines)
	g.last = cur
	g.t.Logf("EPOCH %d (%s)", e, label)
	for _, l := range lines {
		g.t.Log(l)
	}
}

func (g *rig) overview() {
	g.t.Helper()
	o, err := g.cl.GetOverview(g.ctx, connect.NewRequest(&v1.GetOverviewRequest{}))
	if err != nil {
		g.t.Fatal(err)
	}
	m := o.Msg
	g.t.Logf("OVERVIEW tally crit=%d warn=%d info=%d resolved24h=%d", m.GetTally().GetCritical(), m.GetTally().GetWarning(), m.GetTally().GetInfo(), m.GetTally().GetResolved_24H())
	if w := m.GetWorstFinding(); w != nil {
		g.t.Logf("OVERVIEW worst: %s %s since %s: %s", w.GetId(), w.GetSeverity(), w.GetSince().AsTime().Format("15:04:05"), w.GetDetail())
	} else {
		g.t.Log("OVERVIEW worst: none")
	}
	for _, s := range m.GetServers() {
		g.t.Logf("OVERVIEW server %s status=%v", s.GetName(), s.GetStatus())
	}
	for _, tl := range m.GetTiles() {
		g.t.Logf("OVERVIEW tile %s status=%v", tl.GetId(), tl.GetStatus())
	}
}

func (g *rig) detail(id string) {
	g.t.Helper()
	r, err := g.cl.GetFinding(g.ctx, connect.NewRequest(&v1.GetFindingRequest{Id: id}))
	if err != nil {
		g.t.Logf("DETAIL %s: %v", id, err)
		return
	}
	f := r.Msg.GetFinding()
	g.t.Logf("DETAIL %s sev=%v state=%v since=%s resolved=%v\n      detail: %s\n      remediation: %s\n      cause: %v\n      related: %v (%s)", id, f.GetSeverity(), f.GetState(),
		f.GetSince().AsTime().Format("15:04:05"), f.GetResolvedAt() != nil, f.GetDetail(), f.GetRemediation(), f.GetCause(), f.GetRelatedConfig(), f.GetRelatedConfigSource())
}

func (g *rig) stream(name string, replicas int, subjects string, extra func(*jetstream.StreamConfig)) jetstream.Stream {
	g.t.Helper()
	cfg := jetstream.StreamConfig{Name: name, Subjects: []string{subjects}, Replicas: replicas}
	if extra != nil {
		extra(&cfg)
	}
	s, err := g.js.CreateStream(g.ctx, cfg)
	if err != nil {
		g.t.Fatal(err)
	}
	return s
}
