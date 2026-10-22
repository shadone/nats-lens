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

// The subjects store holds 13 epochs, 5 minutes apart, ending at last.
// Epoch 1 and epoch 12 collected subscription lists; the page's epoch is
// 13, so it reads 12's.
const (
	subjEpochs     = 13
	subjCollection = 12
	subjStep       = 5 * time.Minute
)

// subjectsStore writes the fixture. Servers nats-0 to nats-3; nats-4 is
// expected and missed epoch 12. In APP: orders (orders.>, leader nats-0,
// stores 2 messages a second; projector pulls orders.created, mailer
// pushes to deliver.mailer), orders-archive mirrors it, load-test has no
// consumer, returns and returns-all overlap, catalog is sourced into
// search, alerts is an interest stream with an ephemeral push consumer,
// KV_flags has a watcher. APP's JetStream API takes 3 requests a second, 1%
// failing, which nats-0 to nats-2 each report as the cluster's total. SYS
// is the system account.
//
// The collection at 12: on nats-0 (CONNZ complete) two order-service
// connections in queue group workers on orders.place, api-gateway's inbox
// and mailer's deliver subject, inventory-service on alerts' deliver
// subject, and the server's own subscriptions (5 in APP, 40 in SYS); on
// nats-1 checkout-service's inbox and KV_flags' watcher, and audit-tap in
// SYS on >; on nats-2 (CONNZ cut) a third order-service in workers and an
// unlisted connection's 3; on nats-3 (no CONNZ) an unlisted 2. nats-2's
// list was cut at the limit.
func subjectsStore(t *testing.T, last time.Time) *store.Store {
	t.Helper()
	st := emptyStore(t)
	r := rows{t, st}
	at := func(e int64) time.Time { return last.Add(-time.Duration(subjEpochs-e) * subjStep) }
	ids := []string{"S0", "S1", "S2", "S3"}
	// CONNZ's total and returned in the collection's epoch, by server;
	// nats-3's CONNZ did not answer then.
	connzOf := map[string][2]int64{"S0": {5, 5}, "S1": {2, 2}, "S2": {3, 1}}
	for i, id := range ids {
		r.add("servers", map[string]any{"server_id": id, "server_name": fmt.Sprintf("nats-%d", i), "first_epoch": 1,
			"last_epoch": subjEpochs, "first_seen_at": at(1), "last_seen_at": last})
	}
	r.add("accounts", map[string]any{"account": "APP", "account_name": "shop", "is_system": false, "first_epoch": 1, "last_epoch": subjEpochs})
	r.add("accounts", map[string]any{"account": "SYS", "is_system": true, "first_epoch": 1, "last_epoch": subjEpochs})
	for e := int64(1); e <= subjEpochs; e++ {
		missing := "[]"
		if e == subjCollection {
			missing = "['nats-4']"
		}
		if _, err := st.DB().Exec(fmt.Sprintf(`INSERT INTO epochs VALUES (?, ?, ?, 0, 5, 4, [], [], %s, 0, false, [], NULL)`, missing),
			e, at(e), at(e).Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			r.add("epoch_endpoints", map[string]any{"epoch": e, "server_id": id, "endpoint": "JSZ", "state": "ok"})
			// Each answered VARZ, as a server that answers JSZ does; CONNZ
			// as the collection below says.
			cols := map[string]any{"epoch": e, "server_id": id, "meta_leader": "nats-0"}
			if n, ok := connzOf[id]; ok && e == subjCollection {
				cols["connz_total"], cols["connz_returned"] = n[0], n[1]
			}
			r.add("server_stats", cols)
		}
		// orders' leader stores 600 messages every 5 minutes.
		r.add("stream_stats", map[string]any{"epoch": e, "server_id": "S0", "account": "APP", "stream": "orders", "is_leader": true,
			"leader": "nats-0", "messages": 1000 + 600*e, "bytes": 1 << 20, "last_seq": 1000 + 600*e,
			"created": time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)})
		// Every server reports the account's JetStream API total across
		// the cluster: nats-0 and nats-1 the same; nats-2 lower from 12 on,
		// as another server's restart took its share away.
		for _, id := range []string{"S0", "S1", "S2"} {
			total, errs := 900*e, 9*e
			if id == "S2" && e >= 12 {
				total, errs = total-5000, errs-50
			}
			r.add("account_stats", map[string]any{"epoch": e, "server_id": id, "account": "APP", "js_streams": 9,
				"js_api_total": total, "js_api_errors": errs})
		}
	}
	stream := func(name, retention, mirror string, subjects, sources []string, msgs int64) {
		r.add("streams", map[string]any{"account": "APP", "stream": name, "first_epoch": 1, "last_epoch": subjEpochs})
		r.add("stream_config", map[string]any{"server_id": "S0", "account": "APP", "stream": name, "first_epoch": 1, "last_epoch": subjEpochs,
			"config_hash": "h", "replicas": 1, "retention": retention, "mirror": mirror, "subjects": subjects, "sources": sources})
		if name != "orders" {
			r.add("stream_stats", map[string]any{"epoch": int64(subjEpochs), "server_id": "S0", "account": "APP", "stream": name,
				"is_leader": true, "leader": "nats-0", "messages": msgs, "bytes": 100 * msgs})
		}
	}
	stream("orders", "limits", "", []string{"orders.>"}, nil, 0)
	stream("orders-archive", "limits", "orders", nil, nil, 8800)
	stream("load-test", "limits", "", []string{"loadtest.>"}, nil, 50)
	stream("returns", "limits", "", []string{"returns.*.eu"}, nil, 1)
	stream("returns-all", "limits", "", []string{"returns.created.*"}, nil, 1)
	stream("catalog", "limits", "", []string{"catalog.>"}, nil, 3)
	stream("search", "limits", "", nil, []string{"catalog"}, 3)
	stream("alerts", "interest", "", []string{"alerts.>"}, nil, 2)
	stream("KV_flags", "limits", "", []string{"$KV.flags.>"}, nil, 4)
	consumer := func(stream, name string, durable bool, filters []string, deliver string) {
		cols := map[string]any{"server_id": "S0", "account": "APP", "stream": stream, "consumer": name, "first_epoch": 1,
			"last_epoch": subjEpochs, "config_hash": "c", "filter_subjects": filters, "deliver_subject": deliver}
		if durable {
			cols["durable_name"] = name
			r.add("consumers", map[string]any{"account": "APP", "stream": stream, "consumer": name, "first_epoch": 1, "last_epoch": subjEpochs})
		}
		r.add("consumer_config", cols)
		r.add("consumer_stats", map[string]any{"epoch": int64(subjEpochs), "server_id": "S0", "account": "APP", "stream": stream,
			"consumer": name, "is_leader": true, "leader": "nats-0"})
	}
	// The global account has an orders stream of its own.
	r.add("stream_config", map[string]any{"server_id": "S1", "account": "$G", "stream": "orders", "first_epoch": 1, "last_epoch": subjEpochs,
		"config_hash": "g", "replicas": 1, "retention": "limits", "subjects": []string{"orders.>"}})
	r.add("stream_stats", map[string]any{"epoch": int64(subjEpochs), "server_id": "S1", "account": "$G", "stream": "orders",
		"is_leader": true, "leader": "nats-1", "messages": 7, "bytes": 700})
	r.add("consumer_config", map[string]any{"server_id": "S1", "account": "$G", "stream": "orders", "consumer": "g", "first_epoch": 1,
		"last_epoch": subjEpochs, "config_hash": "c", "durable_name": "g"})
	r.add("consumer_stats", map[string]any{"epoch": int64(subjEpochs), "server_id": "S1", "account": "$G", "stream": "orders",
		"consumer": "g", "is_leader": true, "leader": "nats-1"})
	consumer("orders", "projector", true, []string{"orders.created"}, "")
	consumer("orders", "mailer", true, nil, "deliver.mailer")
	consumer("returns", "refunds", true, nil, "")
	consumer("returns-all", "refunds", true, nil, "")
	consumer("search", "indexer", true, nil, "")
	consumer("alerts", "oc_1", false, nil, "_INBOX.alerts1")
	consumer("KV_flags", "oc_w1", false, nil, "_INBOX.w1")

	c := int64(subjCollection)
	for _, e := range []int64{1, c} {
		for _, id := range ids {
			r.add("epoch_endpoints", map[string]any{"epoch": e, "server_id": id, "endpoint": "SUBSZ", "state": "ok"})
		}
	}
	for _, x := range []struct {
		id            string
		total, listed int64
		connz         bool
	}{{"S0", 12, 12, true}, {"S1", 3, 3, true}, {"S2", 30000, 10000, true}, {"S3", 1, 1, false}} {
		r.add("subscription_lists", map[string]any{"epoch": c, "server_id": x.id, "total": x.total, "listed": x.listed})
		if x.connz {
			r.add("epoch_endpoints", map[string]any{"epoch": c, "server_id": x.id, "endpoint": "CONNZ", "state": "ok"})
		} else {
			r.add("epoch_endpoints", map[string]any{"epoch": c, "server_id": x.id, "endpoint": "CONNZ", "state": "missing"})
		}
	}
	conn := func(id string, cid int64, name, account string) {
		r.add("connections", map[string]any{"epoch": c, "server_id": id, "cid": cid, "name": name, "account": account})
	}
	conn("S0", 1, "order-service", "APP")
	conn("S0", 2, "order-service", "APP")
	conn("S0", 4, "api-gateway", "APP")
	conn("S0", 5, "inventory-service", "APP")
	conn("S0", 8, "mailer-service", "APP")
	conn("S1", 6, "checkout-service", "APP")
	// The global account: CONNZ leaves it empty, SUBSZ names it.
	conn("S1", 3, "audit-tap", "")
	conn("S2", 7, "order-service", "APP")
	sub := func(e int64, id, account, subject, queue string, cid, subs int64) {
		cols := map[string]any{"epoch": e, "server_id": id, "account": account, "subject": subject, "cid": cid, "subs": subs, "msgs": 0}
		if queue != "" {
			cols["queue"] = queue
		}
		r.add("subscriptions", cols)
	}
	sub(c, "S0", "APP", "orders.place", "workers", 1, 1)
	sub(c, "S0", "APP", "orders.place", "workers", 2, 1)
	sub(c, "S0", "APP", "_INBOX.aaa.*", "", 4, 1)
	sub(c, "S0", "APP", "_INBOX.alerts1", "", 5, 1)
	sub(c, "S0", "APP", "deliver.mailer", "", 8, 1)
	sub(c, "S0", "APP", "$JS.API.>", "", 99, 5)
	sub(c, "S0", "SYS", "$SYS.REQ.SERVER.PING", "", 98, 40)
	sub(c, "S1", "APP", "_INBOX.bbb.*", "", 6, 1)
	sub(c, "S1", "APP", "_INBOX.w1", "", 6, 1)
	sub(c, "S1", "SYS", ">", "", 3, 1)
	sub(c, "S2", "APP", "orders.place", "workers", 7, 1)
	sub(c, "S2", "APP", "events.>", "", 50, 3)
	sub(c, "S3", "APP", "x", "", 1, 2)
	// The older collection: never read while 12 is in reach.
	sub(1, "S0", "APP", "old.subject", "", 1, 1)
	return st
}

