package checks

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestNoMetaLeader(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	for i := range 3 {
		id := "S" + string(rune('0'+i))
		f.stats(1, id, m{"meta_cluster_size": 3, "meta_leader": "nats-1"})
		f.stats(2, id, m{"meta_cluster_size": 3, "meta_leader": ""})
	}
	f.fires("system-002", 1)
	r := f.fires("system-002", 2, "")
	want(t, r[""], "critical", "None of the 3 clustered JetStream servers")
}

func TestMetaLeaderChanges(t *testing.T) {
	f := newFx(t)
	f.cluster(20)
	leaders := map[int64]string{}
	for e := int64(1); e <= 20; e++ {
		// Snapshots see nats-0, then nats-1 from epoch 8, nats-0 from 12,
		// nats-2 from 16: three changes.
		l := "nats-0"
		switch {
		case e >= 16:
			l = "nats-2"
		case e >= 12:
			l = "nats-0"
		case e >= 8:
			l = "nats-1"
		}
		leaders[e] = l
		for i := range 3 {
			f.stats(e, "S"+string(rune('0'+i)), m{"meta_cluster_size": 3, "meta_leader": l})
		}
	}
	two := map[string]string{"max_changes": "2"}
	r := f.eval("system-003", 20, two)
	want(t, r[""], "warning", "changed 3 times", "nats-0 to nats-1 to nats-0 to nats-2 (snapshots)")
	if r := f.eval("system-003", 14, two); len(r) != 0 {
		t.Fatalf("epoch 14: %s", rowsText(r))
	}
	if r := f.eval("system-003", 20); len(r) != 0 {
		t.Fatalf("three changes with the default of 4: %s", rowsText(r))
	}
	// Advisories count elections the snapshots missed.
	for i, s := range []string{"nats-1", "nats-2", "nats-1", "nats-0", "nats-2"} {
		f.event(int64(i+1), at(3).Add(time.Duration(i)*time.Second), "meta.leader_elected", m{"server_name": s, "source": "sys"})
	}
	r = f.fires("system-003", 14, "")
	want(t, r[""], "warning", "changed 5 times", "nats-1 to nats-2 to nats-1 to nats-0 to nats-2 (advisories)")
}

// A rolling restart moves the meta leader away from each server it
// restarts: a change whose previous leader restarted or announced lame
// duck within the window is not counted.
func TestMetaLeaderRollIsNotFlapping(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 40, three...)
	leaders := []string{"nats-0", "nats-1", "nats-2", "nats-0", "nats-1", "nats-2"}
	for i, name := range three {
		f.server("A"+name, name, 1, 1, t0.Add(-time.Hour), "2.15.0")
		f.server("B"+name, name, 2, 40, at(int64(5+6*i)), "2.15.0")
	}
	for e := int64(1); e <= 40; e++ {
		l := leaders[min((e-1)/6, 5)]
		f.stats(e, "Bnats-0", m{"meta_leader": l, "meta_cluster_size": 3})
	}
	// Each hand-over at epoch 7, 13, ... follows the previous leader's
	// lame duck within one epoch.
	for i, name := range []string{"nats-0", "nats-1", "nats-2", "nats-0", "nats-1", "nats-2"} {
		f.event(int64(i+1), at(int64(6+6*i)), "server.lameduck", m{"source": "sys", "server_name": name})
	}
	if r := f.eval("system-003", 40, map[string]string{"max_changes": "0"}); len(r) != 0 {
		t.Fatalf("rolling restart counted as leader changes: %s", rowsText(r))
	}
}

// r3 stores APP/<stream> as R3 on S0..S2 in epoch e, led by the
// server in leader ("" for no leader), with the leader's view of its
// peers.
func (f *fx) r3(e int64, stream, leader string, offline ...string) {
	for i, name := range three {
		id := "S" + string(rune('0'+i))
		if contains(offline, name) {
			continue
		}
		reported, off := 2, 0
		for _, o := range offline {
			if o != name {
				off++
			}
		}
		f.streamStats(e, id, stream, m{"leader": leader, "is_leader": leader == name, "replicas_reported": reported,
			"replicas_offline": off, "messages": 10, "bytes": 1000})
		if leader == name {
			for _, peer := range three {
				if peer != name {
					f.streamReplica(e, id, stream, peer, m{"offline": contains(offline, peer), "current": !contains(offline, peer)})
				}
			}
		}
	}
}

func TestStreamQuorumAndReplicas(t *testing.T) {
	f := newFx(t)
	f.cluster(4)
	for i := range 3 {
		f.streamConfig("S"+string(rune('0'+i)), "jobs", 1, 4, nil)
		f.streamConfig("S"+string(rune('0'+i)), "r1", 1, 4, m{"replicas": 1})
	}
	f.r3(1, "jobs", "nats-0")
	f.r3(2, "jobs", "nats-0", "nats-2")
	f.r3(3, "jobs", "", "nats-1", "nats-2")
	f.r3(4, "jobs", "", "nats-1", "nats-2")
	// An R1 stream without a leader field is not about quorum.
	f.streamStats(3, "S0", "r1", m{"leader": "", "is_leader": false})
	f.streamStats(4, "S0", "r1", m{"leader": "", "is_leader": false})
	f.fires("stream-001", 1)
	f.fires("stream-001", 2)
	r := f.fires("stream-001", 4, "APP/jobs")
	want(t, r["APP/jobs"], "critical", "No leader that the reporting servers agree on (nats-0: none), for 2 collections")
	f.fires("stream-002", 1)
	r = f.fires("stream-002", 2, "APP/jobs")
	want(t, r["APP/jobs"], "warning", "Leader nats-0 reports 1 replica offline: nats-2")
	// A leader that still thinks it leads with both peers offline.
	g := newFx(t)
	g.cluster(2)
	g.streamConfig("S0", "jobs", 1, 2, nil)
	for e := int64(1); e <= 2; e++ {
		g.streamStats(e, "S0", "jobs", m{"leader": "nats-0", "is_leader": true, "replicas_reported": 2, "replicas_offline": 2})
	}
	g.fires("stream-001", 1)
	r = g.fires("stream-001", 2, "APP/jobs")
	want(t, r["APP/jobs"], "critical", "sees 1 of 3 replicas online; quorum needs 2")
}

func TestStreamReplicaLagging(t *testing.T) {
	f := newFx(t)
	f.cluster(1)
	f.streamStats(1, "S0", "jobs", m{"leader": "nats-0", "is_leader": true})
	f.streamReplica(1, "S0", "jobs", "nats-1", m{"lag": 5000})
	f.streamReplica(1, "S0", "jobs", "nats-2", m{"lag": 10})
	// A follower's view of the leader does not count.
	f.streamReplica(1, "S1", "jobs", "nats-2", m{"lag": 9000})
	r := f.fires("stream-003", 1, "APP/jobs")
	want(t, r["APP/jobs"], "warning", "nats-1 5000 operations behind")
	if r := f.eval("stream-003", 1, map[string]string{"max_lag": "10000"}); len(r) != 0 {
		t.Fatalf("above max_lag: %s", rowsText(r))
	}
}

