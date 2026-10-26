package fake_test

import (
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
)

func demoApps(t *testing.T, c natslensv1connect.LensServiceClient, sys string, end time.Time, account string, tooling bool) *v1.GetApplicationsResponse {
	t.Helper()
	return must(c.GetApplications(ctx, connect.NewRequest(&v1.GetApplicationsRequest{System: sys,
		Range: &v1.TimeRange{Start: timestamppb.New(end.Add(-time.Hour)), End: timestamppb.New(end)}, Account: account, IncludeTooling: tooling})))
}

func demoApp(res *v1.GetApplicationsResponse, name string) *v1.Application {
	for _, a := range res.GetApplications() {
		if a.GetName() == name {
			return a
		}
	}
	return nil
}

// example-prod: callback-dispatcher first with its client-004 finding and
// six stale reconnects, resize-worker's four pods on three servers, an
// unnamed script whose traffic starts at 10:00, nats-0 holding most
// connections, and go and nats.js.
func TestDemoProdApplications(t *testing.T) {
	c := demo(t)
	res := demoApps(t, c, fake.Cluster, fake.FixedNow, "", false)
	if res.GetEpoch() == 0 || res.GetApplicationsTotal() != 10 || res.GetConnectionsTotal() != 187 || res.GetToolingHidden() != 0 ||
		!res.GetEventsCaptured() || res.GetConnzCut() {
		t.Fatalf("prod: epoch %d, %d applications, %d connections, %d hidden", res.GetEpoch(), res.GetApplicationsTotal(),
			res.GetConnectionsTotal(), res.GetToolingHidden())
	}
	cd := res.GetApplications()[0]
	if cd.GetName() != "callback-dispatcher" || len(cd.GetFindings()) != 1 || cd.GetFindings()[0].GetId() != "client-004:callback-dispatcher" ||
		cd.GetReconnects_24H() != 6 || cd.GetStale_24H() != 6 {
		t.Errorf("first: %s, findings %v, reconnects %d, stale %d", cd.GetName(), cd.GetFindings(), cd.GetReconnects_24H(), cd.GetStale_24H())
	}
	rw := demoApp(res, "resize-worker")
	if rw.GetConnections() != 4 || rw.GetInstances() != 4 || rw.GetPodsTotal() != 4 || len(rw.GetServers()) != 3 ||
		rw.GetServers()[0].GetServer() != "nats-0" || rw.GetServers()[0].GetConnections() != 2 {
		t.Errorf("resize-worker: %d connections, %d instances, servers %v", rw.GetConnections(), rw.GetInstances(), rw.GetServers())
	}
	un := demoApp(res, api.UnnamedApplication)
	tr := un.GetTraffic()
	if un.GetConnections() != 1 || tr[19].InMsgs != nil || tr[20].InMsgs == nil || tr[23].GetInMsgs() == 0 || !tr[20].GetHour().AsTime().Equal(at(10, 0, 0)) {
		t.Errorf("unnamed: %d connections, traffic %v", un.GetConnections(), tr)
	}
	// The media workers reconnected as each server restarted, a server
	// shutdown each time: no reconnect.
	if mw := demoApp(res, "media-worker"); mw.GetReconnects_24H() != 0 || mw.GetInMsgs_24H() == 0 {
		t.Errorf("media-worker: reconnects %d, in %d", mw.GetReconnects_24H(), mw.GetInMsgs_24H())
	}
	if ca := demoApp(res, "catalog-api"); ca.GetShortLived_24H() == 0 || ca.GetReconnects_24H() != 0 {
		t.Errorf("catalog-api: short-lived %d, reconnects %d", ca.GetShortLived_24H(), ca.GetReconnects_24H())
	}
	if sp := res.GetSpread(); sp.GetServer() != "nats-0" || sp.GetConnections() != 100 || sp.GetTotal() != 187 || !sp.GetConcentrated() {
		t.Errorf("spread %v", sp)
	}
	libs := res.GetLibraries()
	if len(libs) != 2 || libs[0].GetLang() != "nats.js" || libs[0].GetApplications() != 6 || libs[1].GetLang() != "go" || libs[1].GetApplications() != 4 {
		t.Errorf("libraries %v", libs)
	}
	if d := res.GetLastDay(); d.GetStale() != 6 || d.GetReconnecting() != 1 {
		t.Errorf("last day %v", d)
	}

	// Three hours earlier client-004 had not fired; three reconnects were
	// in the day before.
	past := demoApps(t, c, fake.Cluster, at(11, 30, 0), "", false)
	if p := demoApp(past, "callback-dispatcher"); past.GetEpochAt().AsTime().After(at(11, 30, 0)) || p.GetReconnects_24H() != 2 || len(p.GetFindings()) != 0 {
		t.Errorf("at 11:30: epoch %v, callback-dispatcher %v", past.GetEpochAt().AsTime(), p)
	}
}

// example-global: label-printer's slow consumers, a library version mix, nats-lens's own connection
// hidden as tooling, and ops-monitor alone in the system account.
func TestDemoGlobalApplications(t *testing.T) {
	c := demo(t)
	res := demoApps(t, c, fake.GlobalSystem, fake.FixedNow, "", false)
	if res.GetToolingHidden() != 1 || demoApp(res, "natslens-"+fake.GlobalSystem) != nil {
		t.Errorf("global: %d hidden", res.GetToolingHidden())
	}
	if lp := demoApp(res, "label-printer"); lp.GetSlowConsumers_24H() == 0 || res.GetLastDay().GetSlowConsumers() != lp.GetSlowConsumers_24H() {
		t.Errorf("label-printer: %d slow consumers, last day %v", lp.GetSlowConsumers_24H(), res.GetLastDay())
	}
	if ss := demoApp(res, "shipping-service"); len(ss.GetLibraries()) != 2 || ss.GetLibraries()[1].GetVersion() != "2.29.3" {
		t.Errorf("shipping-service libraries %v", ss.GetLibraries())
	}
	var libs []string
	for _, l := range res.GetLibraries() {
		libs = append(libs, l.GetLang()+" "+l.GetVersion())
	}
	if !slices.Equal(libs, []string{"nats.js 3.0.2", "go 1.54.0", "nats.js 2.29.3"}) {
		t.Errorf("libraries %v", libs)
	}
	all := demoApps(t, c, fake.GlobalSystem, fake.FixedNow, "", true)
	if nl := demoApp(all, "natslens-"+fake.GlobalSystem); nl == nil || !nl.GetTooling() || all.GetToolingHidden() != 0 {
		t.Errorf("with tooling: %v", nl)
	}
	sys := demoApps(t, c, fake.GlobalSystem, fake.FixedNow, "SYS", false)
	if len(sys.GetApplications()) != 1 || sys.GetApplications()[0].GetName() != "ops-monitor" || sys.GetToolingHidden() != 1 {
		t.Errorf("SYS: %v, hidden %d", sys.GetApplications(), sys.GetToolingHidden())
	}
}

// example-staging stopped collecting: its page is its last epoch's.
func TestDemoStagingApplications(t *testing.T) {
	c := demo(t)
	res := demoApps(t, c, fake.StagingSystem, fake.FixedNow, "", false)
	if res.GetEpoch() != 0 {
		t.Errorf("staging over the last hour: epoch %d", res.GetEpoch())
	}
	day := must(c.GetApplications(ctx, connect.NewRequest(&v1.GetApplicationsRequest{System: fake.StagingSystem})))
	if day.GetEpoch() == 0 || len(day.GetApplications()) == 0 {
		t.Errorf("staging over the day: epoch %d, %d applications", day.GetEpoch(), len(day.GetApplications()))
	}
}