func subjectsProvider(t *testing.T, last time.Time) *storeprov.Provider {
	t.Helper()
	clock := last.Add(30 * time.Second)
	return &storeprov.Provider{St: subjectsStore(t, last), Interval: subjStep, Live: true, Clock: func() time.Time { return clock },
		Retention: 14 * day}
}

func subjRow(res *v1.GetSubjectsResponse, account, key string) *v1.SubjectRow {
	for _, r := range res.GetRows() {
		if r.GetAccount() == account && (r.GetStream() == key || (r.GetStream() == "" && r.GetPatterns()[0] == key)) {
			return r
		}
	}
	return nil
}

// The store reads every part of the page from the epoch, the range and
// the newest subscription collection.
func TestSubjectsFromStore(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	_, c := apitest.Serve(t, subjectsProvider(t, last))
	res := must(c.GetSubjects(ctx, connect.NewRequest(&v1.GetSubjectsRequest{})))
	if res.GetEpoch() != subjEpochs || !res.GetSubscriptionsAt().AsTime().Equal(last.Add(-subjStep)) {
		t.Fatalf("epoch %d, subscriptions at %v", res.GetEpoch(), res.GetSubscriptionsAt().AsTime())
	}
	orders := subjRow(res, "APP", "orders")
	if orders == nil || orders.GetStoredPerSecond() != 2 || orders.GetDurableConsumers() != 2 || orders.GetAccountName() != "shop" ||
		!slices.Equal(orders.GetClientNames(), []string{"mailer-service"}) || !slices.Equal(orders.GetMirroredBy(), []string{"orders-archive"}) {
		t.Errorf("orders = %v", orders)
	}
	if a := subjRow(res, "APP", "orders-archive"); a == nil || a.GetMirrorOf() != "orders" || len(a.GetPatterns()) != 0 || a.StoredPerSecond != nil {
		t.Errorf("archive = %v", a)
	}
	if l := subjRow(res, "APP", "load-test"); l == nil || !l.GetNoConsumer() {
		t.Errorf("load-test = %v", l)
	}
	if r := subjRow(res, "APP", "returns"); r == nil || !slices.Equal(r.GetOverlaps(), []string{"returns-all"}) {
		t.Errorf("returns = %v", r)
	}
	if r := subjRow(res, "APP", "catalog"); r == nil || r.GetNoConsumer() || !slices.Equal(r.GetSourcedInto(), []string{"search"}) {
		t.Errorf("catalog = %v", r)
	}
	if r := subjRow(res, "APP", "alerts"); r == nil || r.GetEphemeralConsumers() != 1 || !slices.Equal(r.GetClientNames(), []string{"inventory-service"}) {
		t.Errorf("alerts = %v", r)
	}
	if r := subjRow(res, "APP", "KV_flags"); r == nil || r.GetKind() != v1.SubjectRowKind_SUBJECT_ROW_KIND_KEY_VALUE ||
		!slices.Equal(r.GetClientNames(), []string{"checkout-service"}) {
		t.Errorf("KV_flags = %v", r)
	}
	if r := subjRow(res, "APP", "orders.place"); r == nil || r.GetConnections() != 3 || r.GetQueue() != "workers" ||
		!slices.Equal(r.GetClientNames(), []string{"order-service"}) {
		t.Errorf("queue row = %v", r)
	}
	if r := subjRow(res, "APP", api.InboxPattern); r == nil || r.GetConnections() != 2 ||
		!slices.Equal(r.GetClientNames(), []string{"api-gateway", "checkout-service"}) {
		t.Errorf("inbox = %v", r)
	}
	// Deliver subjects are no rows of their own.
	for _, k := range []string{"deliver.mailer", "_INBOX.alerts1", "_INBOX.w1", "$JS.API.>"} {
		if r := subjRow(res, "APP", k); r != nil && r.GetKind() != v1.SubjectRowKind_SUBJECT_ROW_KIND_JETSTREAM_API {
			t.Errorf("row %v", r)
		}
	}
	if r := subjRow(res, "SYS", ">"); r == nil || !r.GetWide() || !r.GetSystemAccount() || !slices.Equal(r.GetClientNames(), []string{"audit-tap"}) {
		t.Errorf("> = %v", r)
	}
	js := subjRow(res, "APP", api.JetStreamAPIPattern)
	if js == nil || js.GetApiPerSecond() != 3 || js.GetApiErrorShare() != 0.01 {
		t.Errorf("JetStream API = %v", js)
	}
	if res.GetServerSubscriptions() != 45 || res.GetUnlistedConnectionSubscriptions() != 5 {
		t.Errorf("server %d, unlisted %d", res.GetServerSubscriptions(), res.GetUnlistedConnectionSubscriptions())
	}
	var servers []string
	for _, s := range res.GetSubszServers() {
		servers = append(servers, fmt.Sprintf("%s %v %d/%d", s.GetServerName(), s.GetAnswered(), s.GetListed(), s.GetTotal()))
	}
	if want := []string{"nats-0 true 12/12", "nats-1 true 3/3", "nats-2 true 10000/30000", "nats-3 true 1/1", "nats-4 false 0/0"}; !slices.Equal(servers, want) {
		t.Errorf("servers %v, want %v", servers, want)
	}
	if res.GetAllCount() != 15 || res.GetNoConsumerCount() != 1 || res.GetOverlappingCount() != 2 || res.GetWideCount() != 1 || res.GetStoredCount() != 10 {
		t.Errorf("counts all %d none %d overlap %d wide %d stored %d", res.GetAllCount(), res.GetNoConsumerCount(), res.GetOverlappingCount(),
			res.GetWideCount(), res.GetStoredCount())
	}
	// The scope.
	sys := must(c.GetSubjects(ctx, connect.NewRequest(&v1.GetSubjectsRequest{Account: "SYS"})))
	if len(sys.GetRows()) != 1 || sys.GetServerSubscriptions() != 40 || sys.GetUnlistedConnectionSubscriptions() != 0 {
		t.Errorf("SYS = %v", sys)
	}
}

