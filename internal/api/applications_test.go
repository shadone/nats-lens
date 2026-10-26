package api_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

var appT0 = time.Date(2026, 10, 17, 14, 20, 0, 0, time.UTC)

func rttOf(ms int) *time.Duration {
	d := time.Duration(ms) * time.Millisecond
	return &d
}

func appConn(server string, cid int64, name, ip string) api.AppConnectionFact {
	return api.AppConnectionFact{Server: server, CID: cid, Name: name, Account: "APP", IP: ip, Lang: "go", Version: "1.54.0"}
}

func appByName(res *v1.GetApplicationsResponse, name string) *v1.Application {
	for _, a := range res.GetApplications() {
		if a.GetName() == name {
			return a
		}
	}
	return nil
}

func TestApplicationOfAndPodOf(t *testing.T) {
	for _, c := range []struct{ name, app, pod string }{
		{"resize-worker@resize-worker-7d9f", "resize-worker", "resize-worker-7d9f"},
		{"order-service", "order-service", ""},
		{"", "(unnamed)", ""},
		{"@pod-z", "(unnamed)", "pod-z"},
		{"svc@", "svc", ""},
		{"a@b@c", "a", "b"},
	} {
		if app, pod := api.ApplicationOf(c.name), api.PodOf(c.name); app != c.app || pod != c.pod {
			t.Errorf("%q: application %q pod %q, want %q %q", c.name, app, pod, c.app, c.pod)
		}
	}
}

func TestTrafficHoursAreTheWholeHoursBeforeTheEpochs(t *testing.T) {
	hours := api.TrafficHours(appT0)
	if len(hours) != 24 || !hours[0].Equal(time.Date(2026, 10, 16, 14, 0, 0, 0, time.UTC)) ||
		!hours[23].Equal(time.Date(2026, 10, 17, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("hours %v .. %v", hours[0], hours[len(hours)-1])
	}
	// An epoch on the hour still leaves its own hour out.
	if h := api.TrafficHours(time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)); !h[23].Equal(time.Date(2026, 10, 17, 13, 0, 0, 0, time.UTC)) {
		t.Errorf("last hour %v", h[23])
	}
}

