package store

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/events"
)

func accountName(t *testing.T, s *Store, acc string) string {
	t.Helper()
	var name string
	if err := s.DB().QueryRow(`SELECT account_name FROM accounts WHERE account = ?`, acc).Scan(&name); err != nil {
		t.Fatal(err)
	}
	return name
}

// An account that JSZ and STATZ do not name is named by the name tag CONNZ
// gives its connections, or by the collector's ACCOUNTZ lookup.
func TestAccountNamedFromConnzAndLookup(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	snap := fixtureSnapshot(t, t0)
	for _, v := range snap.Servers {
		v.Accountz.Accounts = append(v.Accountz.Accounts, "ATAGGEDACCOUNT", "ALOOKEDUPACCOUNT")
		v.Connz.Conns = append(v.Connz.Conns, &server.ConnInfo{Cid: 8, Name: "x", Account: "ATAGGEDACCOUNT", NameTag: "TAGGED"})
	}
	snap.AccountNames = map[string]string{"ALOOKEDUPACCOUNT": "LOOKED"}
	if _, err := s.WriteSnapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	if got := accountName(t, s, "ATAGGEDACCOUNT"); got != "TAGGED" {
		t.Errorf("tagged account name = %q", got)
	}
	if got := accountName(t, s, "ALOOKEDUPACCOUNT"); got != "LOOKED" {
		t.Errorf("looked up account name = %q", got)
	}
}

// A connect event's name tag names an account that has none yet, and never
// replaces a name.
func TestAccountNamedFromEvents(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	snap := fixtureSnapshot(t, t0)
	for _, v := range snap.Servers {
		v.Accountz.Accounts = append(v.Accountz.Accounts, "AUNNAMED")
	}
	if _, err := s.WriteSnapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteEvents(ctx, []events.Event{{Time: t0, ReceivedAt: t0, Source: events.SourceSys, Kind: "client.connect",
		Summary: "c", Account: "AUNNAMED", AccountName: "LATER"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.LearnAccountNames(ctx, map[string]string{"AFAKEAPPACCOUNT": "OTHER"}); err != nil {
		t.Fatal(err)
	}
	if got := accountName(t, s, "AUNNAMED"); got != "LATER" {
		t.Errorf("unnamed account = %q, want LATER", got)
	}
	if got := accountName(t, s, "AFAKEAPPACCOUNT"); got != "APP" {
		t.Errorf("named account = %q, want APP kept", got)
	}
}

// A name the snapshots give (CONNZ, ACCOUNTZ, JSZ, STATZ) replaces one
// learnt from events; events never replace a name the snapshots gave.
func TestSnapshotNamesWinOverEventNames(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	snap := fixtureSnapshot(t, t0)
	for _, v := range snap.Servers {
		v.Accountz.Accounts = append(v.Accountz.Accounts, "AEVENTFIRST")
	}
	if _, err := s.WriteSnapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	if err := s.LearnAccountNames(ctx, map[string]string{"AEVENTFIRST": "FROM-EVENT"}); err != nil {
		t.Fatal(err)
	}
	next := fixtureSnapshot(t, t0.Add(30*time.Second))
	for _, v := range next.Servers {
		v.Accountz.Accounts = append(v.Accountz.Accounts, "AEVENTFIRST")
		v.Connz.Conns = append(v.Connz.Conns, &server.ConnInfo{Cid: 8, Name: "x", Account: "AEVENTFIRST", NameTag: "FROM-CONNZ"})
	}
	if _, err := s.WriteSnapshot(ctx, next); err != nil {
		t.Fatal(err)
	}
	if got := accountName(t, s, "AEVENTFIRST"); got != "FROM-CONNZ" {
		t.Errorf("after CONNZ = %q", got)
	}
	if err := s.LearnAccountNames(ctx, map[string]string{"AEVENTFIRST": "FROM-EVENT"}); err != nil {
		t.Fatal(err)
	}
	if got := accountName(t, s, "AEVENTFIRST"); got != "FROM-CONNZ" {
		t.Errorf("an event replaced the CONNZ name: %q", got)
	}
}