// A collection older than 24 hours before the epoch is not read; a range
// whose newest epoch is the collection reads it; the tester answers from
// the same collection.
func TestSubjectsCollectionReach(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	p := subjectsProvider(t, last)
	if _, err := p.St.DB().Exec(`DELETE FROM epoch_endpoints WHERE endpoint = 'SUBSZ' AND epoch = ?`, subjCollection); err != nil {
		t.Fatal(err)
	}
	_, c := apitest.Serve(t, p)
	// Epoch 1 lies an hour before: in reach. It has no CONNZ, so its one
	// subscription is unlisted.
	res := must(c.GetSubjects(ctx, connect.NewRequest(&v1.GetSubjectsRequest{})))
	if res.SubscriptionsAt == nil || !res.GetSubscriptionsAt().AsTime().Equal(last.Add(-12*subjStep)) || res.GetUnlistedConnectionSubscriptions() != 1 {
		t.Fatalf("subscriptions at %v", res.GetSubscriptionsAt())
	}
	// Move epoch 1 back two days: out of reach.
	if _, err := p.St.DB().Exec(`UPDATE epochs SET started_at = started_at - INTERVAL 2 DAY WHERE epoch = 1`); err != nil {
		t.Fatal(err)
	}
	res = must(c.GetSubjects(ctx, connect.NewRequest(&v1.GetSubjectsRequest{})))
	if res.SubscriptionsAt != nil || res.GetServerSubscriptions() != 0 || subjRow(res, "APP", "orders").GetDurableConsumers() != 2 {
		t.Errorf("out of reach: %v", res)
	}
}