func TestStreamNearLimit(t *testing.T) {
	f := newFx(t)
	f.cluster(1)
	for _, s := range []struct {
		name, discard string
		msgs, max     int64
	}{{"new-warn", "new", 85, 100}, {"new-crit", "new", 99, 100}, {"old", "old", 99, 100}, {"fine", "new", 10, 100}, {"nolimit", "new", 99, -1}} {
		f.streamConfig("S0", s.name, 1, 1, m{"discard": s.discard, "max_msgs": s.max, "max_age_ns": int64(7 * 24 * 3600e9)})
		f.streamStats(1, "S0", s.name, m{"leader": "nats-0", "is_leader": true, "messages": s.msgs, "bytes": 1})
	}
	r := f.fires("stream-004", 1, "APP/new-warn", "APP/new-crit", "APP/old")
	want(t, r["APP/new-warn"], "warning", "85 of 100 messages (85.0%)", "discard new")
	want(t, r["APP/new-crit"], "critical")
	want(t, r["APP/old"], "info", "discard old")
	if r["APP/old"].RelatedConfig["max_age"] != "7d" {
		t.Errorf("related = %v", r["APP/old"].RelatedConfig)
	}
}

func TestStreamWithoutLimits(t *testing.T) {
	f := newFx(t)
	f.cluster(1)
	f.streamConfig("S0", "archive", 1, 1, nil)
	f.streamConfig("S1", "archive", 1, 1, nil)
	f.streamConfig("S0", "jobs", 1, 1, m{"max_age_ns": int64(7 * 24 * 3600e9)})
	// A configuration run that ended before the epoch is not current.
	f.streamConfig("S0", "gone", 0, 0, nil)
	f.streamStats(1, "S1", "archive", m{"is_leader": true, "leader": "nats-1"})
	r := f.fires("stream-005", 1, "APP/archive")
	want(t, r["APP/archive"], "info", "max_msgs -1, max_bytes -1, max_age 0")
	if r["APP/archive"].RelatedSource != "nats-1" {
		t.Errorf("related source = %q, want the leader", r["APP/archive"].RelatedSource)
	}
}

func TestStreamFullSoon(t *testing.T) {
	f := newFx(t)
	for e := int64(1); e <= 7; e++ {
		f.epochAt(e, t0.Add(time.Duration(e-1)*time.Hour), three)
	}
	f.server("S0", "nats-0", 1, 7, t0, "2.15.0")
	for _, s := range []string{"grow-new", "grow-old", "flat"} {
		discard := "new"
		if s == "grow-old" {
			discard = "old"
		}
		f.streamConfig("S0", s, 1, 7, m{"max_bytes": int64(10) << 30, "discard": discard})
		for e := int64(1); e <= 7; e++ {
			b := int64(1) << 30
			if s != "flat" {
				b = (2 + e) << 30
			}
			f.streamStats(e, "S0", s, m{"is_leader": true, "leader": "nats-0", "bytes": b})
		}
	}
	// grow-old is a limits stream with discard old: reaching max_bytes only
	// drops its oldest messages.
	r := f.fires("stream-006", 7, "APP/grow-new")
	want(t, r["APP/grow-new"], "critical", "Grows 24.0 GiB per day", "in 1.0 hours")
}

func TestStreamAgreement(t *testing.T) {
	f := newFx(t)
	f.cluster(1)
	created := t0.Add(-48 * time.Hour)
	recreated := t0.Add(-time.Hour)
	f.streamConfig("S0", "jobs", 1, 1, m{"created": created, "max_age_ns": int64(7 * 24 * 3600e9)})
	f.streamConfig("S1", "jobs", 1, 1, m{"created": created, "max_age_ns": int64(7 * 24 * 3600e9)})
	f.streamConfig("S2", "jobs", 1, 1, m{"created": recreated, "max_age_ns": int64(24 * 3600e9), "replicas": 1})
	f.streamConfig("S0", "same", 1, 1, m{"created": created})
	f.streamConfig("S1", "same", 1, 1, m{"created": created})
	// An older run of S2 with another configuration is not current.
	f.streamConfig("S2", "same", 0, 0, m{"created": recreated, "replicas": 5})
	r := f.fires("stream-007", 1, "APP/jobs")
	want(t, r["APP/jobs"], "warning", "2 configurations (nats-0, nats-1 | nats-2)", "differing: replicas, max_age")
	r = f.fires("stream-008", 1, "APP/jobs")
	want(t, r["APP/jobs"], "info", "nats-0, nats-1 2026-10-15T09:00:00Z; nats-2 2026-10-17T08:00:00Z")
}

func TestStreamPeerSetAndMissingReplica(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	for e := int64(1); e <= 2; e++ {
		// nats-0 leads and assigns nats-1 and nats-2; nats-1 agrees; nats-2
		// answered JSZ but does not hold the stream.
		f.streamStats(e, "S0", "jobs", m{"leader": "nats-0", "is_leader": true})
		f.streamReplica(e, "S0", "jobs", "nats-1", nil)
		f.streamReplica(e, "S0", "jobs", "nats-2", nil)
		f.streamStats(e, "S1", "jobs", m{"leader": "nats-0"})
		f.streamReplica(e, "S1", "jobs", "nats-0", nil)
		f.streamReplica(e, "S1", "jobs", "nats-2", nil)
		// For "moved", nats-1 still lists nats-3 instead of nats-2.
		f.streamStats(e, "S0", "moved", m{"leader": "nats-0", "is_leader": true})
		f.streamReplica(e, "S0", "moved", "nats-1", nil)
		f.streamReplica(e, "S0", "moved", "nats-2", nil)
		f.streamStats(e, "S1", "moved", m{"leader": "nats-0"})
		f.streamReplica(e, "S1", "moved", "nats-0", nil)
		f.streamReplica(e, "S1", "moved", "nats-3", nil)
		f.streamStats(e, "S2", "moved", m{"leader": "nats-0"})
		f.streamReplica(e, "S2", "moved", "nats-0", nil)
		f.streamReplica(e, "S2", "moved", "nats-1", nil)
	}
	f.fires("stream-009", 1)
	r := f.fires("stream-009", 2, "APP/moved")
	want(t, r["APP/moved"], "warning", "nats-0, nats-2 see [nats-0,nats-1,nats-2]", "nats-1 see [nats-0,nats-1,nats-3]")
	r = f.fires("stream-010", 1, "APP/jobs")
	want(t, r["APP/jobs"], "warning", "Leader nats-0 assigns the stream to nats-2, which answered JSZ without it")
	// A server that did not answer JSZ is not counted as missing the stream.
	g := newFx(t)
	g.epoch(1, three)
	g.server("S0", "nats-0", 1, 1, t0, "2.15.0")
	g.server("S2", "nats-2", 1, 1, t0, "2.15.0")
	g.row("epoch_endpoints", m{"epoch": 1, "server_id": "S0", "endpoint": "JSZ", "state": "ok"})
	g.row("epoch_endpoints", m{"epoch": 1, "server_id": "S2", "endpoint": "JSZ", "state": "missing"})
	g.streamStats(1, "S0", "jobs", m{"leader": "nats-0", "is_leader": true})
	g.streamReplica(1, "S0", "jobs", "nats-2", nil)
	g.fires("stream-010", 1)
}

