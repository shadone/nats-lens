package fake_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func accountsAndAuth(t *testing.T, system string) *v1.GetAccountsAndAuthResponse {
	t.Helper()
	return must(demo(t).GetAccountsAndAuth(ctx, connect.NewRequest(&v1.GetAccountsAndAuthRequest{System: system})))
}

// example-global runs in operator mode: APP with limits and a JWT without
// expiry, the idle SUPPLIERS whose JWT expires in 12 days, SYS last; the
// revoked label-printer and the timeouts from 203.0.113.12; nats-eu-0's
// client certificate and the pos-terminal user JWT.
func TestGlobalAccountsAndAuth(t *testing.T) {
	res := accountsAndAuth(t, fake.GlobalSystem)
	if !res.GetOperatorMode() || res.GetCaptureGaps() || res.GetAccountsTotal() != 3 {
		t.Fatalf("res = %v", res)
	}
	var accs []string
	for _, a := range res.GetAccounts() {
		accs = append(accs, fmt.Sprintf("%s %s %d %d/%d js %v %v %v", a.GetAccount()[:3], a.GetAccountName(), a.GetConnections(), a.GetConnLimit(),
			a.GetDiskLimit(), a.GetJetstreamEnabled(), a.GetJwtExpires() != nil, a.GetIsSystem()))
	}
	want := []string{"APP  1142 2000/536870912000 js true false false", "ADS SUPPLIERS 0 50/0 js false true false", "SYS  0 -1/0 js false false true"}
	if !slices.Equal(accs, want) {
		t.Errorf("accounts %v, want %v", accs, want)
	}
	if app := res.GetAccounts()[0]; app.GetJsStorage() >= app.GetDiskLimit() || app.GetStreams() != 16 || app.GetImports() != 1 {
		t.Errorf("APP = %v", app)
	}
	var logins []string
	for _, l := range res.GetFailedLogins() {
		logins = append(logins, fmt.Sprintf("%s %s %s %s %d", l.GetUser(), l.GetHost(), l.GetReason(), l.GetAccount(), l.GetCount()))
	}
	if want := []string{"label-printer 10.20.4.31 User Authentication Revoked APP 1440", " 203.0.113.12 Authentication Timeout  96"}; !slices.Equal(logins, want) {
		t.Errorf("failed logins %v, want %v", logins, want)
	}
	epoch := res.GetEpochAt().AsTime()
	var exp []string
	for _, e := range res.GetExpiring() {
		exp = append(exp, fmt.Sprintf("%s %s%s%s%s %d days, %d", e.GetKind(), e.GetServer(), e.GetListener(), e.GetAccountName(), e.GetUser(),
			int(e.GetExpires().AsTime().Sub(epoch)/(24*time.Hour)), e.GetConnections()))
	}
	want = []string{
		"EXPIRING_KIND_USER_JWT pos-terminal 5 days, 3",
		"EXPIRING_KIND_ACCOUNT_JWT SUPPLIERS 12 days, 0",
		"EXPIRING_KIND_SERVER_CERTIFICATE nats-eu-0client 16 days, 0",
	}
	if !slices.Equal(exp, want) {
		t.Errorf("expiring %v, want %v", exp, want)
	}
	// 23 answering servers with three certificates each, but the one
	// listed; APP's and SYS's JWTs and app's user JWTs never expire.
	if res.GetExpireLater() != 68 || res.GetNeverExpire() != 3 {
		t.Errorf("later %d, never %d", res.GetExpireLater(), res.GetNeverExpire())
	}
}

// The failed logins are on the Events page, and the pos-terminal
// connections in the connections list.
func TestGlobalAuthOnOtherPages(t *testing.T) {
	c := demo(t)
	n := map[string]int{}
	for token := ""; ; {
		res := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{System: fake.GlobalSystem, Kinds: []string{"client.auth_error"},
			PageSize: 1000, PageToken: token})))
		for _, e := range res.GetEvents() {
			n[e.GetClientHost()+" "+e.GetReason()]++
		}
		if token = res.GetNextPageToken(); token == "" {
			break
		}
	}
	if n["10.20.4.31 User Authentication Revoked"] != 1440 || n["203.0.113.12 Authentication Timeout"] != 96 {
		t.Errorf("auth error events in the last day: %v", n)
	}
	conns := must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{System: fake.GlobalSystem, PageSize: 2000}))).GetConnections()
	var pos []string
	for _, x := range conns {
		if x.GetUser() == "pos-terminal" {
			pos = append(pos, x.GetKey()+" "+x.GetName())
		}
	}
	if want := []string{"nats-eu-0/102 checkout-service", "nats-eu-1/102 checkout-service", "nats-eu-2/102 checkout-service"}; !slices.Equal(pos, want) {
		t.Errorf("pos-terminal connections %v, want %v", pos, want)
	}
}

// example-prod runs in config mode with its certificates more than 30
// days out; example-staging's APP was never looked up.
func TestProdAndStagingAccountsAndAuth(t *testing.T) {
	prod := accountsAndAuth(t, fake.Cluster)
	if prod.GetOperatorMode() || len(prod.GetAccounts()) != 1 || !prod.GetAccounts()[0].GetDetailsKnown() || prod.GetAccounts()[0].GetHasJwt() ||
		len(prod.GetExpiring()) != 0 || prod.GetExpireLater() != 6 || len(prod.GetFailedLogins()) != 0 {
		t.Errorf("prod = %v", prod)
	}
	staging := accountsAndAuth(t, fake.StagingSystem)
	if staging.GetEpoch() == 0 || len(staging.GetAccounts()) != 1 || staging.GetAccounts()[0].GetDetailsKnown() || len(staging.GetExpiring()) != 0 {
		t.Errorf("staging = %v", staging)
	}
}