func TestFinishApplicationsGroupsAtTheAt(t *testing.T) {
	f := &api.ApplicationFacts{Epoch: 7, EpochAt: appT0, Connections: []api.AppConnectionFact{
		appConn("nats-0", 1, "resize-worker@pod-a", "10.0.0.1"),
		appConn("nats-0", 2, "resize-worker@pod-b", "10.0.0.2"),
		appConn("nats-1", 3, "resize-worker@pod-c", "10.0.0.3"),
		// One pod with two connections.
		appConn("nats-2", 4, "resize-worker@pod-c", "10.0.0.3"),
		appConn("nats-1", 5, "order-service", "10.0.0.5"),
		appConn("nats-2", 5, "order-service", "10.0.0.6"),
		appConn("nats-0", 9, "", "10.0.0.9"),
		appConn("nats-0", 10, "@pod-z", "10.0.0.10"),
	}}
	f.Connections[4].RTT, f.Connections[5].RTT = rttOf(2), rttOf(5)
	f.Connections[4].Lang, f.Connections[4].Version = "nats.js", "3.0.2"
	f.Connections[0].Subscriptions, f.Connections[1].Subscriptions = i64(3), i64(4)
	f.Connections[0].PendingBytes, f.Connections[1].PendingBytes = i64(100), i64(700)
	res := api.FinishApplications(f, "", false)
	if res.GetApplicationsTotal() != 3 || res.GetConnectionsTotal() != 8 {
		t.Fatalf("%d applications, %d connections", res.GetApplicationsTotal(), res.GetConnectionsTotal())
	}
	rw := appByName(res, "resize-worker")
	if rw.GetConnections() != 4 || rw.GetInstances() != 3 || rw.GetPodsTotal() != 3 || !slices.Equal(rw.GetPods(), []string{"pod-a", "pod-b", "pod-c"}) {
		t.Errorf("resize-worker: %d connections, %d instances, pods %v", rw.GetConnections(), rw.GetInstances(), rw.GetPods())
	}
	if s := rw.GetServers(); len(s) != 3 || s[0].GetServer() != "nats-0" || s[0].GetConnections() != 2 || s[1].GetServer() != "nats-1" {
		t.Errorf("resize-worker servers %v", s)
	}
	if rw.GetSubscriptions() != 7 || rw.GetPendingBytes() != 800 || rw.GetPendingBytesMax() != 700 || rw.RttP50 != nil {
		t.Errorf("resize-worker sums: subs %v pending %v max %v rtt %v", rw.Subscriptions, rw.PendingBytes, rw.PendingBytesMax, rw.RttP50)
	}
	if rw.GetAddressesTotal() != 3 || len(rw.GetAccounts()) != 1 || rw.GetAccounts()[0].GetAccount() != "APP" {
		t.Errorf("resize-worker addresses %v accounts %v", rw.GetAddresses(), rw.GetAccounts())
	}
	os := appByName(res, "order-service")
	if os.GetInstances() != 2 || os.GetPodsTotal() != 0 || os.GetRttP50().AsDuration() != 3500*time.Microsecond || len(os.GetLibraries()) != 2 {
		t.Errorf("order-service: %d instances, rtt %v, libraries %v", os.GetInstances(), os.GetRttP50().AsDuration(), os.GetLibraries())
	}
	if os.Subscriptions != nil || os.PendingBytes != nil {
		t.Errorf("order-service sums set without a report: %v %v", os.Subscriptions, os.PendingBytes)
	}
	// The empty name and @pod-z are both unnamed: one without a pod, one
	// with pod-z.
	un := appByName(res, api.UnnamedApplication)
	if un.GetConnections() != 2 || un.GetInstances() != 2 || un.GetPodsTotal() != 1 {
		t.Errorf("unnamed: %d connections, %d instances, %d pods", un.GetConnections(), un.GetInstances(), un.GetPodsTotal())
	}
	// No event was captured: the 24-hour counts are unset.
	if rw.Reconnects_24H != nil || res.GetLastDay().Reconnecting != nil || res.GetEventsCaptured() {
		t.Errorf("counts without events: %v %v", rw.Reconnects_24H, res.GetLastDay())
	}
}

func TestFinishApplicationsOrderAndLimit(t *testing.T) {
	f := &api.ApplicationFacts{Epoch: 1, EpochAt: appT0}
	for i := range api.MaxApplications + 5 {
		n := 1 + i%3
		for c := range n {
			f.Connections = append(f.Connections, appConn("nats-0", int64(i*10+c), fmt.Sprintf("app-%04d@p%d", i, c), "10.0.0.1"))
		}
	}
	// A muted finding does not lift its application; an unmuted one does,
	// matched by its full connection name.
	f.Findings = []*v1.Finding{
		{Id: "client-002:app-0003@p0", CheckCode: "client-002", Severity: v1.Severity_SEVERITY_WARNING, State: v1.FindingState_FINDING_STATE_FIRING,
			Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CLIENT, Key: "app-0003@p0"}},
		{Id: "client-001:app-0006", CheckCode: "client-001", Severity: v1.Severity_SEVERITY_WARNING, State: v1.FindingState_FINDING_STATE_FIRING,
			Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CLIENT, Key: "app-0006"}, Mute: &v1.Mute{Id: "m"}},
		{Id: "client-004:app-0009@gone", CheckCode: "client-004", Severity: v1.Severity_SEVERITY_WARNING, State: v1.FindingState_FINDING_STATE_RESOLVED,
			Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CLIENT, Key: "app-0009@gone"}},
		{Id: "stream-001:APP/app-0012", CheckCode: "stream-001", Severity: v1.Severity_SEVERITY_CRITICAL, State: v1.FindingState_FINDING_STATE_FIRING,
			Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "app-0012"}},
	}
	res := api.FinishApplications(f, "", false)
	apps := res.GetApplications()
	if res.GetApplicationsTotal() != api.MaxApplications+5 || len(apps) != api.MaxApplications {
		t.Fatalf("%d of %d", len(apps), res.GetApplicationsTotal())
	}
	if apps[0].GetName() != "app-0003" || len(apps[0].GetFindings()) != 1 || apps[0].GetFindings()[0].GetEntityKey() != "app-0003@p0" {
		t.Errorf("first %s %v", apps[0].GetName(), apps[0].GetFindings())
	}
	// Then three connections each, by name.
	if apps[1].GetName() != "app-0002" || apps[1].GetConnections() != 3 || apps[2].GetName() != "app-0005" {
		t.Errorf("then %s (%d), %s", apps[1].GetName(), apps[1].GetConnections(), apps[2].GetName())
	}
	if m := appByName(res, "app-0006"); m == nil || len(m.GetFindings()) != 1 || !m.GetFindings()[0].GetMuted() {
		t.Errorf("muted finding on app-0006: %v", m.GetFindings())
	}
	for _, a := range apps {
		if a.GetName() == "app-0009" || a.GetName() == "app-0012" {
			if len(a.GetFindings()) != 0 {
				t.Errorf("%s: %v", a.GetName(), a.GetFindings())
			}
		}
	}
	if res.GetConnectionsTotal() != int32(len(f.Connections)) {
		t.Errorf("connections total %d, want %d", res.GetConnectionsTotal(), len(f.Connections))
	}
}

