package collect

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

const (
	keyNamed   = "ACJQEXMPEXAMPLEAPPACCOUNTKEYQQQQQQQQQQQQQQQQQQQQQQQQQQQQ"
	keyTagged  = "AD5PEXMPEXAMPLETAGGEDACCOUNTKEYQQQQQQQQQQQQQQQQQQQQQQQQQ"
	keyUnnamed = "AAAAAAYYEXAMPLETAGGEDACCOUNTKEYQQQQQQQQQQQQQQQQQQQQQQQQQ"
)

func TestAccountsToLookUp(t *testing.T) {
	snap := NewSnapshot(time.Now())
	v := snap.view(server.ServerInfo{ID: "N0", Name: "nats-0"})
	v.Accountz = &server.Accountz{Accounts: []string{keyNamed, keyTagged, keyUnnamed, "$G", "APP"}}
	v.Jsz = &server.JSInfo{AccountDetails: []*server.AccountDetail{{Id: keyNamed, Name: "APP"}}}
	v.Connz = &server.Connz{Conns: []*server.ConnInfo{{Account: keyTagged, NameTag: "SYS"}}}
	now := time.Now()
	asked := map[string]time.Time{}
	got := accountsToLookUp(snap, map[string]string{}, asked, now)
	if len(got) != 1 || got[0] != keyUnnamed {
		t.Fatalf("to look up = %v, want only the unnamed operator account", got)
	}
	// Asked again only after the retry period.
	asked[keyUnnamed] = now
	if got := accountsToLookUp(snap, map[string]string{}, asked, now.Add(time.Minute)); len(got) != 0 {
		t.Errorf("asked again within the retry period: %v", got)
	}
	if got := accountsToLookUp(snap, map[string]string{}, asked, now.Add(accountLookupRetry)); len(got) != 1 {
		t.Errorf("not asked again after the retry period: %v", got)
	}
	// A name already learnt is not looked up.
	if got := accountsToLookUp(snap, map[string]string{keyUnnamed: "X"}, map[string]time.Time{}, now); len(got) != 0 {
		t.Errorf("known name looked up: %v", got)
	}
}

func TestAccountNameFromAccountz(t *testing.T) {
	tag := `{"server":{"id":"N0","name":"nats-0"},"data":{"account_detail":{"account_name":"` + keyUnnamed +
		`","name_tag":"APP","jwt":"eyJ.secret"}}}`
	if got := accountNameFromReply([]byte(tag), keyUnnamed); got != "APP" {
		t.Errorf("name tag reply = %q", got)
	}
	claim := `{"server":{"id":"N0","name":"nats-0"},"data":{"account_detail":{"account_name":"` + keyUnnamed +
		`","decoded_jwt":{"name":"WORKERS"}}}}`
	if got := accountNameFromReply([]byte(claim), keyUnnamed); got != "WORKERS" {
		t.Errorf("decoded JWT reply = %q", got)
	}
	if got := accountNameFromReply([]byte(claim), keyNamed); got != "" {
		t.Errorf("reply for another account = %q", got)
	}
}

// At most accountLookupsPerEpoch accounts are looked up in one epoch, the
// longest-unasked first, and a name already learnt is looked up again
// once accountRecheck has passed.
func TestAccountLookupCapsAndRecheck(t *testing.T) {
	snap := NewSnapshot(time.Now())
	v := snap.view(server.ServerInfo{ID: "N0", Name: "nats-0"})
	var ids []string
	for i := range 40 {
		ids = append(ids, fmt.Sprintf("A%055d", i))
	}
	v.Accountz = &server.Accountz{Accounts: ids}
	now := time.Now()
	if got := accountsToLookUp(snap, map[string]string{}, map[string]time.Time{}, now); len(got) != accountLookupsPerEpoch {
		t.Fatalf("looked up %d accounts, want %d", len(got), accountLookupsPerEpoch)
	}
	one := &server.Accountz{Accounts: ids[:1]}
	v.Accountz = one
	known := map[string]string{ids[0]: "APP"}
	if got := accountsToLookUp(snap, known, map[string]time.Time{ids[0]: now.Add(-time.Hour)}, now); len(got) != 0 {
		t.Errorf("a name learnt an hour ago looked up again: %v", got)
	}
	if got := accountsToLookUp(snap, known, map[string]time.Time{ids[0]: now.Add(-25 * time.Hour)}, now); len(got) != 1 {
		t.Errorf("a name learnt 25 hours ago not rechecked: %v", got)
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

// An account without a name goes before the rechecks of learnt names,
// however long ago those were asked.
func TestAccountLookupFirstTimeBeforeRecheck(t *testing.T) {
	snap := NewSnapshot(time.Now())
	v := snap.view(server.ServerInfo{ID: "N0", Name: "nats-0"})
	now := time.Now()
	known, asked := map[string]string{}, map[string]time.Time{}
	var ids []string
	for i := range accountLookupsPerEpoch {
		id := fmt.Sprintf("A%055d", i)
		ids = append(ids, id)
		known[id], asked[id] = "N", now.Add(-48*time.Hour)
	}
	unnamed := fmt.Sprintf("A%055d", 999)
	ids = append(ids, unnamed)
	asked[unnamed] = now.Add(-15 * time.Minute)
	v.Accountz = &server.Accountz{Accounts: ids}
	got := accountsToLookUp(snap, known, asked, now)
	if len(got) != accountLookupsPerEpoch || got[0] != unnamed {
		t.Fatalf("first lookups %v, want the unnamed account first", got[:min(3, len(got))])
	}
}