// inventory stores an expected asset.
func (f *fx) inventory(kind, name, stream, consumer string, cols m) {
	row := merge(m{"account": "APP", "kind": kind, "name": name, "stream": stream, "source": "inv.yaml", "loaded_at": t0}, cols)
	if consumer != "" {
		row["consumer"] = consumer
	}
	f.row("inventory", row)
}

func TestExpectedStreamMissing(t *testing.T) {
	f := newFx(t)
	f.cluster(3)
	f.row("accounts", m{"account": "APP", "account_name": "APP", "is_system": false, "first_epoch": 1, "last_epoch": 3})
	f.inventory("stream", "order-jobs", "order-jobs", "", m{"replicas": 3})
	f.inventory("kv", "customer-info", "KV_customer-info", "", nil)
	f.inventory("object_store", "image-input", "OBJ_image-input", "", nil)
	f.row("streams", m{"account": "APP", "stream": "KV_customer-info", "first_epoch": 1, "last_epoch": 2})
	for e := int64(1); e <= 3; e++ {
		f.streamStats(e, "S0", "order-jobs", m{"is_leader": true})
		if e <= 2 {
			f.streamStats(e, "S0", "KV_customer-info", m{"is_leader": true})
		}
	}
	// The delete event is the bucket's; an older delete of the same name
	// before it was last seen is not the cause.
	f.event(7, at(1).Add(-time.Hour), "stream.deleted", m{"account": "APP", "stream": "KV_customer-info", "client_name": "old"})
	f.event(9, at(2).Add(10*time.Second), "stream.deleted", m{"account": "APP", "stream": "KV_customer-info",
		"client_name": "account-admin", "client_host": "10.0.0.9"})
	f.fires("stream-011", 2, "APP/OBJ_image-input")
	r := f.fires("stream-011", 3, "APP/KV_customer-info", "APP/OBJ_image-input")
	kv := r["APP/KV_customer-info"]
	want(t, kv, "critical", "KV bucket customer-info (stream KV_customer-info) is in the inventory and no server reports it",
		"Deleted 2026-10-17 09:00:40 UTC by account-admin (10.0.0.9)")
	if kv.CauseEventID != 9 {
		t.Errorf("cause = %d, want 9", kv.CauseEventID)
	}
	want(t, r["APP/OBJ_image-input"], "critical", "Object store image-input", "not reported within the retention window")
	// When a server that held the bucket did not answer, it may still
	// hold it; a server that never held it does not count.
	f.epoch(4, three, "nats-0")
	f.streamStats(4, "S1", "order-jobs", m{"is_leader": true})
	r = f.fires("stream-011", 4, "APP/KV_customer-info", "APP/OBJ_image-input")
	want(t, r["APP/KV_customer-info"], "critical")
	f.exec(`DELETE FROM events WHERE id = 9`)
	f.epoch(5, three, "nats-0")
	r = f.fires("stream-011", 5, "APP/KV_customer-info", "APP/OBJ_image-input", "APP/order-jobs")
	// order-jobs was last reported by nats-1, which answers: gone.
	want(t, r["APP/order-jobs"], "critical")
	want(t, r["APP/KV_customer-info"], "warning", "may be on nats-0, which did not answer")
	want(t, r["APP/OBJ_image-input"], "warning", "may be on nats-0, which did not answer")
}

// A holder that is restarting, or announced its shutdown, is a planned
// gap: info. One answering with JetStream disabled is a warning.
func TestExpectedStreamOnServerNotReady(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 3, three...)
	f.restarted(3, 3)
	f.server("S1", "nats-1", 1, 3, t0.Add(-time.Hour), "2.15.0")
	f.inventory("stream", "a", "a", "", nil)
	f.inventory("stream", "b", "b", "", nil)
	f.streamStats(2, "OLD0", "a", m{"is_leader": true})
	f.streamStats(2, "S1", "b", m{"is_leader": true})
	f.stats(3, "S1", m{"js_enabled": false})
	r := f.fires("stream-011", 3, "APP/a", "APP/b")
	want(t, r["APP/a"], "info", "may be on nats-0, which is restarting")
	want(t, r["APP/b"], "warning", "may be on nats-1, which has JetStream disabled")
}

func TestStreamDiffersFromInventory(t *testing.T) {
	f := newFx(t)
	f.cluster(1)
	f.inventory("stream", "order-jobs", "order-jobs", "", m{"replicas": 3, "max_age_ns": int64(7 * 24 * 3600e9),
		"max_msgs": 1000000, "discard": "old"})
	f.inventory("kv", "customer-info", "KV_customer-info", "", m{"max_msgs_per_subject": 1, "storage": "file"})
	f.streamConfig("S0", "order-jobs", 1, 1, m{"max_age_ns": int64(24 * 3600e9), "max_msgs": 1000000, "replicas": 1})
	f.streamStats(1, "S0", "order-jobs", m{"is_leader": true})
	f.streamConfig("S0", "KV_customer-info", 1, 1, m{"max_msgs_per_subject": 1})
	r := f.fires("stream-012", 1, "APP/order-jobs")
	want(t, r["APP/order-jobs"], "warning", "replicas 1 (inventory 3)", "max_age 1d (inventory 7d)")
	if r["APP/order-jobs"].RelatedSource != "inventory" || r["APP/order-jobs"].RelatedConfig["max_age"] != "7d" {
		t.Errorf("related = %v from %q", r["APP/order-jobs"].RelatedConfig, r["APP/order-jobs"].RelatedSource)
	}
}

func TestStreamConfigChanged(t *testing.T) {
	f := newFx(t)
	f.cluster(200)
	f.event(1, at(150), "stream.updated", m{"account": "APP", "stream": "jobs", "client_name": "nack"})
	f.event(2, at(180), "stream.updated", m{"account": "APP", "stream": "jobs", "client_name": "alice-laptop",
		"client_host": "10.1.2.3"})
	f.event(3, at(10), "stream.updated", m{"account": "APP", "stream": "old", "client_name": "nack"})
	r := f.fires("stream-013", 200, "APP/jobs")
	want(t, r["APP/jobs"], "info", "Updated 2 times in the last 1h, the last at 10:29:30 UTC by alice-laptop (10.1.2.3)")
	if r["APP/jobs"].CauseEventID != 2 {
		t.Errorf("cause = %d", r["APP/jobs"].CauseEventID)
	}
}