func TestFinishApplicationsToolingAndAccount(t *testing.T) {
	f := &api.ApplicationFacts{Epoch: 1, EpochAt: appT0, EventsCaptured: true, Connections: []api.AppConnectionFact{
		appConn("nats-0", 1, "order-service", "10.0.0.1"),
		{Server: "nats-0", CID: 2, Name: "natslens-prod", Account: "SYS", IP: "10.0.0.2", Tooling: true},
		{Server: "nats-1", CID: 3, Name: "cli", Account: "", IP: "127.0.0.1", Tooling: true},
		{Server: "nats-1", CID: 4, Name: "cli", Account: "", IP: "10.0.0.4"},
	}, Events: []api.AppEventFact{
		{Name: "cli", Account: "$G", Tooling: true, Drops: 4, Connects: 4},
		{Name: "cli", Account: "$G", Drops: 1, Connects: 1},
	}, Traffic: []api.AppTrafficFact{
		{App: "cli", Account: "", Tooling: true, Hour: appT0.Truncate(time.Hour).Add(-time.Hour), InMsgs: 50},
		{App: "cli", Account: "", Hour: appT0.Truncate(time.Hour).Add(-time.Hour), InMsgs: 7},
	}}
	res := api.FinishApplications(f, "", false)
	if res.GetToolingHidden() != 2 || res.GetApplicationsTotal() != 2 || appByName(res, "natslens-prod") != nil {
		t.Fatalf("hidden %d, %d applications", res.GetToolingHidden(), res.GetApplicationsTotal())
	}
	cli := appByName(res, "cli")
	if cli.GetConnections() != 1 || cli.GetReconnects_24H() != 1 || cli.GetInMsgs_24H() != 7 || cli.GetTooling() {
		t.Errorf("cli without tooling: %d connections, %d reconnects, %d in", cli.GetConnections(), cli.GetReconnects_24H(), cli.GetInMsgs_24H())
	}
	if cli.GetAccounts()[0].GetAccount() != "$G" {
		t.Errorf("cli accounts %v", cli.GetAccounts())
	}
	all := api.FinishApplications(f, "", true)
	cli = appByName(all, "cli")
	if all.GetToolingHidden() != 0 || cli.GetConnections() != 2 || cli.GetReconnects_24H() != 5 || cli.GetInMsgs_24H() != 57 || cli.GetTooling() {
		t.Errorf("cli with tooling: %d connections, %d reconnects, %d in", cli.GetConnections(), cli.GetReconnects_24H(), cli.GetInMsgs_24H())
	}
	if nl := appByName(all, "natslens-prod"); nl == nil || !nl.GetTooling() {
		t.Errorf("natslens-prod with tooling: %v", nl)
	}
	sys := api.FinishApplications(f, "SYS", true)
	if sys.GetApplicationsTotal() != 1 || sys.GetApplications()[0].GetName() != "natslens-prod" || sys.GetConnectionsTotal() != 1 {
		t.Errorf("SYS: %v", sys.GetApplications())
	}
	g := api.FinishApplications(f, "$G", false)
	if g.GetApplicationsTotal() != 1 || g.GetApplications()[0].GetName() != "cli" || g.GetToolingHidden() != 1 {
		t.Errorf("$G: %v hidden %d", g.GetApplications(), g.GetToolingHidden())
	}
}

