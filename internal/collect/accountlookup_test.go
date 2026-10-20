package collect

import (
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

const (
	keyNamed   = "ACJQEXMPEXAMPLEAPPACCOUNTKEYQQQQQQQQQQQQQQQQQQQQQQQQQQQQ"
	keyUnnamed = "AAAAAAYYEXAMPLETAGGEDACCOUNTKEYQQQQQQQQQQQQQQQQQQQQQQQQQ"
)

// Every account ACCOUNTZ lists is looked up, in operator and config mode
// alike, and an account is not asked again within accountLookupInterval.
func TestAccountsToLookUp(t *testing.T) {
	snap := NewSnapshot(time.Now())
	v := snap.view(server.ServerInfo{ID: "N0", Name: "nats-0"})
	v.Accountz = &server.Accountz{Accounts: []string{keyNamed, keyUnnamed, "$G", "APP"}}
	w := snap.view(server.ServerInfo{ID: "N1", Name: "nats-1"})
	w.Accountz = &server.Accountz{Accounts: []string{"APP", "SYS"}}
	now := time.Now()
	asked := map[string]time.Time{}
	got := accountsToLookUp(snap, asked, now)
	want := []string{"$G", keyUnnamed, keyNamed, "APP", "SYS"}
	if !slices.Equal(got, want) {
		t.Fatalf("to look up = %v, want every listed account once: %v", got, want)
	}
	for _, id := range got {
		asked[id] = now
	}
	if got := accountsToLookUp(snap, asked, now.Add(accountLookupInterval-time.Second)); len(got) != 0 {
		t.Errorf("asked again within the interval: %v", got)
	}
	if got := accountsToLookUp(snap, asked, now.Add(accountLookupInterval)); len(got) != len(want) {
		t.Errorf("not asked again after the interval: %v", got)
	}
}

// At most accountLookupsPerEpoch accounts are looked up in one epoch: the
// ones never asked first, then the ones asked longest ago.
func TestAccountLookupOrderAndCap(t *testing.T) {
	snap := NewSnapshot(time.Now())
	v := snap.view(server.ServerInfo{ID: "N0", Name: "nats-0"})
	now := time.Now()
	asked := map[string]time.Time{}
	var ids []string
	// 30 accounts asked between 11 and 40 minutes ago, account i at 11+i.
	for i := range 30 {
		id := fmt.Sprintf("A%055d", i)
		ids = append(ids, id)
		asked[id] = now.Add(-time.Duration(11+i) * time.Minute)
	}
	// 5 accounts never asked, 5 asked a minute ago.
	for i := range 5 {
		ids = append(ids, fmt.Sprintf("N%d", i))
		recent := fmt.Sprintf("R%d", i)
		ids = append(ids, recent)
		asked[recent] = now.Add(-time.Minute)
	}
	v.Accountz = &server.Accountz{Accounts: ids}
	got := accountsToLookUp(snap, asked, now)
	if len(got) != accountLookupsPerEpoch {
		t.Fatalf("looked up %d accounts, want %d", len(got), accountLookupsPerEpoch)
	}
	if !slices.Equal(got[:5], []string{"N0", "N1", "N2", "N3", "N4"}) {
		t.Errorf("first lookups %v, want the never-asked accounts", got[:5])
	}
	// Then the asked ones, longest ago first: account 29 down to 3.
	for k, id := range got[5:] {
		if want := fmt.Sprintf("A%055d", 29-k); id != want {
			t.Fatalf("lookup %d = %s, want %s", 5+k, id, want)
		}
	}
	for _, id := range got {
		if id[0] == 'R' {
			t.Errorf("account %s asked a minute ago looked up again", id)
		}
	}
}

// The lookups run at most accountLookupParallel at a time.
func TestAccountLookupParallelism(t *testing.T) {
	var cur, peak atomic.Int32
	ids := make([]string, 30)
	for i := range ids {
		ids[i] = fmt.Sprint(i)
	}
	forEachLimited(ids, accountLookupParallel, func(string) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		cur.Add(-1)
	})
	if p := peak.Load(); p > accountLookupParallel || p < 2 {
		t.Errorf("peak parallelism %d, want 2..%d", p, accountLookupParallel)
	}
}
