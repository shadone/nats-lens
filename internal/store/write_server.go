package store

import (
	"sort"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

func startTime(v *collect.ServerView) time.Time {
	switch {
	case v.Varz != nil:
		return v.Varz.Start
	case v.Statsz != nil:
		return v.Statsz.Start
	}
	return time.Time{}
}

func (w *writer) writeServerIdent(v *collect.ServerView) {
	i := v.Info
	at := w.snap.StartedAt.UTC()
	w.exec(`INSERT INTO servers (server_id, server_name, host, cluster, domain, version, jetstream, tags,
    start_time, first_epoch, last_epoch, first_seen_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (server_id) DO UPDATE SET
    server_name = excluded.server_name, host = excluded.host, cluster = excluded.cluster,
    domain = excluded.domain, version = excluded.version, jetstream = excluded.jetstream,
    tags = excluded.tags, start_time = coalesce(excluded.start_time, servers.start_time),
    last_epoch = excluded.last_epoch, last_seen_at = excluded.last_seen_at`,
		i.ID, i.Name, i.Host, i.Cluster, i.Domain, i.Version, i.JetStream, strs(i.Tags),
		ts(startTime(v)), w.epoch, w.epoch, at, at)
}

var serverStatsCols = []string{
	"epoch", "server_id", "version", "start_time", "uptime_s", "config_load_time", "config_digest",
	"mem_bytes", "cpu_percent", "cores", "gomaxprocs", "connections", "total_connections", "max_connections",
	"subscriptions", "routes", "leafnodes", "active_accounts", "active_servers",
	"in_msgs", "in_bytes", "out_msgs", "out_bytes",
	"slow_consumers", "slow_consumers_clients", "slow_consumers_routes", "slow_consumers_gateways", "slow_consumers_leafs",
	"stale_connections", "stalled_clients", "connz_total", "connz_returned",
	"js_enabled", "js_max_memory", "js_max_storage", "js_memory", "js_storage", "js_reserved_memory", "js_reserved_storage",
	"js_streams", "js_streams_leader", "js_consumers", "js_consumers_leader", "js_messages", "js_bytes", "js_ha_assets",
	"js_api_level", "js_api_total", "js_api_errors", "js_api_inflight",
	"meta_cluster", "meta_leader", "meta_cluster_size", "meta_quorum_needed", "meta_pending",
	"meta_peers_offline", "meta_peers_not_current",
	"max_payload", "max_pending", "write_deadline_ns", "ping_interval_ns", "max_pings_out",
	"tls_cert_not_after", "cluster_tls_cert_not_after", "gateway_tls_cert_not_after",
	"leafnode_tls_cert_not_after", "websocket_tls_cert_not_after", "mqtt_tls_cert_not_after",
}

// process holds the process and traffic numbers, from VARZ or else STATSZ.
type process struct {
	version, configDigest                               any
	configLoad                                          any
	mem, totalConns, inMsgs, inBytes, outMsgs, outBytes any
	slow, stale, stalled, subs                          any
	cpu                                                 any
	cores, maxprocs, conns, maxConns, routes, leafs     any
	slowClients, slowRoutes, slowGateways, slowLeafs    any
	activeAccounts, activeServers                       any
	// The connection limits; VARZ only.
	maxPayload, maxPending, writeDeadline, pingInterval, maxPingsOut any
	// The listener certificates' expiry, client, cluster, gateway, leaf
	// node, websocket and MQTT; VARZ only, NULL for a listener without TLS.
	certs [6]any
}

func processOf(v *collect.ServerView) process {
	var p process
	if st := v.Statsz; st != nil {
		p = process{
			version: v.Info.Version, mem: st.Mem, cpu: st.CPU, cores: st.Cores, maxprocs: st.MaxProcs,
			conns: st.Connections, totalConns: i64(st.TotalConnections), subs: int64(st.NumSubs),
			routes: len(st.Routes), inMsgs: st.Received.Msgs, inBytes: st.Received.Bytes,
			outMsgs: st.Sent.Msgs, outBytes: st.Sent.Bytes, slow: st.SlowConsumers,
			stale: st.StaleConnections, stalled: st.StalledClients,
			activeAccounts: st.ActiveAccounts, activeServers: st.ActiveServers,
		}
		p.slowClients, p.slowRoutes, p.slowGateways, p.slowLeafs = slowStats(st.SlowConsumersStats)
	}
	if vz := v.Varz; vz != nil {
		p.version, p.configDigest, p.configLoad = vz.Version, vz.ConfigDigest, ts(vz.ConfigLoadTime)
		p.mem, p.cpu, p.cores, p.maxprocs = vz.Mem, vz.CPU, vz.Cores, vz.MaxProcs
		p.conns, p.totalConns, p.maxConns = vz.Connections, i64(vz.TotalConnections), vz.MaxConn
		p.subs, p.routes, p.leafs = int64(vz.Subscriptions), vz.Routes, vz.Leafs
		p.inMsgs, p.inBytes, p.outMsgs, p.outBytes = vz.InMsgs, vz.InBytes, vz.OutMsgs, vz.OutBytes
		p.slow, p.stale, p.stalled = vz.SlowConsumers, vz.StaleConnections, vz.StalledClients
		p.slowClients, p.slowRoutes, p.slowGateways, p.slowLeafs = slowStats(vz.SlowConsumersStats)
		p.maxPayload, p.maxPending, p.maxPingsOut = int64(vz.MaxPayload), vz.MaxPending, vz.MaxPingsOut
		p.writeDeadline, p.pingInterval = vz.WriteDeadline.Nanoseconds(), vz.PingInterval.Nanoseconds()
		p.certs = [6]any{ts(vz.TLSCertNotAfter), ts(vz.Cluster.TLSCertNotAfter), ts(vz.Gateway.TLSCertNotAfter),
			ts(vz.LeafNode.TLSCertNotAfter), ts(vz.Websocket.TLSCertNotAfter), ts(vz.MQTT.TLSCertNotAfter)}
	}
	return p
}

func slowStats(s *server.SlowConsumersStats) (any, any, any, any) {
	if s == nil {
		return nil, nil, nil, nil
	}
	return i64(s.Clients), i64(s.Routes), i64(s.Gateways), i64(s.Leafs)
}

// jetstream holds the JetStream numbers, from JSZ or else VARZ.
type jetstream struct {
	enabled                                                 any
	maxMem, maxStore, mem, store, resMem, resStore          any
	streams, streamsLeader, consumers, consumersLeader      any
	msgs, bytes, ha, apiLevel, apiTotal, apiErrors, apiInfl any
	meta                                                    *server.MetaClusterInfo
}

func jetstreamOf(v *collect.ServerView) jetstream {
	if j := v.Jsz; j != nil {
		return jetstream{
			enabled: !j.Disabled, maxMem: j.Config.MaxMemory, maxStore: j.Config.MaxStore,
			mem: i64(j.Memory), store: i64(j.Store), resMem: i64(j.ReservedMemory), resStore: i64(j.ReservedStore),
			streams: j.Streams, streamsLeader: j.StreamsLeader, consumers: j.Consumers, consumersLeader: j.ConsumersLeader,
			msgs: i64(j.Messages), bytes: i64(j.Bytes), ha: j.HAAssets, apiLevel: j.API.Level,
			apiTotal: i64(j.API.Total), apiErrors: i64(j.API.Errors), apiInfl: i64(j.API.Inflight), meta: j.Meta,
		}
	}
	var out jetstream
	if v.Varz != nil && v.Varz.JetStream.Config != nil {
		js := v.Varz.JetStream
		out.enabled, out.maxMem, out.maxStore, out.meta = true, js.Config.MaxMemory, js.Config.MaxStore, js.Meta
		if st := js.Stats; st != nil {
			out.mem, out.store, out.resMem, out.resStore = i64(st.Memory), i64(st.Store), i64(st.ReservedMemory), i64(st.ReservedStore)
			out.ha, out.apiLevel, out.apiTotal, out.apiErrors = st.HAAssets, st.API.Level, i64(st.API.Total), i64(st.API.Errors)
		}
	}
	return out
}

func (w *writer) writeServerStats(v *collect.ServerView) {
	p := processOf(v)
	j := jetstreamOf(v)
	start := startTime(v)
	var uptime any
	if !start.IsZero() {
		uptime = int64(w.snap.StartedAt.Sub(start).Seconds())
	}
	var connzTotal, connzReturned any
	if v.Connz != nil {
		// A null entry is not counted: it has no connections row.
		returned := 0
		for _, c := range v.Connz.Conns {
			if c != nil {
				returned++
			}
		}
		connzTotal, connzReturned = v.Connz.Total, returned
	}
	var metaCluster, metaLeader, metaSize, metaQuorum, metaPending, metaOffline, metaNotCurrent any
	if m := j.meta; m != nil {
		metaCluster, metaLeader, metaSize, metaQuorum, metaPending = m.Name, m.Leader, m.Size, m.QuorumNeeded, m.Pending
		offline, notCurrent := 0, 0
		for _, r := range m.Replicas {
			if r == nil {
				continue
			}
			if r.Offline {
				offline++
			}
			if !r.Current {
				notCurrent++
			}
		}
		metaOffline, metaNotCurrent = offline, notCurrent
	}
	w.insert("server_stats", serverStatsCols,
		w.epoch, v.Info.ID, p.version, ts(start), uptime, p.configLoad, p.configDigest,
		p.mem, p.cpu, p.cores, p.maxprocs, p.conns, p.totalConns, p.maxConns,
		p.subs, p.routes, p.leafs, p.activeAccounts, p.activeServers,
		p.inMsgs, p.inBytes, p.outMsgs, p.outBytes,
		p.slow, p.slowClients, p.slowRoutes, p.slowGateways, p.slowLeafs,
		p.stale, p.stalled, connzTotal, connzReturned,
		j.enabled, j.maxMem, j.maxStore, j.mem, j.store, j.resMem, j.resStore,
		j.streams, j.streamsLeader, j.consumers, j.consumersLeader, j.msgs, j.bytes, j.ha,
		j.apiLevel, j.apiTotal, j.apiErrors, j.apiInfl,
		metaCluster, metaLeader, metaSize, metaQuorum, metaPending, metaOffline, metaNotCurrent,
		p.maxPayload, p.maxPending, p.writeDeadline, p.pingInterval, p.maxPingsOut,
		p.certs[0], p.certs[1], p.certs[2], p.certs[3], p.certs[4], p.certs[5],
	)
}

var routeCols = []string{
	"epoch", "server_id", "rid", "remote_id", "remote_name", "did_solicit", "is_configured", "ip", "port",
	"start_time", "last_activity", "rtt_ns", "pending_bytes", "in_msgs", "out_msgs", "in_bytes", "out_bytes",
	"subscriptions", "account", "compression",
}

func (w *writer) writeRoutes(v *collect.ServerView) {
	if v.Routez == nil {
		return
	}
	for _, r := range v.Routez.Routes {
		if r == nil {
			continue
		}
		w.insert("routes", routeCols, w.epoch, v.Info.ID, i64(r.Rid), r.RemoteID, r.RemoteName, r.DidSolicit,
			r.IsConfigured, r.IP, r.Port, ts(r.Start), ts(r.LastActivity), durNS(r.RTT), r.Pending,
			r.InMsgs, r.OutMsgs, r.InBytes, r.OutBytes, int64(r.NumSubs), r.Account, r.Compression)
	}
}

var gatewayCols = []string{
	"epoch", "server_id", "gateway", "direction", "is_configured", "cid", "ip", "port", "rtt_ns",
	"pending_bytes", "in_msgs", "out_msgs", "in_bytes", "out_bytes",
}

func (w *writer) writeGateway(v *collect.ServerView, name, dir string, g *server.RemoteGatewayz) {
	if g == nil {
		return
	}
	vals := []any{w.epoch, v.Info.ID, name, dir, g.IsConfigured}
	if c := g.Connection; c != nil {
		vals = append(vals, i64(c.Cid), c.IP, c.Port, durNS(c.RTT), c.Pending, c.InMsgs, c.OutMsgs, c.InBytes, c.OutBytes)
	} else {
		vals = append(vals, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	}
	w.insert("gateways", gatewayCols, vals...)
}

func (w *writer) writeGateways(v *collect.ServerView) {
	if v.Gatewayz == nil {
		return
	}
	for _, name := range sortedKeys(v.Gatewayz.OutboundGateways) {
		w.writeGateway(v, name, "outbound", v.Gatewayz.OutboundGateways[name])
	}
	for _, name := range sortedKeys(v.Gatewayz.InboundGateways) {
		for _, g := range v.Gatewayz.InboundGateways[name] {
			w.writeGateway(v, name, "inbound", g)
		}
	}
}

var ipqueueCols = []string{"epoch", "server_id", "queue", "pending", "in_progress"}

func (w *writer) writeIpqueues(v *collect.ServerView) {
	for _, name := range sortedKeys(v.Ipqueuesz) {
		q := v.Ipqueuesz[name]
		w.insert("ipqueues", ipqueueCols, w.epoch, v.Info.ID, name, q.Pending, q.InProgress)
	}
}

var healthCols = []string{"epoch", "server_id", "status", "status_code", "error", "error_count", "errors"}

func (w *writer) writeHealth(v *collect.ServerView) {
	h := v.Healthz
	if h == nil {
		return
	}
	var errs any
	if len(h.Errors) > 0 {
		errs = jsonText(h.Errors)
	}
	w.insert("health", healthCols, w.epoch, v.Info.ID, h.Status, h.StatusCode, h.Error, len(h.Errors), errs)
}

func sortedKeys[K ~string, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
