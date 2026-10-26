package storeprov_test

import (
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
)

var appsLast = time.Date(2026, 10, 17, 14, 25, 0, 0, time.UTC)

func appsClient(t *testing.T, p *storeprov.Provider) natslensv1connect.LensServiceClient {
	t.Helper()
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: api.Systems{{Ref: &v1.SystemRef{Id: "apps", Name: "apps"}, Provider: p}}})
	return c
}

func appsPage(t *testing.T, c natslensv1connect.LensServiceClient, end time.Time, account string, tooling bool) *v1.GetApplicationsResponse {
	t.Helper()
	return must(c.GetApplications(ctx, connect.NewRequest(&v1.GetApplicationsRequest{
		Range:   &v1.TimeRange{Start: timestamppb.New(end.Add(-time.Hour)), End: timestamppb.New(end)},
		Account: account, IncludeTooling: tooling})))
}

func appNamed(res *v1.GetApplicationsResponse, name string) *v1.Application {
	for _, a := range res.GetApplications() {
		if a.GetName() == name {
			return a
		}
	}
	return nil
}

func names(res *v1.GetApplicationsResponse) []string {
	var out []string
	for _, a := range res.GetApplications() {
		out = append(out, a.GetName())
	}
	return out
}

func TestStoreApplications(t *testing.T) {
	c := appsClient(t, appsProvider(t, appsLast))
	res := appsPage(t, c, appsLast.Add(time.Minute), "", false)
	if res.GetEpoch() != appsE3 || !res.GetEpochAt().AsTime().Equal(appsLast) || !res.GetConnzCut() || !res.GetEventsCaptured() {
		t.Fatalf("epoch %d at %v, cut %v, events %v", res.GetEpoch(), res.GetEpochAt().AsTime(), res.GetConnzCut(), res.GetEventsCaptured())
	}
	want := []string{"resize-worker", "callback-dispatcher", "order-service", "(unnamed)", "ops-monitor"}
	if got := names(res); !slices.Equal(got, want) || res.GetApplicationsTotal() != 5 || res.GetConnectionsTotal() != 15 || res.GetToolingHidden() != 2 {
		t.Fatalf("applications %v (%d), %d connections, %d hidden", got, res.GetApplicationsTotal(), res.GetConnectionsTotal(), res.GetToolingHidden())
	}

	rw := appNamed(res, "resize-worker")
	if rw.GetConnections() != 5 || rw.GetInstances() != 4 || !slices.Equal(rw.GetPods(), []string{"rw-a", "rw-b", "rw-c", "rw-d"}) {
		t.Errorf("resize-worker: %d connections, %d instances, pods %v", rw.GetConnections(), rw.GetInstances(), rw.GetPods())
	}
	var servers []string
	for _, s := range rw.GetServers() {
		servers = append(servers, s.GetServer())
	}
	if !slices.Equal(servers, []string{"nats-0", "nats-2", "nats-1"}) || rw.GetRttP50().AsDuration() != 4*time.Millisecond ||
		rw.GetSubscriptions() != 18 || rw.GetPendingBytes() != 300 || rw.GetPendingBytesMax() != 300 {
		t.Errorf("resize-worker: servers %v, rtt %v, subs %d, pending %d", servers, rw.GetRttP50().AsDuration(), rw.GetSubscriptions(), rw.GetPendingBytes())
	}
	// Its events: a slow consumer drop of rw-a and a stale drop of rw-old,
	// a pod gone by the epoch, with two connects.
	if rw.GetReconnects_24H() != 2 || rw.GetSlowConsumers_24H() != 1 || rw.GetStale_24H() != 1 || rw.GetShortLived_24H() != 0 {
		t.Errorf("resize-worker 24h: %v %v %v %v", rw.Reconnects_24H, rw.SlowConsumers_24H, rw.Stale_24H, rw.ShortLived_24H)
	}
	if len(rw.GetFindings()) != 1 || rw.GetFindings()[0].GetId() != "client-002:resize-worker@rw-c" || rw.GetFindings()[0].GetMuted() {
		t.Errorf("resize-worker findings %v", rw.GetFindings())
	}
	tr := rw.GetTraffic()
	if len(tr) != 24 || tr[15].InMsgs != nil || tr[16].GetInMsgs() != 8000 || tr[23].GetInMsgs() != 1000 || tr[23].GetOutBytes() != 200_000 ||
		!tr[23].GetHour().AsTime().Equal(appsLast.Truncate(time.Hour).Add(-time.Hour)) || rw.GetInMsgs_24H() != 36000 {
		t.Errorf("resize-worker traffic: %v, total %d", tr, rw.GetInMsgs_24H())
	}

	cd := appNamed(res, "callback-dispatcher")
	if cd.GetInstances() != 2 || cd.GetPodsTotal() != 0 || len(cd.GetLibraries()) != 2 || cd.GetReconnects_24H() != 6 || cd.GetStale_24H() != 6 {
		t.Errorf("callback-dispatcher: %d instances, libraries %v, reconnects %d, stale %d", cd.GetInstances(), cd.GetLibraries(),
			cd.GetReconnects_24H(), cd.GetStale_24H())
	}
	// Its own hour and those before the 24 are left out.
	if cd.GetInMsgs_24H() != 240 || len(cd.GetFindings()) != 1 || cd.GetFindings()[0].GetCheckCode() != "client-004" {
		t.Errorf("callback-dispatcher: in %d, findings %v", cd.GetInMsgs_24H(), cd.GetFindings())
	}
	os := appNamed(res, "order-service")
	if os.GetReconnects_24H() != 0 || os.GetShortLived_24H() != 12 || os.GetInMsgs_24H() != 50 || os.GetInstances() != 5 {
		t.Errorf("order-service: reconnects %d, short-lived %d, in %d", os.GetReconnects_24H(), os.GetShortLived_24H(), os.GetInMsgs_24H())
	}
	un := appNamed(res, api.UnnamedApplication)
	if un.GetConnections() != 2 || un.GetInstances() != 2 || !slices.Equal(un.GetPods(), []string{"pod-z"}) {
		t.Errorf("unnamed: %d connections, %d instances, pods %v", un.GetConnections(), un.GetInstances(), un.GetPods())
	}
	if om := appNamed(res, "ops-monitor"); om.GetAccounts()[0].GetAccount() != "SYS" || om.GetReconnects_24H() != 1 {
		t.Errorf("ops-monitor: %v, reconnects %d", om.GetAccounts(), om.GetReconnects_24H())
	}

	if sp := res.GetSpread(); sp.GetServer() != "nats-0" || sp.GetConnections() != 8 || sp.GetTotal() != 15 || sp.GetServers() != 3 || !sp.GetConcentrated() {
		t.Errorf("spread %v", sp)
	}
	var libs []string
	for _, l := range res.GetLibraries() {
		libs = append(libs, l.GetLang()+" "+l.GetVersion())
	}
	if !slices.Equal(libs, []string{"go 1.54.0", "nats.js 3.0.2", "nats.js 2.29.3", "python3 2.7.0"}) {
		t.Errorf("libraries %v", libs)
	}
	if d := res.GetLastDay(); d.GetSlowConsumers() != 1 || d.GetStale() != 7 || d.GetReconnecting() != 3 {
		t.Errorf("last day %v", d)
	}
}