// Checking an older epoch uses what was known then: the stream was last
// seen before the epoch, even though it is back in a later one, and the
// delete between is the cause.
func TestExpectedStreamMissingAtAnOlderEpoch(t *testing.T) {
	f := newFx(t)
	f.cluster(3)
	f.inventory("stream", "jobs", "jobs", "", nil)
	f.row("streams", m{"account": "APP", "stream": "jobs", "first_epoch": 1, "last_epoch": 3})
	f.streamStats(1, "S0", "jobs", m{"is_leader": true})
	f.streamStats(3, "S0", "jobs", m{"is_leader": true})
	f.event(4, at(1).Add(10*time.Second), "stream.deleted", m{"account": "APP", "stream": "jobs", "client_name": "cleanup"})
	r := f.fires("stream-011", 2, "APP/jobs")
	want(t, r["APP/jobs"], "critical", "Deleted 2026-10-17 09:00:10 UTC by cleanup")
}

// A restarted server that thinks it leads alone is not believed: the
// others agree on another leader that sees its peers.
func TestStreamQuorumIgnoresRestartedServer(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 3, three...)
	f.restarted(2, 3)
	f.server("S1", "nats-1", 1, 3, t0.Add(-time.Hour), "2.15.0")
	f.server("S2", "nats-2", 1, 3, t0.Add(-time.Hour), "2.15.0")
	for _, id := range []string{"S0", "S1", "S2"} {
		f.streamConfig(id, "jobs", 1, 3, nil)
	}
	for e := int64(2); e <= 3; e++ {
		f.streamStats(e, "S0", "jobs", m{"leader": "nats-0", "is_leader": true, "replicas_reported": 2, "replicas_offline": 2})
		f.streamStats(e, "S1", "jobs", m{"leader": "nats-2"})
		f.streamStats(e, "S2", "jobs", m{"leader": "nats-2", "is_leader": true, "replicas_reported": 2, "replicas_offline": 0})
	}
	f.fires("stream-001", 3)
}

// Quorum lost in one epoch only is an election, not a finding.
func TestStreamQuorumNeedsTwoEpochs(t *testing.T) {
	f := newFx(t)
	f.cluster(3)
	for i := range 3 {
		f.streamConfig("S"+string(rune('0'+i)), "jobs", 1, 3, nil)
	}
	f.r3(1, "jobs", "nats-0")
	f.r3(2, "jobs", "", "nats-1", "nats-2")
	f.r3(3, "jobs", "", "nats-1", "nats-2")
	f.fires("stream-001", 2)
	r := f.fires("stream-001", 3, "APP/jobs")
	want(t, r["APP/jobs"], "critical", "No leader", "for 2 collections")
}

// A restarted server that sees only itself is not a disagreement.
func TestStreamPeerSetIgnoresRestartedServer(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 3, three...)
	f.restarted(2, 3)
	f.server("S1", "nats-1", 1, 3, t0.Add(-time.Hour), "2.15.0")
	f.server("S2", "nats-2", 1, 3, t0.Add(-time.Hour), "2.15.0")
	for e := int64(2); e <= 3; e++ {
		f.streamStats(e, "S0", "jobs", m{"leader": ""})
		for _, v := range []struct{ id, self, a, b string }{{"S1", "nats-1", "nats-0", "nats-2"}, {"S2", "nats-2", "nats-0", "nats-1"}} {
			f.streamStats(e, v.id, "jobs", m{"leader": "nats-1", "is_leader": v.id == "S1"})
			f.streamReplica(e, v.id, "jobs", v.a, nil)
			f.streamReplica(e, v.id, "jobs", v.b, nil)
		}
	}
	f.fires("stream-009", 3)
}

// Settings are compared one by one: a value on one server and none on
// another is a difference, which joining the values into one text missed.
func TestStreamConfigNullDiffers(t *testing.T) {
	f := newFx(t)
	f.cluster(1)
	f.streamConfig("S0", "jobs", 1, 1, m{"max_consumers": nil, "max_msg_size": int64(10)})
	f.streamConfig("S1", "jobs", 1, 1, m{"max_consumers": int64(10), "max_msg_size": nil})
	r := f.fires("stream-007", 1, "APP/jobs")
	want(t, r["APP/jobs"], "warning", "differing: max_consumers, max_msg_size")
}

// A restarting server's configuration, creation time and missing assets
// are not a disagreement: it reports them as it catches up.
func TestAgreementIgnoresRestartingServer(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 3, three...)
	f.restarted(3, 3)
	f.server("S1", "nats-1", 1, 3, t0.Add(-time.Hour), "2.15.0")
	for _, id := range []string{"S0", "S1"} {
		f.row("epoch_endpoints", m{"epoch": 3, "server_id": id, "endpoint": "JSZ", "state": "ok"})
	}
	f.streamConfig("S1", "jobs", 1, 3, m{"created": t0.Add(-48 * time.Hour)})
	f.streamConfig("S0", "jobs", 3, 3, m{"created": t0.Add(-time.Hour), "replicas": 1})
	f.consumerConfig("S1", "jobs", "w", 1, 3, m{"created": t0.Add(-48 * time.Hour)})
	f.consumerConfig("S0", "jobs", "w", 3, 3, m{"created": t0.Add(-time.Hour), "max_ack_pending": 1})
	f.streamStats(3, "S1", "jobs", m{"leader": "nats-1", "is_leader": true})
	f.streamReplica(3, "S1", "jobs", "nats-0", nil)
	f.consumerStats(3, "S1", "jobs", "w", m{"leader": "nats-1", "is_leader": true})
	f.consumerReplica(3, "S1", "jobs", "w", "nats-0", nil)
	for _, code := range []string{"stream-007", "stream-008", "stream-010", "consumer-008", "consumer-009", "consumer-012"} {
		f.fires(code, 3)
	}
}

