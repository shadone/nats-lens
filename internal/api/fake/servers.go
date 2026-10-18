package fake

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Version is the nats-server version every server of example-prod runs.
const Version = "2.12.2"

type serverSpec struct {
	name    string
	id      string
	oldID   string
	h, m, s int // start time on 2026-10-17, or on day of October 2026
	day     int
	conns   int64
	cluster string
	version string
	// store and maxStore are the JetStream file store in use and its
	// limit, mem the resident memory, cpu the percent of one core.
	store, maxStore int64
	mem             int64
	cpu             float64
	// down is when the server stopped answering, in the fixed frame; zero
	// while it answers.
	down time.Time
	// tags are the server's configured tags.
	tags []string
	// node is the Kubernetes node of the server's pod; empty means one
	// named after the server. podPending is a pod no node has room for.
	node       string
	podPending bool
	// maxPayload is the server's max_payload; zero means 1 MiB.
	maxPayload int64
	// slow are the fixed-frame times of the slow consumer disconnects on
	// the server; nil on every server shares the scenario's among them.
	slow []time.Time
}

// started is when the server's process started in the scenario.
func (s scen) started(sp serverSpec) time.Time {
	if sp.day == 0 {
		return s.at(sp.h, sp.m, sp.s)
	}
	return time.Date(2026, 10, sp.day, sp.h, sp.m, sp.s, 0, time.UTC).Add(s.shift)
}

// answering reports whether the server answers at the scenario's present.
func (sp serverSpec) answering() bool { return sp.down.IsZero() }

// server summarizes one server; cur are the findings firing at the range's
// end.
func (s scen) server(sp serverSpec, r *v1.TimeRange, cur []*v1.Finding) *v1.ServerSummary {
	start := s.started(sp)
	// Uptime as of the newest epoch inside the range, or the last one the
	// server answered in.
	seen := earliest(s.lastEpoch(), r.GetEnd().AsTime())
	if !sp.answering() {
		seen = earliest(sp.down.Add(s.shift), seen)
	}
	out := &v1.ServerSummary{
		Name: sp.name, ServerId: sp.id, Version: sp.version, Cluster: sp.cluster, StartTime: ts(start),
		Uptime: dur(seen.Sub(start).Truncate(time.Second)), Role: v1.ServerRole_SERVER_ROLE_FOLLOWER,
		Present: true, Healthz: "ok", Connections: ptr(sp.conns),
		JsStorageBytes: ptr(sp.store), JsMaxStorageBytes: ptr(sp.maxStore), MemBytes: ptr(sp.mem), CpuPercent: ptr(sp.cpu),
		Status: v1.Status_STATUS_OK, Tags: sp.tags,
		SlowConsumersInRange: ptr(s.slowCount(sp, r.GetStart().AsTime(), r.GetEnd().AsTime())),
	}
	streams, consumers := s.leads(sp)
	out.StreamLeaders, out.ConsumerLeaders = ptr(streams), ptr(consumers)
	if sp.name == s.sc.metaLeader {
		out.Role = v1.ServerRole_SERVER_ROLE_META_LEADER
	}
	if inRange(start, r) {
		out.Restarts = append(out.Restarts, ts(start))
	}
	if !sp.answering() {
		// A server that stopped answering keeps the numbers of its last
		// answer, without a health reply.
		out.Present, out.Healthz = false, ""
	}
	out.Status, _ = worstStatus(cur, serverRef(sp.name))
	return out
}

func (s scen) servers(r *v1.TimeRange, all []*v1.Finding) []*v1.ServerSummary {
	cur := current(all, r)
	out := make([]*v1.ServerSummary, len(s.sc.servers))
	for i, sp := range s.sc.servers {
		out[i] = s.server(sp, r, cur)
	}
	return out
}

// ListServers returns every server, the expected names and the servers
// that did not answer.
func (p *Provider) ListServers(_ context.Context, req *v1.ListServersRequest) (*v1.ListServersResponse, error) {
	s := p.scen()
	res := &v1.ListServersResponse{Servers: s.servers(req.GetRange(), p.findings(s))}
	for _, sp := range p.sc.servers {
		res.Expected = append(res.Expected, sp.name)
		if !sp.answering() {
			res.Missing = append(res.Missing, sp.name)
		}
	}
	return res, nil
}

