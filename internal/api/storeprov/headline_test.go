package storeprov_test

import (
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// An expiry warning that fires longer than an outage of the same severity
// does not head it: the owner's decision of 2026-10-27 for the headline of
// the systems list and the Overview. That the Findings list also puts
// server-001 first is the session's reading, which the owner confirmed on
// 2026-10-27.
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

// Severity comes before the expiry rank, so a critical
// expiry heads a warning outage, and among criticals an expiry ranks as any
// other (the longest firing first); between two expiries of one severity
// the longest firing leads; resolved and muted keep their places.
func TestCompareWorstOrder(t *testing.T) {
	at := func(h int) *timestamppb.Timestamp {
		return timestamppb.New(time.Date(2026, 10, 27, h, 0, 0, 0, time.UTC))
	}
	fs := []*v1.Finding{
		{Id: "out-warn", Severity: v1.Severity_SEVERITY_WARNING, Since: at(5)},
		{Id: "out-crit", Severity: v1.Severity_SEVERITY_CRITICAL, Since: at(7)},
		{Id: "exp-crit", Severity: v1.Severity_SEVERITY_CRITICAL, Expiry: true, Since: at(6)},
		{Id: "exp-warn-new", Severity: v1.Severity_SEVERITY_WARNING, Expiry: true, Since: at(4)},
		{Id: "exp-warn-old", Severity: v1.Severity_SEVERITY_WARNING, Expiry: true, Since: at(1)},
		{Id: "out-info", Severity: v1.Severity_SEVERITY_INFO, Since: at(1)},
		{Id: "muted-crit", Severity: v1.Severity_SEVERITY_CRITICAL, Since: at(1), Mute: &v1.Mute{Id: "1"}},
		{Id: "resolved-crit", Severity: v1.Severity_SEVERITY_CRITICAL, Since: at(1), State: v1.FindingState_FINDING_STATE_RESOLVED},
	}
	slices.SortStableFunc(fs, api.CompareWorst)
	var got []string
	for _, f := range fs {
		got = append(got, f.GetId())
	}
	want := []string{"exp-crit", "out-crit", "out-warn", "exp-warn-old", "exp-warn-new", "out-info", "muted-crit", "resolved-crit"}
	if !slices.Equal(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
}

// A critical expiry heads a warning outage that fires longer, in the
// systems list and the Overview, and GetFinding marks it expiry.
func TestCriticalExpiryHeadsWarningOutage(t *testing.T) {
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
			case c.Code == "account-003" && epoch >= 2:
				run.Rows = []store.FindingRow{{EntityKey: supplier, Severity: "critical", Detail: "SUPPLIERS: account JWT expired."}}
			case c.Code == "server-001":
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
	if sys.GetWorstFinding().GetCheckCode() != "account-003" || ov.GetWorstFinding().GetCheckCode() != "account-003" ||
		!ov.GetWorstFinding().GetExpiry() {
		t.Fatalf("systems worst %v, overview worst %v", sys.GetWorstFinding(), ov.GetWorstFinding())
	}
	f := must(c.GetFinding(ctx, connect.NewRequest(&v1.GetFindingRequest{Id: ov.GetWorstFinding().GetId()}))).GetFinding()
	if !f.GetExpiry() {
		t.Errorf("GetFinding expiry = false")
	}
}
