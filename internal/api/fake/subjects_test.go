package fake_test

import (
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
)

func subjectsOf(t *testing.T, c natslensv1connect.LensServiceClient, system string) *v1.GetSubjectsResponse {
	t.Helper()
	return must(c.GetSubjects(ctx, connect.NewRequest(&v1.GetSubjectsRequest{System: system})))
}

// subjectRow finds a row by its stream, else its account and first
// pattern.
func subjectRow(res *v1.GetSubjectsResponse, account, key string) *v1.SubjectRow {
	for _, r := range res.GetRows() {
		if r.GetAccount() == account && (r.GetStream() == key || (r.GetStream() == "" && len(r.GetPatterns()) > 0 && r.GetPatterns()[0] == key)) {
			return r
		}
	}
	return nil
}

// example-global tells the page's story: a stream without a consumer from
// a load test, an interest stream whose ephemeral push consumer names its
// connection, an empty work queue, a KV bucket's named watchers, a mirror,
// three order-service connections in a queue group, the folded inboxes,
// nats-lens's and a monitoring tool's wide subscriptions in the system
// account, and one server's list cut at the limit.
func TestGlobalSubjectsStory(t *testing.T) {
	res := subjectsOf(t, demo(t), fake.GlobalSystem)
	if res.SubscriptionsAt == nil || res.GetSubscriptionsAt().AsTime().After(res.GetEpochAt().AsTime()) {
		t.Fatalf("subscriptions at %v, epoch at %v", res.GetSubscriptionsAt(), res.GetEpochAt())
	}
	if r := res.GetRows()[0]; !r.GetNoConsumer() {
		t.Errorf("first row %v, want a stream without a consumer", r)
	}
	app := fake.Account
	if r := subjectRow(res, app, "checkout-load-test"); r == nil || !r.GetNoConsumer() || r.GetRetention() != "limits" ||
		!slices.Equal(r.GetPatterns(), []string{"loadtest.checkout.>"}) {
		t.Errorf("load test = %v", r)
	}
	if r := subjectRow(res, app, "stock-alerts"); r == nil || r.GetRetention() != "interest" || r.GetEphemeralConsumers() != 1 ||
		!slices.Equal(r.GetClientNames(), []string{"inventory-service"}) {
		t.Errorf("interest stream = %v", r)
	}
	if r := subjectRow(res, app, "gift-card-jobs"); r == nil || r.GetRetention() != "workqueue" || r.GetMessages() != 0 || r.Messages == nil ||
		r.GetDurableConsumers() != 1 {
		t.Errorf("work queue = %v", r)
	}
	if r := subjectRow(res, app, "KV_feature-flags"); r == nil || r.GetKind() != v1.SubjectRowKind_SUBJECT_ROW_KIND_KEY_VALUE ||
		r.GetEphemeralConsumers() != 22 || len(r.GetClientNames()) != 3 || r.GetMoreNames() != 1 {
		t.Errorf("KV bucket = %v", r)
	}
	if r := subjectRow(res, app, "order-archive"); r == nil || r.GetMirrorOf() != "order-events" || len(r.GetPatterns()) != 0 {
		t.Errorf("mirror = %v", r)
	}
	if r := subjectRow(res, app, "order-events"); r == nil || !slices.Equal(r.GetMirroredBy(), []string{"order-archive"}) {
		t.Errorf("mirrored = %v", r)
	}
	if r := subjectRow(res, app, "orders.place"); r == nil || r.GetConnections() != 3 || r.GetQueue() != "order-service" ||
		!slices.Equal(r.GetClientNames(), []string{"order-service"}) {
		t.Errorf("queue group = %v", r)
	}
	if r := subjectRow(res, app, api.InboxPattern); r == nil || r.GetKind() != v1.SubjectRowKind_SUBJECT_ROW_KIND_INBOX || r.GetConnections() < 2 {
		t.Errorf("inboxes = %v", r)
	}
	if r := subjectRow(res, "SYS", ">"); r == nil || !r.GetWide() || !r.GetSystemAccount() || !slices.Equal(r.GetClientNames(), []string{"ops-monitor"}) {
		t.Errorf("> = %v", r)
	}
	if r := subjectRow(res, "SYS", "$SYS.ACCOUNT.>"); r == nil || !slices.Equal(r.GetClientNames(), []string{"natslens-" + fake.GlobalSystem}) {
		t.Errorf("nats-lens = %v", r)
	}
	if last := res.GetRows()[len(res.GetRows())-1]; last.GetAccount() != "SYS" {
		t.Errorf("last row %v, want the system account's", last)
	}
	if r := subjectRow(res, app, api.JetStreamAPIPattern); r == nil || r.ApiPerSecond == nil || r.ApiErrorShare == nil {
		t.Errorf("JetStream API = %v", r)
	}
	var cut, silent []string
	for _, s := range res.GetSubszServers() {
		if !s.GetAnswered() {
			silent = append(silent, s.GetServerName())
		} else if s.GetListed() < s.GetTotal() {
			cut = append(cut, s.GetServerName())
		}
	}
	if !slices.Equal(cut, []string{"nats-eu-3"}) || !slices.Equal(silent, []string{"nats-us-4"}) {
		t.Errorf("cut %v, silent %v", cut, silent)
	}
	if res.GetServerSubscriptions() == 0 || res.GetUnlistedConnectionSubscriptions() != 0 || res.GetOverlappingCount() != 0 {
		t.Errorf("server %d, unlisted %d, overlapping %d", res.GetServerSubscriptions(), res.GetUnlistedConnectionSubscriptions(), res.GetOverlappingCount())
	}
}

