package perf

import (
	"fmt"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

// Names of the synthetic entities.
func serverName(i int) string { return fmt.Sprintf("nats-%d", i) }
func serverID(i int) string   { return fmt.Sprintf("NPERFSERVER%02dID", i) }
func accountID(i int) string  { return fmt.Sprintf("APERFACCOUNT%03d", i) }
func accountName(i int) string {
	if i == 0 {
		return "APP"
	}
	return fmt.Sprintf("SHOP%02d", i)
}
func streamName(i int) string { return fmt.Sprintf("order-jobs-%04d", i) }
func consumerName(s, c int) string {
	return fmt.Sprintf("resize-worker-%04d-%d", s, c)
}

// started is when every synthetic server and asset began, before the
// history.
var started = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

// Snapshot builds the epoch n (1-based) of the profile, started at at:
// every server answers VARZ, CONNZ, ROUTEZ, ACCSTATZ, JSZ and HEALTHZ.
// Stream s lives on servers s..s+Replicas-1 (mod Servers), led by the
// first; its consumers are placed and led the same way. Counters grow
// with n at fixed per-entity rates.
func (p Profile) Snapshot(n int, at time.Time) *collect.Snapshot {
	snap := collect.NewSnapshot(at)
	for i := range p.Servers {
		snap.Expected = append(snap.Expected, serverName(i))
	}
	elapsed := at.Sub(started)
	for si := range p.Servers {
		info := server.ServerInfo{Name: serverName(si), ID: serverID(si), Cluster: "shop-east", Version: "2.15.2",
			JetStream: true, Host: fmt.Sprintf("10.0.0.%d", si+1)}
		v := &collect.ServerView{Info: info, Status: map[collect.Endpoint]collect.EndpointStatus{}}
		for _, e := range []collect.Endpoint{collect.Varz, collect.Connz, collect.Routez, collect.AccountStatz, collect.Jsz, collect.Healthz} {
			v.Status[e] = collect.EndpointStatus{State: collect.StateOK}
		}
		perServer := p.Connections / p.Servers
		in := int64(n) * 30_000 * int64(si+1)
		v.Varz = &server.Varz{ID: info.ID, Name: info.Name, Version: info.Version, Start: started, Now: at,
			Uptime: elapsed.String(), Mem: 512 << 20, CPU: 20 + float64((n+si)%15), Cores: 8, MaxProcs: 8,
			Connections: perServer, TotalConnections: uint64(perServer + n), Routes: p.Servers - 1,
			Subscriptions: uint32(perServer * 4), InMsgs: in, OutMsgs: 2 * in, InBytes: 512 * in, OutBytes: 1024 * in,
			SlowConsumers: int64(n / 500), MaxPayload: 1 << 20, MaxConn: 65536, SystemAccount: "SYS",
			JetStream: server.JetStreamVarz{
				Config: &server.JetStreamConfig{MaxStore: 1 << 40, MaxMemory: 8 << 30},
				Stats:  &server.JetStreamStats{Store: uint64(200<<30) + uint64(n)<<16, Memory: 1 << 20},
				Meta:   &server.MetaClusterInfo{Name: "shop-east", Leader: serverName(0), Size: p.Servers},
			}}
		v.Connz = p.connz(si, n, at)
		v.Routez = p.routez(si, n, at)
		v.AccountStatz = p.accountStatz(si, n, at)
		v.Jsz = p.jsz(si, n, at)
		v.Healthz = &server.HealthStatus{Status: "ok", StatusCode: 200}
		snap.Servers[info.ID] = v
	}
	snap.FinishedAt = at.Add(400 * time.Millisecond)
	return snap
}

func (p Profile) connz(si, n int, at time.Time) *server.Connz {
	total := p.Connections / p.Servers
	listed := min(total, p.ConnzLimit)
	c := &server.Connz{ID: serverID(si), Now: at, NumConns: listed, Total: total, Limit: p.ConnzLimit}
	for k := range listed {
		g := si*total + k // global connection number
		acc := g % p.Accounts
		msgs := int64(n) * int64(1+g%50)
		c.Conns = append(c.Conns, &server.ConnInfo{Cid: uint64(1000 + k), Kind: "Client", Type: "nats",
			Name: fmt.Sprintf("media-worker-%d", g%400), Lang: []string{"go", "nats.js", "python3", "java"}[g%4], Version: "1.40.0",
			Account: accountID(acc), NameTag: accountName(acc), IP: fmt.Sprintf("10.%d.%d.%d", 1+si, (g/250)%250, g%250), Port: 40000 + g%20000,
			Start: started, LastActivity: at.Add(-time.Duration(g%30) * time.Second), RTT: fmt.Sprintf("%dµs", 200+g%800),
			InMsgs: msgs, OutMsgs: 2 * msgs, InBytes: 256 * msgs, OutBytes: 512 * msgs, NumSubs: uint32(1 + g%8)})
	}
	return c
}

func (p Profile) routez(si, n int, at time.Time) *server.Routez {
	r := &server.Routez{ID: serverID(si), Name: serverName(si), Now: at}
	for o := range p.Servers {
		if o == si {
			continue
		}
		msgs := int64(n) * 5000
		r.Routes = append(r.Routes, &server.RouteInfo{Rid: uint64(o + 1), RemoteID: serverID(o), RemoteName: serverName(o),
			DidSolicit: o > si, IsConfigured: true, IP: fmt.Sprintf("10.0.0.%d", o+1), Port: 6222, Start: started,
			LastActivity: at, RTT: "300µs", InMsgs: msgs, OutMsgs: msgs, InBytes: 512 * msgs, OutBytes: 512 * msgs, NumSubs: 400})
	}
	return r
}

func (p Profile) accountStatz(si, n int, at time.Time) *server.AccountStatz {
	a := &server.AccountStatz{ID: serverID(si), Now: at}
	for i := range p.Accounts {
		msgs := int64(n) * int64(100*(i+1))
		a.Accounts = append(a.Accounts, &server.AccountStat{Account: accountID(i), Name: accountName(i),
			Conns: p.Connections / p.Servers / p.Accounts, TotalConns: p.Connections / p.Servers / p.Accounts, NumSubs: 400,
			Sent: server.DataStats{Msgs: msgs, Bytes: 512 * msgs}, Received: server.DataStats{Msgs: msgs, Bytes: 512 * msgs}})
	}
	return a
}

// replicaServers are the servers stream s is placed on, leader first.
func (p Profile) replicaServers(s int) []int {
	out := make([]int, 0, p.Replicas)
	for r := range p.Replicas {
		out = append(out, (s+r)%p.Servers)
	}
	return out
}

func (p Profile) cluster(group string, servers []int, self int) *server.ClusterInfo {
	since := started
	ci := &server.ClusterInfo{Name: "shop-east", RaftGroup: group, Leader: serverName(servers[0]), LeaderSince: &since}
	for _, o := range servers {
		if o == self {
			continue
		}
		ci.Replicas = append(ci.Replicas, &server.PeerInfo{Name: serverName(o), Peer: fmt.Sprintf("P%02d", o), Current: true,
			Active: 120 * time.Millisecond})
	}
	return ci
}

func (p Profile) jsz(si, n int, at time.Time) *server.JSInfo {
	perStream := max(p.Consumers/max(p.Streams, 1), 0)
	accs := make([]*server.AccountDetail, p.Accounts)
	for i := range accs {
		accs[i] = &server.AccountDetail{Name: accountName(i), Id: accountID(i)}
	}
	streams, consumers := 0, 0
	for s := range p.Streams {
		servers := p.replicaServers(s)
		on := false
		for _, o := range servers {
			on = on || o == si
		}
		if !on {
			continue
		}
		streams++
		acc := s % p.Accounts
		last := uint64(n) * uint64(1+s%20)
		first := last / 2
		sd := server.StreamDetail{Name: streamName(s), Created: started,
			Cluster: p.cluster(fmt.Sprintf("S-R%d-%04d", p.Replicas, s), servers, si),
			Config: &server.StreamConfig{Name: streamName(s), Subjects: []string{fmt.Sprintf("orders.%04d.>", s)},
				Retention: server.LimitsPolicy, Storage: server.FileStorage, Replicas: p.Replicas, MaxConsumers: -1,
				MaxMsgs: 1_000_000, MaxBytes: 10 << 30, MaxAge: 7 * 24 * time.Hour, MaxMsgsPer: -1, MaxMsgSize: -1,
				Discard: server.DiscardOld, Duplicates: 2 * time.Minute},
			State: server.StreamState{Msgs: last - first + 1, Bytes: (last - first + 1) * 512, FirstSeq: first, FirstTime: at.Add(-time.Hour),
				LastSeq: last, LastTime: at, NumSubjects: 1 + s%40, Consumers: perStream}}
		for c := range perStream {
			delivered := last - min(uint64(c%3)*uint64(n%7), last)
			ack := delivered - min(uint64(c%2), delivered)
			lastActive := at.Add(-time.Second)
			sd.Consumer = append(sd.Consumer, &server.ConsumerInfo{Stream: streamName(s), Name: consumerName(s, c), Created: started,
				Config: &server.ConsumerConfig{Durable: consumerName(s, c), DeliverPolicy: server.DeliverAll, AckPolicy: server.AckExplicit,
					AckWait: 30 * time.Second, MaxDeliver: 5, MaxAckPending: 1000, MaxWaiting: 512, Replicas: p.Replicas,
					FilterSubject: fmt.Sprintf("orders.%04d.%d", s, c)},
				Delivered:     server.SequenceInfo{Consumer: delivered, Stream: delivered, Last: &lastActive},
				AckFloor:      server.SequenceInfo{Consumer: ack, Stream: ack, Last: &lastActive},
				NumAckPending: int(delivered - ack), NumPending: last - delivered, NumWaiting: c % 4,
				Cluster:   p.cluster(fmt.Sprintf("C-R%d-%04d-%d", p.Replicas, s, c), servers, si),
				TimeStamp: at})
			consumers++
		}
		accs[acc].Streams = append(accs[acc].Streams, sd)
	}
	return &server.JSInfo{ID: serverID(si), Now: at, Streams: streams, Consumers: consumers,
		Config: server.JetStreamConfig{MaxStore: 1 << 40}, JetStreamStats: server.JetStreamStats{Store: 200 << 30},
		Meta: &server.MetaClusterInfo{Name: "shop-east", Leader: serverName(0), Size: p.Servers}, AccountDetails: accs}
}
