package api_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

var accEpochAt = time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)

const opKey = "ADKEYFAKEACCOUNTEXAMPLE000000000000000000000000000000001"

// A range without an epoch gives the empty response.
func TestFinishAccountsNoEpoch(t *testing.T) {
	for _, f := range []*api.AccountsFacts{nil, {}, {Accounts: []api.AccountFact{{Account: "APP"}}}} {
		if res := api.FinishAccounts(f); res.GetEpoch() != 0 || len(res.GetAccounts()) != 0 || res.GetEpochAt() != nil {
			t.Errorf("FinishAccounts(%v) = %v", f, res)
		}
	}
}

// Operator mode follows an account public key; the system account sorts
// last, the others by connections, then by name; unknown connections
// count as none.
func TestFinishAccountsOrderAndMode(t *testing.T) {
	f := &api.AccountsFacts{Epoch: 7, EpochAt: accEpochAt, Accounts: []api.AccountFact{
		{Account: "SYS", IsSystem: true, Connections: i32(90)},
		{Account: "b", Connections: i32(5)},
		{Account: "a", Connections: i32(5)},
		{Account: "z", Name: "c-name", Connections: i32(5)},
		{Account: "none"},
		{Account: "busy", Connections: i32(40)},
	}}
	res := api.FinishAccounts(f)
	if res.GetOperatorMode() {
		t.Error("config-mode identifiers read as operator mode")
	}
	var got []string
	for _, a := range res.GetAccounts() {
		got = append(got, a.GetAccount())
	}
	if want := []string{"busy", "a", "b", "z", "none", "SYS"}; !slices.Equal(got, want) {
		t.Errorf("order %v, want %v", got, want)
	}
	f.Accounts = append(f.Accounts, api.AccountFact{Account: opKey})
	if !api.FinishAccounts(f).GetOperatorMode() {
		t.Error("an account key does not make operator mode")
	}
	if api.IsAccountKey("APP") || api.IsAccountKey("N"+opKey[1:]) || !api.IsAccountKey(opKey) {
		t.Error("IsAccountKey")
	}
}

// At most 500 accounts are listed, with the total.
func TestFinishAccountsLimit(t *testing.T) {
	f := &api.AccountsFacts{Epoch: 1, EpochAt: accEpochAt}
	for i := range 620 {
		f.Accounts = append(f.Accounts, api.AccountFact{Account: fmt.Sprintf("acc-%04d", i), Connections: i32(int32(i))})
	}
	res := api.FinishAccounts(f)
	if len(res.GetAccounts()) != api.MaxAccountRows || res.GetAccountsTotal() != 620 || res.GetAccounts()[0].GetAccount() != "acc-0619" {
		t.Errorf("%d accounts of %d, first %s", len(res.GetAccounts()), res.GetAccountsTotal(), res.GetAccounts()[0].GetAccount())
	}
}