func TestFinishApplicationsDayAndTraffic(t *testing.T) {
	hours := api.TrafficHours(appT0)
	f := &api.ApplicationFacts{Epoch: 1, EpochAt: appT0, EventsCaptured: true, Connections: []api.AppConnectionFact{
		appConn("nats-0", 1, "callback-dispatcher@a", "10.0.0.1"),
		appConn("nats-0", 2, "callback-dispatcher@b", "10.0.0.2"),
		appConn("nats-1", 3, "media-worker", "10.0.0.3"),
	}, Events: []api.AppEventFact{
		// Pod a dropped and came back six times; pod b's connects pair
		// with no drop of its own.
		{Name: "callback-dispatcher@a", Account: "APP", Drops: 6, Connects: 6, Stale: 6},
		{Name: "callback-dispatcher@b", Account: "APP", Connects: 9, ShortLived: 2},
		{Name: "media-worker", Account: "APP", Drops: 3, Slow: 3},
		// An application without a connection in the epoch is no row.
		{Name: "stock-poller", Account: "APP", Connects: 40, ShortLived: 40},
	}, Traffic: []api.AppTrafficFact{
		{App: "callback-dispatcher", Account: "APP", Hour: hours[22], InMsgs: 10, OutMsgs: 20, InBytes: 100, OutBytes: 200},
		{App: "callback-dispatcher", Account: "APP", Hour: hours[23], InMsgs: 1, OutMsgs: 2, InBytes: 3, OutBytes: 4},
		// Outside the hours.
		{App: "callback-dispatcher", Account: "APP", Hour: hours[23].Add(time.Hour), InMsgs: 1000},
	}}
	res := api.FinishApplications(f, "", false)
	cd := appByName(res, "callback-dispatcher")
	if cd.GetReconnects_24H() != 6 || cd.GetStale_24H() != 6 || cd.GetShortLived_24H() != 2 || cd.GetSlowConsumers_24H() != 0 {
		t.Errorf("callback-dispatcher: %v %v %v %v", cd.Reconnects_24H, cd.Stale_24H, cd.ShortLived_24H, cd.SlowConsumers_24H)
	}
	mw := appByName(res, "media-worker")
	if mw.GetReconnects_24H() != 0 || mw.Reconnects_24H == nil || mw.GetSlowConsumers_24H() != 3 {
		t.Errorf("media-worker: %v %v", mw.Reconnects_24H, mw.SlowConsumers_24H)
	}
	if d := res.GetLastDay(); d.GetSlowConsumers() != 3 || d.GetStale() != 6 || d.GetReconnecting() != 1 {
		t.Errorf("last day %v", d)
	}
	tr := cd.GetTraffic()
	if len(tr) != 24 || tr[0].InMsgs != nil || tr[22].GetInMsgs() != 10 || tr[23].GetOutBytes() != 4 || !tr[23].GetHour().AsTime().Equal(hours[23]) {
		t.Fatalf("traffic %v", tr)
	}
	if cd.GetInMsgs_24H() != 11 || cd.GetOutMsgs_24H() != 22 || cd.GetInBytes_24H() != 103 || cd.GetOutBytes_24H() != 204 {
		t.Errorf("totals %v %v %v %v", cd.InMsgs_24H, cd.OutMsgs_24H, cd.InBytes_24H, cd.OutBytes_24H)
	}
	if mw.InMsgs_24H != nil || len(mw.GetTraffic()) != 24 {
		t.Errorf("media-worker without rollup: %v, %d hours", mw.InMsgs_24H, len(mw.GetTraffic()))
	}
}