// A work queue or interest stream that drops its oldest messages at the
// limit loses work nobody processed: warning, critical at crit_ratio. A
// limits stream with discard old is a ring buffer: info, and no forecast.
func TestStreamLimitByRetention(t *testing.T) {
	f := newFx(t)
	f.cluster(1)
	for _, s := range []struct {
		name, retention string
		msgs            int64
	}{{"wq-near", "workqueue", 85}, {"wq-full", "workqueue", 100}, {"interest", "interest", 90}, {"ring", "limits", 100}} {
		f.streamConfig("S0", s.name, 1, 1, m{"retention": s.retention, "discard": "old", "max_msgs": 100})
		f.streamStats(1, "S0", s.name, m{"is_leader": true, "messages": s.msgs, "bytes": 1})
	}
	r := f.fires("stream-004", 1, "APP/wq-near", "APP/wq-full", "APP/interest", "APP/ring")
	want(t, r["APP/wq-near"], "warning", "retention workqueue")
	want(t, r["APP/wq-full"], "critical")
	want(t, r["APP/interest"], "warning")
	want(t, r["APP/ring"], "info")
	g := newFx(t)
	for e := int64(1); e <= 7; e++ {
		g.epochAt(e, t0.Add(time.Duration(e-1)*time.Hour), three)
	}
	g.server("S0", "nats-0", 1, 7, t0, "2.15.0")
	for _, s := range []string{"wq", "ring"} {
		ret := "workqueue"
		if s == "ring" {
			ret = "limits"
		}
		g.streamConfig("S0", s, 1, 7, m{"max_bytes": int64(10) << 30, "discard": "old", "retention": ret})
		for e := int64(1); e <= 7; e++ {
			g.streamStats(e, "S0", s, m{"is_leader": true, "leader": "nats-0", "bytes": (2 + e) << 30})
		}
	}
	r = g.fires("stream-006", 7, "APP/wq")
	want(t, r["APP/wq"], "critical")
}

// The same for a stream, and only the leader's rows count: a follower
// that copies a burst late does not make the stream grow.
func TestStreamFullSoonIgnoresStepAndFollowers(t *testing.T) {
	f := newFx(t)
	for e := int64(1); e <= 13; e++ {
		f.epochAt(e, t0.Add(time.Duration(e-1)*30*time.Minute), three)
	}
	f.server("S0", "nats-0", 1, 13, t0, "2.15.0")
	f.server("S1", "nats-1", 1, 13, t0, "2.15.0")
	for _, s := range []string{"step", "follower"} {
		f.streamConfig("S0", s, 1, 13, m{"max_bytes": int64(10) << 30, "discard": "new"})
	}
	for e := int64(1); e <= 13; e++ {
		b := int64(1) << 30
		if e > 11 {
			b = 8 << 30
		}
		f.streamStats(e, "S0", "step", m{"is_leader": true, "leader": "nats-0", "bytes": b})
		f.streamStats(e, "S0", "follower", m{"is_leader": true, "leader": "nats-0", "bytes": int64(1) << 30})
		f.streamStats(e, "S1", "follower", m{"is_leader": false, "leader": "nats-0", "bytes": e << 30})
	}
	f.fires("stream-006", 13)
}

// A stream seen within the lookback that no server reports any more has
// disappeared, unless the inventory lists it; while a server that held it
// is not ready, it is a warning.
func TestStreamDisappeared(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	for _, s := range []string{"gone", "KV_bucket", "listed", "on-missing"} {
		for e := int64(1); e <= 2; e++ {
			f.streamStats(e, "S0", s, m{"is_leader": true})
		}
	}
	f.streamStats(1, "S2", "on-missing", m{})
	f.exec(`DELETE FROM stream_stats WHERE stream = 'on-missing' AND server_id = 'S0'`)
	f.inventory("stream", "listed", "listed", "", nil)
	f.event(3, at(2).Add(time.Second), "stream.deleted", m{"account": "APP", "stream": "KV_bucket", "client_name": "alice"})
	f.epoch(3, three, "nats-2")
	for _, id := range []string{"S0", "S1"} {
		f.row("epoch_endpoints", m{"epoch": 3, "server_id": id, "endpoint": "JSZ", "state": "ok"})
	}
	r := f.fires("stream-014", 3, "APP/gone", "APP/KV_bucket", "APP/on-missing")
	want(t, r["APP/on-missing"], "warning", "may be on nats-2, which did not answer")
	want(t, r["APP/KV_bucket"], "critical", "KV bucket bucket (stream KV_bucket) was last reported by nats-0 at", "Deleted 2026-10-17 09:00:31 UTC by alice")
	want(t, r["APP/gone"], "critical", "No delete event recorded")
}

// The servers agree on a leader that does not report, and only one of
// three replicas reports at all: no leader view, but the quorum is gone.
func TestStreamQuorumLeaderDoesNotReport(t *testing.T) {
	f := newFx(t)
	f.cluster(1)
	f.epoch(2, three, "nats-0", "nats-1")
	f.epoch(3, three, "nats-0", "nats-1")
	f.s3cfg(1, 3)
	for e := int64(2); e <= 3; e++ {
		f.streamStats(e, "S2", "jobs", m{"leader": "nats-0", "is_leader": false})
	}
	r := f.fires("stream-001", 3, "APP/jobs")
	want(t, r["APP/jobs"], "critical", "Leader nats-0 does not report; only 1 of 3 replicas report")
	if v := r["APP/jobs"].Value; v != nil {
		t.Errorf("value = %v, want none: the leader does not report its online replicas", *v)
	}
}

// A restarting server's peer set counts when the others cannot form a
// majority of the replicas without it.
func TestStreamPeerSetRestartedServerCountsWithoutMajority(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 3, three...)
	f.restarted(2, 3)
	f.server("S2", "nats-2", 1, 3, t0.Add(-time.Hour), "2.15.0")
	f.s3cfg(1, 3)
	for e := int64(2); e <= 3; e++ {
		f.streamStats(e, "S0", "jobs", m{"leader": "nats-2"})
		f.streamReplica(e, "S0", "jobs", "nats-3", nil)
		f.streamStats(e, "S2", "jobs", m{"leader": "nats-2", "is_leader": true})
		f.streamReplica(e, "S2", "jobs", "nats-0", nil)
		f.streamReplica(e, "S2", "jobs", "nats-1", nil)
	}
	f.fires("stream-009", 3, "APP/jobs")
}

// A stream deleted by a client while the server that held it is in lame
// duck is critical with the client named: the delete event decides.
func TestStreamDeletedWhileHolderLameDuck(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	for e := int64(1); e <= 2; e++ {
		f.streamStats(e, "S1", "jobs", m{"is_leader": true})
	}
	f.event(1, at(2).Add(5*time.Second), "server.lameduck", m{"source": "sys", "server_name": "nats-1"})
	f.event(2, at(2).Add(8*time.Second), "stream.deleted", m{"account": "APP", "stream": "jobs", "client_name": "w"})
	f.epoch(3, three, "nats-1")
	r := f.fires("stream-014", 3, "APP/jobs")
	want(t, r["APP/jobs"], "critical", "Deleted 2026-10-17 09:00:38 UTC by w")
	g := newFx(t)
	g.cluster(2)
	for e := int64(1); e <= 2; e++ {
		g.streamStats(e, "S1", "jobs", m{"is_leader": true})
	}
	g.epoch(3, three, "nats-1")
	r = g.fires("stream-014", 3, "APP/jobs")
	want(t, r["APP/jobs"], "warning", "may be on nats-1, which did not answer", "No delete event recorded")
}

