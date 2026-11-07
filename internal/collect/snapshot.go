package collect

import (
	"sort"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

// State is the outcome of one endpoint request for one server.
type State string

const (
	// StateOK means the server replied with data.
	StateOK State = "ok"
	// StateError means the server replied with an error, or the reply did not decode.
	StateError State = "error"
	// StateMissing means the server answered other requests in the epoch but not this one.
	StateMissing State = "missing"
)

// EndpointStatus records how one endpoint request went for one server.
type EndpointStatus struct {
	State State
	Error string
	// Unknown are the values of a reply that decoded only with them left
	// out: values of enum fields that this nats-server version does not
	// know, as a newer server can send.
	Unknown []UnknownValue
}

// ServerView is everything one server reported in one epoch. Fields are nil
// when the server did not answer that endpoint.
type ServerView struct {
	Info         server.ServerInfo
	Statsz       *server.ServerStats
	Varz         *server.Varz
	Connz        *server.Connz
	Routez       *server.Routez
	Gatewayz     *server.Gatewayz
	Leafz        *server.Leafz
	Accountz     *server.Accountz
	AccountStatz *server.AccountStatz
	Jsz          *server.JSInfo
	Healthz      *server.HealthStatus
	Raftz        RaftzStatus
	Ipqueuesz    server.IpqueueszStatus
	// Subsz is the server's subscription list, set only in an epoch in
	// which a subscription collection was due and the server answered.
	Subsz *server.Subsz
	// UserJWTs holds, by connection ID, what was read from the user JWT
	// of each CONNZ connection that reported one. The JWT itself is
	// removed from Connz when the reply is applied.
	UserJWTs map[uint64]UserJWT
	Status   map[Endpoint]EndpointStatus
}

// Snapshot is one epoch: every server's own view, never merged.
type Snapshot struct {
	StartedAt  time.Time
	FinishedAt time.Time
	// Expected are the server names the collector expected to answer.
	Expected []string
	// Servers holds one view per server ID that answered any request.
	Servers map[string]*ServerView
	// Errors are failures of the collector itself (publish failed, connection lost).
	Errors []string
	// AccountNames maps account IDs to the readable names the collector
	// looked up with ACCOUNTZ, for accounts no other reply names.
	AccountNames map[string]string
	// AccountDetails are the ACCOUNTZ lookups answered in this epoch,
	// sorted by account; an account not looked up this epoch has none.
	AccountDetails []AccountDetails
	// Micro holds what each account connection's micro services answered,
	// sorted by label; nil without account connections.
	Micro []MicroAccount
}

// NewSnapshot returns an empty snapshot started at start.
func NewSnapshot(start time.Time) *Snapshot {
	return &Snapshot{StartedAt: start, Servers: map[string]*ServerView{}}
}

func (s *Snapshot) view(info server.ServerInfo) *ServerView {
	v, ok := s.Servers[info.ID]
	if !ok {
		v = &ServerView{Info: info, Status: map[Endpoint]EndpointStatus{}}
		s.Servers[info.ID] = v
	}
	return v
}

// Observed returns the names of servers that answered, sorted.
func (s *Snapshot) Observed() []string {
	names := make([]string, 0, len(s.Servers))
	for _, v := range s.Servers {
		names = append(names, v.Info.Name)
	}
	sort.Strings(names)
	return names
}

// Missing returns the expected server names that did not answer, sorted.
func (s *Snapshot) Missing() []string {
	seen := map[string]bool{}
	for _, v := range s.Servers {
		seen[v.Info.Name] = true
	}
	var out []string
	for _, n := range s.Expected {
		if !seen[n] {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// Partial reports whether the epoch lacks a server, an endpoint reply or
// had a collector error.
func (s *Snapshot) Partial() bool {
	if len(s.Missing()) > 0 || len(s.Errors) > 0 {
		return true
	}
	for _, v := range s.Servers {
		for _, st := range v.Status {
			if st.State != StateOK {
				return true
			}
		}
	}
	return false
}

// SortedViews returns the server views ordered by server name, then ID.
func (s *Snapshot) SortedViews() []*ServerView {
	out := make([]*ServerView, 0, len(s.Servers))
	for _, v := range s.Servers {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Info.Name != out[j].Info.Name {
			return out[i].Info.Name < out[j].Info.Name
		}
		return out[i].Info.ID < out[j].Info.ID
	})
	return out
}

// markMissing records StateMissing for every endpoint a server did not answer.
func (s *Snapshot) markMissing(endpoints []Endpoint) {
	for _, v := range s.Servers {
		for _, e := range endpoints {
			if _, ok := v.Status[e]; !ok {
				v.Status[e] = EndpointStatus{State: StateMissing}
			}
		}
	}
}

// forget removes every server's status for an endpoint, and for an account
// RAFTZ request that account's raft groups.
func (s *Snapshot) forget(e Endpoint) {
	acc, isRaftz := e.raftzAccount()
	for _, v := range s.Servers {
		delete(v.Status, e)
		if isRaftz {
			delete(v.Raftz, acc)
		}
	}
}
