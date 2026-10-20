package collect

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/testcluster"
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

// lookupCollector returns a collector on a one-server cluster, and the
// want of an epoch in which a second server answered STATZ and then went
// silent.
func lookupCollector(t *testing.T, window time.Duration) (*Collector, until) {
	t.Helper()
	c := testcluster.Start(t, 1)
	col := New(c.Connect(0, testcluster.SysUser, testcluster.SysPass), Options{Window: window, ConnzLimit: 10,
		Tracker: NewTracker(time.Hour), Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	return col, allIDs([]string{c.Servers[0].ID(), "NSILENTSERVER"})
}

// All lookups of an epoch end within one window, even when a server that
// answered STATZ is silent; the accounts whose turn came after the window
// are not asked, so they stay first in line.
func TestAccountLookupsShareOneWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	window := 300 * time.Millisecond
	col, want := lookupCollector(t, window)
	var ids []string
	for i := range accountLookupsPerEpoch {
		ids = append(ids, fmt.Sprintf("ACC%02d", i))
	}
	start := time.Now()
	found, sent := col.lookUpAccounts(context.Background(), ids, want)
	if took := time.Since(start); took > window+window/2 {
		t.Errorf("32 lookups took %v, %.1f windows, want at most one", took, float64(took)/float64(window))
	}
	if len(found) != 0 {
		t.Errorf("found %+v for accounts that do not exist", found)
	}
	if len(sent) == 0 || len(sent) >= len(ids) {
		t.Errorf("asked %d of %d accounts, want the first rounds only", len(sent), len(ids))
	}
}

// A lookup stops at the first reply that holds the account instead of
// waiting for a silent server.
func TestAccountLookupStopsAtFirstHoldingReply(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	window := 2 * time.Second
	col, want := lookupCollector(t, window)
	start := time.Now()
	found, sent := col.lookUpAccounts(context.Background(), []string{"APP", "SYS"}, want)
	if took := time.Since(start); took > window/2 {
		t.Errorf("lookups of loaded accounts took %v of a %v window", took, window)
	}
	if len(found) != 2 || !slices.Equal(sent, []string{"APP", "SYS"}) {
		t.Errorf("found %+v, asked %v", found, sent)
	}
}

// The lookup times and names of accounts that ACCOUNTZ no longer lists
// are dropped; an epoch without any ACCOUNTZ reply keeps them.
func TestForgetUnlistedAccounts(t *testing.T) {
	now := time.Now()
	c := &Collector{accountAsked: map[string]time.Time{"APP": now, "GONE": now, keyNamed: now},
		accountNames: map[string]string{keyNamed: "APP", keyUnnamed: "OLD"}}
	silent := NewSnapshot(now)
	silent.view(server.ServerInfo{ID: "N0", Name: "nats-0"})
	c.forgetUnlistedAccounts(silent)
	if len(c.accountAsked) != 3 || len(c.accountNames) != 2 {
		t.Fatalf("an epoch without ACCOUNTZ dropped %v %v", c.accountAsked, c.accountNames)
	}
	snap := NewSnapshot(now)
	snap.view(server.ServerInfo{ID: "N0", Name: "nats-0"}).Accountz = &server.Accountz{Accounts: []string{"APP"}}
	snap.view(server.ServerInfo{ID: "N1", Name: "nats-1"}).Accountz = &server.Accountz{Accounts: []string{keyNamed}}
	c.forgetUnlistedAccounts(snap)
	if _, ok := c.accountAsked["GONE"]; ok || len(c.accountAsked) != 2 {
		t.Errorf("asked = %v, want APP and the key only", c.accountAsked)
	}
	if _, ok := c.accountNames[keyUnnamed]; ok || c.accountNames[keyNamed] != "APP" {
		t.Errorf("names = %v, want only the listed key's", c.accountNames)
	}
}