// One lame duck excuses one hand-over, right after it: a server that
// announced lame duck once does not excuse the leader leaving it again
// and again.
func TestMetaLeaderOneExcusePerStepDown(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	f.event(1, at(11).Add(5*time.Second), "server.lameduck", m{"source": "sys", "server_name": "nats-0"})
	f.event(2, at(11).Add(6*time.Second), "server.lameduck", m{"source": "sys", "server_name": "nats-1"})
	for e := int64(1); e <= 40; e++ {
		l := "nats-0"
		if e >= 12 {
			l = []string{"nats-1", "nats-0"}[(e/2)%2]
		}
		for i := range 3 {
			f.stats(e, "S"+string(rune('0'+i)), m{"meta_cluster_size": 3, "meta_leader": l})
		}
	}
	r := f.fires("system-003", 40, "")
	want(t, r[""], "warning")
}

// A recovering server (JetStream not ready, no leader) is not one of "the
// others" that let a restarting server's report be left out: with only one
// healthy reporter, the restarted server's view of the leader counts.
func TestStreamQuorumRecoveringIsNotHealthy(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 3, three...)
	f.restarted(2, 3)
	f.server("S1", "nats-1", 1, 3, t0.Add(-time.Hour), "2.15.0")
	f.server("S2", "nats-2", 1, 3, t0.Add(-time.Hour), "2.15.0")
	f.s3cfg(1, 3)
	for e := int64(2); e <= 3; e++ {
		f.streamStats(e, "S0", "jobs", m{"leader": "nats-2"})
		f.streamStats(e, "S1", "jobs", m{"leader": ""})
		f.row("health", m{"epoch": e, "server_id": "S1", "status": "unavailable", "error_count": 1,
			"errors": `[{"type":"JETSTREAM","error":"JetStream is not current with the meta leader"}]`})
		f.streamStats(e, "S2", "jobs", m{"leader": "nats-2", "is_leader": true, "replicas_reported": 2, "replicas_offline": 0})
	}
	f.fires("stream-001", 3)
}

// A restarted leader that sees fewer than a quorum online is kept as
// evidence even when the others, still naming it, could form a majority.
func TestStreamQuorumRestartedLeaderEvidence(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 3, three...)
	f.restarted(2, 3)
	f.server("S1", "nats-1", 1, 3, t0.Add(-time.Hour), "2.15.0")
	f.server("S2", "nats-2", 1, 3, t0.Add(-time.Hour), "2.15.0")
	f.s3cfg(1, 3)
	for e := int64(2); e <= 3; e++ {
		f.streamStats(e, "S0", "jobs", m{"leader": "nats-0", "is_leader": true, "replicas_reported": 2, "replicas_offline": 2})
		f.streamStats(e, "S1", "jobs", m{"leader": "nats-0"})
		f.streamStats(e, "S2", "jobs", m{"leader": "nats-0"})
	}
	r := f.fires("stream-001", 3, "APP/jobs")
	want(t, r["APP/jobs"], "critical", "Leader nats-0 sees 1 of 3 replicas online")
}

// A recovering server that so far lists only part of the peers is not a
// disagreement: its view is a strict subset of the majority's. Other
// differences of a recovering server still count when the healthy
// servers have no majority.
func TestStreamPeerSetRecoveringSubset(t *testing.T) {
	f := newFx(t)
	f.cluster(3)
	f.s3cfg(1, 3)
	for e := int64(2); e <= 3; e++ {
		f.row("health", m{"epoch": e, "server_id": "S1", "status": "unavailable", "error_count": 1,
			"errors": `[{"type":"JETSTREAM","error":"JetStream is not current with the meta leader"}]`})
		f.streamStats(e, "S1", "jobs", m{"leader": ""})
		for _, v := range []struct{ id, a, b string }{{"S0", "nats-1", "nats-2"}, {"S2", "nats-0", "nats-1"}} {
			f.streamStats(e, v.id, "jobs", m{"leader": "nats-0", "is_leader": v.id == "S0"})
			f.streamReplica(e, v.id, "jobs", v.a, nil)
			f.streamReplica(e, v.id, "jobs", v.b, nil)
		}
	}
	f.fires("stream-009", 3)
	// One healthy server only: the recovering nats-1's partial view is
	// still left out as a subset; nats-2's other view ("y") counts.
	g := newFx(t)
	g.cluster(3)
	g.s3cfg(1, 3)
	for _, st := range []string{"x", "y"} {
		g.streamConfig("S0", st, 1, 3, nil)
	}
	for e := int64(2); e <= 3; e++ {
		for _, id := range []string{"S1", "S2"} {
			g.row("raft_stats", m{"epoch": e, "server_id": id, "group_id": "g", "catching_up": true})
		}
		for _, st := range []string{"x", "y"} {
			g.streamStats(e, "S0", st, m{"leader": "nats-0", "is_leader": true})
			g.streamReplica(e, "S0", st, "nats-1", nil)
			g.streamReplica(e, "S0", st, "nats-2", nil)
			g.streamStats(e, "S1", st, m{"leader": ""})
			g.streamStats(e, "S2", st, m{"leader": "nats-0"})
			g.streamReplica(e, "S2", st, "nats-0", nil)
		}
		g.streamReplica(e, "S2", "x", "nats-1", nil)
		g.streamReplica(e, "S2", "y", "nats-3", nil)
	}
	g.fires("stream-009", 3, "APP/y")
}

// While a stream has lost its quorum, its peer sets differing is part of
// that, not a second finding.
func TestPeerSetQuietDuringQuorumLoss(t *testing.T) {
	f := newFx(t)
	f.cluster(3)
	f.s3cfg(1, 3)
	for e := int64(2); e <= 3; e++ {
		f.streamStats(e, "S0", "jobs", m{"leader": ""})
		f.streamReplica(e, "S0", "jobs", "nats-1", nil)
		f.streamStats(e, "S2", "jobs", m{"leader": ""})
		f.streamReplica(e, "S2", "jobs", "nats-0", nil)
		f.streamReplica(e, "S2", "jobs", "nats-1", nil)
	}
	got := f.evalAll(3)
	if _, ok := got["stream-001 APP/jobs critical"]; !ok {
		t.Fatalf("stream-001 did not fire: %v", got)
	}
	for k := range got {
		if strings.HasPrefix(k, "stream-009 ") {
			t.Errorf("stream-009 fired during the quorum loss: %s", k)
		}
	}
}