func TestStoreApplicationsToolingAndScope(t *testing.T) {
	p := appsProvider(t, appsLast)
	c := appsClient(t, p)
	end := appsLast.Add(time.Minute)
	all := appsPage(t, c, end, "", true)
	if all.GetToolingHidden() != 0 || all.GetApplicationsTotal() != 7 || all.GetConnectionsTotal() != 17 {
		t.Fatalf("with tooling: %v, %d connections, %d hidden", names(all), all.GetConnectionsTotal(), all.GetToolingHidden())
	}
	nl, cli := appNamed(all, "natslens-apps"), appNamed(all, "nats-cli")
	if !nl.GetTooling() || nl.GetReconnects_24H() != 1 || nl.GetInMsgs_24H() != 70 {
		t.Errorf("natslens-apps: tooling %v, reconnects %d, in %d", nl.GetTooling(), nl.GetReconnects_24H(), nl.GetInMsgs_24H())
	}
	if !cli.GetTooling() || cli.GetReconnects_24H() != 3 || cli.GetInMsgs_24H() != 7 || cli.GetAccounts()[0].GetAccount() != "$G" {
		t.Errorf("nats-cli: tooling %v, reconnects %d, in %d, accounts %v", cli.GetTooling(), cli.GetReconnects_24H(), cli.GetInMsgs_24H(), cli.GetAccounts())
	}
	if os := appNamed(all, "order-service"); os.GetInMsgs_24H() != 55 {
		t.Errorf("order-service with its loopback hour: %d", os.GetInMsgs_24H())
	}

	sys := appsPage(t, c, end, "SYS", false)
	if !slices.Equal(names(sys), []string{"ops-monitor"}) || sys.GetToolingHidden() != 1 || sys.GetLastDay().GetReconnecting() != 1 {
		t.Errorf("SYS: %v, hidden %d, %v", names(sys), sys.GetToolingHidden(), sys.GetLastDay())
	}
	g := appsPage(t, c, end, "$G", true)
	if !slices.Equal(names(g), []string{"nats-cli"}) || g.GetApplications()[0].GetReconnects_24H() != 3 {
		t.Errorf("$G: %v", names(g))
	}

	// With loopback not counted as tooling, nats-cli is an application and
	// the loopback hours count.
	p.LoopbackNotTooling = true
	res := appsPage(t, c, end, "", false)
	if cli := appNamed(res, "nats-cli"); cli == nil || cli.GetTooling() || cli.GetInMsgs_24H() != 7 || res.GetToolingHidden() != 1 {
		t.Errorf("loopback not tooling: %v, hidden %d", cli, res.GetToolingHidden())
	}
	if os := appNamed(res, "order-service"); os.GetInMsgs_24H() != 55 {
		t.Errorf("order-service with loopback not tooling: %d", os.GetInMsgs_24H())
	}
}