// The tester reads the store: a stream, its consumers by filter and the
// queue group once.
func TestMatchSubjectFromStore(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	_, c := apitest.Serve(t, subjectsProvider(t, last))
	match := func(account, subject string) *v1.MatchSubjectResponse {
		return must(c.MatchSubject(ctx, connect.NewRequest(&v1.MatchSubjectRequest{Account: account, Subject: subject})))
	}
	res := match("APP", "orders.place")
	if len(api.MatchedStreams(res)) != 1 || !slices.Equal(api.MatchedStreams(res)[0].GetConsumers(), []string{"mailer"}) ||
		!slices.Equal(api.MatchedStreams(res)[0].GetMirrors(), []string{"orders-archive"}) || len(api.MatchedSubscribers(res)) != 1 ||
		api.MatchedSubscribers(res)[0].GetConnections() != 3 || api.MatchedSubscribers(res)[0].GetQueue() != "workers" {
		t.Errorf("orders.place = %v", res)
	}
	if res = match("APP", "orders.created"); !slices.Equal(api.MatchedStreams(res)[0].GetConsumers(), []string{"mailer", "projector"}) {
		t.Errorf("orders.created = %v", api.MatchedStreams(res))
	}
	// audit-tap's > is in SYS; the unlisted connection's events.> is left out.
	if res = match("APP", "events.x"); len(api.MatchedStreams(res)) != 0 || len(api.MatchedSubscribers(res)) != 0 {
		t.Errorf("events.x = %v", res)
	}
	// Without a scope SYS answers only a $SYS. subject.
	if res = match("", "events.x"); len(res.GetAccounts()) != 0 {
		t.Errorf("every account = %v", res.GetAccounts())
	}
	if res = match("", "$SYS.SERVER.x"); len(res.GetAccounts()) != 1 || !res.GetAccounts()[0].GetSystemAccount() ||
		api.MatchedSubscribers(res)[0].GetSubject() != ">" {
		t.Errorf("$SYS subject = %v", res.GetAccounts())
	}
	// Two accounts store orders: one group each, the global account's
	// stream in its own.
	res = match("", "orders.shipped")
	var groups []string
	for _, a := range res.GetAccounts() {
		groups = append(groups, fmt.Sprintf("%s %d", a.GetAccount(), len(a.GetMatches())))
	}
	if !slices.Equal(groups, []string{"$G 1", "APP 1"}) {
		t.Errorf("groups = %v", groups)
	}
	if res = match("APP", "$JS.API.INFO"); !res.GetAccounts()[0].GetJetstreamApi() {
		t.Errorf("$JS.API.INFO = %v", res)
	}
	if res = match("APP", "_INBOX.aaa.r1"); len(api.MatchedSubscribers(res)) != 1 || api.MatchedSubscribers(res)[0].GetClientNames()[0] != "api-gateway" {
		t.Errorf("inbox = %v", api.MatchedSubscribers(res))
	}
}

