package api_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

var subjAt = time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)

// Matching and overlap follow the server's rules, wildcards included.
func TestSubjectMatchingAndOverlap(t *testing.T) {
	matches := []struct {
		literal, pattern string
		want             bool
	}{
		{"orders.created", "orders.created", true},
		{"orders.created", "orders.*", true},
		{"orders.created.eu", "orders.*", false},
		{"orders.created.eu", "orders.>", true},
		{"orders", "orders.>", false},
		{"orders.created", ">", true},
		{"orders.created", "*.created", true},
		{"payments.created", "orders.*", false},
	}
	for _, c := range matches {
		if got := api.SubjectMatches(c.literal, c.pattern); got != c.want {
			t.Errorf("SubjectMatches(%q, %q) = %v, want %v", c.literal, c.pattern, got, c.want)
		}
	}
	overlaps := []struct {
		a, b string
		want bool
	}{
		{"orders.>", "orders.created", true},
		{"orders.*", "orders.*.eu", false},
		{"orders.*.eu", "orders.created.*", true},
		{"orders.>", "payments.>", false},
		{">", "anything.at.all", true},
		{"*.created", "orders.*", true},
		{"a.b", "a.b", true},
		{"a.b", "a.c", false},
	}
	for _, c := range overlaps {
		if got := api.SubjectsOverlap(c.a, c.b); got != c.want {
			t.Errorf("SubjectsOverlap(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
		if got := api.SubjectsOverlap(c.b, c.a); got != c.want {
			t.Errorf("SubjectsOverlap(%q, %q) = %v, want %v", c.b, c.a, got, c.want)
		}
	}
}

// The tester takes a literal subject of at most 255 bytes, with no empty
// token and no white space.
func TestValidateLiteralSubject(t *testing.T) {
	for _, ok := range []string{"orders.created", "a", "orders.created.eu-west.1", "$JS.API.INFO", "_INBOX.abc", "a*b.c>d",
		strings.Repeat("a", api.MaxSubjectLength)} {
		if err := api.ValidateLiteralSubject(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "orders.*", "orders.>", ">", "*", "orders..created", ".orders", "orders.", "orders created",
		"orders.\tcreated", "orders\vcreated", "a\x00b", "orders.\x1bcreated", "orders\x7f", strings.Repeat("a", api.MaxSubjectLength+1)} {
		if err := api.ValidateLiteralSubject(bad); !errors.Is(err, api.ErrInvalid) {
			t.Errorf("%q: %v, want invalid", bad, err)
		}
	}
}

// subsFixture is a collection on two servers: s1's CONNZ listed every
// connection, s2's was cut. Client 1 and 2 are order-service in queue
// group workers; 3 is audit-tap on >; 4 and 5 hold inboxes, 5 also the
// deliver subject of a push consumer; 9 on s1 is the server's own; 7 on
// s2 is not listed.
func subsFixture() ([]api.SubscriptionFact, map[string]api.ServerConnections) {
	conns := map[string]api.ServerConnections{
		"s1": {Complete: true, Names: map[uint64]string{1: "order-service", 2: "order-service", 3: "audit-tap", 4: "api-gateway"}},
		"s2": {Complete: false, Names: map[uint64]string{5: "checkout-service", 6: ""}},
	}
	subs := []api.SubscriptionFact{
		{Server: "s1", Account: "APP", Subject: "orders.place", Queue: "workers", CID: 1, Subs: 1},
		{Server: "s1", Account: "APP", Subject: "orders.place", Queue: "workers", CID: 2, Subs: 2},
		{Server: "s1", Account: "APP", Subject: ">", CID: 3, Subs: 1},
		{Server: "s1", Account: "APP", Subject: "_INBOX.aaa.*", CID: 4, Subs: 1},
		{Server: "s2", Account: "APP", Subject: "_INBOX.bbb.*", CID: 5, Subs: 1},
		{Server: "s2", Account: "APP", Subject: "_INBOX.push1", CID: 5, Subs: 1},
		{Server: "s2", Account: "APP", Subject: "orders.place", CID: 6, Subs: 1},
		{Server: "s1", Account: "SYS", Subject: "$SYS.REQ.SERVER.PING", CID: 9, Subs: 4},
		{Server: "s1", Account: "APP", Subject: "$JS.API.>", CID: 9, Subs: 1},
		{Server: "s2", Account: "APP", Subject: "events.>", CID: 7, Subs: 3},
		// A server that sent no CONNZ: every subscription is unlisted.
		{Server: "s3", Account: "APP", Subject: "orders.place", CID: 1, Subs: 2},
	}
	return subs, conns
}

func groupBy(gs []api.SubscriptionGroup) map[string]api.SubscriptionGroup {
	out := map[string]api.SubscriptionGroup{}
	for _, g := range gs {
		out[g.Account+" "+g.Subject+" "+g.Queue] = g
	}
	return out
}

// The server-subscription rule, the deliver subjects, the inbox fold and
// the queue rule.
func TestGroupSubscriptions(t *testing.T) {
	subs, conns := subsFixture()
	g := api.GroupSubscriptions(subs, conns, map[[2]string]bool{{"APP", "_INBOX.push1"}: true})
	if g.ServerSubscriptions["SYS"] != 4 || g.ServerSubscriptions["APP"] != 1 || len(g.ServerSubscriptions) != 2 {
		t.Errorf("server subscriptions = %v, want SYS 4, APP 1", g.ServerSubscriptions)
	}
	if g.Unlisted["APP"] != 5 || len(g.Unlisted) != 1 {
		t.Errorf("unlisted = %v, want APP 5", g.Unlisted)
	}
	got := groupBy(g.Groups)
	want := map[string]api.SubscriptionGroup{
		// Two order-service connections in workers and an unnamed one
		// without a queue: no queue shared.
		"APP orders.place ": {Account: "APP", Subject: "orders.place", Connections: 3, Names: []string{"order-service"}, NameCount: 1},
		"APP > ":            {Account: "APP", Subject: ">", Connections: 1, Names: []string{"audit-tap"}, NameCount: 1},
		"APP _INBOX.> ": {Account: "APP", Subject: api.InboxPattern, Connections: 2, Names: []string{"api-gateway", "checkout-service"},
			NameCount: 2},
	}
	if len(got) != len(want) {
		t.Errorf("groups = %v", g.Groups)
	}
	for k, w := range want {
		// The rates are TestGroupDeliveredRates'.
		g := got[k]
		g.Rates = nil
		if fmt.Sprint(g) != fmt.Sprint(w) {
			t.Errorf("group %q = %+v, want %+v", k, got[k], w)
		}
	}
	if len(g.Deliver) != 1 || g.Deliver[0].Subject != "_INBOX.push1" || !slices.Equal(g.Deliver[0].Names, []string{"checkout-service"}) {
		t.Errorf("deliver = %+v", g.Deliver)
	}
	// Without the unnamed connection, the queue is shared.
	g = api.GroupSubscriptions(slices.DeleteFunc(slices.Clone(subs), func(s api.SubscriptionFact) bool { return s.CID == 6 }), conns, nil)
	if q := groupBy(g.Groups)["APP orders.place workers"]; q.Connections != 2 || q.Queue != "workers" {
		t.Errorf("queue group = %+v, want 2 connections in workers", q)
	}
	// Without a deliver subject the push subscription folds with the
	// inboxes.
	if in := groupBy(g.Groups)["APP _INBOX.> "]; in.Connections != 2 {
		t.Errorf("inbox = %+v", in)
	}
}

// The tester's groups keep each subject and queue apart, without a fold,
// and leave out the servers' own and unlisted connections.
func TestCoreGroups(t *testing.T) {
	subs, conns := subsFixture()
	got := groupBy(api.CoreGroups(subs, conns))
	if q := got["APP orders.place workers"]; q.Connections != 2 || q.Queue != "workers" {
		t.Errorf("workers = %+v", q)
	}
	if q := got["APP orders.place "]; q.Connections != 1 || q.NameCount != 0 {
		t.Errorf("no queue = %+v", q)
	}
	if _, ok := got["APP _INBOX.push1 "]; !ok {
		t.Errorf("deliver subject missing: %v", got)
	}
	for k := range got {
		if strings.HasPrefix(k, "SYS") || strings.Contains(k, "events.>") || strings.Contains(k, "$JS.API") {
			t.Errorf("%q is a server's own or unlisted", k)
		}
	}
}

// subjectsFacts is a system with an account APP and the system account
// SYS: orders (two durables, one push consumer), an archive mirroring
// it, a stream without a consumer, two overlapping streams, a stream
// sourced into another, an empty work queue, an interest stream with an
// ephemeral push consumer, a KV bucket with watchers, and subscriptions.
func subjectsFacts() *api.SubjectsFacts {
	sub := func(acc, subj, q string, n int32, names ...string) api.SubscriptionGroup {
		return api.SubscriptionGroup{Account: acc, Subject: subj, Queue: q, Connections: n, Names: names, NameCount: int32(len(names))}
	}
	return &api.SubjectsFacts{
		Epoch: 10, EpochAt: subjAt, SystemAccount: "SYS",
		Streams: []api.SubjectStreamFact{
			{Account: "APP", Stream: "orders", Retention: "limits", Subjects: []string{"orders.>"}, Messages: i64(100), StoredPerSecond: f64(5),
				Consumers: []api.SubjectConsumerFact{{Name: "projector", Durable: true}, {Name: "mailer", Durable: true, DeliverSubject: "deliver.mailer"}}},
			{Account: "APP", Stream: "orders-archive", Mirror: "orders", Messages: i64(100), StoredPerSecond: f64(5)},
			{Account: "APP", Stream: "load-test", Retention: "limits", Subjects: []string{"loadtest.>"}, Messages: i64(9), StoredPerSecond: f64(0)},
			{Account: "APP", Stream: "returns", Retention: "limits", Subjects: []string{"returns.*.eu"}, Messages: i64(1),
				Consumers: []api.SubjectConsumerFact{{Name: "r", Durable: true}}},
			{Account: "APP", Stream: "returns-all", Retention: "limits", Subjects: []string{"returns.created.*"}, Messages: i64(1),
				Consumers: []api.SubjectConsumerFact{{Name: "r", Durable: true}}},
			{Account: "APP", Stream: "catalog", Retention: "limits", Subjects: []string{"catalog.>"}, Messages: i64(3), StoredPerSecond: f64(1)},
			{Account: "APP", Stream: "search", Retention: "limits", Sources: []string{"catalog"}, Messages: i64(3), StoredPerSecond: f64(1),
				Consumers: []api.SubjectConsumerFact{{Name: "indexer", Durable: true}}},
			{Account: "APP", Stream: "jobs", Retention: "workqueue", Subjects: []string{"jobs.*"}, Messages: i64(0), StoredPerSecond: f64(0.5),
				Consumers: []api.SubjectConsumerFact{{Name: "worker", Durable: true, Filters: []string{"jobs.resize"}}}},
			{Account: "APP", Stream: "alerts", Retention: "interest", Subjects: []string{"alerts.>"}, Messages: i64(2),
				Consumers: []api.SubjectConsumerFact{{Name: "eph1", DeliverSubject: "_INBOX.alerts1"}}},
			{Account: "APP", Stream: "KV_flags", Retention: "limits", Subjects: []string{"$KV.flags.>"}, Messages: i64(4),
				Consumers: []api.SubjectConsumerFact{{Name: "w1", DeliverSubject: "_INBOX.w1"}, {Name: "w2", DeliverSubject: "_INBOX.w2"}}},
			{Account: "OTHER", Stream: "orders", Retention: "limits", Subjects: []string{"orders.>"}, Messages: i64(1),
				Consumers: []api.SubjectConsumerFact{{Name: "o", Durable: true}}},
		},
		APIs: []api.JetStreamAPIFact{{Account: "APP", PerSecond: f64(3.1), ErrorShare: f64(0.002)}},
		Subscriptions: &api.SubscriptionFacts{
			At: subjAt.Add(-2 * time.Minute),
			Servers: []api.SubszServerFact{{Name: "s2", Answered: true, Total: i64(48211), Listed: i64(10000)},
				{Name: "s1", Answered: true, Total: i64(40), Listed: i64(40)}, {Name: "s3"}},
			Groups: []api.SubscriptionGroup{
				sub("APP", "orders.place", "workers", 3, "order-service"),
				sub("APP", api.InboxPattern, "", 7, "a", "b", "c", "d", "e"),
				sub("APP", ">", "", 1, "audit-tap"),
				sub("APP", "prices.>", "", 2, "search-api", "api-gateway"),
				sub("SYS", "$SYS.>", "", 1, "natslens-example"),
				sub("OTHER", "x", "", 1),
			},
			Deliver: []api.SubscriptionGroup{
				sub("APP", "deliver.mailer", "", 1, "mailer-service"),
				sub("APP", "_INBOX.alerts1", "", 1, "inventory-service"),
				sub("APP", "_INBOX.w1", "", 1, "search-api"), sub("APP", "_INBOX.w2", "", 1, "api-gateway"),
			},
			ServerSubscriptions: map[string]int64{"SYS": 400, "APP": 12, "OTHER": 1},
			Unlisted:            map[string]int64{"APP": 7},
		},
	}
}

func rowsByKey(res *v1.GetSubjectsResponse) map[string]*v1.SubjectRow {
	out := map[string]*v1.SubjectRow{}
	for _, r := range res.GetRows() {
		k := r.GetAccount() + " " + r.GetStream()
		if r.GetStream() == "" {
			k = r.GetAccount() + " " + strings.Join(r.GetPatterns(), ",")
		}
		out[k] = r
	}
	return out
}

// A range without an epoch gives the empty response.
func TestFinishSubjectsNoEpoch(t *testing.T) {
	for _, f := range []*api.SubjectsFacts{nil, {}, {Streams: subjectsFacts().Streams}} {
		if res := api.FinishSubjects(f, "", 0); res.GetEpoch() != 0 || len(res.GetRows()) != 0 || res.GetAllCount() != 0 {
			t.Errorf("FinishSubjects(%v) = %v", f, res)
		}
	}
}

// Each row's facts for the notes: consumers and their clients, mirror and
// source links, overlap, no consumer, the work queue, the inbox fold, the
// wide subscriptions, the JetStream API.
func TestFinishSubjectsRows(t *testing.T) {
	res := api.FinishSubjects(subjectsFacts(), "", 0)
	rows := rowsByKey(res)
	orders := rows["APP orders"]
	if orders.GetDurableConsumers() != 2 || orders.GetEphemeralConsumers() != 0 || !slices.Equal(orders.GetClientNames(), []string{"mailer-service"}) ||
		!slices.Equal(orders.GetMirroredBy(), []string{"orders-archive"}) || orders.GetNoConsumer() || orders.GetStoredPerSecond() != 5 {
		t.Errorf("orders = %v", orders)
	}
	if a := rows["APP orders-archive"]; a.GetMirrorOf() != "orders" || len(a.GetPatterns()) != 0 || a.GetNoConsumer() || len(a.GetOverlaps()) != 0 {
		t.Errorf("mirror = %v", a)
	}
	if l := rows["APP load-test"]; !l.GetNoConsumer() || l.GetKind() != v1.SubjectRowKind_SUBJECT_ROW_KIND_STREAM {
		t.Errorf("load-test = %v, want no consumer", l)
	}
	// catalog has no consumer, but search sources it.
	if c := rows["APP catalog"]; c.GetNoConsumer() || !slices.Equal(c.GetSourcedInto(), []string{"search"}) {
		t.Errorf("catalog = %v", c)
	}
	if r := rows["APP returns"]; !slices.Equal(r.GetOverlaps(), []string{"returns-all"}) {
		t.Errorf("returns overlaps = %v", r.GetOverlaps())
	}
	if r := rows["APP returns-all"]; !slices.Equal(r.GetOverlaps(), []string{"returns"}) {
		t.Errorf("returns-all overlaps = %v", r.GetOverlaps())
	}
	// Same subjects in another account do not overlap.
	if o := rows["OTHER orders"]; len(o.GetOverlaps()) != 0 || len(rows["APP orders"].GetOverlaps()) != 0 {
		t.Errorf("accounts overlap: %v", o)
	}
	if j := rows["APP jobs"]; j.GetRetention() != "workqueue" || j.GetMessages() != 0 || j.Messages == nil {
		t.Errorf("jobs = %v", j)
	}
	if a := rows["APP alerts"]; a.GetEphemeralConsumers() != 1 || !slices.Equal(a.GetClientNames(), []string{"inventory-service"}) || a.GetRetention() != "interest" {
		t.Errorf("alerts = %v", a)
	}
	if kv := rows["APP KV_flags"]; kv.GetKind() != v1.SubjectRowKind_SUBJECT_ROW_KIND_KEY_VALUE || kv.GetEphemeralConsumers() != 2 ||
		!slices.Equal(kv.GetClientNames(), []string{"api-gateway", "search-api"}) || kv.GetNoConsumer() {
		t.Errorf("KV_flags = %v", kv)
	}
	if q := rows["APP orders.place"]; q.GetKind() != v1.SubjectRowKind_SUBJECT_ROW_KIND_SUBSCRIPTION || q.GetQueue() != "workers" ||
		q.GetConnections() != 3 || q.GetWide() {
		t.Errorf("queue row = %v", q)
	}
	in := rows["APP _INBOX.>"]
	if in.GetKind() != v1.SubjectRowKind_SUBJECT_ROW_KIND_INBOX || in.GetWide() || in.GetConnections() != 7 ||
		!slices.Equal(in.GetClientNames(), []string{"a", "b", "c"}) || in.GetMoreNames() != 2 {
		t.Errorf("inbox = %v", in)
	}
	if w := rows["APP >"]; !w.GetWide() {
		t.Errorf("> = %v, want wide", w)
	}
	if w := rows["APP prices.>"]; !w.GetWide() || !slices.Equal(w.GetClientNames(), []string{"api-gateway", "search-api"}) {
		t.Errorf("prices.> = %v", w)
	}
	if s := rows["SYS $SYS.>"]; !s.GetWide() || !s.GetSystemAccount() {
		t.Errorf("SYS row = %v", s)
	}
	js := rows["APP $JS.API.>"]
	if js.GetKind() != v1.SubjectRowKind_SUBJECT_ROW_KIND_JETSTREAM_API || js.GetApiPerSecond() != 3.1 || js.GetApiErrorShare() != 0.002 {
		t.Errorf("JetStream API = %v", js)
	}
	if res.GetSubscriptionsAt().AsTime() != subjAt.Add(-2*time.Minute) || res.GetServerSubscriptions() != 413 || res.GetUnlistedConnectionSubscriptions() != 7 {
		t.Errorf("subscriptions at %v, server %d, unlisted %d", res.GetSubscriptionsAt(), res.GetServerSubscriptions(), res.GetUnlistedConnectionSubscriptions())
	}
	var servers []string
	for _, s := range res.GetSubszServers() {
		servers = append(servers, s.GetServerName())
	}
	if !slices.Equal(servers, []string{"s1", "s2", "s3"}) || res.GetSubszServers()[2].GetAnswered() || res.GetSubszServers()[2].Total != nil {
		t.Errorf("servers = %v", res.GetSubszServers())
	}
	// Every row: 11 streams, 6 subscription groups, 1 API row.
	if res.GetAllCount() != 18 || res.GetStoredCount() != 11 || res.GetNoConsumerCount() != 1 || res.GetOverlappingCount() != 2 ||
		res.GetWideCount() != 3 || res.GetRowsTotal() != 18 || len(res.GetRows()) != 18 {
		t.Errorf("counts all %d stored %d none %d overlap %d wide %d total %d", res.GetAllCount(), res.GetStoredCount(), res.GetNoConsumerCount(),
			res.GetOverlappingCount(), res.GetWideCount(), res.GetRowsTotal())
	}
}

// Without a subscription collection there are no subscription rows, the
// stream rows name no connections, and the API row stays.
func TestFinishSubjectsWithoutSubscriptions(t *testing.T) {
	f := subjectsFacts()
	f.Subscriptions = nil
	res := api.FinishSubjects(f, "", 0)
	if res.SubscriptionsAt != nil || len(res.GetSubszServers()) != 0 || res.GetServerSubscriptions() != 0 {
		t.Errorf("response = %v", res)
	}
	for _, r := range res.GetRows() {
		switch {
		case r.GetKind() == v1.SubjectRowKind_SUBJECT_ROW_KIND_SUBSCRIPTION || r.GetKind() == v1.SubjectRowKind_SUBJECT_ROW_KIND_INBOX:
			t.Errorf("subscription row %v", r)
		case len(r.GetClientNames()) > 0:
			t.Errorf("names without a collection: %v", r)
		}
	}
	if rowsByKey(res)["APP orders"].GetDurableConsumers() != 2 || rowsByKey(res)["APP $JS.API.>"] == nil {
		t.Errorf("rows = %v", res.GetRows())
	}
}

// The order: warnings first; then by account with the system account
// last; streams before the other rows; streams by rate, unset last, then
// pattern; the others by connections, then pattern.
func TestFinishSubjectsOrder(t *testing.T) {
	res := api.FinishSubjects(subjectsFacts(), "", 0)
	var got []string
	for _, r := range res.GetRows() {
		k := r.GetStream()
		if k == "" {
			k = r.GetPatterns()[0]
		}
		got = append(got, r.GetAccount()+" "+k)
	}
	want := []string{
		"APP load-test", "APP returns", "APP returns-all",
		"APP orders-archive", "APP orders", "APP catalog", "APP search", "APP jobs", "APP KV_flags", "APP alerts",
		"APP _INBOX.>", "APP orders.place", "APP prices.>", "APP >", "APP $JS.API.>",
		"OTHER orders", "OTHER x",
		"SYS $SYS.>",
	}
	if !slices.Equal(got, want) {
		t.Errorf("order\n got %v\nwant %v", got, want)
	}
}

// The scope keeps one account's rows and counts; the filter selects rows
// and the counts stay those of the scope.
func TestFinishSubjectsScopeAndFilter(t *testing.T) {
	res := api.FinishSubjects(subjectsFacts(), "SYS", 0)
	if len(res.GetRows()) != 1 || res.GetServerSubscriptions() != 400 || res.GetUnlistedConnectionSubscriptions() != 0 || res.GetAllCount() != 1 {
		t.Errorf("SYS scope = %v", res)
	}
	cases := map[v1.SubjectFilter][]string{
		v1.SubjectFilter_SUBJECT_FILTER_NO_CONSUMER: {"load-test"},
		v1.SubjectFilter_SUBJECT_FILTER_OVERLAPPING: {"returns", "returns-all"},
		v1.SubjectFilter_SUBJECT_FILTER_WIDE:        {">", "prices.>"},
	}
	for filter, want := range cases {
		res := api.FinishSubjects(subjectsFacts(), "APP", filter)
		var got []string
		for _, r := range res.GetRows() {
			got = append(got, cmpOr(r.GetStream(), r.GetPatterns()))
		}
		slices.Sort(got)
		if !slices.Equal(got, want) || int(res.GetRowsTotal()) != len(want) || res.GetAllCount() != 15 || res.GetWideCount() != 2 {
			t.Errorf("%v: rows %v total %d all %d", filter, got, res.GetRowsTotal(), res.GetAllCount())
		}
	}
	if res := api.FinishSubjects(subjectsFacts(), "APP", v1.SubjectFilter_SUBJECT_FILTER_STORED); res.GetRowsTotal() != 10 {
		t.Errorf("stored = %d rows", res.GetRowsTotal())
	}
}

func cmpOr(stream string, patterns []string) string {
	if stream != "" {
		return stream
	}
	return patterns[0]
}

// The rows are cut at 1,000 after the order, with the total kept.
func TestFinishSubjectsCut(t *testing.T) {
	f := &api.SubjectsFacts{Epoch: 1, EpochAt: subjAt, Subscriptions: &api.SubscriptionFacts{At: subjAt}}
	for i := range 1200 {
		f.Subscriptions.Groups = append(f.Subscriptions.Groups, api.SubscriptionGroup{Account: "APP", Subject: fmt.Sprintf("s.%04d", i),
			Connections: int32(i % 7)})
	}
	f.Streams = []api.SubjectStreamFact{{Account: "APP", Stream: "lonely", Subjects: []string{"lonely"}}}
	res := api.FinishSubjects(f, "", 0)
	if len(res.GetRows()) != api.MaxSubjectRows || res.GetRowsTotal() != 1201 || res.GetAllCount() != 1201 || res.GetNoConsumerCount() != 1 {
		t.Errorf("rows %d total %d all %d", len(res.GetRows()), res.GetRowsTotal(), res.GetAllCount())
	}
	if res.GetRows()[0].GetStream() != "lonely" {
		t.Errorf("first row = %v, want the warning", res.GetRows()[0])
	}
}

// The tester: the stream with the pattern that matched, the consumers
// whose filter matches, mirrors and sources, the subscription groups that
// match, and the scope.
func TestFinishMatch(t *testing.T) {
	f := subjectsFacts()
	at := f.Subscriptions.At
	m := &api.MatchFacts{Epoch: f.Epoch, EpochAt: f.EpochAt, Streams: f.Streams, SubscriptionsAt: &at, Core: []api.SubscriptionGroup{
		{Account: "APP", Subject: "orders.place", Queue: "workers", Connections: 3, Names: []string{"order-service"}, NameCount: 1},
		{Account: "APP", Subject: "orders.place", Connections: 1, Names: []string{"a", "b", "c", "d"}, NameCount: 4},
		{Account: "APP", Subject: ">", Connections: 1, Names: []string{"audit-tap"}, NameCount: 1},
		{Account: "APP", Subject: "jobs.other", Connections: 1},
		{Account: "OTHER", Subject: "orders.*", Connections: 1},
	}}
	res := api.FinishMatch(m, "APP", "orders.place")
	if len(api.MatchedStreams(res)) != 1 {
		t.Fatalf("matches = %v", api.MatchedStreams(res))
	}
	o := api.MatchedStreams(res)[0]
	if o.GetStream() != "orders" || o.GetPattern() != "orders.>" || !slices.Equal(o.GetConsumers(), []string{"mailer", "projector"}) ||
		!slices.Equal(o.GetMirrors(), []string{"orders-archive"}) {
		t.Errorf("orders = %v", o)
	}
	if len(api.MatchedSubscribers(res)) != 3 || api.MatchedSubscribers(res)[0].GetSubject() != ">" || api.MatchedSubscribers(res)[2].GetQueue() != "workers" ||
		api.MatchedSubscribers(res)[1].GetMoreNames() != 1 {
		t.Errorf("subscribers = %v", api.MatchedSubscribers(res))
	}
	// A consumer whose filter does not match is left out; the source link.
	res = api.FinishMatch(m, "", "jobs.other")
	if len(api.MatchedStreams(res)) != 1 || len(api.MatchedStreams(res)[0].GetConsumers()) != 0 {
		t.Errorf("jobs.other = %v", api.MatchedStreams(res))
	}
	if res = api.FinishMatch(m, "", "jobs.resize"); !slices.Equal(api.MatchedStreams(res)[0].GetConsumers(), []string{"worker"}) {
		t.Errorf("jobs.resize = %v", api.MatchedStreams(res))
	}
	if res = api.FinishMatch(m, "", "catalog.item"); !slices.Equal(api.MatchedStreams(res)[0].GetSourcedBy(), []string{"search"}) {
		t.Errorf("catalog.item = %v", api.MatchedStreams(res))
	}
	if res = api.FinishMatch(m, "", "alerts.low"); api.MatchedStreams(res)[0].GetEphemeralConsumers() != 1 {
		t.Errorf("alerts.low = %v", api.MatchedStreams(res))
	}
	// Every account: both orders streams and OTHER's wildcard.
	res = api.FinishMatch(m, "", "orders.place")
	if len(api.MatchedStreams(res)) != 2 || len(api.MatchedSubscribers(res)) != 4 {
		t.Errorf("every account: %v %v", api.MatchedStreams(res), api.MatchedSubscribers(res))
	}
	// Nothing matches but the > subscription; without a collection no
	// subscriber.
	m.SubscriptionsAt = nil
	res = api.FinishMatch(m, "APP", "nothing.here")
	if len(api.MatchedStreams(res)) != 0 || len(api.MatchedSubscribers(res)) != 0 || res.SubscriptionsAt != nil || res.GetEpoch() != 10 {
		t.Errorf("nothing = %v", res)
	}
}
