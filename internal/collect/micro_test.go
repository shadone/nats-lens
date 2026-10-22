package collect

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"

	"github.com/shadone/nats-lens/internal/testcluster"
)

// microConf is one server with the account SHOP, whose users differ in
// what they may do with $SRV and reply inboxes, and the account EMPTY,
// which runs no service. These are test-only credentials.
const microConf = `
server_name: micro-0
listen: 127.0.0.1:%d
accounts {
  SYS { users: [ { user: sys, password: sys-test } ] }
  SHOP { users: [
    { user: shop, password: shop-test },
    { user: shop-asker, password: shop-asker-test, permissions: {
      publish: { allow: ["$SYS.REQ.USER.INFO", "$SRV.INFO", "$SRV.STATS"] },
      subscribe: { allow: ["_INBOX.>"] } } },
    { user: shop-nosrv, password: shop-nosrv-test, permissions: {
      publish: { allow: ["$SYS.REQ.USER.INFO"] },
      subscribe: { allow: ["_INBOX.>"] } } },
    { user: shop-noinbox, password: shop-noinbox-test, permissions: {
      publish: { allow: ["$SYS.REQ.USER.INFO", "$SRV.>"] },
      subscribe: { deny: ["_INBOX.>"] } } }
  ] }
  EMPTY { users: [ { user: empty, password: empty-test } ] }
}
system_account: SYS
`