// example-staging collected no subscription list: no subscription rows,
// and its stream rows keep their consumers; its data ended three hours
// ago, so no rate.
func TestStagingSubjectsWithoutSubscriptions(t *testing.T) {
	res := subjectsOf(t, demo(t), fake.StagingSystem)
	if res.GetEpoch() == 0 || res.SubscriptionsAt != nil || len(res.GetSubszServers()) != 0 {
		t.Fatalf("response = %v", res)
	}
	for _, r := range res.GetRows() {
		if r.GetKind() == v1.SubjectRowKind_SUBJECT_ROW_KIND_SUBSCRIPTION || r.GetKind() == v1.SubjectRowKind_SUBJECT_ROW_KIND_INBOX ||
			r.StoredPerSecond != nil || r.ApiPerSecond != nil {
			t.Errorf("row %v", r)
		}
	}
	if r := subjectRow(res, fake.Account, "order-jobs"); r == nil || r.GetDurableConsumers() != 2 {
		t.Errorf("order-jobs = %v", r)
	}
}

// The page agrees with the demo's other pages in every system: its
// stream rows are the streams ListStreams lists with their consumers, every
// name it shows is a connection's on the Connections page, and the system
// account's subscriptions add up to its count on the Accounts page.
func TestDemoSubjectsAgreeWithOtherPages(t *testing.T) {
	c := demo(t)
	for _, sys := range []string{fake.Cluster, fake.GlobalSystem, fake.StagingSystem} {
		res := subjectsOf(t, c, sys)
		streams := map[string]*v1.StreamSummary{}
		for _, s := range must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{System: sys, IncludeKv: true}))).GetStreams() {
			streams[s.GetEntity().GetStream()] = s
		}
		names := map[string]bool{}
		for _, x := range must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{System: sys, IncludeTooling: true,
			PageSize: 1000}))).GetConnections() {
			names[x.GetName()] = true
		}
		var stored int
		var sysClients int64
		for _, r := range res.GetRows() {
			if r.GetStream() != "" {
				stored++
				s := streams[r.GetStream()]
				if s == nil || s.GetConsumers() != r.GetDurableConsumers()+r.GetEphemeralConsumers() || s.GetMessages() != r.GetMessages() {
					t.Errorf("%s %s: row %v, ListStreams %v", sys, r.GetStream(), r, s)
				}
			}
			for _, n := range r.GetClientNames() {
				if !names[n] {
					t.Errorf("%s: %q is on no connection", sys, n)
				}
			}
			if r.GetSystemAccount() {
				sysClients += int64(r.GetConnections())
			}
		}
		if stored != len(streams) {
			t.Errorf("%s: %d stream rows, %d streams", sys, stored, len(streams))
		}
		if sys != fake.GlobalSystem {
			continue
		}
		for _, a := range must(c.GetAccountsAndAuth(ctx, connect.NewRequest(&v1.GetAccountsAndAuthRequest{System: sys}))).GetAccounts() {
			if !a.GetIsSystem() {
				continue
			}
			// Each client connection in SYS holds one subscription per row.
			scoped := must(c.GetSubjects(ctx, connect.NewRequest(&v1.GetSubjectsRequest{System: sys, Account: a.GetAccount()})))
			if got := scoped.GetServerSubscriptions() + sysClients; got != a.GetSubscriptions() {
				t.Errorf("SYS subscriptions %d on the Subjects page, %d on the Accounts page", got, a.GetSubscriptions())
			}
			if int(a.GetConnections()) != 2 {
				t.Errorf("SYS connections = %d", a.GetConnections())
			}
		}
	}
}

