package checks

import (
	"testing"
	"time"
)

func (f *fx) sourceAt(e int64, stream, id string, active any, err any, cols m) {
	f.row("stream_source_stats", merge(m{"epoch": e, "server_id": "S0", "account": "APP", "stream": stream, "source_id": id, "name": "orders", "filter_subject": "", "config": "{}", "reported": true, "active_ns": active, "lag": 0, "error": err}, cols))
}

func TestSourceHealthChecks(t *testing.T) {
	f := newFx(t)
	f.cluster(12)
	streams := []string{"silent", "idle", "never", "new", "missing", "error", "once", "removed", "changed", "recovering"}
	for _, s := range streams {
		f.streamConfig("S0", s, 1, 12, nil)
	}
	for e := int64(1); e <= 12; e++ {
		f.streamStats(e, "S1", "idle", m{"is_leader": false, "leader": "nats-0", "source_config_known": true})
		f.sourceAt(e, "idle", "a", int64(-1), "follower error", m{"server_id": "S1"})
		for _, s := range streams {
			cols := m{"leader": "nats-0", "is_leader": true, "source_config_known": true, "leader_since": t0.Add(-time.Hour), "created": t0.Add(-time.Hour)}
			if s == "new" {
				cols["leader_since"] = at(11)
			}
			f.streamStats(e, "S0", s, cols)
			if s == "removed" && e == 12 {
				continue
			}
			active := any(int64(time.Second))
			var err any
			if s == "silent" || s == "error" || s == "once" {
				active = int64(3 * time.Minute)
			}
			if s == "never" || s == "new" {
				active = int64(-1)
			}
			if s == "error" && e%2 == 0 || s == "once" && e == 12 {
				err = "stream not found"
			}
			id := "a"
			if s == "changed" && e == 12 {
				id = "b"
				active = int64(-1)
			}
			if s == "missing" {
				f.sourceAt(e, s, id, nil, nil, m{"reported": false})
				continue
			}
			// Catching up is judged by stream-016, healthy activity is never silence.
			sc := m{}
			if s == "recovering" {
				sc["lag"] = 100000 - e*1000
			}
			f.sourceAt(e, s, id, active, err, sc)
		}
	}
	f.exec(`UPDATE stream_source_stats SET config=json_object('name','orders','external',json_object('api','$JS.eu.API','deliver','deliver.eu'),'filter_subject','orders.eu.>') WHERE stream='error'`)
	r := f.fires("stream-022", 12, "APP/silent", "APP/never")
	want(t, r["APP/silent"], "warning", "orders", "not heard")
	want(t, r["APP/never"], "warning", "never heard")
	r = f.fires("stream-023", 12, "APP/error")
	want(t, r["APP/error"], "warning", "stream not found", "$JS.eu.API", "orders.eu.>")
	// Replay must not use future reports or configuration.
	f.fires("stream-023", 1)
	f.fires("stream-022", 1, "APP/silent", "APP/error", "APP/once")
}