// The store's SQL groups a collection as api.GroupSubscriptions does.
func TestSubjectsGroupingAgreesWithRules(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	p := subjectsProvider(t, last)
	db := p.St.DB()
	var subs []api.SubscriptionFact
	q, err := db.Query(`SELECT server_id, account, subject, coalesce(queue, ''), cid, subs FROM subscriptions WHERE epoch = ?`, subjCollection)
	if err != nil {
		t.Fatal(err)
	}
	for q.Next() {
		var s api.SubscriptionFact
		if err := q.Scan(&s.Server, &s.Account, &s.Subject, &s.Queue, &s.CID, &s.Subs); err != nil {
			t.Fatal(err)
		}
		subs = append(subs, s)
	}
	_ = q.Close()
	conns := map[string]api.ServerConnections{
		"S0": {Complete: true, Names: map[uint64]string{1: "order-service", 2: "order-service", 4: "api-gateway", 5: "inventory-service", 8: "mailer-service"}},
		"S1": {Complete: true, Names: map[uint64]string{6: "checkout-service", 3: "audit-tap"}},
		"S2": {Complete: false, Names: map[uint64]string{7: "order-service"}},
	}
	deliver := map[[2]string]bool{{"APP", "deliver.mailer"}: true, {"APP", "_INBOX.alerts1"}: true, {"APP", "_INBOX.w1"}: true}
	want := api.GroupSubscriptions(subs, conns, deliver)
	_, c := apitest.Serve(t, p)
	res := must(c.GetSubjects(ctx, connect.NewRequest(&v1.GetSubjectsRequest{Range: &v1.TimeRange{Start: timestamppb.New(last.Add(-time.Hour)),
		End: timestamppb.New(last.Add(time.Second))}})))
	got := map[string]string{}
	for _, r := range res.GetRows() {
		if r.GetConnections() > 0 {
			got[r.GetAccount()+" "+r.GetPatterns()[0]] = fmt.Sprintf("%d %s %v", r.GetConnections(), r.GetQueue(), r.GetClientNames())
		}
	}
	for _, g := range want.Groups {
		if w := fmt.Sprintf("%d %s %v", g.Connections, g.Queue, g.Names); got[g.Account+" "+g.Subject] != w {
			t.Errorf("%s %s: store %q, rules %q", g.Account, g.Subject, got[g.Account+" "+g.Subject], w)
		}
	}
	if len(got) != len(want.Groups) {
		t.Errorf("store groups %v, rules %v", got, want.Groups)
	}
	var server, unlisted int64
	for _, n := range want.ServerSubscriptions {
		server += n
	}
	for _, n := range want.Unlisted {
		unlisted += n
	}
	if res.GetServerSubscriptions() != server || res.GetUnlistedConnectionSubscriptions() != unlisted {
		t.Errorf("store %d/%d, rules %d/%d", res.GetServerSubscriptions(), res.GetUnlistedConnectionSubscriptions(), server, unlisted)
	}
}

