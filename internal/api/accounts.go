package api

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Accounts and auth limits and rules.
const (
	// MaxAccountRows is the most accounts a GetAccountsAndAuth response
	// lists.
	MaxAccountRows = 500
	// MaxFailedLoginGroups is the most failed-login groups it lists.
	MaxFailedLoginGroups = 100
	// MaxExpiringRows is the most expiring rows it lists.
	MaxExpiringRows = 100
	// FailedLoginWindow is how far before the epoch failed logins are
	// counted.
	FailedLoginWindow = 24 * time.Hour
	// AccountDetailsMaxAge is how much older than the epoch an account's
	// lookup may be and still be shown; an older one reads as not looked
	// up yet.
	AccountDetailsMaxAge = 24 * time.Hour
	// ExpiringWindow is how far after the epoch an expiry is listed as
	// expiring soon.
	ExpiringWindow = 30 * 24 * time.Hour
)

// The listeners whose certificates a server reports.
const (
	ListenerClient    = "client"
	ListenerCluster   = "cluster"
	ListenerGateway   = "gateway"
	ListenerLeafnode  = "leafnode"
	ListenerWebsocket = "websocket"
	ListenerMQTT      = "mqtt"
)

// IsAccountKey reports whether an account identifier is an account public
// key (operator mode), by the collector's rule: 56 characters starting
// with A.
func IsAccountKey(id string) bool { return len(id) == 56 && strings.HasPrefix(id, "A") }

// AccountsFacts are what a provider read for one GetAccountsAndAuth: the
// epoch's accounts with their newest lookup, the failed logins grouped,
// and the certificates and user JWTs of the epoch, without order or
// limits. FinishAccounts turns them into the response, so the store and
// the demo apply the same rules.
type AccountsFacts struct {
	// Epoch is the range's newest epoch; 0 when the range holds none.
	Epoch   int64
	EpochAt time.Time
	// Accounts are the accounts of the epoch.
	Accounts []AccountFact
	// FailedLogins are the failed-login groups of the window, at most
	// MaxFailedLoginGroups of the most frequent; FailedLoginGroups and
	// FailedLoginsCount count every group.
	FailedLogins      []FailedLoginFact
	FailedLoginGroups int32
	FailedLoginsCount int64
	// WindowStart is where failed logins are counted from.
	WindowStart time.Time
	// EventsCaptured is whether any event is stored up to the epoch;
	// CaptureGaps whether capture, while it ran, stopped for part of the
	// window.
	EventsCaptured, CaptureGaps bool
	// Certificates are the listener certificates of the servers the epoch
	// expected, each from the epoch or from the server's newest collection
	// with VARZ within 24 hours.
	Certificates []CertificateFact
	// UserJWTs are the epoch's connections with a user JWT, by account and
	// user.
	UserJWTs []UserJWTFact
}

// AccountFact is one account of the epoch.
type AccountFact struct {
	Account, Name string
	IsSystem      bool
	// Connections, Leafnodes and Subscriptions are summed over the server
	// processes; nil when no server reported them.
	Connections, Leafnodes *int32
	Subscriptions          *int64
	// LimitLeafnodes are the leaf node connections of the server that
	// holds the most, LimitLeafnodesServer, which nats-server counts
	// against the connection limit with every server's clients (see
	// checks.AccountConnectionsSQL); nil as Connections, the server empty
	// without leaf nodes.
	LimitLeafnodes       *int32
	LimitLeafnodesServer string
	// JSStorage and JSMemory are the largest any server reports.
	JSStorage, JSMemory *int64
	// Streams is the distinct streams of the account; nil when not known.
	Streams *int32
	// Details is the lookup of the newest epoch up to this one, read after
	// that epoch started; nil when there is none or it is older than
	// AccountDetailsMaxAge. FinishAccounts applies the age rule again.
	Details *AccountDetails
}

// AccountDetails is what one ACCOUNTZ lookup read.
type AccountDetails struct {
	At               time.Time
	HasJWT, Expired  bool
	Imports, Exports int32
	// JWTExpires is nil when the JWT never expires or there is none.
	JWTExpires *time.Time
	// LimitConnections, LimitJSDisk and LimitJSMemory are the JWT's
	// limits, -1 no limit; nil without a JWT. JetStream disk and memory
	// both 0 means JetStream is disabled for the account.
	LimitConnections, LimitJSDisk, LimitJSMemory *int64
}

