package storeprov_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// Account public keys of the operator-mode fixture.
const (
	keyApp       = "AAPPFAKEACCOUNTKEYSTOREFIXTURE00000000000000000000000001"
	keySys       = "ASYSFAKEACCOUNTKEYSTOREFIXTURE00000000000000000000000002"
	keySuppliers = "ASUPFAKEACCOUNTKEYSTOREFIXTURE00000000000000000000000003"
	keyLegacy    = "ALEGFAKEACCOUNTKEYSTOREFIXTURE00000000000000000000000004"
)

const day = 24 * time.Hour

// accountsStore is an operator-mode system of four servers whose newest
// epoch (3) started at last, an hour after epoch 2 and 26 hours after its
// first:
//   - APP: 15 connections over two servers, two streams, looked up with
//     limits and a JWT without expiry (an older lookup says otherwise);
//   - SYS: the system account, looked up;
//   - SUPPLIERS: idle, its JWT expires in 12 days;
//   - LEGACY: looked up only 26 hours before the epoch, so not looked up
//     yet.
//
// scale-terminal-07 failed 5 times in the window and once before it; a client
// without a user timed out twice. nats-0's client certificate expires in
// 16 days; nats-1's VARZ failed in the newest epoch, and its websocket
// certificate, read an hour before, expired a day ago; nats-2 is missing
// and its client certificate, read an hour before, expires in 10 days;
// nats-3 was last read 26 hours before, too old to be shown. pos-terminal
// holds three connections whose JWT expires in 5 days and one later; app
// two without expiry; one connection uses a bearer JWT. withGap adds a
// time nats-lens was not running inside the window.
func accountsStore(t *testing.T, last time.Time, withGap bool) *store.Store {
	t.Helper()
	st := emptyStore(t)
	r := rows{t, st}
	first := last.Add(-26 * time.Hour)
	names := []string{"nats-0", "nats-1", "nats-2", "nats-3"}
	for i, n := range names {
		r.server(fmt.Sprintf("S%d", i), n)
	}
	// epoch writes epoch n at at, with every server expected, those of
	// varz answering VARZ (and so writing server_stats) and the others
	// missing; S1 answers, but its VARZ fails in the newest epoch.
	epoch := func(n int64, at time.Time, varz ...string) {
		var missing []string
		for i, name := range names {
			id := fmt.Sprintf("S%d", i)
			switch {
			case slices.Contains(varz, id):
				r.add("epoch_endpoints", map[string]any{"epoch": n, "server_id": id, "endpoint": "VARZ", "state": "ok"})
			case id == "S1" && n == 3:
				r.add("epoch_endpoints", map[string]any{"epoch": n, "server_id": id, "endpoint": "VARZ", "state": "error"})
				r.add("server_stats", map[string]any{"epoch": n, "server_id": id})
			default:
				missing = append(missing, name)
			}
		}
		if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, 4, ?, ?, [], ?, 0, ?, [], NULL)`,
			n, at, at, 4-len(missing), names, missing, len(missing) > 0); err != nil {
			t.Fatal(err)
		}
	}
	epoch(1, first, "S0", "S1", "S2", "S3")
	epoch(2, last.Add(-time.Hour), "S0", "S1", "S2")
	epoch(3, last, "S0")
	// nats-3 answered VARZ last 26 hours ago: too old to be read.
	r.add("server_stats", map[string]any{"epoch": 1, "server_id": "S3", "tls_cert_not_after": last.Add(3 * day)})
	// nats-2, missing now, answered an hour ago.
	r.add("server_stats", map[string]any{"epoch": 2, "server_id": "S2", "tls_cert_not_after": last.Add(10 * day)})
	r.add("server_stats", map[string]any{"epoch": 2, "server_id": "S0", "tls_cert_not_after": last.Add(99 * day)})
	for _, a := range []struct {
		id, name string
		system   bool
	}{{keyApp, "APP", false}, {keySys, "SYS", true}, {keySuppliers, "SUPPLIERS", false}, {keyLegacy, "LEGACY", false}} {
		r.add("accounts", map[string]any{"account": a.id, "account_name": a.name, "is_system": a.system, "first_epoch": 1, "last_epoch": 3})
	}
	stat := func(id, acc string, conns, leafs, subs, disk int64) {
		r.add("account_stats", map[string]any{"epoch": 3, "server_id": id, "account": acc, "conns": conns, "leafnodes": leafs,
			"subscriptions": subs, "js_storage": disk, "js_memory": 0, "js_streams": 0})
	}
	stat("S0", keyApp, 10, 1, 50, 1000)
	stat("S1", keyApp, 5, 0, 20, 1000)
	stat("S0", keySys, 2, 0, 400, 0)
	stat("S0", keySuppliers, 0, 0, 0, 0)
	stat("S0", keyLegacy, 1, 0, 3, 0)
	for _, s := range []struct{ id, stream string }{{"S0", "orders"}, {"S1", "orders"}, {"S0", "payments"}} {
		r.add("stream_stats", map[string]any{"epoch": 3, "server_id": s.id, "account": keyApp, "stream": s.stream, "is_leader": s.id == "S0"})
	}
	lookup := func(epoch int64, at time.Time, acc string, cols map[string]any) {
		row := map[string]any{"epoch": epoch, "server_id": "S0", "account": acc, "looked_up_at": at, "is_system": false,
			"expired": false, "imports": 0, "exports": 0, "has_jwt": true}
		for k, v := range cols {
			row[k] = v
		}
		r.add("account_details", row)
	}
	lookup(1, first.Add(2*time.Second), keyApp, map[string]any{"imports": 7, "limit_connections": 10, "limit_js_disk": 10, "jwt_expires": last.Add(day)})
	lookup(3, last.Add(2*time.Second), keyApp, map[string]any{"imports": 1, "limit_connections": 2000, "limit_js_disk": 500 << 30,
		"limit_js_memory": 16 << 30})
	lookup(3, last.Add(3*time.Second), keySys, map[string]any{"is_system": true, "exports": 11, "limit_connections": -1, "limit_js_disk": 0,
		"limit_js_memory": 0})
	lookup(3, last.Add(4*time.Second), keySuppliers, map[string]any{"exports": 1, "limit_connections": 50, "limit_js_disk": 0,
		"limit_js_memory": 0, "jwt_expires": last.Add(12 * day)})
	lookup(1, first.Add(2*time.Second), keyLegacy, map[string]any{"limit_connections": 5, "limit_js_disk": 5})

	id := 0
	event := func(at time.Time, kind string, cols map[string]any) {
		id++
		row := map[string]any{"id": id, "time": at, "received_at": at, "source": "sys", "kind": kind, "summary": kind}
		for k, v := range cols {
			row[k] = v
		}
		r.add("events", row)
	}
	event(first, "collector.started", map[string]any{"source": "collector"})
	for i := range 6 {
		event(last.Add(-time.Duration(i)*time.Hour-19*time.Hour-30*time.Minute), "client.auth_error", map[string]any{"client_user": "scale-terminal-07",
			"client_host": "10.20.4.31", "reason": "User Authentication Revoked", "account": keyApp})
	}
	for _, d := range []time.Duration{time.Hour, 3 * time.Hour} {
		event(last.Add(-d), "client.auth_error", map[string]any{"client_host": "203.0.113.12", "reason": "Authentication Timeout"})
	}
	if withGap {
		event(last.Add(-5*time.Hour), "collector.gap", map[string]any{"source": "collector", "received_at": last.Add(-4 * time.Hour)})
	}

	r.add("server_stats", map[string]any{"epoch": 3, "server_id": "S0", "tls_cert_not_after": last.Add(16 * day),
		"cluster_tls_cert_not_after": last.Add(75 * day)})
	r.add("server_stats", map[string]any{"epoch": 2, "server_id": "S1", "tls_cert_not_after": last.Add(75 * day),
		"websocket_tls_cert_not_after": last.Add(-day)})

	cid := int64(0)
	conn := func(user string, jwt bool, expires any) {
		cid++
		r.add("connections", map[string]any{"epoch": 3, "server_id": "S0", "cid": cid, "account": keyApp, "authorized_user": user,
			"user_jwt": jwt, "jwt_expires": expires})
	}
	for range 3 {
		conn("pos-terminal", true, last.Add(5*day))
	}
	conn("pos-terminal", true, last.Add(20*day))
	conn("app", true, nil)
	conn("app", true, nil)
	conn("kiosk", false, nil)
	return st
}

// accountsConfigStore is a config-mode system: APP and $SYS looked up
// without JWTs, events captured without gaps or failed logins, and a
// cluster certificate that expires in 10 days.
func accountsConfigStore(t *testing.T, last time.Time) *store.Store {
	t.Helper()
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	r.epoch(1, last.Add(-time.Hour), "S0")
	r.epoch(2, last, "S0")
	for _, a := range []struct {
		id     string
		system bool
	}{{"APP", false}, {"$SYS", true}} {
		r.add("accounts", map[string]any{"account": a.id, "account_name": a.id, "is_system": a.system, "first_epoch": 1, "last_epoch": 2})
		r.add("account_stats", map[string]any{"epoch": 2, "server_id": "S0", "account": a.id, "conns": 3, "leafnodes": 0,
			"subscriptions": 9, "js_storage": 0, "js_memory": 0, "js_streams": 0, "js_reserved_storage": 1 << 30})
		r.add("account_details", map[string]any{"epoch": 2, "server_id": "S0", "account": a.id, "looked_up_at": last.Add(time.Second), "account_name": a.id,
			"is_system": a.system, "expired": false, "imports": 0, "exports": 0, "has_jwt": false})
	}
	r.add("events", map[string]any{"id": 1, "time": last.Add(-time.Hour), "received_at": last.Add(-time.Hour), "source": "collector",
		"kind": "collector.started", "summary": "started"})
	r.add("epoch_endpoints", map[string]any{"epoch": 2, "server_id": "S0", "endpoint": "VARZ", "state": "ok"})
	r.add("server_stats", map[string]any{"epoch": 2, "server_id": "S0", "tls_cert_not_after": last.Add(80 * day),
		"cluster_tls_cert_not_after": last.Add(10 * day)})
	r.add("connections", map[string]any{"epoch": 2, "server_id": "S0", "cid": 1, "account": "APP", "authorized_user": "app", "user_jwt": false})
	return st
}

func accountsOf(t *testing.T, st *store.Store, clock time.Time, r *v1.TimeRange) *v1.GetAccountsAndAuthResponse {
	t.Helper()
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Clock: func() time.Time { return clock }})
	return must(c.GetAccountsAndAuth(ctx, connect.NewRequest(&v1.GetAccountsAndAuthRequest{Range: r})))
}

// The store reads accounts with their newest recent lookup, distinct
// streams and summed connections; failed logins of the window grouped;
// certificates; user JWTs by user with the connections holding the
// soonest; and names the accounts.
func TestAccountsAndAuthFromStore(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	res := accountsOf(t, accountsStore(t, last, false), last.Add(30*time.Second), nil)
	if res.GetEpoch() != 3 || !res.GetEpochAt().AsTime().Equal(last) || !res.GetOperatorMode() || res.GetCaptureGaps() || !res.GetEventsCaptured() {
		t.Fatalf("res = %v", res)
	}
	var accs []string
	for _, a := range res.GetAccounts() {
		limit := func(p *int64) string {
			if p == nil {
				return "-"
			}
			return fmt.Sprint(*p)
		}
		js := "-"
		if a.JetstreamEnabled != nil {
			js = fmt.Sprint(a.GetJetstreamEnabled())
		}
		accs = append(accs, fmt.Sprintf("%s c%d l%d s%d d%d st%d lim %s/%s js %s imp %v exp %v known %v jwt %v until %v", a.GetAccountName(),
			a.GetConnections(), a.GetLeafnodes(), a.GetSubscriptions(), a.GetJsStorage(), a.GetStreams(), limit(a.ConnLimit), limit(a.DiskLimit),
			js, a.Imports != nil && a.GetImports() == 1, a.GetExports(), a.GetDetailsKnown(), a.GetHasJwt(), a.GetJwtExpires() != nil))
	}
	want := []string{
		"APP c15 l1 s70 d1000 st2 lim 2000/536870912000 js true imp true exp 0 known true jwt true until false",
		"LEGACY c1 l0 s3 d0 st0 lim -/- js - imp false exp 0 known false jwt false until false",
		"SUPPLIERS c0 l0 s0 d0 st0 lim 50/0 js false imp false exp 1 known true jwt true until true",
		"SYS c2 l0 s400 d0 st0 lim -1/0 js false imp false exp 11 known true jwt true until false",
	}
	if !slices.Equal(accs, want) {
		t.Errorf("accounts\n%v\nwant\n%v", accs, want)
	}
	if !res.GetAccounts()[0].GetDetailsAt().AsTime().Equal(last.Add(2 * time.Second)) {
		t.Errorf("APP details at %v", res.GetAccounts()[0].GetDetailsAt())
	}
	var logins []string
	for _, l := range res.GetFailedLogins() {
		logins = append(logins, fmt.Sprintf("%s|%s|%s|%s|%d|%s", l.GetUser(), l.GetHost(), l.GetReason(), l.GetAccountName(), l.GetCount(),
			l.GetLast().AsTime().Format(time.TimeOnly)))
	}
	if want := []string{"scale-terminal-07|10.20.4.31|User Authentication Revoked|APP|5|18:30:00", "|203.0.113.12|Authentication Timeout||2|13:00:00"}; !slices.Equal(logins, want) {
		t.Errorf("failed logins %v, want %v", logins, want)
	}
	if res.GetFailedLoginGroupsTotal() != 2 || res.GetFailedLoginsTotal() != 7 || !res.GetFailedLoginsWindowStart().AsTime().Equal(last.Add(-day)) {
		t.Errorf("totals %d groups, %d logins, from %v", res.GetFailedLoginGroupsTotal(), res.GetFailedLoginsTotal(), res.GetFailedLoginsWindowStart())
	}
	var exp []string
	for _, e := range res.GetExpiring() {
		seen := "now"
		if e.GetSeenAt() != nil {
			seen = e.GetSeenAt().AsTime().Sub(last).String()
		}
		exp = append(exp, fmt.Sprintf("%s %s%s %s%s %v %d %s seen %s", e.GetKind(), e.GetServer(), e.GetListener(), e.GetAccountName(), e.GetUser(),
			e.GetExpired(), e.GetConnections(), e.GetExpires().AsTime().Sub(last), seen))
	}
	wantExp := []string{
		"EXPIRING_KIND_SERVER_CERTIFICATE nats-1websocket  true 0 -24h0m0s seen -1h0m0s",
		"EXPIRING_KIND_USER_JWT  APPpos-terminal false 3 120h0m0s seen now",
		"EXPIRING_KIND_SERVER_CERTIFICATE nats-2client  false 0 240h0m0s seen -1h0m0s",
		"EXPIRING_KIND_ACCOUNT_JWT  SUPPLIERS false 0 288h0m0s seen now",
		"EXPIRING_KIND_SERVER_CERTIFICATE nats-0client  false 0 384h0m0s seen now",
	}
	if !slices.Equal(exp, wantExp) {
		t.Errorf("expiring\n%v\nwant\n%v", exp, wantExp)
	}
	// Later: nats-0's cluster and nats-1's client certificates. Never:
	// the JWTs of APP and SYS, and app's user JWTs.
	// nats-0's epoch-2 row is older than its newest and not read; nats-3's
	// is too old.
	if res.GetExpireLater() != 2 || res.GetNeverExpire() != 3 || res.GetExpiringTotal() != 5 {
		t.Errorf("later %d, never %d, total %d", res.GetExpireLater(), res.GetNeverExpire(), res.GetExpiringTotal())
	}

	// At the first epoch the window starts at the history's start, and
	// the lookups of that epoch are recent.
	first := last.Add(-26 * time.Hour)
	old := accountsOf(t, accountsStore(t, last, false), last.Add(30*time.Second),
		&v1.TimeRange{Start: timestamppb.New(first), End: timestamppb.New(first.Add(time.Second))})
	if old.GetEpoch() != 1 || !old.GetFailedLoginsWindowStart().AsTime().Equal(first) || len(old.GetFailedLogins()) != 0 {
		t.Errorf("first epoch: %v", old)
	}
	for _, a := range old.GetAccounts() {
		if a.GetAccountName() == "LEGACY" && (!a.GetDetailsKnown() || a.GetConnLimit() != 5) {
			t.Errorf("LEGACY at its lookup = %v", a)
		}
	}

	// A range without an epoch.
	none := accountsOf(t, accountsStore(t, last, false), last.Add(30*time.Second),
		&v1.TimeRange{Start: timestamppb.New(last.Add(-30 * day)), End: timestamppb.New(last.Add(-29 * day))})
	if none.GetEpoch() != 0 || len(none.GetAccounts()) != 0 {
		t.Errorf("no epoch: %v", none)
	}
}

// Event capture gaps: a time nats-lens was not running inside the window,
// or an events connection lost for longer than a failover; a failover
// alone is none. No event captured at all is not a gap but
// events_captured false.
func TestAccountsAndAuthCaptureGaps(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	clock := last.Add(30 * time.Second)
	if !accountsOf(t, accountsStore(t, last, true), clock, nil).GetCaptureGaps() {
		t.Error("collector.gap in the window: no capture gap")
	}
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	r.epoch(1, last.Add(-25*time.Hour), "S0")
	r.epoch(2, last, "S0")
	if res := accountsOf(t, st, clock, nil); res.GetEventsCaptured() || res.GetCaptureGaps() {
		t.Errorf("no event captured: events_captured %v, capture_gaps %v; want both false", res.GetEventsCaptured(), res.GetCaptureGaps())
	}
	conn := func(id int, at time.Time, kind string) {
		r.add("events", map[string]any{"id": id, "time": at, "received_at": at, "source": "collector", "kind": kind, "summary": kind,
			"raw": `{"connection":"sys"}`})
	}
	conn(1, last.Add(-3*time.Hour), "collector.disconnected")
	conn(2, last.Add(-3*time.Hour+time.Second), "collector.reconnected")
	if res := accountsOf(t, st, clock, nil); res.GetCaptureGaps() || !res.GetEventsCaptured() {
		t.Errorf("a failover within a second: capture_gaps %v, events_captured %v", res.GetCaptureGaps(), res.GetEventsCaptured())
	}
	conn(3, last.Add(-2*time.Hour), "collector.disconnected")
	conn(4, last.Add(-time.Hour), "collector.reconnected")
	if !accountsOf(t, st, clock, nil).GetCaptureGaps() {
		t.Error("an hour without the sys connection: no capture gap")
	}
}

// Config mode lists certificates only; js_reserved_storage is not a disk
// limit.
func TestAccountsAndAuthConfigMode(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	res := accountsOf(t, accountsConfigStore(t, last), last.Add(30*time.Second), nil)
	if res.GetOperatorMode() || res.GetCaptureGaps() || len(res.GetAccounts()) != 2 || res.GetAccounts()[1].GetAccount() != "$SYS" {
		t.Fatalf("res = %v", res)
	}
	for _, a := range res.GetAccounts() {
		if !a.GetDetailsKnown() || a.GetHasJwt() || a.ConnLimit != nil || a.DiskLimit != nil || a.JetstreamEnabled != nil || a.GetAccountName() != "" {
			t.Errorf("account = %v", a)
		}
	}
	if len(res.GetExpiring()) != 1 || res.GetExpiring()[0].GetListener() != api.ListenerCluster || res.GetExpireLater() != 1 || res.GetNeverExpire() != 0 {
		t.Errorf("expiring %v, later %d, never %d", res.GetExpiring(), res.GetExpireLater(), res.GetNeverExpire())
	}
}
