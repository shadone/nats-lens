package collect

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

// ACCOUNTZ lookups of account details.
const (
	// accountLookupInterval is the shortest time between two lookups of
	// one account, answered or not.
	accountLookupInterval = 10 * time.Minute
	// accountLookupsPerEpoch caps the lookups of one epoch, the accounts
	// never asked first, then the ones asked longest ago.
	accountLookupsPerEpoch = 32
	// accountLookupParallel caps the lookups in flight at once.
	accountLookupParallel = 8
)

// isAccountKey reports whether id is an operator-mode account public key.
// A config-mode account's identifier is its name.
func isAccountKey(id string) bool { return len(id) == 56 && strings.HasPrefix(id, "A") }

// accountsToLookUp returns the accounts any server's ACCOUNTZ lists that
// were not asked within accountLookupInterval: those never asked first,
// then the ones asked longest ago, at most accountLookupsPerEpoch.
func accountsToLookUp(snap *Snapshot, asked map[string]time.Time, now time.Time) []string {
	set := map[string]bool{}
	for _, v := range snap.Servers {
		if v.Accountz == nil {
			continue
		}
		for _, id := range v.Accountz.Accounts {
			if at, ok := asked[id]; ok && now.Sub(at) < accountLookupInterval {
				continue
			}
			set[id] = true
		}
	}
	out := sortedKeys(set)
	// A never-asked account has the zero time, which is before any other.
	sort.SliceStable(out, func(i, j int) bool { return asked[out[i]].Before(asked[out[j]]) })
	if len(out) > accountLookupsPerEpoch {
		out = out[:accountLookupsPerEpoch]
	}
	return out
}

// forEachLimited calls fn for every id, at most limit at a time, and
// returns when all calls returned.
func forEachLimited(ids []string, limit int, fn func(string)) {
	sem := make(chan struct{}, max(limit, 1))
	var wg sync.WaitGroup
	for _, id := range ids {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			fn(id)
		})
	}
	wg.Wait()
}

// lookUpAccounts asks ACCOUNTZ for each account, in parallel, and returns
// the details of the first reply that holds the account, sorted by
// account. The requests are best effort: they are not endpoints of the
// epoch, and an account without a reply is only asked again later.
func (c *Collector) lookUpAccounts(ctx context.Context, ids []string, want until) []AccountDetails {
	var out []AccountDetails
	var mu sync.Mutex
	forEachLimited(ids, accountLookupParallel, func(id string) {
		body, _ := json.Marshal(server.AccountzOptions{Account: id})
		msgs, err := gather(ctx, c.conn(), Accountz.Subject(), body, c.window, want)
		if err != nil {
			c.log.Debug("account lookup failed", "account", id, "err", err)
			return
		}
		at := c.now()
		for _, b := range msgs {
			if d, ok := accountDetailsFromReply(b, id, at); ok {
				mu.Lock()
				out = append(out, d)
				mu.Unlock()
				return
			}
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Account < out[j].Account })
	return out
}