func TestSourceHealthHysteresisAndMissingReports(t *testing.T) {
	f := newFx(t)
	f.cluster(12)
	for _, s := range []string{"hold", "clear", "unknown", "removed", "newleader", "restart", "newstream"} {
		f.streamConfig("S0", s, 1, 12, nil)
	}
	for e := int64(1); e <= 12; e++ {
		for _, s := range []string{"hold", "clear", "unknown", "removed", "newleader", "restart", "newstream"} {
			cols := m{"leader": "nats-0", "is_leader": true, "source_config_known": true, "leader_since": t0.Add(-time.Hour), "created": t0.Add(-time.Hour)}
			if e == 12 && s == "removed" {
				f.streamStats(e, "S0", s, cols)
				continue
			}
			if s == "newleader" {
				cols["leader_since"] = at(11)
			}
			if s == "newstream" {
				cols["created"] = at(11)
			}
			if s == "restart" {
				f.stats(e, "S0", m{"start_time": at(11)})
			}
			f.streamStats(e, "S0", s, cols)
			if e == 12 && s == "unknown" {
				f.sourceAt(e, s, "a", nil, nil, m{"reported": false})
				continue
			}
			active := int64(time.Second)
			var err any
			if s == "hold" && e == 9 {
				err = "origin unavailable"
			}
			if s == "clear" && e == 8 {
				err = "origin unavailable"
			}
			if s == "newleader" || s == "restart" || s == "newstream" {
				active = -1
			}
			f.sourceAt(e, s, "a", active, err, nil)
		}
	}
	// The server_stats fixture above applies to the shared server. Remove it
	// except the restart stream's explicit process on S1.
	f.exec(`DELETE FROM server_stats`)
	f.exec(`UPDATE stream_stats SET server_id='S1', leader='nats-1' WHERE stream='restart'`)
	f.exec(`UPDATE stream_source_stats SET server_id='S1' WHERE stream='restart'`)
	for e := int64(1); e <= 12; e++ {
		f.stats(e, "S1", m{"start_time": at(11)})
	}
	for _, s := range []string{"hold", "clear", "unknown", "removed"} {
		f.sourceFiring("stream-023", s, "a", 11)
	}
	f.fires("stream-023", 12, "APP/hold")
	f.fires("stream-022", 12)
	// Silence holds to 80%, but a healthy heartbeat resolves it.
	f.exec(`UPDATE stream_source_stats SET active_ns=100000000000 WHERE stream='hold' AND epoch=12`)
	f.exec(`UPDATE stream_source_stats SET error=NULL`)
	f.sourceFiring("stream-022", "hold", "a", 11)
	f.fires("stream-022", 12, "APP/hold")
	f.exec(`UPDATE stream_source_stats SET active_ns=1000000000 WHERE stream='hold' AND epoch=12`)
	f.fires("stream-022", 12)
}

func (f *fx) sourceFiring(code, stream, id string, e int64) {
	f.streamFiringAt(code, "APP/"+stream, e)
	f.exec(`UPDATE findings SET related_config=json_object('source_ids', ?, 'created_us', ?::VARCHAR) WHERE id=?`, id, t0.Add(-time.Hour).UnixMicro(), code+":APP/"+stream)
}

func TestSourceHealthIdentityAndContinuity(t *testing.T) {
	f := newFx(t)
	f.cluster(12)
	for _, s := range []string{"mixed", "replacement", "readded", "reborn", "gap"} {
		f.streamConfig("S0", s, 1, 12, nil)
	}
	for e := int64(1); e <= 12; e++ {
		for _, s := range []string{"mixed", "replacement", "readded", "reborn", "gap"} {
			if s == "gap" && e == 12 {
				continue
			}
			created := t0.Add(-time.Hour)
			if s == "reborn" && e == 12 {
				created = at(12)
			}
			f.streamStats(e, "S0", s, m{"is_leader": true, "source_config_known": true, "leader": "nats-0", "created": created, "leader_since": t0.Add(-time.Hour)})
			if s == "readded" && e == 11 {
				continue
			}
			id := "a"
			active := int64(-1)
			if s == "replacement" {
				active = int64(100 * time.Second)
				if e == 12 {
					id = "b"
				}
			}
			f.sourceAt(e, s, id, active, nil, nil)
			if s == "mixed" {
				f.sourceAt(e, s, "b", int64(-1), "origin missing", nil)
			}
		}
	}
	f.sourceFiring("stream-022", "replacement", "a", 11)
	f.sourceFiring("stream-022", "reborn", "a", 11)
	f.sourceFiring("stream-022", "gap", "a", 11)
	f.fires("stream-022", 12, "APP/mixed", "APP/gap")
	f.fires("stream-023", 12, "APP/mixed")
}

func TestSourceHealthMissingConfiguration(t *testing.T) {
	f := newFx(t)
	f.cluster(12)
	f.streamConfig("S0", "unknown", 1, 11, nil)
	f.streamConfig("S0", "empty", 1, 12, nil)
	for e := int64(1); e <= 12; e++ {
		for _, s := range []string{"unknown", "empty"} {
			f.streamStats(e, "S0", s, m{"is_leader": true, "leader": "nats-0", "created": t0.Add(-time.Hour), "source_config_known": s == "empty" || e < 12})
			if e < 12 {
				f.sourceAt(e, s, "a", int64(-1), "origin missing", nil)
			}
		}
	}
	f.sourceFiring("stream-023", "unknown", "a", 11)
	f.sourceFiring("stream-023", "empty", "a", 11)
	// A missing configuration is unknown, while a known empty source list
	// authoritatively removes the old source.
	f.fires("stream-023", 12, "APP/unknown")
}
