package collect

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

// AccountDetails is one answered ACCOUNTZ lookup of one account. It holds
// what the account's JWT claims say, never the JWT itself.
type AccountDetails struct {
	Account string
	// LookedUpAt is when the reply was read.
	LookedUpAt time.Time
	// ServerID is the server whose reply was read.
	ServerID string
	// Name is the readable name: the name tag, else the name in the JWT;
	// in config mode the account identifier. Empty when none is known.
	Name     string
	IsSystem bool
	// Expired is the server's verdict that the account JWT has expired.
	Expired bool
	// Imports and Exports count the account's imports and exports, without
	// the service imports from the system account that the server adds to
	// every account itself.
	Imports, Exports int
	// HasJWT reports that the reply carried a decoded account JWT. The
	// fields below are set only then.
	HasJWT bool
	// JWTIssuedAt and JWTExpires are zero when the JWT does not set them;
	// a zero JWTExpires means the JWT never expires.
	JWTIssuedAt, JWTExpires time.Time
	Limits                  AccountLimits
}

// AccountLimits are the limits of an account JWT: -1 is no limit. The
// connection, leaf node, subscription, data and payload limits are as the
// JWT states them. The JetStream limits are the ones nats-server enforces
// (accountJSLimits): the account-wide ones or the tiers combined, with
// stream and consumer limits of 0 read as -1; JetStream disk and memory of
// 0 mean none is allowed, which without tiers means JetStream is not
// enabled for the account.
type AccountLimits struct {
	Connections, LeafNodes, Subscriptions, Data, Payload int64
	JSDisk, JSMemory, JSStreams, JSConsumers             int64
}

// jsLimits is the JetStream part of an account JWT's limits.
type jsLimits struct {
	Memory    int64 `json:"mem_storage"`
	Disk      int64 `json:"disk_storage"`
	Streams   int64 `json:"streams"`
	Consumers int64 `json:"consumer"`
}

// accountzDetail is the part of an ACCOUNTZ account_detail that is read.
// The JWT string (field "jwt") is deliberately not declared, so it is
// never decoded into any value.
type accountzDetail struct {
	Name     string `json:"account_name"`
	NameTag  string `json:"name_tag"`
	IsSystem bool   `json:"is_system"`
	Expired  bool   `json:"expired"`
	Imports  []struct {
		Account string `json:"account"`
		Subject string `json:"subject"`
	} `json:"imports"`
	Exports []json.RawMessage `json:"exports"`
	Claim   *struct {
		Name     string `json:"name"`
		IssuedAt int64  `json:"iat"`
		Expires  int64  `json:"exp"`
		Nats     struct {
			Limits struct {
				Conn    int64 `json:"conn"`
				Leaf    int64 `json:"leaf"`
				Subs    int64 `json:"subs"`
				Data    int64 `json:"data"`
				Payload int64 `json:"payload"`
				jsLimits
				Tiered map[string]jsLimits `json:"tiered_limits"`
			} `json:"limits"`
		} `json:"nats"`
	} `json:"decoded_jwt"`
}

// accountDetailsFromReply reads account id's details from an ACCOUNTZ reply
// with the account filter. It reports false when the reply does not hold
// the account, for example an error reply from a server that has not
// loaded it, or does not say which server sent it.
func accountDetailsFromReply(b []byte, id string, at time.Time) (AccountDetails, bool) {
	var r reply[struct {
		SystemAccount string          `json:"system_account"`
		Account       *accountzDetail `json:"account_detail"`
	}]
	if err := json.Unmarshal(b, &r); err != nil || r.Error != nil || r.Server == nil || r.Server.ID == "" ||
		r.Data.Account == nil || r.Data.Account.Name != id {
		return AccountDetails{}, false
	}
	a := r.Data.Account
	d := AccountDetails{Account: id, LookedUpAt: at.UTC(), ServerID: r.Server.ID, IsSystem: a.IsSystem,
		Expired: a.Expired, Exports: len(a.Exports)}
	for _, imp := range a.Imports {
		if !serverAddedImport(imp.Account, imp.Subject, r.Data.SystemAccount) {
			d.Imports++
		}
	}
	// nats-server reports the account's own identifier as its name tag
	// when it has none, so a tag equal to the identifier names nothing.
	switch {
	case a.NameTag != "" && a.NameTag != id:
		d.Name = a.NameTag
	case a.Claim != nil && a.Claim.Name != "" && a.Claim.Name != id:
		d.Name = a.Claim.Name
	case !isAccountKey(id):
		d.Name = id
	}
	if c := a.Claim; c != nil {
		d.HasJWT = true
		d.JWTIssuedAt, d.JWTExpires = unixTime(c.IssuedAt), unixTime(c.Expires)
		l := c.Nats.Limits
		js := accountJSLimits(l.jsLimits, l.Tiered)
		d.Limits = AccountLimits{
			Connections: l.Conn, LeafNodes: l.Leaf, Subscriptions: l.Subs, Data: l.Data, Payload: l.Payload,
			JSDisk: js.Disk, JSMemory: js.Memory, JSStreams: js.Streams, JSConsumers: js.Consumers,
		}
	}
	return d, true
}