func TestStoreApplicationsPastRange(t *testing.T) {
	c := appsClient(t, appsProvider(t, appsLast))
	e2 := appsLast.Add(-3 * time.Hour)
	res := appsPage(t, c, e2.Add(time.Minute), "", false)
	if res.GetEpoch() != appsE2 || res.GetConnzCut() || !slices.Equal(names(res), []string{"resize-worker", "callback-dispatcher"}) {
		t.Fatalf("past: epoch %d, cut %v, %v", res.GetEpoch(), res.GetConnzCut(), names(res))
	}
	rw, cd := res.GetApplications()[0], res.GetApplications()[1]
	if rw.GetInstances() != 2 || len(rw.GetFindings()) != 1 {
		t.Errorf("resize-worker then: %d instances, findings %v", rw.GetInstances(), rw.GetFindings())
	}
	// The 24 hours before E2 hold the six stale drops and the two older
	// ones; the connects number six. The rollup reaches 22 of its hours.
	if cd.GetStale_24H() != 8 || cd.GetReconnects_24H() != 6 || len(cd.GetFindings()) != 0 {
		t.Errorf("callback-dispatcher then: stale %d, reconnects %d, findings %v", cd.GetStale_24H(), cd.GetReconnects_24H(), cd.GetFindings())
	}
	if h := cd.GetTraffic()[23].GetHour().AsTime(); !h.Equal(e2.Truncate(time.Hour).Add(-time.Hour)) || cd.GetInMsgs_24H() != 220 || cd.GetTraffic()[1].InMsgs != nil || cd.GetTraffic()[2].GetInMsgs() != 10 {
		t.Errorf("callback-dispatcher then: last hour %v, in %d", h, cd.GetInMsgs_24H())
	}
	// Before E1 there is no epoch.
	if res := appsPage(t, c, appsLast.Add(-27*time.Hour), "", false); res.GetEpoch() != 0 || len(res.GetApplications()) != 0 {
		t.Errorf("before E1: %v", res)
	}
}

// The page reads the same in any session time zone: its times are passed
// as Unix microseconds.
func TestStoreApplicationsAnyTimeZone(t *testing.T) {
	p := appsProvider(t, appsLast)
	c := appsClient(t, p)
	setZone := func(zone string) {
		t.Helper()
		if _, err := p.St.DB().Exec(`SET GLOBAL TimeZone = '` + zone + `'`); err != nil {
			t.Fatal(err)
		}
	}
	pages := func() []*v1.GetApplicationsResponse {
		return []*v1.GetApplicationsResponse{appsPage(t, c, appsLast.Add(time.Minute), "", true),
			appsPage(t, c, appsLast.Add(-3*time.Hour+time.Minute), "", false)}
	}
	setZone("UTC")
	want := pages()
	for _, zone := range []string{"Pacific/Auckland", "America/Los_Angeles", "Asia/Kolkata"} {
		setZone(zone)
		for i, got := range pages() {
			if !proto.Equal(got, want[i]) {
				t.Errorf("%s, page %d differs from UTC", zone, i)
			}
		}
	}
	setZone("UTC")
}
