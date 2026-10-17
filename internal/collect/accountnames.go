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

// ACCOUNTZ lookups of account names.
const (
	// accountLookupRetry is how long an account whose lookup gave no name
	// waits before it is asked again.
	accountLookupRetry = 10 * time.Minute
	// accountRecheck is how long a learnt name is kept before it is
	// looked up again, so that a renamed account is noticed.
	accountRecheck = 24 * time.Hour
	// accountLookupsPerEpoch caps the lookups of one epoch, the accounts
	// asked longest ago first.
	accountLookupsPerEpoch = 32
	// accountLookupParallel caps the lookups in flight at once.
	accountLookupParallel = 8
)

// isAccountKey reports whether id is an operator-mode account public key.
// A config-mode account's identifier is its name, so it is never looked up.
func isAccountKey(id string) bool { return len(id) == 56 && strings.HasPrefix(id, "A") }

// reportedAccountNames returns the readable names the snapshot's replies
// give: JSZ account details, account STATZ and the name tags of CONNZ.
func reportedAccountNames(snap *Snapshot) map[string]string {
	out := map[string]string{}
	add := func(id, name string) {
		if id != "" && name != "" && name != id {
			out[id] = name
		}
	}
	for _, v := range snap.Servers {
		if v.Jsz != nil {
			for _, d := range v.Jsz.AccountDetails {
				if d != nil {
					add(d.Id, d.Name)
				}
			}
		}
		if v.AccountStatz != nil {
			for _, a := range v.AccountStatz.Accounts {
				if a != nil {
					add(a.Account, a.Name)
				}
			}
		}
		if v.Connz != nil {
			for _, c := range v.Connz.Conns {
				if c != nil {
					add(c.Account, c.NameTag)
				}
			}
		}
	}
	return out
}

// accountsToLookUp returns the operator-mode accounts ACCOUNTZ lists that
// no reply names: those no earlier lookup named, unless asked within
// accountLookupRetry, and those a lookup named more than accountRecheck
// ago. At most accountLookupsPerEpoch are returned: accounts without a
// name before rechecks, and the ones asked longest ago (or never) first.
func accountsToLookUp(snap *Snapshot, known map[string]string, asked map[string]time.Time, now time.Time) []string {
	reported := reportedAccountNames(snap)
	set := map[string]bool{}
	for _, v := range snap.Servers {
		if v.Accountz == nil {
			continue
		}
		for _, id := range v.Accountz.Accounts {
			if !isAccountKey(id) || reported[id] != "" {
				continue
			}
			at, ok := asked[id]
			if known[id] != "" && (!ok || now.Sub(at) < accountRecheck) {
				continue
			}
			if known[id] == "" && ok && now.Sub(at) < accountLookupRetry {
				continue
			}
			set[id] = true
		}
	}
	out := sortedKeys(set)
	// Accounts without a name go first, then the rechecks; within each,
	// the ones asked longest ago.
	sort.SliceStable(out, func(i, j int) bool {
		if ni, nj := known[out[i]] == "", known[out[j]] == ""; ni != nj {
			return ni
		}
		return asked[out[i]].Before(asked[out[j]])
	})
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

// accountNameFromReply reads the readable name of account id from an
// ACCOUNTZ reply with the account filter: its name tag, else the name in
// its decoded JWT. Only the name is read; the JWT itself is not kept.
func accountNameFromReply(b []byte, id string) string {
	var r reply[struct {
		Account *struct {
			Name    string `json:"account_name"`
			NameTag string `json:"name_tag"`
			Claim   *struct {
				Name string `json:"name"`
			} `json:"decoded_jwt"`
		} `json:"account_detail"`
	}]
	if err := json.Unmarshal(b, &r); err != nil || r.Data.Account == nil || r.Data.Account.Name != id {
		return ""
	}
	if a := r.Data.Account; a.NameTag != "" {
		return a.NameTag
	} else if a.Claim != nil && a.Claim.Name != id {
		return a.Claim.Name
	}
	return ""
}

// lookUpAccountNames asks ACCOUNTZ for each account, in parallel, and
// returns the names found. The requests are best effort: they are not
// endpoints of the epoch, and a missing reply only means the account keeps
// its identifier until the next try.
func (c *Collector) lookUpAccountNames(ctx context.Context, ids []string, want until) map[string]string {
	out := map[string]string{}
	var mu sync.Mutex
	forEachLimited(ids, accountLookupParallel, func(id string) {
		body, _ := json.Marshal(server.AccountzOptions{Account: id})
		msgs, err := gather(ctx, c.conn(), Accountz.Subject(), body, c.window, want)
		if err != nil {
			c.log.Debug("account name lookup failed", "account", id, "err", err)
			return
		}
		for _, b := range msgs {
			if name := accountNameFromReply(b, id); name != "" {
				mu.Lock()
				out[id] = name
				mu.Unlock()
				return
			}
		}
	})
	return out
}