// FailedLoginFact is the failed logins of one user from one address for
// one reason.
type FailedLoginFact struct {
	User, Host, Reason, Account string
	Count                       int64
	Last                        time.Time
}

// CertificateFact is one listener certificate of one server. SeenAt is
// when the collection it was read from started, when that is older than
// the epoch; zero when read from the epoch.
type CertificateFact struct {
	Server, Listener string
	NotAfter, SeenAt time.Time
}

// UserJWTFact is the connections of one user of an account that hold a
// user JWT: the soonest expiry and how many connections hold it. Expires
// is nil when none of them reports an expiry.
type UserJWTFact struct {
	Account, User string
	Expires       *time.Time
	Connections   int32
}

// detailsOf is the account's lookup when it is recent enough for the
// epoch: read at most AccountDetailsMaxAge before the epoch started. The
// lookup of the epoch itself is read after the epoch started.
func (f *AccountsFacts) detailsOf(a AccountFact) *AccountDetails {
	d := a.Details
	if d == nil || f.EpochAt.Sub(d.At) > AccountDetailsMaxAge {
		return nil
	}
	return d
}

// FinishAccounts builds the response from the facts: the mode, the
// accounts in order, the failed logins in order, and the expiring rows
// with the counts of the rest.
func FinishAccounts(f *AccountsFacts) *v1.GetAccountsAndAuthResponse {
	if f == nil || f.Epoch == 0 {
		return &v1.GetAccountsAndAuthResponse{}
	}
	res := &v1.GetAccountsAndAuthResponse{Epoch: f.Epoch, EpochAt: timestamppb.New(f.EpochAt), EventsCaptured: f.EventsCaptured,
		CaptureGaps: f.EventsCaptured && f.CaptureGaps}
	if !f.WindowStart.IsZero() {
		res.FailedLoginsWindowStart = timestamppb.New(f.WindowStart)
	}
	var exp expiringList
	for _, a := range f.Accounts {
		res.OperatorMode = res.OperatorMode || IsAccountKey(a.Account)
		row := &v1.AccountRow{Account: a.Account, AccountName: a.Name, IsSystem: a.IsSystem, Connections: a.Connections,
			Leafnodes: a.Leafnodes, Subscriptions: a.Subscriptions, JsStorage: a.JSStorage, JsMemory: a.JSMemory, Streams: a.Streams,
			LimitLeafnodes: a.LimitLeafnodes}
		if a.LimitLeafnodes != nil && *a.LimitLeafnodes > 0 {
			row.LimitLeafnodesServer = a.LimitLeafnodesServer
		}
		if d := f.detailsOf(a); d != nil {
			row.DetailsKnown, row.DetailsAt, row.HasJwt, row.Expired = true, timestamppb.New(d.At), d.HasJWT, d.Expired
			row.Imports, row.Exports = ptrTo(d.Imports), ptrTo(d.Exports)
			if d.HasJWT {
				row.ConnLimit, row.DiskLimit = d.LimitConnections, d.LimitJSDisk
				row.JetstreamEnabled = jetStreamEnabled(d)
				if d.JWTExpires != nil {
					row.JwtExpires = timestamppb.New(*d.JWTExpires)
				}
				exp.add(f.EpochAt, &v1.Expiring{Kind: v1.ExpiringKind_EXPIRING_KIND_ACCOUNT_JWT, Account: a.Account, AccountName: a.Name},
					d.JWTExpires, d.Expired)
			}
		}
		res.Accounts = append(res.Accounts, row)
	}
	slices.SortStableFunc(res.Accounts, compareAccountRows)
	res.AccountsTotal = int32(len(res.Accounts))
	res.Accounts = res.Accounts[:min(len(res.Accounts), MaxAccountRows)]

	names := map[string]string{}
	for _, a := range f.Accounts {
		names[a.Account] = a.Name
	}
	for _, l := range f.FailedLogins {
		res.FailedLogins = append(res.FailedLogins, &v1.FailedLogin{User: l.User, Host: l.Host, Reason: l.Reason, Account: l.Account,
			AccountName: names[l.Account], Count: l.Count, Last: timestamppb.New(l.Last)})
	}
	slices.SortStableFunc(res.FailedLogins, func(a, b *v1.FailedLogin) int {
		return cmp.Or(cmp.Compare(b.GetCount(), a.GetCount()), b.GetLast().AsTime().Compare(a.GetLast().AsTime()),
			cmp.Compare(a.GetUser(), b.GetUser()), cmp.Compare(a.GetHost(), b.GetHost()), cmp.Compare(a.GetReason(), b.GetReason()))
	})
	res.FailedLogins = res.FailedLogins[:min(len(res.FailedLogins), MaxFailedLoginGroups)]
	res.FailedLoginGroupsTotal = max(f.FailedLoginGroups, int32(len(f.FailedLogins)))
	res.FailedLoginsTotal = f.FailedLoginsCount
	if res.FailedLoginsTotal == 0 {
		for _, l := range f.FailedLogins {
			res.FailedLoginsTotal += l.Count
		}
	}

	for _, c := range f.Certificates {
		e := &v1.Expiring{Kind: v1.ExpiringKind_EXPIRING_KIND_SERVER_CERTIFICATE, Server: c.Server, Listener: c.Listener}
		if !c.SeenAt.IsZero() && c.SeenAt.Before(f.EpochAt) {
			e.SeenAt = timestamppb.New(c.SeenAt)
		}
		exp.add(f.EpochAt, e, &c.NotAfter, false)
	}
	for _, u := range f.UserJWTs {
		exp.add(f.EpochAt, &v1.Expiring{Kind: v1.ExpiringKind_EXPIRING_KIND_USER_JWT, Account: u.Account, AccountName: names[u.Account],
			User: u.User, Connections: u.Connections}, u.Expires, false)
	}
	slices.SortStableFunc(exp.rows, compareExpiring)
	res.ExpiringTotal = int32(len(exp.rows))
	res.Expiring = exp.rows[:min(len(exp.rows), MaxExpiringRows)]
	res.ExpireLater, res.NeverExpire = exp.later, exp.never
	return res
}

