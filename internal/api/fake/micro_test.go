package fake_test

import (
	"math"
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

func microOf(t *testing.T, c natslensv1connect.LensServiceClient, system string, end time.Time) *v1.GetMicroServicesResponse {
	t.Helper()
	req := &v1.GetMicroServicesRequest{System: system}
	if !end.IsZero() {
		req.Range = &v1.TimeRange{Start: timestamppb.New(end.Add(-time.Hour)), End: timestamppb.New(end)}
	}
	return must(c.GetMicroServices(ctx, connect.NewRequest(req)))
}

func microByName(res *v1.GetMicroServicesResponse, name string) *v1.MicroService {
	for _, s := range res.GetServices() {
		if s.GetName() == name {
			return s
		}
	}
	return nil
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// example-global tells the board's story: orders' get endpoint ten times
// its 7-day level since 08:12 and a duplicate order on create; pricing on
// two versions, one instance restarted at 14:21 without a gap in the rate;
// inventory healthy; SUPPLIERS asked and refused, its request held since
// the refusal 5 minutes before.
func TestGlobalMicroStory(t *testing.T) {
	res := microOf(t, demo(t), fake.GlobalSystem, time.Time{})
	var names []string
	for _, s := range res.GetServices() {
		names = append(names, s.GetName())
	}
	if !slices.Equal(names, []string{"orders", "pricing", "inventory"}) || res.SubscriptionsAt == nil {
		t.Fatalf("services %v, subscriptions at %v", names, res.GetSubscriptionsAt())
	}
	o := microByName(res, "orders")
	if o.GetNote() != v1.MicroNote_MICRO_NOTE_SLOW_ENDPOINT || o.GetInstances() != 3 || round1(o.GetRequestsPerSecond()) != 161.7 ||
		o.GetVersions()[0].GetVersion() != "1.4.2" || o.GetDescription() != "Order intake and lookup" ||
		!slices.Equal(o.GetConnectionNames(), []string{"order-service"}) {
		t.Errorf("orders = %v", o)
	}
	get := o.GetEndpoints()[0]
	if get.GetName() != "get" || get.GetSubject() != "orders.get.*" || get.GetQueueGroup() != "q" || !get.GetSlow() || round1(get.GetAverageMs()) != 41 ||
		get.GetLevelMs() != 4.1 || get.GetSlowSince().AsTime().Format("15:04") != "08:12" {
		t.Errorf("get = %v", get)
	}
	for _, e := range o.GetEndpoints() {
		if e.GetName() == "create" && (e.GetLastError() != "409 duplicate order id" || e.GetLastErrorInstance() == "" || e.GetSlow() ||
			round1(e.GetErrorShare()*100) != 0.1) {
			t.Errorf("create = %v", e)
		}
	}
	p := microByName(res, "pricing")
	if p.GetNote() != v1.MicroNote_MICRO_NOTE_MIXED_VERSIONS || p.GetInstances() != 4 || len(p.GetVersions()) != 2 ||
		p.GetVersions()[0].GetInstances() != 3 || p.GetVersions()[1].GetVersion() != "1.9.3" || round1(p.GetRequestsPerSecond()) != 88 ||
		len(p.GetMissing()) != 0 {
		t.Errorf("pricing = %v", p)
	}
	restarted := 0
	for _, in := range p.GetInstanceList() {
		if in.GetStarted().AsTime().After(res.GetEpochAt().AsTime().Add(-api.RateSpan)) {
			restarted++
		}
	}
	if restarted != 1 {
		t.Errorf("%d pricing instances restarted in the window", restarted)
	}
	inv := microByName(res, "inventory")
	if inv.GetNote() != v1.MicroNote_MICRO_NOTE_HEALTHY || inv.GetInstances() != 2 || round1(inv.GetRequestsPerSecond()) != 12.6 {
		t.Errorf("inventory = %v", inv)
	}
	// By account: SUPPLIERS' key sorts before APP.
	if len(res.GetAccounts()) != 2 || res.GetAccounts()[1].GetAccount() != fake.Account || !res.GetAccounts()[1].GetAsked() ||
		res.GetAccounts()[1].GetStats().GetReplies() != 9 || res.GetAccounts()[0].GetAccount() != fake.SuppliersAccount ||
		res.GetAccounts()[0].GetStats().GetState() != v1.MicroRequestState_MICRO_REQUEST_STATE_HELD || res.GetAccounts()[0].GetServices() != 0 ||
		res.GetAccounts()[0].GetStats().GetError() == "" ||
		!res.GetAccounts()[0].GetStats().GetRefusedAt().AsTime().Equal(res.GetEpochAt().AsTime().Add(-5*time.Minute)) ||
		!res.GetAccounts()[0].GetStats().GetRetryAfter().AsTime().Equal(res.GetEpochAt().AsTime().Add(5*time.Minute)) ||
		res.GetAccounts()[0].GetAccountName() != "SUPPLIERS" {
		t.Errorf("accounts = %v", res.GetAccounts())
	}
}

// An inventory instance hung from 10:05 to 10:47: missing then, its
// subscriptions still listed and the rate halved; before and after,
// healthy. orders' get endpoint was not slow before 08:12.
func TestGlobalMicroHistory(t *testing.T) {
	c := demo(t)
	day := fake.FixedNow.Truncate(24 * time.Hour)
	hung := microByName(microOf(t, c, fake.GlobalSystem, day.Add(10*time.Hour+30*time.Minute)), "inventory")
	if hung.GetNote() != v1.MicroNote_MICRO_NOTE_INSTANCE_MISSING || hung.GetInstances() != 1 || len(hung.GetMissing()) != 1 ||
		!hung.GetMissing()[0].GetStillSubscribed() || hung.GetMissing()[0].GetSince().AsTime().Format("15:04") != "10:05" ||
		hung.GetMissing()[0].GetConnectionName() != "inventory-service" || round1(hung.GetRequestsPerSecond()) != 6.3 {
		t.Errorf("inventory at 10:30 = %v", hung)
	}
	for _, h := range []time.Duration{10 * time.Hour, 11 * time.Hour} {
		if s := microByName(microOf(t, c, fake.GlobalSystem, day.Add(h)), "inventory"); s.GetNote() != v1.MicroNote_MICRO_NOTE_HEALTHY {
			t.Errorf("inventory before %v = %v", h, s)
		}
	}
	o := microByName(microOf(t, c, fake.GlobalSystem, day.Add(8*time.Hour)), "orders")
	if o.GetNote() != v1.MicroNote_MICRO_NOTE_HEALTHY || o.GetEndpoints()[0].GetSlow() || o.GetEndpoints()[0].LevelMs == nil {
		t.Errorf("orders at 08:00 = %v", o)
	}
	// Before the restart, the old pricing instance answers.
	p := microByName(microOf(t, c, fake.GlobalSystem, day.Add(14*time.Hour+20*time.Minute)), "pricing")
	for _, in := range p.GetInstanceList() {
		if in.GetStarted().AsTime().After(day.Add(14 * time.Hour)) {
			t.Errorf("pricing at 14:20 has %v", in)
		}
	}
}

// example-prod's account is not asked: its services are seen in
// subscriptions only. example-staging has none.
func TestProdAndStagingMicro(t *testing.T) {
	c := demo(t)
	res := microOf(t, c, fake.Cluster, time.Time{})
	if len(res.GetServices()) != 2 || len(res.GetAccounts()) != 1 || res.GetAccounts()[0].GetAsked() {
		t.Fatalf("prod = %v", res)
	}
	for _, s := range res.GetServices() {
		if s.GetAsked() || s.GetNote() != v1.MicroNote_MICRO_NOTE_NOT_ASKED || s.GetInstances() != 2 || len(s.GetEndpoints()) != 0 ||
			len(s.GetVersions()) != 0 || s.GetDescription() != "" || s.RequestsPerSecond != nil {
			t.Errorf("prod service = %v", s)
		}
	}
	st := microOf(t, c, fake.StagingSystem, time.Time{})
	if st.GetEpoch() == 0 || len(st.GetServices()) != 0 || len(st.GetAccounts()) != 0 || st.SubscriptionsAt != nil {
		t.Errorf("staging = %v", st)
	}
}

// The instances' $SRV subscriptions and endpoints are rows of the Subjects
// page, held by the connections the Micro services page names.
func TestGlobalMicroOnSubjectsPage(t *testing.T) {
	c := demo(t)
	subjects := subjectsOf(t, c, fake.GlobalSystem)
	if r := subjectRow(subjects, fake.Account, "orders.get.*"); r == nil || r.GetConnections() != 3 || r.GetQueue() != "q" ||
		!slices.Equal(r.GetClientNames(), []string{"order-service"}) {
		t.Errorf("orders.get.* = %v", r)
	}
	if r := subjectRow(subjects, fake.Account, "$SRV.PING"); r == nil || r.GetConnections() != 9 {
		t.Errorf("$SRV.PING = %v", r)
	}
	for _, s := range microOf(t, c, fake.GlobalSystem, time.Time{}).GetServices() {
		for _, in := range s.GetInstanceList() {
			if r := subjectRow(subjects, fake.Account, "$SRV.STATS."+s.GetName()+"."+in.GetId()); r == nil || r.GetConnections() != 1 ||
				!slices.Equal(r.GetClientNames(), []string{in.GetConnectionName()}) {
				t.Errorf("%s/%s: row %v", s.GetName(), in.GetId(), r)
			}
		}
	}
}

// A connection's subscription count on the Connections page is what the
// Subjects page lists for it: an instance's connection holds at least its
// nine $SRV subscriptions and its endpoints'.
func TestGlobalMicroConnectionsCountTheirSubscriptions(t *testing.T) {
	c := demo(t)
	// The most subscriptions a connection of a name holds on a server: an
	// instance names its connection by name and server only.
	most := map[string]int64{}
	for _, x := range must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{System: fake.GlobalSystem, IncludeTooling: true,
		PageSize: 1000}))).GetConnections() {
		k := x.GetServer() + "/" + x.GetName()
		most[k] = max(most[k], x.GetSubscriptions())
	}
	for _, s := range microOf(t, c, fake.GlobalSystem, time.Time{}).GetServices() {
		for _, in := range s.GetInstanceList() {
			if n := most[in.GetServer()+"/"+in.GetConnectionName()]; n < int64(9+len(s.GetEndpoints())) {
				t.Errorf("%s/%s on %s: at most %d subscriptions", s.GetName(), in.GetId(), in.GetServer(), n)
			}
		}
	}
}