func TestFinishApplicationsSummaries(t *testing.T) {
	f := &api.ApplicationFacts{Epoch: 1, EpochAt: appT0}
	add := func(server, name, lang, version string, n int) {
		for range n {
			c := appConn(server, int64(len(f.Connections)), name, "10.0.0.1")
			c.Lang, c.Version = lang, version
			f.Connections = append(f.Connections, c)
		}
	}
	add("nats-0", "a", "nats.js", "3.0.2", 4)
	add("nats-1", "a", "nats.js", "3.0.2", 1)
	add("nats-0", "b", "go", "1.54.0", 2)
	add("nats-2", "c", "nats.js", "3.0.2", 2)
	add("nats-2", "c", "nats.js", "2.29.3", 1)
	res := api.FinishApplications(f, "", false)
	sp := res.GetSpread()
	if sp.GetServer() != "nats-0" || sp.GetConnections() != 6 || sp.GetTotal() != 10 || sp.GetServers() != 3 || !sp.GetConcentrated() {
		t.Errorf("spread %v", sp)
	}
	libs := res.GetLibraries()
	if len(libs) != 3 || libs[0].GetLang() != "nats.js" || libs[0].GetVersion() != "3.0.2" || libs[0].GetApplications() != 2 ||
		libs[1].GetLang() != "go" || libs[2].GetVersion() != "2.29.3" {
		t.Errorf("libraries %v", libs)
	}
	// Exactly half is not more than half; one server alone does not
	// speak.
	f.Connections = f.Connections[:4]
	add("nats-1", "b", "go", "1.54.0", 4)
	if sp := api.FinishApplications(f, "", false).GetSpread(); sp.GetConcentrated() || sp.GetConnections() != 4 || sp.GetTotal() != 8 || sp.GetServer() != "nats-0" {
		t.Errorf("half: %v", sp)
	}
	f.Connections = f.Connections[:4]
	if sp := api.FinishApplications(f, "", false).GetSpread(); sp.GetConcentrated() || sp.GetServers() != 1 {
		t.Errorf("one server: %v", sp)
	}
	if res := api.FinishApplications(&api.ApplicationFacts{}, "", false); res.GetEpoch() != 0 || res.GetSpread() != nil {
		t.Errorf("no epoch: %v", res)
	}
}

func TestCountClientEvent(t *testing.T) {
	var f api.AppEventFact
	for _, e := range []struct {
		kind, reason string
		churn        int32
	}{
		{"client.disconnect", "Client Closed", 0},
		{"client.disconnect", "Server Shutdown", 0},
		{"client.disconnect", "Slow Consumer (Write Deadline)", 0},
		{"client.disconnect", "Stale Connection", 0},
		{"client.disconnect", "", 0},
		{"client.connect", "", 0},
		{"client.churn", "Client Closed", 6},
	} {
		api.CountClientEvent(&f, e.kind, e.reason, e.churn)
	}
	if f.Drops != 3 || f.Connects != 7 || f.Slow != 1 || f.Stale != 1 || f.ShortLived != 6 {
		t.Errorf("%+v", f)
	}
}

// A rolling deploy drops every old pod and connects every new one under
// other names: client-004 pairs per name, so that is no reconnect. Two
// unrelated unnamed scripts, one only dropping and one only connecting,
// do not pair either; a name that drops and comes back does.
func TestFinishApplicationsReconnectsPairPerName(t *testing.T) {
	f := &api.ApplicationFacts{Epoch: 1, EpochAt: appT0, EventsCaptured: true, Connections: []api.AppConnectionFact{
		appConn("nats-0", 1, "svc@new-0", "10.0.0.1"),
		appConn("nats-0", 2, "", "10.0.0.2"),
	}}
	for i := range 10 {
		f.Events = append(f.Events, api.AppEventFact{Name: fmt.Sprintf("svc@old-%d", i), Account: "APP", Drops: 1},
			api.AppEventFact{Name: fmt.Sprintf("svc@new-%d", i), Account: "APP", Connects: 1})
	}
	f.Events = append(f.Events, api.AppEventFact{Name: "", Account: "APP", Drops: 2}, api.AppEventFact{Name: "@script-b", Account: "APP", Connects: 2},
		// The same name in two accounts is one name, as client-004 groups it.
		api.AppEventFact{Name: "svc@old-3", Account: "OPS", Connects: 1})
	res := api.FinishApplications(f, "", false)
	if svc := appByName(res, "svc"); svc.GetReconnects_24H() != 1 {
		t.Errorf("svc after a rolling deploy: %d reconnects, want 1 (svc@old-3)", svc.GetReconnects_24H())
	}
	if un := appByName(res, api.UnnamedApplication); un.GetReconnects_24H() != 0 {
		t.Errorf("unnamed scripts: %d reconnects", un.GetReconnects_24H())
	}
	if res.GetLastDay().GetReconnecting() != 1 {
		t.Errorf("reconnecting %d", res.GetLastDay().GetReconnecting())
	}
}

