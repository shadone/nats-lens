package storeprov_test

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The account checks and server-016 fire for exactly the
// entities the Accounts page shows over their thresholds, for the same data.
func TestAccountChecksMatchAccountsPage(t *testing.T) {
	last := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	st := accountsStore(t, last, false)
	r := rows{t, st}
	// Extra accounts: near the connection limit, near the disk limit, a JWT
	// the server reports expired while its exp is in the future, a lookup
	// 23 hours old in an epoch without lookups since.
	add := func(id, name string, conns, disk int64, cols map[string]any, lookupEpoch int64, at time.Time) {
		r.add("accounts", map[string]any{"account": id, "account_name": name, "is_system": false, "first_epoch": 1, "last_epoch": 3})
		r.add("account_stats", map[string]any{"epoch": 3, "server_id": "S0", "account": id, "conns": conns, "js_storage": disk})
		row := map[string]any{"epoch": lookupEpoch, "server_id": "S0", "account": id, "looked_up_at": at, "is_system": false,
			"expired": false, "imports": 0, "exports": 0, "has_jwt": true}
		for k, v := range cols {
			row[k] = v
		}
		r.add("account_details", row)
	}
	key := func(c string) string {
		return (fmt.Sprintf("A%sREVIEWFAKEACCOUNTKEY", c) + "0000000000000000000000000000000000000000")[:56]
	}
	add(key("N"), "NEAR", 9, 0, map[string]any{"limit_connections": 10, "limit_js_disk": -1}, 3, last.Add(time.Second))
	add(key("D"), "DISK", 1, 95, map[string]any{"limit_connections": -1, "limit_js_disk": 100}, 3, last.Add(time.Second))
	add(key("F"), "FLAG", 0, 0, map[string]any{"expired": true, "jwt_expires": last.Add(40 * day)}, 3, last.Add(time.Second))
	add(key("O"), "OLD", 10, 0, map[string]any{"limit_connections": 10, "jwt_expires": last.Add(2 * day)}, 2,
		last.Add(-time.Hour+time.Second))

	res := accountsOf(t, st, last.Add(30*time.Second), nil)
	page := map[string][]string{}
	for _, a := range res.GetAccounts() {
		if a.ConnLimit != nil && a.GetConnLimit() > 0 && float64(a.GetConnections())/float64(a.GetConnLimit()) >= 0.9 {
			page["account-001"] = append(page["account-001"], a.GetAccount())
		}
		if a.DiskLimit != nil && a.GetDiskLimit() > 0 && float64(a.GetJsStorage())/float64(a.GetDiskLimit()) >= 0.9 {
			page["account-002"] = append(page["account-002"], a.GetAccount())
		}
	}
	soon := res.GetEpochAt().AsTime().Add(14 * day)
	for _, e := range res.GetExpiring() {
		if !e.GetExpired() && e.GetExpires().AsTime().After(soon) {
			continue
		}
		switch e.GetKind() {
		case v1.ExpiringKind_EXPIRING_KIND_ACCOUNT_JWT:
			page["account-003"] = append(page["account-003"], e.GetAccount())
		case v1.ExpiringKind_EXPIRING_KIND_USER_JWT:
			page["account-004"] = append(page["account-004"], e.GetAccount())
		case v1.ExpiringKind_EXPIRING_KIND_SERVER_CERTIFICATE:
			page["server-016"] = append(page["server-016"], e.GetServer())
		}
	}

	set, err := checks.Load(config.Checks{Timeout: config.Duration(10 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	runs, err := (&checks.Engine{Set: set}).Evaluate(ctx, st.DB(), res.GetEpoch())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	for _, run := range runs {
		switch run.Code {
		case "account-001", "account-002", "account-003", "account-004", "server-016":
		default:
			continue
		}
		if run.Error != "" {
			t.Fatalf("%s: %s", run.Code, run.Error)
		}
		for _, row := range run.Rows {
			got[run.Code] = append(got[run.Code], row.EntityKey)
		}
	}
	norm := func(m map[string][]string) string {
		for k := range m {
			s := map[string]bool{}
			var u []string
			for _, x := range m[k] {
				if !s[x] {
					s[x] = true
					u = append(u, x)
				}
			}
			sort.Strings(u)
			m[k] = u
		}
		return fmt.Sprint(m)
	}
	if norm(page) != norm(got) {
		t.Errorf("page over thresholds %s\nchecks fire %s", norm(page), norm(got))
	}
}