// accountJSLimits picks the JetStream limits nats-server enforces for an
// account JWT (accounts.go, updateAccountClaimsWithRefresh): the
// account-wide limits when they set disk or memory, else the tiers. The
// stream and consumer limits are enforced only when positive, so 0 reads
// as -1, no limit.
func accountJSLimits(wide jsLimits, tiers map[string]jsLimits) jsLimits {
	out := wide
	if wide.Disk == 0 && wide.Memory == 0 && len(tiers) > 0 {
		out = sumTiers(tiers)
	}
	out.Streams, out.Consumers = positiveOrNoLimit(out.Streams), positiveOrNoLimit(out.Consumers)
	return out
}

func positiveOrNoLimit(v int64) int64 {
	if v <= 0 {
		return -1
	}
	return v
}

// sumTiers reads tiered JetStream limits as one limit per kind, comparable
// with the account's usage in JSZ, which counts replicated bytes. A tier
// "R<n>" limits single-copy bytes: nats-server checks the tier's
// replicated usage against its limit times n (jetstream.go,
// wouldExceedLimits). So disk and memory are each tier's limit times its
// replicas, summed, since the account can use every tier at once; -1 when
// any tier has no limit, and when the sum overflows. Streams are summed
// the same way, without the factor. The consumer limit applies per stream,
// and nats-server counts each consumer against the tier of its own
// replicas (jetstream_cluster.go, isSameTier), so one stream can hold
// consumers of every tier: the sum over the tiers is the most any one
// stream can hold. A tier key that is not R<n> is never selected by the
// server and is left out.
func sumTiers(tiers map[string]jsLimits) jsLimits {
	var out jsLimits
	add := func(sum *int64, v int64) {
		if *sum < 0 || v < 0 || v > math.MaxInt64-*sum {
			*sum = -1
			return
		}
		*sum += v
	}
	for key, t := range tiers {
		n, ok := tierReplicas(key)
		if !ok {
			continue
		}
		add(&out.Memory, scaled(t.Memory, n))
		add(&out.Disk, scaled(t.Disk, n))
		add(&out.Streams, positiveOrNoLimit(t.Streams))
		add(&out.Consumers, positiveOrNoLimit(t.Consumers))
	}
	return out
}

// tierReplicas returns n of a tier key "R<n>", the only keys nats-server
// selects (jetstream.go, tierName).
func tierReplicas(key string) (int64, bool) {
	rest, ok := strings.CutPrefix(key, "R")
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || n < 1 || strconv.FormatInt(n, 10) != rest {
		return 0, false
	}
	return n, true
}

// scaled multiplies a limit by a tier's replicas; -1 stays -1, and an
// overflow reads as no limit.
func scaled(v, n int64) int64 {
	if v < 0 {
		return -1
	}
	if v > math.MaxInt64/n {
		return -1
	}
	return v * n
}

// serverAddedImport reports whether an import is one that nats-server
// adds itself, which the operator did not configure: the service imports
// from the system account of the CONNZ and STATZ pings and the user info
// request (subjects under $SYS.REQ.), and in a JetStream account the
// JetStream API ($JS.API.>).
func serverAddedImport(account, subject, system string) bool {
	return system != "" && account == system && (strings.HasPrefix(subject, "$SYS.REQ.") || subject == "$JS.API.>")
}

// unixTime returns the time of a JWT's seconds since the epoch, zero for 0.
func unixTime(s int64) time.Time {
	if s <= 0 {
		return time.Time{}
	}
	return time.Unix(s, 0).UTC()
}
