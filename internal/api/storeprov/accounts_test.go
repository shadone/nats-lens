package storeprov_test

import (
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func TestListAccounts(t *testing.T) {
	st := fixtureStore(t)
	// A system account that STATZ reported with connections and JSZ with
	// no streams, and an account that only the first epoch reported.
	for _, q := range []string{
		`INSERT INTO accounts VALUES ('SYS', 'SYS', true, 1, 3), ('OLD', 'old-team', false, 1, 1)`,
		`INSERT INTO account_stats (epoch, server_id, account, conns, js_streams, js_consumers) VALUES
			(3, 'NFAKESERVER0ID', 'SYS', 2, 0, 0), (3, 'NFAKESERVER1NEW', 'SYS', 1, 0, 0), (1, 'NFAKESERVER0ID', 'OLD', 4, 1, 1)`,
	} {
		if _, err := st.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Clock: func() time.Time { return now }})
	res := must(c.ListAccounts(ctx, connect.NewRequest(&v1.ListAccountsRequest{Range: hour()})))
	byID := map[string]*v1.Account{}
	var order []string
	for _, a := range res.GetAccounts() {
		byID[a.GetAccount()] = a
		order = append(order, a.GetAccount())
	}
	// OLD is not in the newest epoch, so it is not listed.
	if want := []string{account, "SYS"}; !slices.Equal(order, want) {
		t.Fatalf("accounts = %v, want %v", order, want)
	}
	// The fixture's JSZ gives five streams and six consumers across two
	// servers; there is no STATZ, so connections are unknown.
	app := byID[account]
	if app.GetAccountName() != "APP" || app.GetIsSystem() || app.Connections != nil || app.GetStreams() != 5 || app.GetConsumers() != 6 {
		t.Errorf("app = %v", app)
	}
	sys := byID["SYS"]
	if !sys.GetIsSystem() || sys.GetAccountName() != "" || sys.GetConnections() != 3 || sys.Streams == nil || sys.GetStreams() != 0 || sys.GetConsumers() != 0 {
		t.Errorf("sys = %v", sys)
	}

	// The first epoch lists OLD with its own numbers.
	first := &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(time.Second))}
	res = must(c.ListAccounts(ctx, connect.NewRequest(&v1.ListAccountsRequest{Range: first})))
	if len(res.GetAccounts()) != 3 || res.GetAccounts()[1].GetAccount() != "OLD" || res.GetAccounts()[1].GetConnections() != 4 {
		t.Errorf("accounts of the first epoch = %v", res.GetAccounts())
	}

	// A range without an epoch lists no accounts.
	past := &v1.TimeRange{Start: timestamppb.New(t0.Add(-2 * time.Hour)), End: timestamppb.New(t0.Add(-time.Hour))}
	if res := must(c.ListAccounts(ctx, connect.NewRequest(&v1.ListAccountsRequest{Range: past}))); len(res.GetAccounts()) != 0 {
		t.Errorf("accounts before the first epoch = %v", res.GetAccounts())
	}
}

func TestStoreBucketListsAreEmpty(t *testing.T) {
	_, c := apitest.Serve(t, &storeprov.Provider{St: fixtureStore(t), Interval: 30 * time.Second, Clock: func() time.Time { return now }})
	if res := must(c.ListKeyValueStores(ctx, connect.NewRequest(&v1.ListKeyValueStoresRequest{}))); len(res.GetKeyValueStores()) != 0 {
		t.Errorf("key-value stores = %v", res)
	}
	if res := must(c.ListObjectStores(ctx, connect.NewRequest(&v1.ListObjectStoresRequest{}))); len(res.GetObjectStores()) != 0 {
		t.Errorf("object stores = %v", res)
	}
}
