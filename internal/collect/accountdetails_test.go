package collect

import (
	"testing"
	"time"
)

var lookupAt = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

// accountzReply wraps an account_detail in the ACCOUNTZ envelope of nats-0.
func accountzReply(detail string) []byte {
	return []byte(`{"server":{"id":"NSERVER0","name":"nats-0"},"data":{"server_id":"NSERVER0","system_account":"` + keySys +
		`","account_detail":` + detail + `}}`)
}

const keySys = "ADSYSEXAMPLESYSTEMACCOUNTKEYQQQQQQQQQQQQQQQQQQQQQQQQQQQQ"

// The imports the server adds to every account itself, and to a
// JetStream account the JetStream API.
const sysImports = `[
 {"subject":"$SYS.REQ.ACCOUNT.PING.CONNZ","account":"` + keySys + `","type":"service","invalid":false,"share":false,"tracking":false},
 {"subject":"$SYS.REQ.SERVER.PING.CONNZ","account":"` + keySys + `","type":"service","invalid":false,"share":false,"tracking":false},
 {"subject":"$SYS.REQ.ACCOUNT.PING.STATZ","account":"` + keySys + `","type":"service","invalid":false,"share":false,"tracking":false},
 {"subject":"$SYS.REQ.USER.INFO","account":"` + keySys + `","type":"service","invalid":false,"share":true,"tracking":false},
 {"subject":"$JS.API.>","account":"` + keySys + `","type":"service","invalid":false,"share":true,"tracking":false}`

// An operator-mode account with a JWT: the name tag, the issue time, the
// expiry and the limits are read, the JWT string is not.
func TestAccountDetailsFromJWT(t *testing.T) {
	b := accountzReply(`{"account_name":"` + keyNamed + `","update_time":"2026-10-20T08:00:00Z","expired":false,
 "complete":true,"jetstream_enabled":true,"leafnode_connections":0,"client_connections":12,"subscriptions":40,
 "exports":[{"subject":"inventory.changes","type":"stream"}],
 "imports":` + sysImports + `,
  {"subject":"prices.>","account":"ASUPPLIERSKEY","type":"stream","invalid":false,"share":false,"tracking":false}],
 "jwt":"eyJ0eXAiOiJKV1QiLCJhbGciOiJlZDI1NTE5LW5rZXkifQ.eyJzZWNyZXQiOiJ0b2tlbiJ9.c2lnbmF0dXJl",
 "issuer_key":"OPERATORKEY","name_tag":"APP",
 "decoded_jwt":{"jti":"X","iat":1760000000,"exp":1793000000,"iss":"OPERATORKEY","name":"APP","sub":"` + keyNamed + `",
  "nats":{"limits":{"subs":-1,"data":-1,"payload":1048576,"imports":-1,"exports":-1,"wildcards":true,"conn":2000,"leaf":-1,
   "mem_storage":1073741824,"disk_storage":536870912000,"streams":20,"consumer":-1},
   "signing_keys":["ASIGNINGKEY"],"type":"account","version":2}},
 "validation_result_jwt":[]}`)
	d, ok := accountDetailsFromReply(b, keyNamed, lookupAt)
	if !ok {
		t.Fatal("reply not read")
	}
	want := AccountDetails{
		Account: keyNamed, LookedUpAt: lookupAt, ServerID: "NSERVER0", Name: "APP", Imports: 1, Exports: 1,
		HasJWT: true, JWTIssuedAt: time.Unix(1760000000, 0).UTC(), JWTExpires: time.Unix(1793000000, 0).UTC(),
		Limits: AccountLimits{Connections: 2000, LeafNodes: -1, Subscriptions: -1, Data: -1, Payload: 1 << 20,
			JSDisk: 500 << 30, JSMemory: 1 << 30, JSStreams: 20, JSConsumers: -1},
	}
	if d != want {
		t.Fatalf("details\n got %+v\nwant %+v", d, want)
	}
}

// A JWT without expiry and without limits: no expiry, and every limit
// unlimited as the JWT says; JetStream not enabled reads as 0.
func TestAccountDetailsUnlimitedNoExpiry(t *testing.T) {
	b := accountzReply(`{"account_name":"` + keyNamed + `","expired":false,"imports":` + sysImports + `],
 "decoded_jwt":{"iat":1760000000,"name":"APP","sub":"` + keyNamed + `",
  "nats":{"limits":{"subs":-1,"data":-1,"payload":-1,"imports":-1,"exports":-1,"wildcards":true,"conn":-1,"leaf":-1}}}}`)
	d, ok := accountDetailsFromReply(b, keyNamed, lookupAt)
	if !ok {
		t.Fatal("reply not read")
	}
	if !d.JWTExpires.IsZero() || d.Name != "APP" || d.Imports != 0 || d.Exports != 0 {
		t.Errorf("details %+v, want no expiry, name from the JWT, no imports or exports", d)
	}
	want := AccountLimits{Connections: -1, LeafNodes: -1, Subscriptions: -1, Data: -1, Payload: -1}
	if d.Limits != want {
		t.Errorf("limits %+v, want %+v", d.Limits, want)
	}
}

