package api_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func matchGroups(res *v1.MatchSubjectResponse) []string {
	var out []string
	for _, a := range res.GetAccounts() {
		out = append(out, fmt.Sprintf("%s %d/%d api %v sys %v", a.GetAccount(), len(a.GetMatches()), len(a.GetSubscribers()), a.GetJetstreamApi(),
			a.GetSystemAccount()))
	}
	return out
}

// A message reaches only the account it is published in: without a scope
// each account answers for itself and the system account only for a $SYS.
// subject; with the system account as the scope it answers everything it
// matches. A $JS.API. subject is answered by the JetStream API of each
// account that uses JetStream. A scoped account has its group also when
// nothing matches; without a scope an account without a match has none.
func TestFinishMatchOwnAccount(t *testing.T) {
	at := subjAt
	m := &api.MatchFacts{Epoch: 1, EpochAt: at, SystemAccount: "SYS", SubscriptionsAt: &at, JetStreamAccounts: []string{"APP", "OTHER"},
		Streams: []api.SubjectStreamFact{{Account: "APP", Stream: "orders", Subjects: []string{"orders.>"}}},
		Core: []api.SubscriptionGroup{
			{Account: "SYS", Subject: ">", Connections: 1, Names: []string{"ops-monitor"}, NameCount: 1},
			{Account: "OTHER", Subject: "orders.*", Connections: 1, Names: []string{"other-app"}, NameCount: 1},
		}}
	cases := []struct {
		account, subject string
		want             []string
	}{
		{"", "orders.place", []string{"APP 1/0 api false sys false", "OTHER 0/1 api false sys false"}},
		{"APP", "orders.place", []string{"APP 1/0 api false sys false"}},
		{"", "$SYS.ACCOUNT.APP.CONNECT", []string{"SYS 0/1 api false sys true"}},
		{"SYS", "orders.place", []string{"SYS 0/1 api false sys true"}},
		{"", "$JS.API.INFO", []string{"APP 0/0 api true sys false", "OTHER 0/0 api true sys false"}},
		{"APP", "$JS.API.STREAM.INFO.orders", []string{"APP 0/0 api true sys false"}},
		{"APP", "nothing.here", []string{"APP 0/0 api false sys false"}},
		{"", "nothing.here", nil},
	}
	for _, c := range cases {
		if got := matchGroups(api.FinishMatch(m, c.account, c.subject)); !slices.Equal(got, c.want) {
			t.Errorf("%q %s: %v, want %v", c.account, c.subject, got, c.want)
		}
	}
}

// The existing facts give one group per account, in account order.
func TestFinishMatchGroups(t *testing.T) {
	f := subjectsFacts()
	at := f.Subscriptions.At
	m := &api.MatchFacts{Epoch: f.Epoch, EpochAt: f.EpochAt, Streams: f.Streams, SubscriptionsAt: &at, Core: []api.SubscriptionGroup{
		{Account: "APP", Subject: "orders.place", Queue: "workers", Connections: 3},
		{Account: "OTHER", Subject: "orders.*", Connections: 1},
	}}
	if got := matchGroups(api.FinishMatch(m, "", "orders.place")); !slices.Equal(got, []string{"APP 1/1 api false sys false", "OTHER 1/1 api false sys false"}) {
		t.Errorf("groups = %v", got)
	}
}

// A match names its stream's kind, so a KV bucket or object store is not
// taken for a stream without a consumer.
func TestFinishMatchKind(t *testing.T) {
	m := &api.MatchFacts{Epoch: 1, EpochAt: subjAt, Streams: []api.SubjectStreamFact{
		{Account: "A", Stream: "orders", Subjects: []string{"orders.>"}},
		{Account: "A", Stream: "KV_flags", Subjects: []string{"$KV.flags.>"}},
		{Account: "A", Stream: "OBJ_files", Subjects: []string{"$O.files.C.>", "$O.files.M.>"}},
	}}
	for subject, want := range map[string]v1.SubjectRowKind{
		"orders.x":       v1.SubjectRowKind_SUBJECT_ROW_KIND_STREAM,
		"$KV.flags.dark": v1.SubjectRowKind_SUBJECT_ROW_KIND_KEY_VALUE,
		"$O.files.M.abc": v1.SubjectRowKind_SUBJECT_ROW_KIND_OBJECT_STORE,
	} {
		got := api.MatchedStreams(api.FinishMatch(m, "A", subject))
		if len(got) != 1 || got[0].GetKind() != want {
			t.Errorf("%s: %v, want %v", subject, got, want)
		}
	}
}