// A lookup at most 24 hours older than the epoch fills the details; an
// older one, or one after the epoch, reads as not looked up yet. Limits
// come from a JWT only.
func TestFinishAccountsDetails(t *testing.T) {
	exp := accEpochAt.Add(40 * 24 * time.Hour)
	jwt := &api.AccountDetails{At: accEpochAt.Add(-24 * time.Hour), HasJWT: true, Imports: 2, Exports: 1, JWTExpires: &exp,
		LimitConnections: i64(2000), LimitJSDisk: i64(-1), LimitJSMemory: i64(0)}
	f := &api.AccountsFacts{Epoch: 1, EpochAt: accEpochAt, Accounts: []api.AccountFact{
		{Account: "jwt", Details: jwt},
		{Account: "old", Details: &api.AccountDetails{At: accEpochAt.Add(-24*time.Hour - time.Second), HasJWT: true, LimitConnections: i64(5)}},
		{Account: "future", Details: &api.AccountDetails{At: accEpochAt.Add(time.Second)}},
		{Account: "config", Details: &api.AccountDetails{At: accEpochAt, LimitConnections: i64(9)}},
		{Account: "disabled", Details: &api.AccountDetails{At: accEpochAt, HasJWT: true, LimitJSDisk: i64(0), LimitJSMemory: i64(0)}},
		{Account: "memory-only", Details: &api.AccountDetails{At: accEpochAt, HasJWT: true, LimitJSDisk: i64(0), LimitJSMemory: i64(1 << 30)}},
	}}
	by := map[string]*v1.AccountRow{}
	for _, a := range api.FinishAccounts(f).GetAccounts() {
		by[a.GetAccount()] = a
	}
	j := by["jwt"]
	if !j.GetDetailsKnown() || !j.GetHasJwt() || j.GetImports() != 2 || j.GetExports() != 1 || j.GetConnLimit() != 2000 ||
		j.GetDiskLimit() != -1 || !j.GetJwtExpires().AsTime().Equal(exp) || !j.GetDetailsAt().AsTime().Equal(jwt.At) {
		t.Errorf("jwt = %v", j)
	}
	// Disk 0 tells JetStream disabled (memory 0 too) from no file storage
	// allowed (memory only) by jetstream_enabled; -1 is no limit.
	if j.JetstreamEnabled == nil || !j.GetJetstreamEnabled() {
		t.Errorf("jwt: jetstream_enabled %v, want true", j.JetstreamEnabled)
	}
	if d := by["disabled"]; d.DiskLimit == nil || d.GetDiskLimit() != 0 || d.JetstreamEnabled == nil || d.GetJetstreamEnabled() {
		t.Errorf("disabled = %v", d)
	}
	if m := by["memory-only"]; m.DiskLimit == nil || m.GetDiskLimit() != 0 || !m.GetJetstreamEnabled() {
		t.Errorf("memory-only = %v", m)
	}
	for _, name := range []string{"old", "future"} {
		a := by[name]
		if a.GetDetailsKnown() || a.Imports != nil || a.Exports != nil || a.ConnLimit != nil || a.GetDetailsAt() != nil || a.GetHasJwt() {
			t.Errorf("%s = %v, want not looked up yet", name, a)
		}
	}
	if c := by["config"]; !c.GetDetailsKnown() || c.ConnLimit != nil || c.DiskLimit != nil || c.GetImports() != 0 || c.Imports == nil {
		t.Errorf("config = %v", c)
	}
	if c := by["config"]; c.JetstreamEnabled != nil || by["old"].JetstreamEnabled != nil {
		t.Errorf("jetstream_enabled set without a known JWT: %v, %v", c, by["old"])
	}
}

// Failed logins: most first, then the newest; at most 100 groups, with
// the totals the provider counted; account names from the accounts.
func TestFinishAccountsFailedLogins(t *testing.T) {
	f := &api.AccountsFacts{Epoch: 1, EpochAt: accEpochAt, WindowStart: accEpochAt.Add(-api.FailedLoginWindow),
		Accounts: []api.AccountFact{{Account: opKey, Name: "SHOP"}},
		FailedLogins: []api.FailedLoginFact{
			{User: "old", Count: 5, Last: accEpochAt.Add(-time.Hour)},
			{User: "new", Count: 5, Last: accEpochAt.Add(-time.Minute), Account: opKey},
			{User: "many", Count: 1440, Last: accEpochAt.Add(-2 * time.Hour)},
		}, FailedLoginGroups: 250, FailedLoginsCount: 4000, CaptureGaps: true}
	res := api.FinishAccounts(f)
	var got []string
	for _, l := range res.GetFailedLogins() {
		got = append(got, l.GetUser())
	}
	if want := []string{"many", "new", "old"}; !slices.Equal(got, want) {
		t.Errorf("order %v, want %v", got, want)
	}
	if res.GetFailedLogins()[1].GetAccountName() != "SHOP" || res.GetFailedLoginGroupsTotal() != 250 || res.GetFailedLoginsTotal() != 4000 ||
		!res.GetCaptureGaps() || !res.GetFailedLoginsWindowStart().AsTime().Equal(f.WindowStart) {
		t.Errorf("res = %v", res)
	}
	f.FailedLogins, f.FailedLoginGroups, f.FailedLoginsCount = nil, 0, 0
	for i := range 130 {
		f.FailedLogins = append(f.FailedLogins, api.FailedLoginFact{Host: fmt.Sprint(i), Count: int64(i + 1), Last: accEpochAt})
	}
	res = api.FinishAccounts(f)
	if len(res.GetFailedLogins()) != api.MaxFailedLoginGroups || res.GetFailedLoginGroupsTotal() != 130 || res.GetFailedLoginsTotal() != 130*131/2 {
		t.Errorf("%d groups of %d, %d logins", len(res.GetFailedLogins()), res.GetFailedLoginGroupsTotal(), res.GetFailedLoginsTotal())
	}
}

