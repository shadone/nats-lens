package storeprov_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// A mute of an account finding, as ListMutes returns it
// from the store, carries the account's readable name, so the mutes list
// and "Lift mute of account-003 for SUPPLIERS…" name it as the finding row
// does.
func TestAccountMuteIsNamed(t *testing.T) {
	const key = "ADSUPPLIERSFAKEACCOUNTKEYEXAMPLEGLOBAL7Q2M4XKV3WPJ6RT5NH"
	st := emptyStore(t)
	r := rows{t, st}
	r.add("accounts", map[string]any{"account": key, "account_name": "SUPPLIERS", "is_system": false, "first_epoch": 1, "last_epoch": 1})
	_, c := serve(t, st)
	must(c.MuteFinding(ctx, connect.NewRequest(&v1.MuteFindingRequest{CheckCode: "account-003", Reason: "renewing", MutedBy: "op",
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_ACCOUNT, Key: key}, ExpiresAt: timestamppb.New(now.Add(time.Hour))})))
	ms := must(c.ListMutes(ctx, connect.NewRequest(&v1.ListMutesRequest{}))).GetMutes()
	if len(ms) != 1 || ms[0].GetEntity().GetAccountName() != "SUPPLIERS" {
		t.Fatalf("mute entity %v, want account_name SUPPLIERS", ms[0].GetEntity())
	}
}

// The Fleet card's headline names the worst finding's
// entity with entityName; for an account finding ListSystems must carry
// the account's name as GetOverview does, or the card shows the key.
func TestSystemsWorstAccountIsNamed(t *testing.T) {
	st := fixtureStore(t)
	set, err := checks.Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	const supplier = "ADSUPPLIERSFAKEACCOUNTKEYEXAMPLEGLOBAL7Q2M4XKV3WPJ6RT5NH"
	rows{t, st}.add("accounts", map[string]any{"account": supplier, "account_name": "SUPPLIERS", "is_system": false, "first_epoch": 1, "last_epoch": 3})
	for epoch := int64(1); epoch <= 3; epoch++ {
		var runs []store.CheckRun
		for _, c := range set.Checks() {
			run := store.CheckRun{Code: c.Code, EntityType: c.Entity, Status: store.RunOK}
			if c.Code == "account-003" {
				run.Rows = []store.FindingRow{{EntityKey: supplier, Severity: "warning", Detail: "SUPPLIERS: account JWT expires soon."}}
			}
			runs = append(runs, run)
		}
		if _, err := st.ApplyCheckRuns(ctx, epoch, runs); err != nil {
			t.Fatal(err)
		}
	}
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Checks: set, Clock: func() time.Time { return now }})
	r := fixtureRange()
	ov := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{Range: r}))).GetWorstFinding()
	sys := must(c.ListSystems(ctx, connect.NewRequest(&v1.ListSystemsRequest{Range: r, StripBuckets: 3}))).GetSystems()[0].GetWorstFinding()
	if ov.GetEntity().GetAccountName() != "SUPPLIERS" || sys.GetEntity().GetAccountName() != "SUPPLIERS" {
		t.Fatalf("overview worst entity %v; systems worst entity %v", ov.GetEntity(), sys.GetEntity())
	}
}
