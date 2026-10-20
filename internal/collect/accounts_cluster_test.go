package collect_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"

	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/testcluster"
)

func quietCollector(nc *nats.Conn) *collect.Collector {
	return collect.New(nc, collect.Options{
		Window: time.Second, ConnzLimit: 100, Tracker: collect.NewTracker(time.Hour),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func detailsOf(snap *collect.Snapshot) map[string]collect.AccountDetails {
	out := map[string]collect.AccountDetails{}
	for _, d := range snap.AccountDetails {
		out[d.Account] = d
	}
	return out
}

// In config mode every account is looked up: no JWT, the identifier as
// the name, no imports beyond the ones the server adds itself; the next
// epoch asks none of them again.
func TestAccountDetailsFromConfigModeCluster(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a 3-node cluster")
	}
	ctx := context.Background()
	c := testcluster.Start(t, 3)
	c.Connect(0, testcluster.AppUser, testcluster.AppPass)
	col := quietCollector(c.Connect(0, testcluster.SysUser, testcluster.SysPass))
	snap := col.Collect(ctx)
	got := detailsOf(snap)
	app, ok := got["APP"]
	if !ok {
		t.Fatalf("APP not looked up: %+v", snap.AccountDetails)
	}
	if app.HasJWT || app.Name != "APP" || app.Imports != 0 || app.IsSystem || app.ServerID == "" {
		t.Errorf("APP details %+v", app)
	}
	if sys := got["SYS"]; !sys.IsSystem {
		t.Errorf("SYS details %+v, want the system account", sys)
	}
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.WriteSnapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.DB().QueryRow(`SELECT count(*) FROM account_details WHERE account = 'APP' AND NOT has_jwt AND limit_connections IS NULL`).Scan(&n); err != nil || n != 1 {
		t.Errorf("APP account_details rows = %d, %v", n, err)
	}
	if err := st.DB().QueryRow(`SELECT count(*) FROM connections WHERE account = 'APP' AND NOT user_jwt AND jwt_expires IS NULL`).Scan(&n); err != nil || n == 0 {
		t.Errorf("APP connections without a user JWT = %d, %v", n, err)
	}
	if again := col.Collect(ctx); len(again.AccountDetails) != 0 {
		t.Errorf("accounts looked up again within the interval: %+v", again.AccountDetails)
	}
}

// operator is a trusted operator with a system account, for a server in
// operator mode with accounts resolved from memory.
type operator struct {
	t        *testing.T
	kp       nkeys.KeyPair
	resolver *server.MemAccResolver
}

func newAccount(t *testing.T) (nkeys.KeyPair, string) {
	t.Helper()
	kp, err := nkeys.CreateAccount()
	if err != nil {
		t.Fatal(err)
	}
	pub, err := kp.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	return kp, pub
}

// store signs an account JWT and makes it resolvable.
func (o *operator) store(ac *jwt.AccountClaims) {
	o.t.Helper()
	tok, err := ac.Encode(o.kp)
	if err != nil {
		o.t.Fatal(err)
	}
	if err := o.resolver.Store(ac.Subject, tok); err != nil {
		o.t.Fatal(err)
	}
}

// userCreds returns a connect option for a new user of the account.
func userCreds(t *testing.T, account nkeys.KeyPair, name string, expires time.Time) nats.Option {
	t.Helper()
	ukp, err := nkeys.CreateUser()
	if err != nil {
		t.Fatal(err)
	}
	upub, _ := ukp.PublicKey()
	seed, _ := ukp.Seed()
	uc := jwt.NewUserClaims(upub)
	uc.Name = name
	if !expires.IsZero() {
		uc.Expires = expires.Unix()
	}
	tok, err := uc.Encode(account)
	if err != nil {
		t.Fatal(err)
	}
	return nats.UserJWTAndSeed(tok, string(seed))
}

// In operator mode the lookups read each account JWT's claims, and the
// connections carry their user JWT's expiry; no JWT is stored.
func TestAccountDetailsFromOperatorModeServer(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	ctx := context.Background()
	okp, err := nkeys.CreateOperator()
	if err != nil {
		t.Fatal(err)
	}
	opub, _ := okp.PublicKey()
	sysKP, sysPub := newAccount(t)
	appKP, appPub := newAccount(t)
	_, supPub := newAccount(t)
	oc := jwt.NewOperatorClaims(opub)
	oc.Name = "example"
	oc.SystemAccount = sysPub
	otok, err := oc.Encode(okp)
	if err != nil {
		t.Fatal(err)
	}
	trusted, err := jwt.DecodeOperatorClaims(otok)
	if err != nil {
		t.Fatal(err)
	}
	o := &operator{t: t, kp: okp, resolver: &server.MemAccResolver{}}
	now := time.Now().UTC().Truncate(time.Second)

	sys := jwt.NewAccountClaims(sysPub)
	sys.Name = "SYS"
	o.store(sys)
	sup := jwt.NewAccountClaims(supPub)
	sup.Name = "SUPPLIERS"
	sup.Expires = now.Add(12 * 24 * time.Hour).Unix()
	sup.Exports = jwt.Exports{&jwt.Export{Subject: "inventory.changes", Type: jwt.Stream}}
	o.store(sup)
	app := jwt.NewAccountClaims(appPub)
	app.Name = "APP"
	app.Limits.Conn = 2000
	app.Limits.DiskStorage = 500 << 30
	app.Limits.MemoryStorage = -1
	app.Imports.Add(&jwt.Import{Account: supPub, Subject: "inventory.changes", Type: jwt.Stream})
	o.store(app)

	srv, err := server.NewServer(&server.Options{
		ServerName: "nats-0", Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true,
		TrustedOperators: []*jwt.OperatorClaims{trusted}, SystemAccount: sysPub, AccountResolver: o.resolver,
	})
	if err != nil {
		t.Fatal(err)
	}
	go srv.Start()
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("server not ready")
	}
	connect := func(opt nats.Option) *nats.Conn {
		nc, err := nats.Connect(srv.ClientURL(), opt)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(nc.Close)
		return nc
	}
	userExpires := now.Add(5 * 24 * time.Hour)
	for range 3 {
		connect(userCreds(t, appKP, "pos-terminal", userExpires))
	}
	col := quietCollector(connect(userCreds(t, sysKP, "natslens", time.Time{})))

	snap := col.Collect(ctx)
	got := detailsOf(snap)
	a, ok := got[appPub]
	if !ok {
		t.Fatalf("APP not looked up: %+v", snap.AccountDetails)
	}
	if !a.HasJWT || a.Name != "APP" || !a.JWTExpires.IsZero() || a.JWTIssuedAt.IsZero() || a.Imports != 1 ||
		a.Limits.Connections != 2000 || a.Limits.JSDisk != 500<<30 || a.Limits.JSMemory != -1 || a.Limits.Subscriptions != -1 {
		t.Errorf("APP details %+v", a)
	}
	if s, ok := got[supPub]; ok {
		if s.Name != "SUPPLIERS" || !s.JWTExpires.Equal(now.Add(12*24*time.Hour)) || s.Exports != 1 || s.Expired {
			t.Errorf("SUPPLIERS details %+v", s)
		}
	} else {
		t.Errorf("SUPPLIERS, which APP imports from, not looked up: %+v", snap.AccountDetails)
	}
	if s := got[sysPub]; !s.IsSystem || !s.HasJWT {
		t.Errorf("SYS details %+v", s)
	}

	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.WriteSnapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	var n int
	var exp sql.NullTime
	if err := st.DB().QueryRow(`SELECT count(*), min(jwt_expires) FROM connections WHERE account = ? AND user_jwt`, appPub).
		Scan(&n, &exp); err != nil {
		t.Fatal(err)
	}
	if n != 3 || !exp.Time.Equal(userExpires) {
		t.Errorf("APP connections with a user JWT = %d expiring %v, want 3 expiring %s", n, exp, userExpires)
	}
	if err := st.DB().QueryRow(`SELECT count(*) FROM connections WHERE strpos(CAST(connections AS VARCHAR), 'eyJ') > 0`).Scan(&n); err != nil || n != 0 {
		t.Errorf("connection rows holding a JWT = %d, %v", n, err)
	}
	if err := st.DB().QueryRow(`SELECT count(*) FROM account_details WHERE strpos(CAST(account_details AS VARCHAR), 'eyJ') > 0`).Scan(&n); err != nil || n != 0 {
		t.Errorf("account_details rows holding a JWT = %d, %v", n, err)
	}
	if err := st.DB().QueryRow(`SELECT count(*) FROM account_details WHERE account = ? AND limit_connections = 2000 AND jwt_expires IS NULL`, appPub).Scan(&n); err != nil || n != 1 {
		t.Errorf("APP account_details rows = %d, %v", n, err)
	}
}