// Expiring: what has expired or expires within 30 days, expired first,
// then the soonest; later expiries and none are counted. Account JWTs of
// accounts not looked up are left out.
func TestFinishAccountsExpiring(t *testing.T) {
	in := func(d time.Duration) *time.Time { t := accEpochAt.Add(d); return &t }
	day := 24 * time.Hour
	f := &api.AccountsFacts{Epoch: 1, EpochAt: accEpochAt, Accounts: []api.AccountFact{
		{Account: "soon", Name: "Soon", Details: &api.AccountDetails{At: accEpochAt, HasJWT: true, JWTExpires: in(12 * day)}},
		{Account: "never", Details: &api.AccountDetails{At: accEpochAt, HasJWT: true}},
		{Account: "flagged", Details: &api.AccountDetails{At: accEpochAt, HasJWT: true, Expired: true}},
		{Account: "stale", Details: &api.AccountDetails{At: accEpochAt.Add(-48 * time.Hour), HasJWT: true, JWTExpires: in(day)}},
		{Account: "config", Details: &api.AccountDetails{At: accEpochAt}},
	}, Certificates: []api.CertificateFact{
		{Server: "nats-0", Listener: api.ListenerClient, NotAfter: *in(16 * day)},
		{Server: "nats-1", Listener: api.ListenerClient, NotAfter: *in(-day)},
		{Server: "nats-1", Listener: api.ListenerCluster, NotAfter: *in(30 * day)},
		{Server: "nats-2", Listener: api.ListenerClient, NotAfter: *in(30*day + time.Second)},
		{Server: "nats-2", Listener: api.ListenerCluster, NotAfter: accEpochAt},
	}, UserJWTs: []api.UserJWTFact{
		{Account: "soon", User: "pos-terminal", Expires: in(5 * day), Connections: 3},
		{Account: "soon", User: "app"},
		{Account: "soon", User: "later", Expires: in(90 * day), Connections: 9},
	}}
	res := api.FinishAccounts(f)
	var got []string
	for _, e := range res.GetExpiring() {
		got = append(got, fmt.Sprintf("%s %s%s%s%s %v %d", e.GetKind(), e.GetServer(), e.GetListener(), e.GetAccountName(), e.GetUser(),
			e.GetExpired(), e.GetConnections()))
	}
	want := []string{
		"EXPIRING_KIND_ACCOUNT_JWT  true 0",
		"EXPIRING_KIND_SERVER_CERTIFICATE nats-1client true 0",
		"EXPIRING_KIND_SERVER_CERTIFICATE nats-2cluster true 0",
		"EXPIRING_KIND_USER_JWT Soonpos-terminal false 3",
		"EXPIRING_KIND_ACCOUNT_JWT Soon false 0",
		"EXPIRING_KIND_SERVER_CERTIFICATE nats-0client false 0",
		"EXPIRING_KIND_SERVER_CERTIFICATE nats-1cluster false 0",
	}
	if !slices.Equal(got, want) {
		t.Errorf("expiring\n%v\nwant\n%v", got, want)
	}
	if res.GetExpiring()[0].GetExpires() != nil || res.GetExpiring()[0].GetAccount() != "flagged" {
		t.Errorf("flagged = %v", res.GetExpiring()[0])
	}
	if res.GetExpireLater() != 2 || res.GetNeverExpire() != 2 || res.GetExpiringTotal() != 7 {
		t.Errorf("later %d, never %d, total %d", res.GetExpireLater(), res.GetNeverExpire(), res.GetExpiringTotal())
	}
	// At most 100 rows, with the total.
	for i := range 150 {
		f.Certificates = append(f.Certificates, api.CertificateFact{Server: fmt.Sprintf("s-%03d", i), Listener: api.ListenerClient, NotAfter: *in(day)})
	}
	if res := api.FinishAccounts(f); len(res.GetExpiring()) != api.MaxExpiringRows || res.GetExpiringTotal() != 157 {
		t.Errorf("%d expiring of %d", len(res.GetExpiring()), res.GetExpiringTotal())
	}
}
