package collect_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/testcluster"
)

// fake is a responder on the system account that answers STATSZ as a
// server that does not exist, to control who replies and when.
type fake struct {
	name, id string
	delay    time.Duration
	dup      bool
}

func startFakes(t *testing.T, nc *nats.Conn, fakes []fake) {
	for _, f := range fakes {
		msg, _ := json.Marshal(server.ServerStatsMsg{Server: server.ServerInfo{Name: f.name, ID: f.id}})
		_, err := nc.Subscribe("$SYS.REQ.SERVER.PING.STATSZ", func(m *nats.Msg) {
			go func() {
				time.Sleep(f.delay)
				_ = m.Respond(msg)
				if f.dup {
					_ = m.Respond(msg)
				}
			}()
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	_ = nc.Flush()
}

func newCol(nc *nats.Conn, known ...string) *collect.Collector {
	tr := collect.NewTracker(time.Hour)
	for _, k := range known {
		tr.Seed(k, time.Now())
	}
	return collect.New(nc, collect.Options{Window: time.Second, ConnzLimit: 10, Tracker: tr,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
}

// Six names are known; a, b and the unknown d answer at once and c after
// 300ms. Six replies arrive before c, so counting replies stops too early.
func TestDiscoveryWaitsForEveryKnownName(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a 3-node cluster")
	}
	c := testcluster.Start(t, 3)
	nc := c.Connect(0, testcluster.SysUser, testcluster.SysPass)
	startFakes(t, nc, []fake{{"a", "IDA", 0, false}, {"b", "IDB", 0, false}, {"d", "IDD", 0, false}, {"c", "IDC", 300 * time.Millisecond, false}})
	snap := newCol(nc, "nats-0", "nats-1", "nats-2", "a", "b", "c").Collect(context.Background())
	if !slices.Contains(snap.Observed(), "c") {
		t.Errorf("c dropped: observed %v missing %v", snap.Observed(), snap.Missing())
	}
}

// a answers twice and b after 300ms: a duplicate reply must not stand in
// for b.
func TestDiscoveryCountsAServerOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a 3-node cluster")
	}
	c := testcluster.Start(t, 3)
	nc := c.Connect(0, testcluster.SysUser, testcluster.SysPass)
	startFakes(t, nc, []fake{{"a", "IDA", 0, true}, {"b", "IDB", 300 * time.Millisecond, false}})
	snap := newCol(nc, "nats-0", "nats-1", "nats-2", "a", "b").Collect(context.Background())
	if !slices.Contains(snap.Observed(), "b") {
		t.Errorf("b dropped: observed %v missing %v", snap.Observed(), snap.Missing())
	}
}

// A responder that answers discovery but no detail request makes every
// detail pass wait its whole window. Once the JetStream accounts are known
// from the previous epoch, their RAFTZ requests ride in the detail pass, so
// such an epoch waits one window, not two.
func TestKnownAccountsSkipTheSecondPass(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a 3-node cluster")
	}
	ctx := context.Background()
	c := testcluster.Start(t, 3)
	js, err := jetstream.New(c.Connect(0, testcluster.AppUser, testcluster.AppPass))
	if err != nil {
		t.Fatal(err)
	}
	createAssets(t, ctx, js)
	nc := c.Connect(0, testcluster.SysUser, testcluster.SysPass)
	startFakes(t, nc, []fake{{"ghost", "IDGHOST", 0, false}})
	col := newCol(nc, "nats-0", "nats-1", "nats-2", "ghost")
	_ = col.Collect(ctx)
	snap := col.Collect(ctx)
	if d := snap.FinishedAt.Sub(snap.StartedAt); d > 1500*time.Millisecond {
		t.Errorf("second epoch took %s, want about one 1s window", d)
	}
	if st := snap.Servers["IDGHOST"].Status[collect.AccountRaftz("APP")]; st.State != collect.StateMissing {
		t.Errorf("ghost RAFTZ:APP = %+v, want missing", st)
	}
}

// An account whose last stream was deleted is dropped from the RAFTZ
// requests in the first epoch that shows it gone (every server answered
// JSZ without it), so no server is recorded as missing that request.
func TestGoneAccountIsNotRecordedMissing(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a 3-node cluster")
	}
	ctx := context.Background()
	c := testcluster.Start(t, 3)
	js, err := jetstream.New(c.Connect(0, testcluster.AppUser, testcluster.AppPass))
	if err != nil {
		t.Fatal(err)
	}
	// Without a deadline of its own a JetStream call gives up after 5s,
	// which an R3 stream create can take on a loaded machine.
	jsCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if _, err := js.CreateStream(jsCtx, jetstream.StreamConfig{Name: "catalog-changes", Subjects: []string{"catalog.changes.>"}, Replicas: 3}); err != nil {
		t.Fatal(err)
	}
	waitStreamOnEveryServer(t, c, "catalog-changes", true)
	nc := c.Connect(0, testcluster.SysUser, testcluster.SysPass)
	// The ghost answers JSZ (without the account) but not RAFTZ, so every
	// server answers JSZ and the ghost misses the account's RAFTZ.
	ghost := &jszFake{name: "ghost", id: "IDGHOST"}
	ghost.answerJSZ.Store(true)
	ghost.start(t, nc)
	col := newCol(nc, "nats-0", "nats-1", "nats-2", "ghost")
	snap := col.Collect(ctx)
	if st := snap.Servers["IDGHOST"].Status[collect.AccountRaftz("APP")]; st.State != collect.StateMissing {
		t.Fatalf("setup: ghost RAFTZ:APP = %+v, want missing", st)
	}
	if err := js.DeleteStream(jsCtx, "catalog-changes"); err != nil {
		t.Fatal(err)
	}
	// The test is about the first epoch in which every server shows the
	// account gone; a replica that still holds the stream is not that.
	waitStreamOnEveryServer(t, c, "catalog-changes", false)
	snap = col.Collect(ctx)
	for _, v := range snap.Servers {
		if st, ok := v.Status[collect.AccountRaftz("APP")]; ok {
			t.Errorf("%s records RAFTZ:APP as %s after the account's last stream was deleted", v.Info.Name, st.State)
		}
	}
}

// waitStreamOnEveryServer waits until every server of c reports the APP
// stream name in its JSZ when present is set, or none does when it is not.
func waitStreamOnEveryServer(t *testing.T, c *testcluster.Cluster, name string, present bool) {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for {
		var have int
		for _, s := range c.Servers {
			z, err := s.Jsz(&server.JSzOptions{Accounts: true, Streams: true})
			if err != nil {
				t.Fatal(err)
			}
			for _, a := range z.AccountDetails {
				if a.Id == "APP" && slices.ContainsFunc(a.Streams, func(d server.StreamDetail) bool { return d.Name == name }) {
					have++
				}
			}
		}
		if (present && have == len(c.Servers)) || (!present && have == 0) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("stream %s on %d of %d servers, want present=%v on all", name, have, len(c.Servers), present)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