// The step-down excuse lasts two collect intervals: a hand-over in the
// next epoch is excused, one two minutes later is not.
func TestMetaLeaderStepDownMargin(t *testing.T) {
	for _, c := range []struct {
		change int64
		fires  bool
	}{{11, false}, {15, true}} {
		f := newFx(t)
		f.cluster(30)
		f.event(1, at(10).Add(5*time.Second), "server.lameduck", m{"source": "sys", "server_name": "nats-0"})
		for e := int64(1); e <= 30; e++ {
			l := "nats-0"
			if e >= c.change {
				l = "nats-1"
			}
			for i := range 3 {
				f.stats(e, "S"+string(rune('0'+i)), m{"meta_cluster_size": 3, "meta_leader": l})
			}
		}
		if got := f.eval("system-003", 30, map[string]string{"max_changes": "0"}); (len(got) > 0) != c.fires {
			t.Errorf("hand-over at epoch %d: %s, want firing %v", c.change, rowsText(got), c.fires)
		}
	}
}

func TestStreamVanishedDuringCollectorGap(t *testing.T) {
	f := newFx(t)
	f.cluster(3)
	for e := int64(1); e <= 2; e++ {
		f.streamStats(e, "S0", "jobs", m{"is_leader": true})
	}
	f.event(1, at(2).Add(10*time.Second), "collector.gap", m{"source": "collector", "received_at": at(3).Add(-time.Second)})
	r := f.fires("stream-014", 3, "APP/jobs")
	want(t, r["APP/jobs"], "critical", "nats-lens was not collecting from 2026-10-17 09:00:40 to 09:00:59 UTC")
}

// A replica offline because its server is restarting or announced its
// shutdown is info; one offline for no known reason is a warning. A last
// seen time under a second is left out.
func TestReplicaOfflinePlanned(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 2, three...)
	f.epoch(3, three, "nats-2")
	f.server("S0", "nats-0", 1, 3, t0.Add(-time.Hour), "2.15.0")
	f.server("S1", "nats-1", 1, 3, t0.Add(-time.Hour), "2.15.0")
	f.server("S2", "nats-2", 1, 2, t0.Add(-time.Hour), "2.15.0")
	f.event(1, at(2).Add(5*time.Second), "server.lameduck", m{"source": "sys", "server_name": "nats-2"})
	f.streamStats(3, "S0", "planned", m{"leader": "nats-0", "is_leader": true})
	f.streamReplica(3, "S0", "planned", "nats-2", m{"offline": true, "active_ns": 3e9})
	f.streamStats(3, "S0", "unplanned", m{"leader": "nats-0", "is_leader": true})
	f.streamReplica(3, "S0", "unplanned", "nats-1", m{"offline": true, "active_ns": 4e8})
	r := f.fires("stream-002", 3, "APP/planned", "APP/unplanned")
	want(t, r["APP/planned"], "info", "nats-2 (last seen 3s ago; announced lame duck at 09:00:35 and did not answer)")
	want(t, r["APP/unplanned"], "warning", "offline: nats-1.")
}

// A KV bucket or object store with a TTL or max_bytes is bounded; one
// without either keeps every key it is given, which the detail says.
func TestStreamWithoutLimitsBuckets(t *testing.T) {
	f := newFx(t)
	f.cluster(1)
	f.streamConfig("S0", "KV_customer-info", 1, 1, m{"max_msgs_per_subject": 1})
	f.streamConfig("S0", "KV_jobs-cache", 1, 1, m{"max_msgs_per_subject": 1, "max_age_ns": int64(48 * 3600e9)})
	f.streamConfig("S0", "OBJ_image-input", 1, 1, m{"max_bytes": int64(8) << 30})
	f.streamConfig("S0", "OBJ_scratch", 1, 1, nil)
	f.streamConfig("S0", "archive", 1, 1, nil)
	r := f.fires("stream-005", 1, "APP/archive", "APP/KV_customer-info", "APP/OBJ_scratch")
	want(t, r["APP/KV_customer-info"], "info", "KV bucket customer-info: keys accumulate without bound; set a TTL or max_bytes, or mute if intended")
	want(t, r["APP/OBJ_scratch"], "info", "Object store scratch: objects accumulate without bound")
}

// The meta leader still names itself but sees more peers offline than the
// quorum allows: the meta group has lost its quorum.
func TestMetaPeersOfflineBeyondQuorum(t *testing.T) {
	f := newFx(t)
	f.cluster(1)
	f.stats(1, "S0", m{"meta_cluster_size": 3, "meta_leader": "nats-0", "meta_quorum_needed": 2, "meta_peers_offline": 2})
	r := f.fires("system-002", 1, "")
	want(t, r[""], "critical", "meta leader nats-0 sees 2 of 3 peers offline; quorum needs 2 online")
	g := newFx(t)
	g.cluster(1)
	g.stats(1, "S0", m{"meta_cluster_size": 3, "meta_leader": "nats-0", "meta_quorum_needed": 2, "meta_peers_offline": 1})
	g.fires("system-002", 1)
}

// A server in a crash loop is away and back every few epochs. Its
// replicas being offline are not left silent: stream-002 keeps firing,
// and server-004 names the cause at critical.
func TestCrashLoopReplicaOffline(t *testing.T) {
	f := newFx(t)
	f.server("S0", "nats-0", 1, 40, t0.Add(-time.Hour), "2.15.0")
	f.server("C0", "nats-2", 1, 1, t0.Add(-time.Hour), "2.15.0")
	for e := int64(1); e <= 40; e++ {
		down := e >= 2 && (e-2)%3 == 0
		if down {
			f.epoch(e, three, "nats-2")
		} else {
			f.epoch(e, three)
		}
		if e >= 3 && (e-3)%3 == 0 {
			f.server(fmt.Sprintf("C%d", e), "nats-2", e, e+1, at(e), "2.15.0")
		}
		f.streamStats(e, "S0", "jobs", m{"leader": "nats-0", "is_leader": true})
		f.streamReplica(e, "S0", "jobs", "nats-2", m{"offline": true, "active_ns": 5e9})
	}
	for _, e := range []int64{20, 29, 40} {
		r := f.fires("stream-002", e, "APP/jobs")
		t.Logf("epoch %d stream-002 %s: %s", e, r["APP/jobs"].Severity, r["APP/jobs"].Detail)
	}
	r := f.fires("server-004", 40, "nats-2")
	want(t, r["nats-2"], "critical")
}