// The tester in example-global: a stream with its consumers and mirror, a
// queue group counted once, a stream that sources it, and nothing.
func TestGlobalMatchSubject(t *testing.T) {
	c := demo(t)
	match := func(account, subject string) *v1.MatchSubjectResponse {
		return must(c.MatchSubject(ctx, connect.NewRequest(&v1.MatchSubjectRequest{System: fake.GlobalSystem, Account: account, Subject: subject})))
	}
	res := match(fake.Account, "order.events.created")
	if len(api.MatchedStreams(res)) != 1 || api.MatchedStreams(res)[0].GetStream() != "order-events" || len(api.MatchedStreams(res)[0].GetConsumers()) != 4 ||
		!slices.Equal(api.MatchedStreams(res)[0].GetMirrors(), []string{"order-archive"}) || len(api.MatchedSubscribers(res)) != 0 {
		t.Errorf("order.events.created = %v", res)
	}
	res = match(fake.Account, "orders.place")
	if len(api.MatchedStreams(res)) != 0 || len(api.MatchedSubscribers(res)) != 1 || api.MatchedSubscribers(res)[0].GetQueue() != "order-service" ||
		api.MatchedSubscribers(res)[0].GetConnections() != 3 {
		t.Errorf("orders.place = %v", res)
	}
	if res = match(fake.Account, "catalog.changes.item"); !slices.Equal(api.MatchedStreams(res)[0].GetSourcedBy(), []string{"search-index"}) {
		t.Errorf("catalog.changes.item = %v", res)
	}
	if res = match(fake.Account, "nothing.listens.here"); len(api.MatchedStreams(res)) != 0 || len(api.MatchedSubscribers(res)) != 0 || res.SubscriptionsAt == nil {
		t.Errorf("nothing = %v", res)
	}
	// Without a scope the monitoring tool's > in SYS answers only a $SYS.
	// subject.
	if res = match("", "nothing.listens.here"); len(res.GetAccounts()) != 0 {
		t.Errorf("every account = %v", res)
	}
	if res = match("", "$SYS.ACCOUNT.APP.CONNECT"); len(res.GetAccounts()) != 1 || !res.GetAccounts()[0].GetSystemAccount() ||
		len(api.MatchedSubscribers(res)) != 2 || !strings.HasPrefix(api.MatchedSubscribers(res)[1].GetClientNames()[0], "ops-") {
		t.Errorf("$SYS subject = %v", res)
	}
	if res = match("", "$JS.API.INFO"); len(res.GetAccounts()) != 1 || !res.GetAccounts()[0].GetJetstreamApi() {
		t.Errorf("$JS.API subject = %v", res)
	}
}

// example-global's connection counts agree: the answering servers' counts, the
// Connections page with tooling, and the newest cluster.connections point.
func TestGlobalConnectionCountsAgree(t *testing.T) {
	c := demo(t)
	sys := fake.GlobalSystem
	var sum int64
	servers := must(c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{System: sys})))
	for _, s := range servers.GetServers() {
		// A server that did not answer keeps its last count.
		if !slices.Contains(servers.GetMissing(), s.GetName()) {
			sum += s.GetConnections()
		}
	}
	all := must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{System: sys, IncludeTooling: true})))
	pts := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{System: sys, Metric: "cluster.connections"}))).GetSeries().GetPoints()
	last := pts[len(pts)-1].GetValue()
	if sum != int64(all.GetTotalCount()) || float64(sum) != last {
		t.Errorf("servers %d, connections with tooling %d, cluster.connections %v", sum, all.GetTotalCount(), last)
	}
}

// A mirror stores what its origin stores, in every demo system.
func TestDemoMirrorStoresAsItsOrigin(t *testing.T) {
	c := demo(t)
	for _, sys := range []string{fake.Cluster, fake.GlobalSystem, fake.StagingSystem} {
		res := subjectsOf(t, c, sys)
		for _, r := range res.GetRows() {
			if r.GetMirrorOf() == "" {
				continue
			}
			o := subjectRow(res, r.GetAccount(), r.GetMirrorOf())
			if o == nil || (r.StoredPerSecond == nil) != (o.StoredPerSecond == nil) || r.GetStoredPerSecond() != o.GetStoredPerSecond() {
				t.Errorf("%s: mirror %s stores %v a second, its origin %v", sys, r.GetStream(), r.StoredPerSecond, o)
			}
		}
	}
}