var endpoints = []string{"STATSZ", "VARZ", "CONNZ", "ROUTEZ", "GATEWAYZ", "LEAFZ", "ACCOUNTZ", "ACCSTATZ", "JSZ", "HEALTHZ", "RAFTZ", "RAFTZ:APP", "IPQUEUESZ"}

// GetServer returns one server with its endpoints and both processes.
func (p *Provider) GetServer(_ context.Context, req *v1.GetServerRequest) (*v1.GetServerResponse, error) {
	s := p.scen()
	sp, ok := p.sc.server(req.GetName())
	if !ok {
		return nil, fmt.Errorf("server %q: %w", req.GetName(), api.ErrNotFound)
	}
	start := s.started(sp)
	lastSeen := s.now.Add(-4 * time.Second)
	state := "ok"
	if !sp.answering() {
		lastSeen, state = sp.down.Add(s.shift), "missing"
	}
	res := &v1.GetServerResponse{
		Server:        s.server(sp, req.GetRange(), current(p.findings(s), req.GetRange())),
		Subscriptions: ptr(int64(1840)), MetaLeader: p.sc.metaLeader, ConfigDigest: "sha256:7f3a91c2",
		Processes: []*v1.ServerProcess{
			{ServerId: sp.oldID, StartTime: ts(s.on(2, 8, 12)), FirstSeen: ts(s.on(3, 0, 0)), LastSeen: ts(start.Add(-2 * time.Minute))},
			{ServerId: sp.id, StartTime: ts(start), FirstSeen: ts(start.Add(30 * time.Second)), LastSeen: ts(lastSeen)},
		},
	}
	for _, e := range endpoints {
		res.Endpoints = append(res.Endpoints, &v1.EndpointOutcome{Endpoint: e, State: state})
	}
	s.detail(sp, res)
	return res, nil
}

// connections builds the connections of the scenario's newest epoch: each
// answering server's count, named in turn after the scenario's clients.
func (s scen) connections() []*v1.Connection {
	var out []*v1.Connection
	for _, sp := range s.sc.servers {
		if !sp.answering() {
			continue
		}
		start := s.started(sp)
		for i := range sp.conns {
			name := s.sc.clients[int(i)%len(s.sc.clients)]
			lang := "nats.js"
			if name == "media-worker" || name == "video-transcoder" || name == "label-printer" {
				lang = "go"
			}
			out = append(out, &v1.Connection{
				Key: fmt.Sprintf("%s/%d", sp.name, 100+i), Server: sp.name, Cid: 100 + i, Name: name, Lang: lang, Version: "3.0.2", Account: Account, User: "app",
				Ip: fmt.Sprintf("10.20.%d.%d", 1+i%4, 10+i), Port: ptr(int32(40000 + i)), StartTime: ts(start.Add(time.Duration(i) * 7 * time.Second)),
				LastActivity: ts(s.now.Add(-time.Duration(i%30) * time.Second)), Rtt: dur(time.Duration(300+i*13) * time.Microsecond),
				PendingBytes: ptr((i % 9) * 128), InMsgs: ptr(1000 + i*311), OutMsgs: ptr(2000 + i*577), InBytes: ptr((1000 + i*311) * 900),
				OutBytes: ptr((2000 + i*577) * 900), Stalls: ptr(i % 23 / 22), Subscriptions: ptr(3 + i%7),
			})
		}
	}
	return out
}

// ListConnections filters the connections and pages through them.
func (p *Provider) ListConnections(_ context.Context, req *v1.ListConnectionsRequest) (*v1.ListConnectionsResponse, error) {
	var out []*v1.Connection
	for _, c := range p.scen().connections() {
		if (req.GetServer() == "" || c.GetServer() == req.GetServer()) &&
			(req.GetAccount() == "" || c.GetAccount() == req.GetAccount()) &&
			strings.Contains(c.GetName(), req.GetNameContains()) {
			out = append(out, c)
		}
	}
	page, next, err := api.Page(out, req.GetPageToken(), req.GetPageSize())
	if err != nil {
		return nil, err
	}
	// The epoch the range's freshness names; the connections themselves
	// are the newest epoch's.
	_, epoch, _ := p.scen().epochBefore(req.GetRange())
	return &v1.ListConnectionsResponse{Connections: page, NextPageToken: next, TotalCount: int32(len(out)), Epoch: epoch}, nil
}
