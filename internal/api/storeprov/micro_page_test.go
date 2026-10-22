package storeprov_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// The micro store holds 14 epochs, 5 minutes apart, ending at last; the
// page's epoch is 14. Subscription lists were collected at 1, 6, 10 and
// 13; nats-2's list at 13 was cut at the limit.
const (
	microEpochs = 14
	microStep   = 5 * time.Minute
)

// microSub is an instance's $SRV subscription in a collection.
type microSub struct {
	epoch           int64
	server, account string
	subject         string
	cid             int64
}

// microStore writes the fixture. In APP: orders with O1 (nats-0 cid 5) and
// O2 (nats-1 cid 7) throughout, and O3 (nats-1 cid 8) until 10, gone with
// its connection; rolled with R1 until 10 and R2 from 13, on one
// connection; pricing with P1 on nats-0 throughout and P9 on nats-2 until
// 10, whose absence at 13 says nothing as nats-2's list was cut; old, seen
// at 1 only, more than an hour before the page's epoch; U1 of orders-bis on
// a connection CONNZ does not list. In OTHER: billing with B1. Subjects
// that are not an instance's are left out.
func microStore(t *testing.T, last time.Time) *store.Store {
	t.Helper()
	st := emptyStore(t)
	r := rows{t, st}
	at := func(e int64) time.Time { return last.Add(-time.Duration(microEpochs-e) * microStep) }
	ids := []string{"S0", "S1", "S2"}
	for i, id := range ids {
		r.add("servers", map[string]any{"server_id": id, "server_name": fmt.Sprintf("nats-%d", i), "first_epoch": 1,
			"last_epoch": microEpochs, "first_seen_at": at(1), "last_seen_at": last})
	}
	r.add("accounts", map[string]any{"account": "APP", "account_name": "shop", "is_system": false, "first_epoch": 1, "last_epoch": microEpochs})
	r.add("accounts", map[string]any{"account": "SYS", "is_system": true, "first_epoch": 1, "last_epoch": microEpochs})
	collections := []int64{1, 6, 10, 13}
	for e := int64(1); e <= microEpochs; e++ {
		if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, 5, 3, [], [], [], 0, false, [], NULL)`, e, at(e),
			at(e).Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			r.add("epoch_endpoints", map[string]any{"epoch": e, "server_id": id, "endpoint": "CONNZ", "state": "ok"})
			if slices.Contains(collections, e) {
				r.add("epoch_endpoints", map[string]any{"epoch": e, "server_id": id, "endpoint": "SUBSZ", "state": "ok"})
				total := int64(50)
				if id == "S2" && e == 13 {
					total = 20000
				}
				r.add("subscription_lists", map[string]any{"epoch": e, "server_id": id, "total": total, "listed": min(total, 10000)})
			}
		}
		for _, c := range []struct {
			server string
			cid    int64
			name   string
		}{{"S0", 5, "order-service"}, {"S1", 7, "order-service"}, {"S0", 9, "rolling"}, {"S0", 11, "pricing-api"}, {"S2", 3, "pricing-api"},
			{"S1", 4, "billing"}} {
			r.add("connections", map[string]any{"epoch": e, "server_id": c.server, "cid": c.cid, "name": c.name, "account": "APP"})
		}
		if e <= 10 {
			r.add("connections", map[string]any{"epoch": e, "server_id": "S1", "cid": 8, "name": "order-service-old", "account": "APP"})
		}
	}
	var subs []microSub
	inst := func(epochs []int64, server, account, service, id string, cid int64) {
		for _, e := range epochs {
			for _, verb := range []string{"PING", "INFO", "STATS"} {
				subs = append(subs, microSub{e, server, account, "$SRV." + verb + "." + service + "." + id, cid},
					microSub{e, server, account, "$SRV." + verb + "." + service, cid}, microSub{e, server, account, "$SRV." + verb, cid})
			}
		}
	}
	inst(collections, "S0", "APP", "orders", "O1", 5)
	inst(collections, "S1", "APP", "orders", "O2", 7)
	inst([]int64{1, 6, 10}, "S1", "APP", "orders", "O3", 8)
	inst([]int64{6, 10}, "S0", "APP", "rolled", "R1", 9)
	inst([]int64{13}, "S0", "APP", "rolled", "R2", 9)
	inst(collections, "S0", "APP", "pricing", "P1", 11)
	inst([]int64{6, 10}, "S2", "APP", "pricing", "P9", 3)
	inst([]int64{1}, "S0", "APP", "old", "X1", 5)
	inst([]int64{13}, "S1", "APP", "orders-bis", "U1", 99)
	inst([]int64{10, 13}, "S1", "OTHER", "billing", "B1", 4)
	for _, e := range collections {
		subs = append(subs, microSub{e, "S0", "APP", "$SRV.PING.orders.*", 5}, microSub{e, "S0", "APP", "$SRV.PING.orders.O1.extra", 5},
			microSub{e, "S0", "APP", "orders.create", 5})
	}
	for _, s := range subs {
		r.add("subscriptions", map[string]any{"epoch": s.epoch, "server_id": s.server, "account": s.account, "subject": s.subject, "cid": s.cid,
			"subs": 1, "msgs": 0})
	}
	return st
}

func microProvider(t *testing.T, last time.Time) *storeprov.Provider {
	t.Helper()
	clock := last.Add(30 * time.Second)
	return &storeprov.Provider{St: microStore(t, last), Interval: microStep, Live: true, Clock: func() time.Time { return clock },
		Retention: 14 * day}
}

func microNamed(res *v1.GetMicroServicesResponse, account, name string) *v1.MicroService {
	for _, s := range res.GetServices() {
		if s.GetAccount() == account && s.GetName() == name {
			return s
		}
	}
	return nil
}

// Without $SRV replies the store reads the services from the newest
// subscription collection: instances by ID with their connections, gone
// instances missing by the count rule, those on a cut list left out.
func TestMicroServicesSeenFromStore(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	at := func(e int64) time.Time { return last.Add(-time.Duration(microEpochs-e) * microStep) }
	_, c := apitest.Serve(t, microProvider(t, last))
	res, err := c.GetMicroServices(ctx, connect.NewRequest(&v1.GetMicroServicesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	m := res.Msg
	if m.GetEpoch() != microEpochs || !m.GetSubscriptionsAt().AsTime().Equal(at(13)) {
		t.Fatalf("epoch %d, subscriptions at %v", m.GetEpoch(), m.GetSubscriptionsAt().AsTime())
	}
	var names []string
	for _, s := range m.GetServices() {
		names = append(names, s.GetAccount()+"/"+s.GetName())
		if s.GetAsked() || len(s.GetEndpoints()) != 0 || s.RequestsPerSecond != nil {
			t.Errorf("%s: %v", s.GetName(), s)
		}
	}
	if want := []string{"APP/orders", "OTHER/billing", "APP/orders-bis", "APP/pricing", "APP/rolled"}; !slices.Equal(names, want) {
		t.Errorf("services %v, want %v", names, want)
	}
	o := microNamed(m, "APP", "orders")
	if o.GetNote() != v1.MicroNote_MICRO_NOTE_INSTANCE_MISSING || o.GetInstances() != 2 || o.GetAccountName() != "shop" ||
		!slices.Equal(o.GetConnectionNames(), []string{"order-service"}) || len(o.GetMissing()) != 1 {
		t.Fatalf("orders = %v", o)
	}
	if g := o.GetMissing()[0]; g.GetId() != "O3" || !g.GetLastSeen().AsTime().Equal(at(10)) || !g.GetSince().AsTime().Equal(at(13)) ||
		g.GetConnectionName() != "order-service-old" || g.GetStillSubscribed() {
		t.Errorf("missing = %v", g)
	}
	if in := o.GetInstanceList()[1]; in.GetId() != "O2" || in.GetServer() != "nats-1" || in.GetConnectionName() != "order-service" {
		t.Errorf("O2 = %v", in)
	}
	if s := microNamed(m, "APP", "rolled"); s.GetNote() != v1.MicroNote_MICRO_NOTE_NOT_ASKED || len(s.GetMissing()) != 0 || s.GetInstances() != 1 {
		t.Errorf("rolled = %v", s)
	}
	if s := microNamed(m, "APP", "pricing"); len(s.GetMissing()) != 0 || s.GetInstances() != 1 {
		t.Errorf("pricing = %v", s)
	}
	if s := microNamed(m, "APP", "orders-bis"); s.GetInstances() != 1 || s.GetInstanceList()[0].GetConnectionName() != "" ||
		s.GetInstanceList()[0].GetServer() != "" {
		t.Errorf("orders-bis = %v", s)
	}
	var accs []string
	for _, a := range m.GetAccounts() {
		accs = append(accs, fmt.Sprintf("%s:%v:%d", a.GetAccount(), a.GetAsked(), a.GetServices()))
	}
	if !slices.Equal(accs, []string{"APP:false:4", "OTHER:false:1"}) {
		t.Errorf("accounts %v", accs)
	}
	scoped, err := c.GetMicroServices(ctx, connect.NewRequest(&v1.GetMicroServicesRequest{Account: "OTHER"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped.Msg.GetServices()) != 1 || scoped.Msg.GetServices()[0].GetName() != "billing" ||
		!slices.Equal(scoped.Msg.GetServices()[0].GetConnectionNames(), []string{"billing"}) {
		t.Errorf("scoped = %v", scoped.Msg)
	}
}