// Tiered JetStream limits are summed over the tiers; a tier without a
// limit makes the sum unlimited; the account-wide JetStream limits are
// ignored, as the server ignores them.
func TestAccountDetailsTieredLimits(t *testing.T) {
	b := accountzReply(`{"account_name":"` + keyNamed + `","decoded_jwt":{"name":"APP","nats":{"limits":{"conn":10,
 "disk_storage":1,"mem_storage":1,"streams":1,"consumer":1,
 "tiered_limits":{"R1":{"mem_storage":1000,"disk_storage":2000,"streams":5,"consumer":-1},
  "R3":{"mem_storage":3000,"disk_storage":6000,"streams":10,"consumer":100}}}}}}`)
	d, ok := accountDetailsFromReply(b, keyNamed, lookupAt)
	if !ok {
		t.Fatal("reply not read")
	}
	if l := d.Limits; l.JSMemory != 4000 || l.JSDisk != 8000 || l.JSStreams != 15 || l.JSConsumers != -1 || l.Connections != 10 {
		t.Errorf("limits %+v, want memory 4000, disk 8000, streams 15, consumers unlimited", l)
	}
}

// A config-mode account has no JWT: its name is its identifier, it has no
// limits, and the system account says so.
func TestAccountDetailsConfigMode(t *testing.T) {
	b := []byte(`{"server":{"id":"NSERVER0","name":"nats-0"},"data":{"system_account":"SYS",
 "account_detail":{"account_name":"APP","expired":false,"complete":true,"jetstream_enabled":true,
  "imports":[{"subject":"$SYS.REQ.ACCOUNT.PING.CONNZ","account":"SYS","type":"service"},
   {"subject":"$SYS.REQ.USER.INFO","account":"SYS","type":"service"},
   {"subject":"orders.>","account":"SHIPPING","type":"stream"}]}}}`)
	d, ok := accountDetailsFromReply(b, "APP", lookupAt)
	if !ok {
		t.Fatal("reply not read")
	}
	want := AccountDetails{Account: "APP", LookedUpAt: lookupAt, ServerID: "NSERVER0", Name: "APP", Imports: 1}
	if d != want {
		t.Fatalf("details\n got %+v\nwant %+v", d, want)
	}
	sys := []byte(`{"server":{"id":"NSERVER0","name":"nats-0"},"data":{"system_account":"SYS",
 "account_detail":{"account_name":"SYS","is_system":true,"exports":[{"subject":"$SYS.REQ.SERVER.PING.CONNZ","type":"service"}]}}}`)
	if d, ok := accountDetailsFromReply(sys, "SYS", lookupAt); !ok || !d.IsSystem || d.HasJWT {
		t.Errorf("system account details %+v, %v", d, ok)
	}
}

// An expired account is reported as the server says.
func TestAccountDetailsExpired(t *testing.T) {
	b := accountzReply(`{"account_name":"` + keyNamed + `","expired":true,"decoded_jwt":{"exp":1700000000}}`)
	d, ok := accountDetailsFromReply(b, keyNamed, lookupAt)
	if !ok || !d.Expired || !d.JWTExpires.Equal(time.Unix(1700000000, 0)) || d.Name != "" {
		t.Errorf("details %+v, %v; want expired, the expiry, no name", d, ok)
	}
}

// A reply that does not hold the account is skipped: an error reply from a
// server that has not loaded it, a reply for another account, one that
// does not decode, or one that does not name its server.
func TestAccountDetailsNotInReply(t *testing.T) {
	for name, b := range map[string]string{
		"error":     `{"server":{"id":"NSERVER1","name":"nats-1"},"data":{},"error":{"code":500,"description":"Account ` + keyNamed + ` does not exist"}}`,
		"other":     string(accountzReply(`{"account_name":"` + keyUnnamed + `"}`)),
		"garbage":   `{"server":`,
		"no server": `{"data":{"account_detail":{"account_name":"` + keyNamed + `"}}}`,
	} {
		if d, ok := accountDetailsFromReply([]byte(b), keyNamed, lookupAt); ok {
			t.Errorf("%s: read %+v", name, d)
		}
	}
}
