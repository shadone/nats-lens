package store

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

func TestUniqueKeysRejectDuplicates(t *testing.T) {
	s := openMem(t)
	if _, err := s.WriteSnapshot(context.Background(), fixtureSnapshot(t, t0)); err != nil {
		t.Fatal(err)
	}
	dups := []string{
		`INSERT INTO server_stats (epoch, server_id) SELECT epoch, server_id FROM server_stats LIMIT 1`,
		`INSERT INTO health (epoch, server_id, error_count) SELECT epoch, server_id, 0 FROM health LIMIT 1`,
		`INSERT INTO epoch_endpoints SELECT * FROM epoch_endpoints LIMIT 1`,
		`INSERT INTO stream_config SELECT * FROM stream_config LIMIT 1`,
		`INSERT INTO consumer_config SELECT * FROM consumer_config LIMIT 1`,
	}
	for _, q := range dups {
		if _, err := s.DB().Exec(q); err == nil {
			t.Errorf("duplicate accepted: %s", q)
		}
	}
}

// Replies can carry null list entries; the writer skips them.
func TestNilEntriesInReplies(t *testing.T) {
	s := openMem(t)
	snap := fixtureSnapshot(t, t0)
	v := snap.Servers["NFAKESERVER0ID"]
	v.Jsz.AccountDetails = append(v.Jsz.AccountDetails, nil)
	v.AccountStatz = &server.AccountStatz{Accounts: []*server.AccountStat{nil, {Account: "SYS", Name: "SYS"}}}
	if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
}

// An epoch in which only ACCOUNTZ names an account (no JSZ, no STATZ name)
// must not replace its readable name with the account identifier.
func TestAccountNameKeptWhenOnlyAccountzReports(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	first := fixtureSnapshot(t, t0)
	if _, err := s.WriteSnapshot(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := fixtureSnapshot(t, t0.Add(time.Minute))
	for _, v := range second.Servers {
		v.Jsz = nil
	}
	if _, err := s.WriteSnapshot(ctx, second); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := s.DB().QueryRow(`SELECT account_name FROM accounts WHERE account = 'AFAKEAPPACCOUNT'`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "APP" {
		t.Fatalf("account_name = %q, want APP", name)
	}
}
