package storeprov_test

import (
	"testing"
	"time"
)

// The collector reads an ACCOUNTZ lookup after the epoch's
// collection started (accountlookup.go: at := c.now() after the gather),
// so the lookup made in epoch N has looked_up_at > epochs.started_at of N.
// The page must still show that lookup for epoch N.
func TestAccountsAndAuthLookupOfTheEpoch(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	r.epoch(1, last.Add(-10*time.Minute), "S0")
	r.epoch(2, last, "S0")
	for _, id := range []string{keyApp, keySuppliers} {
		r.add("accounts", map[string]any{"account": id, "account_name": id, "is_system": false, "first_epoch": 1, "last_epoch": 2})
		r.add("account_stats", map[string]any{"epoch": 2, "server_id": "S0", "account": id, "conns": 1, "leafnodes": 0,
			"subscriptions": 1, "js_storage": 0, "js_memory": 0, "js_streams": 0})
	}
	lookup := func(epoch int64, at time.Time, acc string, limit int64) {
		r.add("account_details", map[string]any{"epoch": epoch, "server_id": "S0", "account": acc, "looked_up_at": at, "is_system": false,
			"expired": false, "imports": 0, "exports": 0, "has_jwt": true, "limit_connections": limit, "limit_js_disk": -1, "limit_js_memory": -1})
	}
	// APP: looked up in epoch 1 (limit 10) and again in epoch 2 (limit 2000),
	// 2 seconds after each epoch started, as the collector does.
	lookup(1, last.Add(-10*time.Minute+2*time.Second), keyApp, 10)
	lookup(2, last.Add(2*time.Second), keyApp, 2000)
	// SUPPLIERS: a new account, first looked up in epoch 2.
	lookup(2, last.Add(2*time.Second), keySuppliers, 50)

	res := accountsOf(t, st, last.Add(30*time.Second), nil)
	for _, a := range res.GetAccounts() {
		switch a.GetAccount() {
		case keyApp:
			if !a.GetDetailsKnown() || a.GetConnLimit() != 2000 {
				t.Errorf("APP at epoch 2: known %v, conn limit %d; want epoch 2's lookup (2000)", a.GetDetailsKnown(), a.GetConnLimit())
			}
		case keySuppliers:
			if !a.GetDetailsKnown() || a.GetConnLimit() != 50 {
				t.Errorf("SUPPLIERS at epoch 2: known %v, conn limit %d; want its epoch-2 lookup (50)", a.GetDetailsKnown(), a.GetConnLimit())
			}
		}
	}
}