// startMicroServer starts the server of microConf and returns its URL.
func startMicroServer(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "micro.conf")
	if err := os.WriteFile(path, fmt.Appendf(nil, microConf, testcluster.FreePort(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	opts, err := server.ProcessConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	opts.NoLog, opts.NoSigs = true, true
	s, err := server.NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	go s.Start()
	if !s.ReadyForConnections(10 * time.Second) {
		t.Fatal("server not ready")
	}
	t.Cleanup(s.Shutdown)
	return s.ClientURL()
}

func connectAs(t *testing.T, url, user string) *nats.Conn {
	t.Helper()
	nc, err := nats.Connect(url, nats.UserInfo(user, user+"-test"), nats.ErrorHandler(func(*nats.Conn, *nats.Subscription, error) {}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	return nc
}

// startOrders adds an instance of the service orders (version) on its own
// connection in SHOP: get on orders.get.* answers, create on
// orders.create answers with an error for the body "dup".
func startOrders(t *testing.T, url, version string) micro.Service {
	t.Helper()
	nc := connectAs(t, url, "shop")
	svc, err := micro.AddService(nc, micro.Config{Name: "orders", Version: version, Description: "Order intake",
		Metadata: map[string]string{"team": "checkout"}})
	if err != nil {
		t.Fatal(err)
	}
	g := svc.AddGroup("orders")
	if err := g.AddEndpoint("get", micro.HandlerFunc(func(r micro.Request) { _ = r.Respond([]byte("{}")) }),
		micro.WithEndpointSubject("get.*")); err != nil {
		t.Fatal(err)
	}
	if err := g.AddEndpoint("create", micro.HandlerFunc(func(r micro.Request) {
		if string(r.Data()) == "dup" {
			_ = r.Error("409", "duplicate order id", nil)
			return
		}
		_ = r.Respond([]byte("ok"))
	})); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop() })
	return svc
}

// microCollector returns a collector without a system connection whose
// clock reads *at, asking the given account connections.
func microCollector(window, interval time.Duration, at *time.Time, conns ...AccountConn) *Collector {
	c := New(nil, Options{Window: window, Tracker: NewTracker(time.Hour), SubscriptionsInterval: interval,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	c.now = func() time.Time { return *at }
	c.SetAccountConns(conns)
	return c
}

func fixedConn(label string, nc *nats.Conn) AccountConn {
	return AccountConn{Label: label, Conn: func() *nats.Conn { return nc }}
}

// askedOf returns the account's requests as "REQUEST state replies".
func askedOf(a MicroAccount) string {
	var out []string
	for _, r := range a.Asked {
		out = append(out, fmt.Sprintf("%s %s %d", r.Request, r.State, r.Replies))
	}
	return strings.Join(out, ", ")
}

func request(t *testing.T, nc *nats.Conn, subject, body string) {
	t.Helper()
	if _, err := nc.Request(subject, []byte(body), 2*time.Second); err != nil {
		t.Fatal(err)
	}
}

// Two instances of a real micro service answer INFO and STATS over the
// account connection: versions, descriptions, endpoints with subject and
// queue group, the counters of the requests made, the last error. The
// account is named as the server names it, and the replies end the
// requests long before the window does.
func TestMicroInfoAndStats(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	url := startMicroServer(t)
	startOrders(t, url, "1.4.2")
	startOrders(t, url, "1.4.2")
	client := connectAs(t, url, "shop")
	for range 3 {
		request(t, client, "orders.get.42", "")
	}
	request(t, client, "orders.create", "dup")
	request(t, client, "orders.create", "new")

	at := time.Now()
	c := microCollector(5*time.Second, 5*time.Minute, &at, fixedConn("shop-label", connectAs(t, url, "shop-asker")))
	start := time.Now()
	snap := c.Collect(context.Background())
	took := time.Since(start)
	if len(snap.Micro) != 1 {
		t.Fatalf("micro accounts = %d", len(snap.Micro))
	}
	m := snap.Micro[0]
	if m.Label != "shop-label" || m.Account != "SHOP" {
		t.Errorf("label %q account %q, want shop-label SHOP", m.Label, m.Account)
	}
	if got := askedOf(m); got != "INFO ok 2, STATS ok 2" {
		t.Errorf("asked = %s", got)
	}
	if len(m.Infos) != 2 || len(m.Stats) != 2 {
		t.Fatalf("infos %d stats %d", len(m.Infos), len(m.Stats))
	}
	in := m.Infos[0]
	if in.Name != "orders" || in.Version != "1.4.2" || in.Description != "Order intake" || in.Metadata["team"] != "checkout" {
		t.Errorf("info = %+v", in)
	}
	eps := map[string]micro.EndpointInfo{}
	for _, e := range in.Endpoints {
		eps[e.Name] = e
	}
	if eps["get"].Subject != "orders.get.*" || eps["get"].QueueGroup != "q" || eps["create"].Subject != "orders.create" {
		t.Errorf("endpoints = %+v", in.Endpoints)
	}
	var gets, creates, errs int
	var lastErr string
	for _, s := range m.Stats {
		if s.Started.IsZero() {
			t.Errorf("instance %s without started", s.ID)
		}
		for _, e := range s.Endpoints {
			switch e.Name {
			case "get":
				gets += e.NumRequests
			case "create":
				creates += e.NumRequests
				errs += e.NumErrors
				if e.LastError != "" {
					lastErr = e.LastError
				}
			}
			if e.Subject == "" || e.QueueGroup != "q" {
				t.Errorf("stats endpoint %s: subject %q queue %q", e.Name, e.Subject, e.QueueGroup)
			}
		}
	}
	if gets != 3 || creates != 2 || errs != 1 || lastErr != "409:duplicate order id" {
		t.Errorf("gets %d creates %d errors %d last error %q", gets, creates, errs, lastErr)
	}
	if took > 2*time.Second {
		t.Errorf("collect took %s with a 5s window: the requests did not end on their replies", took)
	}
}

// INFO goes out on the first epoch, not again within the subscription
// interval, again once it passed, and at once in an epoch whose STATS
// names an instance no INFO described. A stopped instance no longer
// answers STATS.
func TestMicroInfoInterval(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	url := startMicroServer(t)
	first := startOrders(t, url, "1.4.2")
	at := time.Now()
	c := microCollector(2*time.Second, 5*time.Minute, &at, fixedConn("SHOP", connectAs(t, url, "shop-asker")))
	ctx := context.Background()
	if got := askedOf(c.Collect(ctx).Micro[0]); got != "INFO ok 1, STATS ok 1" {
		t.Errorf("first epoch: %s", got)
	}
	at = at.Add(30 * time.Second)
	if got := askedOf(c.Collect(ctx).Micro[0]); got != "STATS ok 1" {
		t.Errorf("within the interval: %s", got)
	}
	// A new instance: STATS names it, INFO follows in the same epoch.
	startOrders(t, url, "1.5.0")
	at = at.Add(30 * time.Second)
	m := c.Collect(ctx).Micro[0]
	if got := askedOf(m); got != "INFO ok 2, STATS ok 2" {
		t.Errorf("new instance: %s", got)
	}
	at = at.Add(30 * time.Second)
	if got := askedOf(c.Collect(ctx).Micro[0]); got != "STATS ok 2" {
		t.Errorf("after the new instance was described: %s", got)
	}
	// The first instance stops: one STATS reply, and no INFO for it.
	if err := first.Stop(); err != nil {
		t.Fatal(err)
	}
	at = at.Add(30 * time.Second)
	m = c.Collect(ctx).Micro[0]
	if got := askedOf(m); got != "STATS ok 1" || m.Stats[0].Version != "1.5.0" {
		t.Errorf("after a stop: %s, %+v", got, m.Stats)
	}
	at = at.Add(5 * time.Minute)
	if got := askedOf(c.Collect(ctx).Micro[0]); got != "INFO ok 1, STATS ok 1" {
		t.Errorf("after the interval: %s", got)
	}
	// The clock steps back: INFO is due at once.
	at = at.Add(-time.Hour)
	if got := askedOf(c.Collect(ctx).Micro[0]); got != "INFO ok 1, STATS ok 1" {
		t.Errorf("after the clock stepped back: %s", got)
	}
}

// With the interval 0, INFO goes out only on the first epoch and for a
// new instance.
func TestMicroInfoIntervalZero(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	url := startMicroServer(t)
	startOrders(t, url, "1.4.2")
	at := time.Now()
	c := microCollector(2*time.Second, 0, &at, fixedConn("SHOP", connectAs(t, url, "shop")))
	ctx := context.Background()
	if got := askedOf(c.Collect(ctx).Micro[0]); got != "INFO ok 1, STATS ok 1" {
		t.Errorf("first epoch: %s", got)
	}
	at = at.Add(24 * time.Hour)
	if got := askedOf(c.Collect(ctx).Micro[0]); got != "STATS ok 1" {
		t.Errorf("a day later: %s", got)
	}
}

// A user that may not publish to $SRV gets each request recorded as an
// error naming the violation, at once rather than after the window; the
// epoch is not failed. A user that may not receive replies sends nothing.
// An account without services answers no responders; an account
// connection that is not connected records an error.
func TestMicroFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	url := startMicroServer(t)
	startOrders(t, url, "1.4.2")
	closed := connectAs(t, url, "shop")
	closed.Close()
	at := time.Now()
	c := microCollector(3*time.Second, 5*time.Minute, &at,
		fixedConn("nosrv", connectAs(t, url, "shop-nosrv")),
		fixedConn("noinbox", connectAs(t, url, "shop-noinbox")),
		fixedConn("empty", connectAs(t, url, "empty")),
		fixedConn("closed", closed),
		AccountConn{Label: "none", Conn: func() *nats.Conn { return nil }},
	)
	start := time.Now()
	snap := c.Collect(context.Background())
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("collect took %s with a 3s window", took)
	}
	got := map[string]MicroAccount{}
	for _, m := range snap.Micro {
		got[m.Label] = m
	}
	for label, want := range map[string]string{
		"nosrv": "INFO error 0, STATS error 0", "noinbox": "INFO error 0, STATS error 0",
		"empty":  "INFO no_responders 0, STATS no_responders 0",
		"closed": "INFO error 0, STATS error 0", "none": "INFO error 0, STATS error 0",
	} {
		if a := askedOf(got[label]); a != want {
			t.Errorf("%s: %s, want %s", label, a, want)
		}
	}
	for _, r := range got["nosrv"].Asked {
		if want := `Permissions Violation for Publish to "` + r.Request.Subject() + `"`; !strings.Contains(r.Error, want) {
			t.Errorf("nosrv %s error %q, want it to name %s", r.Request, r.Error, want)
		}
	}
	if got["nosrv"].Account != "SHOP" {
		t.Errorf("nosrv account %q: the lookup is permitted", got["nosrv"].Account)
	}
	for _, r := range got["noinbox"].Asked {
		if !strings.Contains(r.Error, "Permissions Violation for Subscription to") {
			t.Errorf("noinbox %s error %q", r.Request, r.Error)
		}
	}
	if got["noinbox"].Account != "noinbox" {
		t.Errorf("noinbox account %q: no reply can arrive, so the label stands", got["noinbox"].Account)
	}
	if got["empty"].Account != "EMPTY" {
		t.Errorf("empty account %q", got["empty"].Account)
	}
	for _, label := range []string{"closed", "none"} {
		for _, r := range got[label].Asked {
			if r.Error != "account connection not connected" {
				t.Errorf("%s %s error %q", label, r.Request, r.Error)
			}
		}
	}
	for _, e := range snap.Errors {
		if strings.Contains(e, "$SRV") {
			t.Errorf("snapshot error %q: a micro failure fails no epoch", e)
		}
	}
	// A refused request is asked again in the next epoch; INFO is not due.
	at = at.Add(30 * time.Second)
	snap = c.Collect(context.Background())
	if a := askedOf(snap.Micro[4]); snap.Micro[4].Label != "nosrv" || a != "STATS error 0" {
		t.Errorf("next epoch %s: %s", snap.Micro[4].Label, a)
	}
	// The connection that was not connected asks INFO once it is.
	if a := askedOf(snap.Micro[3]); snap.Micro[3].Label != "none" || a != "INFO error 0, STATS error 0" {
		t.Errorf("next epoch %s: %s", snap.Micro[3].Label, a)
	}
}

// The README's grants for an account user (testcluster's app-scoped user)
// are enough to ask micro services and look up the account.
func TestMicroScopedUser(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	cl := testcluster.Start(t, 1)
	svcConn := cl.Connect(0, testcluster.AppUser, testcluster.AppPass)
	svc, err := micro.AddService(svcConn, micro.Config{Name: "pricing", Version: "2.0.0",
		Endpoint: &micro.EndpointConfig{Subject: "pricing.quote", Handler: micro.HandlerFunc(func(r micro.Request) { _ = r.Respond(nil) })}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Stop() }()
	at := time.Now()
	c := microCollector(2*time.Second, 5*time.Minute, &at,
		fixedConn("app", cl.Connect(0, testcluster.ScopedAppUser, testcluster.ScopedAppPass)))
	m := c.Collect(context.Background()).Micro[0]
	if got := askedOf(m); got != "INFO ok 1, STATS ok 1" || m.Account != "APP" {
		t.Errorf("asked %s, account %q", got, m.Account)
	}
}

// Replies that are not the framework's, and a second reply of one
// instance, are counted or dropped by decodeStats.
func TestMicroDecodeStats(t *testing.T) {
	good := `{"type":"io.nats.micro.v1.stats_response","name":"orders","id":"A","version":"1","endpoints":[]}`
	x := &exchanged{replies: [][]byte{[]byte(good), []byte(good), []byte(`{"type":"other","name":"x","id":"y"}`), []byte(`nope`)}}
	a, stats := decodeStats(x)
	if a.State != MicroOK || a.Replies != 1 || len(stats) != 1 || !strings.HasPrefix(a.Error, "2 of 3 replies did not decode") {
		t.Errorf("asked %+v, stats %d", a, len(stats))
	}
	a, _ = decodeStats(&exchanged{replies: [][]byte{[]byte(`nope`)}})
	if a.State != MicroError {
		t.Errorf("only undecodable: %+v", a)
	}
	a, _ = decodeStats(&exchanged{err: errors.New("boom")})
	if a.State != MicroError || a.Error != "boom" {
		t.Errorf("error: %+v", a)
	}
}

// An endpoint whose STATS reply has no subject takes it from the newest
// INFO.
func TestMicroSubjectFromInfo(t *testing.T) {
	st := &microAccount{endpoints: map[instanceKey][]micro.EndpointInfo{}}
	id := micro.ServiceIdentity{Name: "orders", ID: "A"}
	describe(st, []micro.Info{{ServiceIdentity: id, Endpoints: []micro.EndpointInfo{{Name: "get", Subject: "orders.get.*", QueueGroup: "q"}}}})
	stats := []micro.Stats{{ServiceIdentity: id, Endpoints: []*micro.EndpointStats{{Name: "get"}, {Name: "other"}}}}
	fillEndpointSubjects(st, stats)
	if e := stats[0].Endpoints[0]; e.Subject != "orders.get.*" || e.QueueGroup != "q" {
		t.Errorf("get = %+v", e)
	}
	if e := stats[0].Endpoints[1]; e.Subject != "" {
		t.Errorf("other = %+v", e)
	}
}

// 100 instances of 20 endpoints each answer STATS with one reply each,
// decoded within the window.
func TestMicroManyInstances(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server and 100 services")
	}
	url := startMicroServer(t)
	conns := make([]*nats.Conn, 10)
	for i := range conns {
		conns[i] = connectAs(t, url, "shop")
	}
	for i := range 100 {
		svc, err := micro.AddService(conns[i%len(conns)], micro.Config{Name: fmt.Sprintf("svc%02d", i/10), Version: "1.0.0"})
		if err != nil {
			t.Fatal(err)
		}
		for e := range 20 {
			if err := svc.AddEndpoint(fmt.Sprintf("op%02d", e), micro.HandlerFunc(func(r micro.Request) { _ = r.Respond(nil) }),
				micro.WithEndpointSubject(fmt.Sprintf("shop.svc%03d.op%02d", i, e))); err != nil {
				t.Fatal(err)
			}
		}
		t.Cleanup(func() { _ = svc.Stop() })
	}
	asker := connectAs(t, url, "shop")
	at := time.Now()
	c := microCollector(5*time.Second, 5*time.Minute, &at, fixedConn("SHOP", asker))
	in0 := asker.Stats().InBytes
	start := time.Now()
	m := c.Collect(context.Background()).Micro[0]
	took := time.Since(start)
	bytes := asker.Stats().InBytes - in0
	if got := askedOf(m); got != "INFO ok 100, STATS ok 100" {
		t.Fatalf("asked %s", got)
	}
	endpoints := 0
	for _, s := range m.Stats {
		endpoints += len(s.Endpoints)
	}
	if endpoints != 2000 {
		t.Errorf("endpoints = %d", endpoints)
	}
	t.Logf("100 instances x 20 endpoints: INFO and STATS gathered and decoded in %s, %d bytes received", took, bytes)
	at = at.Add(30 * time.Second)
	in0 = asker.Stats().InBytes
	start = time.Now()
	m = c.Collect(context.Background()).Micro[0]
	t.Logf("STATS alone: %s, %d bytes received, %d replies", time.Since(start), asker.Stats().InBytes-in0, m.Asked[0].Replies)
	x := exchange(context.Background(), asker, newMicroWatch(), map[string]string{"S": MicroStats.Subject()}, time.Now().Add(5*time.Second), microQuiet)
	start = time.Now()
	_, stats := decodeStats(x["S"])
	t.Logf("decoding %d STATS replies took %s", len(stats), time.Since(start))
}
