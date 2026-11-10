package api_test

import (
	"testing"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// jwt is a lookup read a minute before the epoch, with JetStream limits.
func jwt(disk, mem int64) *api.AccountDetails {
	return &api.AccountDetails{At: capAt.Add(-60e9), HasJWT: true, LimitJSDisk: i64(disk), LimitJSMemory: i64(mem)}
}

// Memory store cards follow the file store cards' rules, without
// findings, and appear only when memory is in use somewhere.
func TestCapacityMemoryStores(t *testing.T) {
	servers := []api.ServerStoreFact{
		{Name: "a", Reported: true, Used: i64(1), Limit: i64(10), MemUsed: i64(100), MemLimit: i64(1000), MemPerDay: f64(30)},
		{Name: "b", Reported: true, Used: i64(1), Limit: i64(10), MemUsed: i64(0), MemLimit: i64(0), MemPerDay: f64(0)},
		{Name: "c", MemPerDay: f64(5)},
	}
	f := &api.CapacityFacts{Epoch: 1, EpochAt: capAt, Servers: servers,
		Findings: []*v1.Finding{{Id: "server-007:a", CheckCode: api.CheckStoreHigh, Entity: &v1.EntityRef{Key: "a"}, State: v1.FindingState_FINDING_STATE_FIRING}}}
	res := api.FinishCapacity(f)
	if len(res.GetMemoryStores()) != 3 {
		t.Fatalf("memory stores = %v", res.GetMemoryStores())
	}
	by := map[string]*v1.ServerStore{}
	for _, m := range res.GetMemoryStores() {
		by[m.GetServerName()] = m
	}
	if a := by["a"]; a.GetUsed() != 100 || a.GetLimit() != 1000 || a.GetDaysToFull() != 30 || a.GetFindingId() != "" {
		t.Errorf("a = %v", a)
	}
	if b := by["b"]; b.Limit != nil || b.DaysToFull != nil || b.GetUsed() != 0 {
		t.Errorf("b = %v: a limit of 0 is none", b)
	}
	if c := by["c"]; c.GetReported() || c.PerDay != nil || c.Used != nil {
		t.Errorf("c = %v: not reported shows no rate", c)
	}
	if res.GetServers()[0].GetFindingId() != "server-007:a" {
		t.Errorf("file card a = %v", res.GetServers()[0])
	}
	// Without memory in use and without a memory stream, no cards.
	f.Servers[0].MemUsed = i64(0)
	if res := api.FinishCapacity(f); len(res.GetMemoryStores()) != 0 {
		t.Errorf("memory stores without memory = %v", res.GetMemoryStores())
	}
	f.Streams = []api.CapacityStreamFact{{Account: "APP", Stream: "s", Storage: "memory"}}
	if res := api.FinishCapacity(f); len(res.GetMemoryStores()) != 3 {
		t.Errorf("memory stores with a memory stream = %v", res.GetMemoryStores())
	}
}

// An account card per storage whose JWT sets a limit above 0; none for
// -1, 0, no JWT or a lookup too old.
func TestCapacityAccountStores(t *testing.T) {
	f := &api.CapacityFacts{Epoch: 1, EpochAt: capAt, Accounts: []api.AccountStoreFact{
		{Account: "LIMITED", JSStorage: i64(400), JSMemory: i64(10), StoragePerDay: f64(100), MemoryPerDay: f64(-1), Details: jwt(1000, 100)},
		{Account: "NOLIMIT", JSStorage: i64(400), StoragePerDay: f64(100), Details: jwt(-1, 0)},
		{Account: "NOJWT", JSStorage: i64(400), Details: &api.AccountDetails{At: capAt}},
		{Account: "OLD", JSStorage: i64(400), Details: &api.AccountDetails{At: capAt.Add(-api.AccountDetailsMaxAge - 60e9), HasJWT: true,
			LimitJSDisk: i64(1000), LimitJSMemory: i64(-1)}},
		{Account: "UNREPORTED", StoragePerDay: f64(5), Details: jwt(1000, -1)},
		{Account: "FULL", JSStorage: i64(995), Details: jwt(1000, -1)},
	}}
	res := api.FinishCapacity(f)
	var got []string
	for _, a := range res.GetAccountStores() {
		got = append(got, a.GetAccount()+"/"+a.GetStorage())
	}
	// LIMITED/file fills in 6 days, first; then the fullest; unknown use last.
	want := []string{"LIMITED/file", "FULL/file", "LIMITED/memory", "UNREPORTED/file"}
	if len(got) != len(want) {
		t.Fatalf("account stores = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("account stores = %v, want %v", got, want)
		}
	}
	a := res.GetAccountStores()
	if a[0].GetUsed() != 400 || a[0].GetLimit() != 1000 || a[0].GetPerDay() != 100 || a[0].GetDaysToFull() != 6 {
		t.Errorf("LIMITED/file = %v", a[0])
	}
	if a[2].GetPerDay() != -1 || a[2].DaysToFull != nil {
		t.Errorf("LIMITED/memory = %v", a[2])
	}
	if a[3].Used != nil || a[3].PerDay != nil || a[3].DaysToFull != nil {
		t.Errorf("UNREPORTED/file = %v: no use, no rate", a[3])
	}
}

// The account limit bounds a stream where its own limits do not come
// first; a stream of an account without a limit is unchanged.
func TestCapacityAccountBound(t *testing.T) {
	stream := func(name, account, storage string, s api.CapacityStreamFact) api.CapacityStreamFact {
		s.Account, s.Stream, s.Storage = account, name, storage
		if s.Messages == nil {
			s.Messages = i64(10)
		}
		return s
	}
	f := &api.CapacityFacts{Epoch: 1, EpochAt: capAt,
		Accounts: []api.AccountStoreFact{
			{Account: "GROWS", JSStorage: i64(400), StoragePerDay: f64(100), JSMemory: i64(0), Details: jwt(1000, 50)},
			{Account: "STILL", JSStorage: i64(400), StoragePerDay: f64(0), Details: jwt(1000, -1)},
			{Account: "FULL", JSStorage: i64(995), StoragePerDay: f64(1), Details: jwt(1000, -1)},
			{Account: "FREE", JSStorage: i64(400), StoragePerDay: f64(100), Details: jwt(-1, -1)},
		},
		Streams: []api.CapacityStreamFact{
			stream("none", "GROWS", "file", api.CapacityStreamFact{}),
			stream("unknown-storage", "GROWS", "", api.CapacityStreamFact{}),
			stream("memory", "GROWS", "memory", api.CapacityStreamFact{}),
			stream("later", "GROWS", "file", api.CapacityStreamFact{MaxAge: age(30 * day), FirstTS: since(day)}),
			stream("sooner", "GROWS", "file", api.CapacityStreamFact{MaxAge: age(7 * day), FirstTS: since(4 * day)}),
			stream("steady", "GROWS", "file", api.CapacityStreamFact{MaxAge: age(day), FirstTS: since(day)}),
			stream("empty", "GROWS", "file", api.CapacityStreamFact{Messages: i64(0)}),
			stream("per-subject", "STILL", "file", api.CapacityStreamFact{MaxMsgsPerSubject: i64(1)}),
			stream("not-reached", "STILL", "file", api.CapacityStreamFact{Bytes: i64(5), MaxBytes: i64(100)}),
			stream("refused", "FULL", "file", api.CapacityStreamFact{MaxAge: age(30 * day), FirstTS: since(day)}),
			// A full account refuses every new message, whatever the
			// stream's own limits: also one steady at its age, one at its
			// own refusing limit, and an empty one.
			stream("refused-steady", "FULL", "file", api.CapacityStreamFact{MaxAge: age(day), FirstTS: since(day)}),
			stream("refused-own", "FULL", "file", api.CapacityStreamFact{Messages: i64(100), MaxMsgs: i64(100), Discard: "new", FirstTS: since(day)}),
			stream("refused-empty", "FULL", "file", api.CapacityStreamFact{Messages: i64(0)}),
			stream("free", "FREE", "file", api.CapacityStreamFact{}),
			stream("other", "NOJWT", "file", api.CapacityStreamFact{}),
		},
	}
	res := api.FinishCapacity(f)
	by := map[string]*v1.CapacityStream{}
	for _, s := range res.GetStreams() {
		by[s.GetStream()] = s
	}
	const (
		acc     = v1.NextLimitKind_NEXT_LIMIT_KIND_ACCOUNT
		reaches = v1.NextLimitState_NEXT_LIMIT_STATE_REACHES
		not     = v1.NextLimitState_NEXT_LIMIT_STATE_NOT_REACHED
	)
	for _, c := range []struct {
		name  string
		limit int64
		kind  v1.NextLimitKind
		state v1.NextLimitState
		days  float64
	}{
		{"none", 1000, acc, reaches, 6},
		{"unknown-storage", 1000, acc, reaches, 6},
		{"memory", 50, acc, not, 0},
		{"later", 1000, acc, reaches, 6},
		{"sooner", 1000, v1.NextLimitKind_NEXT_LIMIT_KIND_AGE, reaches, 3},
		{"steady", 1000, v1.NextLimitKind_NEXT_LIMIT_KIND_AGE, v1.NextLimitState_NEXT_LIMIT_STATE_STEADY, 0},
		{"empty", 1000, v1.NextLimitKind_NEXT_LIMIT_KIND_EMPTY, 0, 0},
		{"per-subject", 1000, acc, not, 0},
		{"not-reached", 1000, v1.NextLimitKind_NEXT_LIMIT_KIND_BYTES, not, 0},
		{"refused", 1000, acc, v1.NextLimitState_NEXT_LIMIT_STATE_REACHED_REFUSING, 0},
		{"refused-steady", 1000, acc, v1.NextLimitState_NEXT_LIMIT_STATE_REACHED_REFUSING, 0},
		{"refused-own", 1000, acc, v1.NextLimitState_NEXT_LIMIT_STATE_REACHED_REFUSING, 0},
		{"refused-empty", 1000, acc, v1.NextLimitState_NEXT_LIMIT_STATE_REACHED_REFUSING, 0},
		{"free", 0, v1.NextLimitKind_NEXT_LIMIT_KIND_NONE, 0, 0},
		{"other", 0, v1.NextLimitKind_NEXT_LIMIT_KIND_NONE, 0, 0},
	} {
		s := by[c.name]
		n := s.GetNextLimit()
		if s.GetAccountLimit() != c.limit || n.GetLimit() != c.kind || n.GetState() != c.state || n.GetDays() != c.days {
			t.Errorf("%s: account limit %d, next %v; want %d %v %v %v", c.name, s.GetAccountLimit(), n, c.limit, c.kind, c.state, c.days)
		}
		if n.GetLimit() == acc && !n.GetDiscardNew() {
			t.Errorf("%s: an account limit refuses new messages", c.name)
		}
	}
	// stream-005's status is the check's: a stream without its own limit
	// stays unbounded under an account limit.
	if by["none"].GetStatus() != v1.CapacityStatus_CAPACITY_STATUS_UNBOUNDED {
		t.Errorf("none status = %v", by["none"].GetStatus())
	}
}
