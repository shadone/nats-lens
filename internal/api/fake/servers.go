package fake

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Version is the nats-server version every server runs.
const Version = "2.12.2"

type serverSpec struct {
	name    string
	id      string
	oldID   string
	h, m, s int // restart time on 2026-10-17
	conns   int64
	leader  bool
}

var serverSpecs = []serverSpec{
	{"nats-0", "NDQ4ZFAKE0SERVER0", "NB71FAKE0OLDPROCESS", 9, 18, 0, 61, true},
	{"nats-1", "NC2XFAKE1SERVER1", "NA09FAKE1OLDPROCESS", 9, 31, 2, 60, false},
	{"nats-2", "NE5KFAKE2SERVER2", "NF33FAKE2OLDPROCESS", 9, 44, 21, 61, false},
}

func (s scen) server(sp serverSpec, r *v1.TimeRange) *v1.ServerSummary {
	start := s.at(sp.h, sp.m, sp.s)
	// Uptime as of the newest epoch inside the range.
	seen := earliest(s.lastEpoch(), r.GetEnd().AsTime())
	out := &v1.ServerSummary{
		Name: sp.name, ServerId: sp.id, Version: Version, Cluster: Cluster, StartTime: ts(start),
		Uptime: dur(seen.Sub(start).Truncate(time.Second)), Role: v1.ServerRole_SERVER_ROLE_FOLLOWER,
		Present: true, Healthz: "ok", Connections: ptr(sp.conns),
		JsStorageBytes: ptr(int64(1980 * mib)), JsMaxStorageBytes: ptr(int64(50 * gib)), MemBytes: ptr(int64(412 * mib)), CpuPercent: ptr(7.5),
		Status: v1.Status_STATUS_OK,
	}
	if sp.leader {
		out.Role = v1.ServerRole_SERVER_ROLE_META_LEADER
	}
	if inRange(start, r) {
		out.Restarts = append(out.Restarts, ts(start))
	}
	return out
}

func (s scen) servers(r *v1.TimeRange) []*v1.ServerSummary {
	out := make([]*v1.ServerSummary, len(serverSpecs))
	for i, sp := range serverSpecs {
		out[i] = s.server(sp, r)
	}
	return out
}

// ListServers returns the three servers.
func (p *Provider) ListServers(_ context.Context, req *v1.ListServersRequest) (*v1.ListServersResponse, error) {
	return &v1.ListServersResponse{
		Servers:  p.scen().servers(req.GetRange()),
		Expected: []string{"nats-0", "nats-1", "nats-2"},
	}, nil
}

var endpoints = []string{"STATSZ", "VARZ", "CONNZ", "ROUTEZ", "GATEWAYZ", "LEAFZ", "ACCOUNTZ", "ACCSTATZ", "JSZ", "HEALTHZ", "RAFTZ", "RAFTZ:APP", "IPQUEUESZ"}

// GetServer returns one server with its endpoints and both processes.
func (p *Provider) GetServer(_ context.Context, req *v1.GetServerRequest) (*v1.GetServerResponse, error) {
	s := p.scen()
	for _, sp := range serverSpecs {
		if sp.name != req.GetName() {
			continue
		}
		start := s.at(sp.h, sp.m, sp.s)
		res := &v1.GetServerResponse{
			Server: s.server(sp, req.GetRange()), Subscriptions: ptr(int64(1840)), Routes: ptr(int32(2 * 3)),
			JsStreams: ptr(int32(9)), JsConsumers: ptr(int32(54)), MetaLeader: "nats-0", ConfigDigest: "sha256:7f3a91c2",
			Processes: []*v1.ServerProcess{
				{ServerId: sp.oldID, StartTime: ts(s.on(2, 8, 12)), FirstSeen: ts(s.on(3, 0, 0)), LastSeen: ts(start.Add(-2 * time.Minute))},
				{ServerId: sp.id, StartTime: ts(start), FirstSeen: ts(start.Add(30 * time.Second)), LastSeen: ts(s.now.Add(-4 * time.Second))},
			},
		}
		for _, e := range endpoints {
			res.Endpoints = append(res.Endpoints, &v1.EndpointOutcome{Endpoint: e, State: "ok"})
		}
		return res, nil
	}
	return nil, fmt.Errorf("server %q: %w", req.GetName(), api.ErrNotFound)
}

var clientNames = []string{
	"media-worker", "order-service", "analytics-worker", "ledger-service", "api-gateway",
	"catalog-api", "callback-dispatcher", "video-transcoder",
}

// connections builds the 182 connections of the scenario's newest epoch.
func (s scen) connections() []*v1.Connection {
	var out []*v1.Connection
	for _, sp := range serverSpecs {
		start := s.at(sp.h, sp.m, sp.s)
		for i := range sp.conns {
			name := clientNames[int(i)%len(clientNames)]
			lang := "nats.js"
			if name == "media-worker" || name == "video-transcoder" {
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
	return &v1.ListConnectionsResponse{Connections: page, NextPageToken: next, TotalCount: int32(len(out)), Epoch: 1734}, nil
}