// expiringList collects the expiring rows and counts the rest.
type expiringList struct {
	rows         []*v1.Expiring
	later, never int32
}

// add lists e when it has expired at the epoch (or the server says so) or
// expires within ExpiringWindow; a later expiry counts in later, none in
// never.
func (l *expiringList) add(epochAt time.Time, e *v1.Expiring, expires *time.Time, expired bool) {
	if expires == nil && !expired {
		l.never++
		return
	}
	if expires != nil {
		e.Expires = timestamppb.New(*expires)
		expired = expired || !expires.After(epochAt)
		if !expired && expires.Sub(epochAt) > ExpiringWindow {
			l.later++
			return
		}
	}
	e.Expired = expired
	l.rows = append(l.rows, e)
}

// compareAccountRows puts the system account last and the others by
// connections, most first (unknown as none), then by name.
func compareAccountRows(a, b *v1.AccountRow) int {
	return cmp.Or(compareBool(a.GetIsSystem(), b.GetIsSystem()), cmp.Compare(b.GetConnections(), a.GetConnections()),
		cmp.Compare(cmp.Or(a.GetAccountName(), a.GetAccount()), cmp.Or(b.GetAccountName(), b.GetAccount())),
		cmp.Compare(a.GetAccount(), b.GetAccount()))
}

// compareExpiring puts the expired first, then the soonest; a row without
// a time (an account JWT the server reports expired) first among the
// expired.
func compareExpiring(a, b *v1.Expiring) int {
	at := func(e *v1.Expiring) time.Time {
		if e.GetExpires() == nil {
			return time.Time{}
		}
		return e.GetExpires().AsTime()
	}
	return cmp.Or(compareBool(b.GetExpired(), a.GetExpired()), at(a).Compare(at(b)), cmp.Compare(a.GetKind(), b.GetKind()),
		cmp.Compare(a.GetServer(), b.GetServer()), cmp.Compare(a.GetListener(), b.GetListener()),
		cmp.Compare(a.GetAccount(), b.GetAccount()), cmp.Compare(a.GetUser(), b.GetUser()))
}

// jetStreamEnabled is whether the account JWT enables JetStream: a disk
// or memory limit other than 0, as nats-server enables it. nil when the
// lookup did not read both limits.
func jetStreamEnabled(d *AccountDetails) *bool {
	if d.LimitJSDisk == nil || d.LimitJSMemory == nil {
		return nil
	}
	return ptrTo(*d.LimitJSDisk != 0 || *d.LimitJSMemory != 0)
}
