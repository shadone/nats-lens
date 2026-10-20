package fake

import (
	"time"

	"github.com/shadone/nats-lens/internal/api"
)

const day = 24 * time.Hour

// SuppliersAccount is example-global's account for the suppliers'
// integration: an account public key, as operator mode names accounts.
const SuppliersAccount = "ADSUPPLIERSFAKEACCOUNTKEYEXAMPLEGLOBAL7Q2M4XKV3WPJ6RT5NH"

// globalAuth is example-global in operator mode: APP with limits and a
// JWT without expiry, the system account SYS, and SUPPLIERS, idle, whose
// JWT expires in 12 days. label-printer's credentials were revoked
// yesterday at 12:00 and it retries every minute; something without
// credentials times out 8 times every 2 hours. nats-eu-0's client certificate
// expires in 16 days, and the pos-terminal user JWT of three checkout
// connections in 5 days.
func globalAuth() *authSpec {
	return &authSpec{
		app: &lookupSpec{jwt: true, imports: 1, connLimit: 2000, diskLimit: 500 * gib},
		others: []accountSpec{
			{id: "SYS", system: true, subs: 412, lookup: &lookupSpec{jwt: true, exports: 11, connLimit: -1, diskLimit: 0}},
			{id: SuppliersAccount, name: "SUPPLIERS", subs: 0,
				lookup: &lookupSpec{jwt: true, expires: 12 * day, exports: 1, connLimit: 50, diskLimit: 0}},
		},
		failed: []failedLoginSpec{
			{user: "label-printer", host: "10.20.4.31", reason: "User Authentication Revoked", account: Account, server: "nats-us-3",
				since: time.Date(2026, 10, 16, 12, 0, 30, 0, time.UTC), every: time.Minute},
			{host: "203.0.113.12", reason: "Authentication Timeout", server: "nats-eu-0",
				since: time.Date(2026, 10, 16, 8, 7, 0, 0, time.UTC), every: 2 * time.Hour, burst: 8, spacing: 20 * time.Second},
		},
		certs: func(sp serverSpec) map[string]time.Duration {
			client := 75 * day
			if sp.name == "nats-eu-0" {
				client = 16 * day
			}
			return map[string]time.Duration{api.ListenerClient: client, api.ListenerCluster: 75 * day, api.ListenerGateway: 75 * day}
		},
		userJWTs: map[string]time.Duration{"app": 0, "pos-terminal": 5 * day},
		users:    map[string]string{"nats-eu-0/102": "pos-terminal", "nats-eu-1/102": "pos-terminal", "nats-eu-2/102": "pos-terminal"},
	}
}

// prodAuth is example-prod in config mode: APP alone, looked up, with no
// JWT, and server certificates that expire in more than 30 days.
func prodAuth() *authSpec {
	return &authSpec{
		app: &lookupSpec{},
		certs: func(serverSpec) map[string]time.Duration {
			return map[string]time.Duration{api.ListenerClient: 58 * day, api.ListenerCluster: 58 * day}
		},
	}
}