// On a partial epoch the missing servers' connections are not known, so
// the spread does not say one server holds most of them.
func TestFinishApplicationsPartialEpochSpread(t *testing.T) {
	f := &api.ApplicationFacts{Epoch: 1, EpochAt: appT0, Partial: true, Missing: []string{"nats-0"}, Connections: []api.AppConnectionFact{
		appConn("nats-1", 1, "a", "10.0.0.1"), appConn("nats-1", 2, "a", "10.0.0.2"), appConn("nats-2", 3, "a", "10.0.0.3"),
	}}
	res := api.FinishApplications(f, "", false)
	if sp := res.GetSpread(); sp.GetConcentrated() || sp.GetServer() != "nats-1" || sp.GetConnections() != 2 || !res.GetPartial() ||
		!slices.Equal(res.GetMissing(), []string{"nats-0"}) {
		t.Errorf("partial: spread %v, partial %v, missing %v", sp, res.GetPartial(), res.GetMissing())
	}
	f.Partial, f.Missing = false, nil
	if sp := api.FinishApplications(f, "", false).GetSpread(); !sp.GetConcentrated() {
		t.Errorf("whole: spread %v", sp)
	}
}

// A connection that reports no library is no library: it neither lists
// nor makes its application mixed.
func TestFinishApplicationsSkipsAnEmptyLibrary(t *testing.T) {
	f := &api.ApplicationFacts{Epoch: 1, EpochAt: appT0, Connections: []api.AppConnectionFact{
		appConn("nats-0", 1, "a", "10.0.0.1"),
		{Server: "nats-0", CID: 2, Name: "a", Account: "APP"},
		{Server: "nats-0", CID: 3, Name: "b", Account: "APP"},
	}}
	res := api.FinishApplications(f, "", false)
	if l := appByName(res, "a").GetLibraries(); len(l) != 1 || l[0].GetConnections() != 1 {
		t.Errorf("a: %v", l)
	}
	if l := appByName(res, "b").GetLibraries(); len(l) != 0 {
		t.Errorf("b: %v", l)
	}
	if l := res.GetLibraries(); len(l) != 1 || l[0].GetLang() != "go" || l[0].GetApplications() != 1 {
		t.Errorf("summary: %v", l)
	}
}

// Each row names its connections as the Micro services page does: an
// application named only exactly so tells apart from one with pods or an
// empty pod.
func TestFinishApplicationsConnectionNames(t *testing.T) {
	f := &api.ApplicationFacts{Epoch: 1, EpochAt: appT0, Connections: []api.AppConnectionFact{
		appConn("nats-0", 1, "order-service", "10.0.0.1"), appConn("nats-1", 2, "order-service", "10.0.0.2"),
		appConn("nats-0", 3, "foo@", "10.0.0.3"),
		appConn("nats-0", 4, "rw@d", "10.0.0.4"), appConn("nats-0", 5, "rw@a", "10.0.0.5"), appConn("nats-0", 6, "rw@c", "10.0.0.6"),
		appConn("nats-0", 7, "rw@b", "10.0.0.7"), appConn("nats-0", 8, "rw@a", "10.0.0.8"),
		appConn("nats-0", 9, "", "10.0.0.9"), appConn("nats-0", 10, "@z", "10.0.0.10"),
	}}
	res := api.FinishApplications(f, "", false)
	for _, c := range []struct {
		app   string
		names []string
		more  int32
	}{
		{"order-service", []string{"order-service"}, 0},
		{"foo", []string{"foo@"}, 0},
		{"rw", []string{"rw@a", "rw@b", "rw@c"}, 1},
		{api.UnnamedApplication, []string{"@z"}, 0},
	} {
		a := appByName(res, c.app)
		if !slices.Equal(a.GetConnectionNames(), c.names) || a.GetMoreNames() != c.more {
			t.Errorf("%s: names %v, %d more", c.app, a.GetConnectionNames(), a.GetMoreNames())
		}
	}
}