func TestStreamCauseRankedByReceivedAt(t *testing.T) {
	f := newFx(t)
	f.cluster(3)
	f.inventory("stream", "jobs", "jobs", "", nil)
	for e := int64(1); e <= 2; e++ {
		f.streamStats(e, "S0", "jobs", m{"is_leader": true})
	}
	f.event(7, at(2).Add(20*time.Second), "stream.deleted", m{"account": "APP", "stream": "jobs", "client_name": "skewed",
		"received_at": at(2).Add(5 * time.Second)})
	f.event(8, at(2).Add(10*time.Second), "stream.deleted", m{"account": "APP", "stream": "jobs", "client_name": "latest",
		"received_at": at(2).Add(15 * time.Second)})
	for _, code := range []string{"stream-011", "stream-014"} {
		if code == "stream-014" {
			f.exec(`DELETE FROM inventory`)
		}
		r := f.fires(code, 3, "APP/jobs")
		if r["APP/jobs"].CauseEventID != 8 {
			t.Errorf("%s cause = %d, want the one received last", code, r["APP/jobs"].CauseEventID)
		}
	}
}

// A stream replica rebuilt at another time holds the same messages: info.
// Contents that differ in this and the previous epoch make it a warning.
func TestStreamCreatedDiffersSeverityFollowsContents(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	old, rebuilt := t0.Add(-48*time.Hour), t0.Add(-time.Hour)
	for _, s := range []string{"same", "apart"} {
		f.streamConfig("S0", s, 1, 2, m{"created": old})
		f.streamConfig("S1", s, 1, 2, m{"created": old})
		f.streamConfig("S2", s, 1, 2, m{"created": rebuilt})
	}
	for e := int64(1); e <= 2; e++ {
		for i, id := range []string{"S0", "S1", "S2"} {
			f.streamStats(e, id, "same", m{"is_leader": i == 0, "first_seq": 1, "last_seq": 500, "messages": 500})
			f.streamStats(e, id, "apart", m{"is_leader": i == 0, "first_seq": 1, "last_seq": 500 - 400*int64(i/2),
				"messages": 500 - 400*int64(i/2)})
		}
	}
	r := f.fires("stream-008", 2, "APP/same", "APP/apart")
	want(t, r["APP/same"], "info", "contents agree")
	want(t, r["APP/apart"], "warning", "contents differ", "nats-2 first 1 last 100")
}

// A stream the inventory declares unbounded on purpose (max_age 0 and
// max_bytes -1 given explicitly) is not reported; one that only leaves
// them out still is. A KV bucket that allows per-key TTLs is left out only
// when the inventory lists it; another bucket's detail gives its history
// per key, or says it allows per-message TTLs.
func TestStreamWithoutLimitsRespectsInventoryAndTTLs(t *testing.T) {
	f := newFx(t)
	f.cluster(1)
	f.row("accounts", m{"account": "ACJQ", "account_name": "APP", "is_system": false, "first_epoch": 1, "last_epoch": 1})
	cfg := func(stream string, cols m) {
		f.row("stream_config", merge(m{"server_id": "S0", "account": "ACJQ", "stream": stream, "first_epoch": 1, "last_epoch": 1,
			"config_hash": "h", "replicas": 3, "max_msgs": -1, "max_bytes": -1, "max_age_ns": 0, "max_msgs_per_subject": -1,
			"allow_msg_ttl": false}, cols))
	}
	cfg("order-archive", nil)
	cfg("audit-archive", nil)
	cfg("KV_customer-info", m{"allow_msg_ttl": true, "max_msgs_per_subject": 1})
	cfg("KV_undeclared-ttl", m{"allow_msg_ttl": true, "max_msgs_per_subject": 1})
	cfg("KV_worker-presence", m{"max_msgs_per_subject": 5})
	f.inventory("stream", "order-archive", "order-archive", "", m{"max_age_ns": 0, "max_bytes": -1})
	f.inventory("stream", "audit-archive", "audit-archive", "", m{"max_age_ns": 0})
	f.inventory("kv", "customer-info", "KV_customer-info", "", nil)
	r := f.fires("stream-005", 1, "ACJQ/audit-archive", "ACJQ/KV_worker-presence", "ACJQ/KV_undeclared-ttl")
	want(t, r["ACJQ/KV_worker-presence"], "info", "history 5 per key")
	want(t, r["ACJQ/KV_undeclared-ttl"], "info", "allows per-message TTLs; nats-lens cannot see whether writers set them")
}

// A stream follower a few messages behind the leader agrees with it.
func TestStreamCreatedDiffersBusyFollowerStaysInfo(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	f.streamConfig("S0", "busy", 1, 2, m{"created": t0.Add(-48 * time.Hour)})
	f.streamConfig("S1", "busy", 1, 2, m{"created": t0.Add(-time.Hour)})
	for e := int64(1); e <= 2; e++ {
		f.streamStats(e, "S0", "busy", m{"is_leader": true, "first_seq": 1, "last_seq": 90000 * e, "messages": 90000 * e})
		f.streamStats(e, "S1", "busy", m{"first_seq": 1, "last_seq": 90000*e - 20, "messages": 90000*e - 20})
	}
	r := f.fires("stream-008", 2, "APP/busy")
	want(t, r["APP/busy"], "info", "contents agree")
}

// On a small stream the tolerance does not hide a replica ahead of the
// leader: 80 messages against the leader's 30 disagree.
func TestStreamCreatedDiffersSmallStreamReplicaAhead(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	old, rebuilt := t0.Add(-48*time.Hour), t0.Add(-time.Hour)
	f.streamConfig("S0", "ORD", 1, 2, m{"created": rebuilt})
	f.streamConfig("S1", "ORD", 1, 2, m{"created": rebuilt})
	f.streamConfig("S2", "ORD", 1, 2, m{"created": old})
	for e := int64(1); e <= 2; e++ {
		for i, id := range []string{"S0", "S1", "S2"} {
			n := int64(30)
			if i == 2 {
				n = 80
			}
			f.streamStats(e, id, "ORD", m{"is_leader": i == 0, "first_seq": 1, "last_seq": n, "messages": n})
		}
	}
	r := f.fires("stream-008", 2, "APP/ORD")
	want(t, r["APP/ORD"], "warning", "contents differ")
}

// A stream follower read a moment after the leader is a little ahead;
// within 0.1% of the leader's last sequence that agrees.
func TestStreamCreatedDiffersFollowerAheadBySkew(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	f.streamConfig("S0", "busy", 1, 2, m{"created": t0.Add(-48 * time.Hour)})
	f.streamConfig("S1", "busy", 1, 2, m{"created": t0.Add(-time.Hour)})
	for e := int64(1); e <= 2; e++ {
		f.streamStats(e, "S0", "busy", m{"is_leader": true, "first_seq": 1, "last_seq": 5_000_000 * e, "messages": 5_000_000 * e})
		f.streamStats(e, "S1", "busy", m{"first_seq": 1, "last_seq": 5_000_000*e + 40, "messages": 5_000_000*e + 40})
	}
	r := f.fires("stream-008", 2, "APP/busy")
	want(t, r["APP/busy"], "info", "contents agree")
}