// SUPPLIERS' requests are refused, then held for 10 minutes, then sent
// and refused again: the epochs alternate between one refusal and a run
// of held requests that name it.
func TestGlobalMicroRefusalAndHold(t *testing.T) {
	c := demo(t)
	states := map[v1.MicroRequestState]int{}
	for m := 0; m < 30; m++ {
		end := fake.FixedNow.Add(-time.Duration(m) * 30 * time.Second)
		for _, a := range microOf(t, c, fake.GlobalSystem, end).GetAccounts() {
			if a.GetAccount() != fake.SuppliersAccount {
				continue
			}
			st := a.GetStats()
			states[st.GetState()]++
			if st.GetState() == v1.MicroRequestState_MICRO_REQUEST_STATE_HELD &&
				(st.RefusedAt == nil || !st.GetRefusedAt().AsTime().Before(st.GetAt().AsTime()) || st.GetRetryAfter().AsTime().Sub(st.GetRefusedAt().AsTime()) != 10*time.Minute) {
				t.Errorf("held %v", st)
			}
			if st.GetState() == v1.MicroRequestState_MICRO_REQUEST_STATE_ERROR && (st.RefusedAt != nil || st.GetError() == "") {
				t.Errorf("refused %v", st)
			}
		}
	}
	if states[v1.MicroRequestState_MICRO_REQUEST_STATE_ERROR] == 0 || states[v1.MicroRequestState_MICRO_REQUEST_STATE_HELD] == 0 {
		t.Errorf("states %v", states)
	}
}
