package storeprov_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// An expiry warning that fires longer than an outage of the same severity
// does not head it: the owner's decision of 2026-10-27. The systems list,
// the Overview and the Findings list all put server-001 first.
func TestExpiryWarningDoesNotHeadAnOutage(t *testing.T) {
	st := fixtureStore(t)
	set, err := checks.Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	const supplier = "ADSUPPLIERSFAKEACCOUNTKEYEXAMPLEGLOBAL7Q2M4XKV3WPJ6RT5NH"
	for epoch := int64(1); epoch <= 3; epoch++ {
		var runs []store.CheckRun
		for _, c := range set.Checks() {
			run := store.CheckRun{Code: c.Code, EntityType: c.Entity, Status: store.RunOK}
			switch {
			case c.Code == "account-003":
				run.Rows = []store.FindingRow{{EntityKey: supplier, Severity: "warning", Detail: "SUPPLIERS: account JWT expires soon."}}
			case c.Code == "server-001" && epoch >= 2:
				run.Rows = []store.FindingRow{{EntityKey: "nats-2", Severity: "warning", Detail: "nats-2 answered no monitoring request."}}
			}
			runs = append(runs, run)
		}
		if _, err := st.ApplyCheckRuns(ctx, epoch, runs); err != nil {
			t.Fatal(err)
		}
	}
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Checks: set, Clock: func() time.Time { return now }})
	r := fixtureRange()
	sys := must(c.ListSystems(ctx, connect.NewRequest(&v1.ListSystemsRequest{Range: r, StripBuckets: 3}))).GetSystems()[0]
	ov := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{Range: r})))
	list := must(c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{Range: r}))).GetFindings()
	if sys.GetWorstFinding().GetCheckCode() != "server-001" || ov.GetWorstFinding().GetCheckCode() != "server-001" ||
		len(list) != 2 || list[0].GetCheckCode() != "server-001" || !list[1].GetExpiry() || list[0].GetExpiry() {
		t.Fatalf("systems worst %s, overview worst %s, findings %v", sys.GetWorstFinding().GetCheckCode(),
			ov.GetWorstFinding().GetCheckCode(), list)
	}
}
