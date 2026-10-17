package collect

import (
	"encoding/json"
	"strings"

	"github.com/nats-io/nats-server/v2/server"
)

// Endpoint names one monitoring request made to every server in an epoch.
type Endpoint string

// The endpoints requested every epoch. STATSZ is the discovery request.
const (
	Statsz       Endpoint = "STATSZ"
	Varz         Endpoint = "VARZ"
	Connz        Endpoint = "CONNZ"
	Routez       Endpoint = "ROUTEZ"
	Gatewayz     Endpoint = "GATEWAYZ"
	Leafz        Endpoint = "LEAFZ"
	Accountz     Endpoint = "ACCOUNTZ"
	AccountStatz Endpoint = "ACCSTATZ"
	Jsz          Endpoint = "JSZ"
	Healthz      Endpoint = "HEALTHZ"
	Raftz        Endpoint = "RAFTZ"
	Ipqueuesz    Endpoint = "IPQUEUESZ"
)

// DetailEndpoints are requested after discovery, in this order.
var DetailEndpoints = []Endpoint{
	Varz, Connz, Routez, Gatewayz, Leafz, Accountz, AccountStatz, Jsz, Healthz, Raftz, Ipqueuesz,
}

// raftzAccountPrefix marks a RAFTZ request for one account's raft groups.
// RAFTZ without a filter returns only the system account's groups (the
// JetStream meta group); stream and consumer groups are listed under the
// account of the asset they replicate, one request per account.
const raftzAccountPrefix = "RAFTZ:"

// AccountRaftz is the RAFTZ request for the raft groups of one account.
func AccountRaftz(account string) Endpoint { return Endpoint(raftzAccountPrefix + account) }

// raftzAccount returns the account of an AccountRaftz endpoint.
func (e Endpoint) raftzAccount() (string, bool) {
	return strings.CutPrefix(string(e), raftzAccountPrefix)
}

// Subject is the system account subject that every server answers for e.
func (e Endpoint) Subject() string {
	if e == AccountStatz {
		return "$SYS.REQ.ACCOUNT.PING.STATZ"
	}
	if _, ok := e.raftzAccount(); ok {
		return "$SYS.REQ.SERVER.PING." + string(Raftz)
	}
	return "$SYS.REQ.SERVER.PING." + string(e)
}

// requestBody is the options payload sent with the request for e.
func (e Endpoint) requestBody(connzLimit int) []byte {
	var v any
	if acc, ok := e.raftzAccount(); ok {
		b, _ := json.Marshal(server.RaftzOptions{AccountFilter: acc})
		return b
	}
	switch e {
	case Connz:
		// auth adds the account and user name; subscription counts are
		// always included, subscription lists are not requested.
		v = server.ConnzOptions{Username: true, Limit: connzLimit, Sort: server.ByCid}
	case Jsz:
		v = server.JSzOptions{Accounts: true, Streams: true, Consumer: true, Config: true, RaftGroups: true}
	case Healthz:
		v = server.HealthzOptions{Details: true}
	case AccountStatz:
		// Without include_unused a server with no local connection in an
		// account stays silent, which would read as a missing reply.
		v = server.AccountStatzOptions{IncludeUnused: true}
	default:
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}