// 100,000 subscriptions on 4 servers group in SQL in well under the call's
// time.
func TestSubjectsManySubscriptions(t *testing.T) {
	if testing.Short() {
		t.Skip("scale test")
	}
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	p := subjectsProvider(t, last)
	db := p.St.DB()
	if _, err := db.Exec(`INSERT INTO connections (epoch, server_id, cid, name, account)
SELECT ?, 'S' || (i % 4), 1000 + i, 'client-' || (i % 50), 'APP' FROM range(20000) t(i)`, subjCollection); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscriptions (epoch, server_id, account, subject, queue, cid, subs, msgs)
SELECT ?, 'S' || (i % 4), 'APP', CASE WHEN i % 3 = 0 THEN '_INBOX.' || i || '.*' ELSE 'svc.' || (i % 400) END,
  CASE WHEN i % 3 = 1 THEN 'q' || (i % 400) END, 1000 + (i % 20000), 1, 0 FROM range(100000) t(i)`, subjCollection); err != nil {
		t.Fatal(err)
	}
	_, c := apitest.Serve(t, p)
	start := time.Now()
	res := must(c.GetSubjects(ctx, connect.NewRequest(&v1.GetSubjectsRequest{})))
	took := time.Since(start)
	t.Logf("100,000 subscriptions: %d rows in %s", res.GetAllCount(), took)
	if took > 10*time.Second || subjRow(res, "APP", api.InboxPattern).GetConnections() < 1000 {
		t.Errorf("took %s, inbox %v", took, subjRow(res, "APP", api.InboxPattern))
	}
}

// A server whose counter fell keeps the rate of its newest run: with
// nats-2 alone, APP's API still reads 3 requests a second.
func TestSubjectsAPIRateAfterCounterFell(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	p := subjectsProvider(t, last)
	if _, err := p.St.DB().Exec(`DELETE FROM account_stats WHERE server_id <> 'S2'`); err != nil {
		t.Fatal(err)
	}
	_, c := apitest.Serve(t, p)
	res := must(c.GetSubjects(ctx, connect.NewRequest(&v1.GetSubjectsRequest{})))
	if js := subjRow(res, "APP", api.JetStreamAPIPattern); js == nil || js.GetApiPerSecond() != 3 || js.GetApiErrorShare() != 0.01 {
		t.Errorf("JetStream API = %v", js)
	}
}

// An epoch whose SUBSZ asks all went unanswered does not hide the
// collection before it.
func TestSubjectsSkipsAnUnansweredCollection(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	p := subjectsProvider(t, last)
	for _, id := range []string{"S0", "S1", "S2", "S3"} {
		if _, err := p.St.DB().Exec(`INSERT INTO epoch_endpoints (epoch, server_id, endpoint, state) VALUES (?, ?, 'SUBSZ', 'missing')`,
			subjEpochs, id); err != nil {
			t.Fatal(err)
		}
	}
	_, c := apitest.Serve(t, p)
	res := must(c.GetSubjects(ctx, connect.NewRequest(&v1.GetSubjectsRequest{})))
	if !res.GetSubscriptionsAt().AsTime().Equal(last.Add(-subjStep)) || subjRow(res, "APP", "orders.place") == nil {
		t.Errorf("subscriptions at %v, want the collection at 12", res.GetSubscriptionsAt().AsTime())
	}
}
