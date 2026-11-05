package checks

import "testing"

// consumer-016: a durable with one replica on an R3 stream is reported; on
// an R1 stream, with replicas 0 (the stream's count), with three replicas,
// as an ephemeral or a JS_ consumer, or when no server reports it now, it
// is not.
func TestSingleReplicaOnReplicatedStream(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	for i := range 3 {
		id := "S" + string(rune('0'+i))
		f.streamConfig(id, "jobs", 1, 2, nil)
	}
	f.streamConfig("S1", "single", 1, 2, m{"replicas": 1})
	for name, cols := range map[string]m{
		"r1":        {"replicas": 1},
		"inherit":   {"replicas": 0},
		"r3":        {"replicas": 3},
		"watcher":   {"replicas": 1, "durable_name": ""},
		"JS_mirror": {"replicas": 1},
		"gone":      {"replicas": 1},
	} {
		f.consumerConfig("S2", "jobs", name, 1, 2, cols)
		e := int64(2)
		if name == "gone" {
			e = 1
		}
		f.consumerStats(e, "S2", "jobs", name, m{"is_leader": true, "leader": "nats-2"})
	}
	f.consumerConfig("S1", "single", "r1", 1, 2, m{"replicas": 1})
	f.consumerStats(2, "S1", "single", "r1", m{"is_leader": true, "leader": "nats-1"})

	r := f.fires("consumer-016", 2, "APP/jobs/r1")
	want(t, r["APP/jobs/r1"], "info", "Durable r1 has 1 replica while its stream jobs has 3; its state is on nats-2 only.")
	if v := r["APP/jobs/r1"].Value; v == nil || *v != 3 {
		t.Errorf("value = %v, want 3", v)
	}
	if r["APP/jobs/r1"].RelatedSource != "nats-2" || r["APP/jobs/r1"].RelatedConfig["stream_replicas"] != "3" {
		t.Errorf("related = %q %v", r["APP/jobs/r1"].RelatedSource, r["APP/jobs/r1"].RelatedConfig)
	}
}

// Servers that disagree on the replica count are consumer-008's finding,
// not this one's.
func TestSingleReplicaDisagreementLeftOut(t *testing.T) {
	f := newFx(t)
	f.cluster(1)
	f.streamConfig("S0", "jobs", 1, 1, nil)
	f.consumerConfig("S0", "jobs", "scaling", 1, 1, m{"replicas": 1})
	f.consumerConfig("S1", "jobs", "scaling", 1, 1, m{"replicas": 3})
	f.consumerStats(1, "S0", "jobs", "scaling", m{"is_leader": true})
	f.fires("consumer-016", 1)
}
