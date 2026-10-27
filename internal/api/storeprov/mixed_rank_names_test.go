package storeprov_test

import (
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// A mix of findings across severities, expiry and not,
// one muted, and two accounts of one name. Every list and headline that
// uses CompareWorst agrees, and every EntityRef in the responses carries
// the right parts and the account's name. (A reviewer's case.)
func TestMixedRankAndNames(t *testing.T) {
	st := fixtureStore(t)
	set, err := checks.Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	const s1 = "ADSUPPLIERSFAKEACCOUNTKEYEXAMPLEGLOBAL7Q2M4XKV3WPJ6RT5NH"
	const s2 = "AXSUPPLIERSOTHERACCOUNTKEYEXAMPLEGLOBAL7Q2M4XKV3WPJ6RT5N"
	rows{t, st}.add("accounts", map[string]any{"account": s1, "account_name": "SUPPLIERS", "is_system": false, "first_epoch": 1, "last_epoch": 3})
	rows{t, st}.add("accounts", map[string]any{"account": s2, "account_name": "SUPPLIERS", "is_system": false, "first_epoch": 1, "last_epoch": 3})
	for epoch := int64(1); epoch <= 3; epoch++ {
		var runs []store.CheckRun
		for _, c := range set.Checks() {
			run := store.CheckRun{Code: c.Code, EntityType: c.Entity, Status: store.RunOK}
			switch {
			case c.Code == "account-003":
				run.Rows = []store.FindingRow{{EntityKey: s1, Severity: "critical", Detail: "SUPPLIERS: account JWT expired."}}
			case c.Code == "server-001" && epoch >= 2:
				run.Rows = []store.FindingRow{{EntityKey: "nats-2", Severity: "critical", Detail: "nats-2 answered no monitoring request."}}
			case c.Code == "account-004":
				run.Rows = []store.FindingRow{{EntityKey: s2, Severity: "warning", Detail: "SUPPLIERS: user JWT expires in 10 days."}}
			case c.Code == "stream-005" && epoch >= 3:
				run.Rows = []store.FindingRow{{EntityKey: "APP/order-jobs", Severity: "warning", Detail: "order-jobs has no limit."}}
			case c.Code == "consumer-002" && epoch >= 2:
				run.Rows = []store.FindingRow{{EntityKey: "APP/order-jobs/resize-worker", Severity: "warning", Detail: "backlog grows."}}
			case c.Code == "server-016":
				run.Rows = []store.FindingRow{{EntityKey: "nats-0", Severity: "warning", Detail: "certificate expires in 10 days."}}
			}
			runs = append(runs, run)
		}
		if _, err := st.ApplyCheckRuns(ctx, epoch, runs); err != nil {
			t.Fatal(err)
		}
	}
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Checks: set, Clock: func() time.Time { return now }})
	exp := timestamppb.New(now.Add(24 * time.Hour))
	for _, m := range []*v1.MuteFindingRequest{
		{CheckCode: "server-016", Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: "nats-0"}},
		{CheckCode: "stream-001", Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "APP/order-jobs"}},
		{CheckCode: "consumer-001", Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: "APP/order-jobs/resize-worker"}},
		{CheckCode: "account-002", Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_ACCOUNT, Key: s1}},
		{CheckCode: "client-001", Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CLIENT, Key: "media-worker"}},
		{CheckCode: "server-005"},
	} {
		m.Reason, m.MutedBy, m.ExpiresAt = "review", "op", exp
		must(c.MuteFinding(ctx, connect.NewRequest(m)))
	}
	r := fixtureRange()

	fs := must(c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{Range: r, PageSize: 100, IncludeMuted: true}))).GetFindings()
	var got []string
	for _, f := range fs {
		got = append(got, f.GetCheckCode())
	}
	want := []string{"account-003", "server-001", "consumer-002", "stream-005", "account-004", "server-016"}
	if !slices.Equal(got, want) {
		t.Errorf("ListFindings order %v, want %v", got, want)
	}
	ov := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{Range: r}))).GetWorstFinding()
	sys := must(c.ListSystems(ctx, connect.NewRequest(&v1.ListSystemsRequest{Range: r, StripBuckets: 3}))).GetSystems()[0].GetWorstFinding()
	if ov.GetCheckCode() != "account-003" || !proto.Equal(ov, sys) {
		t.Errorf("overview worst %v; systems worst %v", ov, sys)
	}

	checkRef := func(where string, e *v1.EntityRef) {
		t.Helper()
		switch e.GetType() {
		case v1.EntityType_ENTITY_TYPE_ACCOUNT:
			if e.GetAccount() != e.GetKey() || e.GetAccountName() != "SUPPLIERS" || e.GetServer() != "" || e.GetStream() != "" {
				t.Errorf("%s: account entity %v", where, e)
			}
		case v1.EntityType_ENTITY_TYPE_STREAM:
			if e.GetAccount() != "APP" || e.GetStream() != "order-jobs" || e.GetName() != "order-jobs" || e.GetServer() != "" {
				t.Errorf("%s: stream entity %v", where, e)
			}
		case v1.EntityType_ENTITY_TYPE_CONSUMER:
			if e.GetAccount() != "APP" || e.GetStream() != "order-jobs" || e.GetConsumer() != "resize-worker" || e.GetName() != "resize-worker" {
				t.Errorf("%s: consumer entity %v", where, e)
			}
		case v1.EntityType_ENTITY_TYPE_SERVER:
			if e.GetServer() != e.GetKey() || e.GetName() != e.GetKey() || e.GetAccount() != "" || e.GetAccountName() != "" {
				t.Errorf("%s: server entity %v", where, e)
			}
		case v1.EntityType_ENTITY_TYPE_CLIENT:
			if e.GetName() != e.GetKey() || e.GetAccount() != "" || e.GetServer() != "" {
				t.Errorf("%s: client entity %v", where, e)
			}
		}
	}
	for _, f := range fs {
		checkRef("ListFindings "+f.GetCheckCode(), f.GetEntity())
		g := must(c.GetFinding(ctx, connect.NewRequest(&v1.GetFindingRequest{Id: f.GetId()}))).GetFinding()
		if !proto.Equal(g.GetEntity(), f.GetEntity()) || g.GetExpiry() != f.GetExpiry() {
			t.Errorf("GetFinding %s entity %v expiry %v, list %v expiry %v", f.GetId(), g.GetEntity(), g.GetExpiry(), f.GetEntity(), f.GetExpiry())
		}
		if f.GetMute() != nil {
			checkRef("finding mute "+f.GetCheckCode(), f.GetMute().GetEntity())
		}
	}
	ms := must(c.ListMutes(ctx, connect.NewRequest(&v1.ListMutesRequest{IncludeInactive: true}))).GetMutes()
	if len(ms) != 6 {
		t.Fatalf("%d mutes", len(ms))
	}
	for _, m := range ms {
		if m.GetCheckCode() == "server-005" {
			if m.GetEntity() != nil {
				t.Errorf("mute of every entity has entity %v", m.GetEntity())
			}
			continue
		}
		checkRef("ListMutes "+m.GetCheckCode(), m.GetEntity())
	}
	tl := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{Range: r}))).GetRows()
	for _, row := range tl {
		if row.GetEntity() != nil {
			checkRef("timeline "+row.GetKey(), row.GetEntity())
		}
	}
	tle := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{Range: r,
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_ACCOUNT, Key: s2}}))).GetRows()
	found := false
	for _, row := range tle {
		if row.GetCheckCode() == "account-004" {
			found = true
		}
		if row.GetEntity() != nil {
			checkRef("entity timeline "+row.GetKey(), row.GetEntity())
		}
	}
	if !found {
		t.Errorf("entity timeline of %s has no account-004 row: %v", s2, tle)
	}
	rp := must(c.GetReplay(ctx, connect.NewRequest(&v1.GetReplayRequest{A: timestamppb.New(t0), B: timestamppb.New(now)})))
	accSteps := 0
	for _, s := range rp.GetSteps() {
		for _, e := range s.GetEntities() {
			if e.GetType() == v1.EntityType_ENTITY_TYPE_ACCOUNT {
				accSteps++
			}
			checkRef("replay "+s.GetId(), e)
		}
	}
	if accSteps == 0 {
		t.Errorf("no replay step names an account: %d steps", len(rp.GetSteps()))
	}
	accRows := 0
	for _, row := range tle {
		if row.GetEntity().GetType() == v1.EntityType_ENTITY_TYPE_ACCOUNT {
			accRows++
		}
	}
	if accRows == 0 {
		t.Errorf("no timeline row names an account: %d rows", len(tl))
	}
}
