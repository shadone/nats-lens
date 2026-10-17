package collect

import (
	"os"
	"testing"
	"time"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestApplyJszFromDevCluster(t *testing.T) {
	snap := NewSnapshot(time.Now())
	for _, f := range []string{"jsz-nats-0.json", "jsz-nats-1.json"} {
		if err := snap.Apply(Jsz, readFixture(t, f)); err != nil {
			t.Fatal(err)
		}
	}
	if len(snap.Servers) != 2 {
		t.Fatalf("want 2 server views, got %d", len(snap.Servers))
	}
	v1 := snap.Servers["NFAKESERVER1ID"]
	if v1 == nil || v1.Jsz == nil || v1.Info.Name != "nats-1" {
		t.Fatalf("nats-1 view missing: %+v", v1)
	}
	if v1.Status[Jsz].State != StateOK {
		t.Fatalf("status = %+v", v1.Status[Jsz])
	}
	if v1.Jsz.Meta == nil || v1.Jsz.Meta.Leader != "nats-1" || v1.Jsz.Meta.Size != 3 {
		t.Fatalf("meta = %+v", v1.Jsz.Meta)
	}
	streams := map[string]int{}
	for _, a := range v1.Jsz.AccountDetails {
		for _, s := range a.Streams {
			streams[s.Name] = len(s.Consumer)
			if s.Config == nil {
				t.Errorf("stream %s has no config", s.Name)
			}
		}
	}
	if streams["order-jobs"] != 2 {
		t.Errorf("order-jobs consumers = %d", streams["order-jobs"])
	}
	if _, ok := streams["test-stream"]; !ok {
		t.Errorf("R1 test-stream missing from its host's view")
	}
	v0 := snap.Servers["NFAKESERVER0ID"]
	for _, a := range v0.Jsz.AccountDetails {
		for _, s := range a.Streams {
			if s.Name == "test-stream" {
				t.Errorf("R1 test-stream must not appear on nats-0")
			}
		}
	}
}

func TestApplyArchiveMirror(t *testing.T) {
	snap := NewSnapshot(time.Now())
	if err := snap.Apply(Jsz, readFixture(t, "jsz-nats-1.json")); err != nil {
		t.Fatal(err)
	}
	for _, a := range snap.Servers["NFAKESERVER1ID"].Jsz.AccountDetails {
		for _, s := range a.Streams {
			if s.Name != "order-archive" {
				continue
			}
			if s.Config.Mirror == nil || s.Config.Mirror.Name != "order-jobs" {
				t.Fatalf("mirror config = %+v", s.Config.Mirror)
			}
			if s.Mirror == nil {
				t.Fatal("mirror state missing")
			}
			return
		}
	}
	t.Fatal("order-archive not found")
}

func TestApplyStatsz(t *testing.T) {
	body := `{"server":{"name":"nats-2","host":"0.0.0.0","id":"NFAKE2","ver":"2.15.0","jetstream":true,"seq":3,"time":"2026-10-16T23:17:46Z"},
	"statsz":{"start":"2026-10-15T09:49:00Z","mem":123456,"cores":4,"cpu":3.5,"connections":12,"total_connections":99,
	"active_accounts":3,"subscriptions":420,"sent":{"msgs":10,"bytes":100},"received":{"msgs":20,"bytes":200},
	"slow_consumers":1,"routes":[{"rid":1,"name":"nats-0","sent":{"msgs":1,"bytes":2},"received":{"msgs":3,"bytes":4},"pending":0}],"active_servers":3}}`
	snap := NewSnapshot(time.Now())
	if err := snap.Apply(Statsz, []byte(body)); err != nil {
		t.Fatal(err)
	}
	v := snap.Servers["NFAKE2"]
	if v == nil || v.Statsz == nil || v.Statsz.Connections != 12 || v.Statsz.Routes[0].Name != "nats-0" {
		t.Fatalf("statsz not decoded: %+v", v)
	}
}

func TestApplyRaftzWithWALError(t *testing.T) {
	// The server encodes a Go error value as an empty object.
	body := `{"server":{"name":"nats-0","id":"NFAKE0"},"data":{"APP":{"S-R3F-abc":{
	"id":"S-R3F-abc","state":"FOLLOWER","size":3,"quorum_needed":2,"committed":10,"applied":10,
	"leader":"nats-1","ever_had_leader":true,"term":4,"pterm":4,"pindex":10,"system_account":true,
	"traffic_account":"AFAKE","ipq_proposal_len":0,"ipq_entry_len":0,"ipq_resp_len":0,"ipq_apply_len":0,
	"wal":{"messages":5,"bytes":512,"first_seq":6,"last_seq":10,"first_ts":"2026-10-16T00:00:00Z","last_ts":"2026-10-16T00:00:01Z","consumer_count":0},
	"wal_error":{},
	"peers":{"yrzKKRBu":{"name":"nats-1","known":true,"last_replicated_index":10,"last_seen":"120ms"}}}}}}`
	snap := NewSnapshot(time.Now())
	if err := snap.Apply(Raftz, []byte(body)); err != nil {
		t.Fatal(err)
	}
	g := snap.Servers["NFAKE0"].Raftz["APP"]["S-R3F-abc"]
	if g.Leader != "nats-1" || g.WAL.Msgs != 5 || string(g.WALError) != "{}" {
		t.Fatalf("raft group = %+v", g)
	}
	if g.Peers["yrzKKRBu"].Name != "nats-1" {
		t.Fatalf("peers = %+v", g.Peers)
	}
}

func TestApplyAPIErrorKeepsServer(t *testing.T) {
	body := `{"server":{"name":"nats-0","id":"NFAKE0"},"error":{"code":500,"description":"jetstream not enabled"}}`
	snap := NewSnapshot(time.Now())
	if err := snap.Apply(Jsz, []byte(body)); err != nil {
		t.Fatal(err)
	}
	st := snap.Servers["NFAKE0"].Status[Jsz]
	if st.State != StateError || st.Error == "" {
		t.Fatalf("status = %+v", st)
	}
	if !snap.Partial() {
		t.Fatal("an endpoint error must make the epoch partial")
	}
}

func TestApplyRejectsReplyWithoutServer(t *testing.T) {
	snap := NewSnapshot(time.Now())
	if err := snap.Apply(Varz, []byte(`{"data":{}}`)); err == nil {
		t.Fatal("expected an error")
	}
	if err := snap.Apply(Varz, []byte(`not json`)); err == nil {
		t.Fatal("expected an error")
	}
	if len(snap.Servers) != 0 {
		t.Fatal("no view should be created")
	}
}

func TestAccountRaftzMergesWithSystemRaftz(t *testing.T) {
	snap := NewSnapshot(time.Now())
	sys := `{"server":{"name":"nats-0","id":"NFAKE0"},"data":{"SYS":{"_meta_":{"id":"_meta_","state":"LEADER"}}}}`
	acc := `{"server":{"name":"nats-0","id":"NFAKE0"},"data":{"APP":{"S-R3F-abc":{"id":"S-R3F-abc","state":"FOLLOWER"}}}}`
	if err := snap.Apply(Raftz, []byte(sys)); err != nil {
		t.Fatal(err)
	}
	if err := snap.Apply(AccountRaftz("APP"), []byte(acc)); err != nil {
		t.Fatal(err)
	}
	v := snap.Servers["NFAKE0"]
	if len(v.Raftz) != 2 || v.Raftz["APP"]["S-R3F-abc"].State != "FOLLOWER" {
		t.Fatalf("raftz = %+v", v.Raftz)
	}
	if v.Status[AccountRaftz("APP")].State != StateOK || v.Status[Raftz].State != StateOK {
		t.Fatalf("status = %+v", v.Status)
	}
	if got := AccountRaftz("APP").Subject(); got != "$SYS.REQ.SERVER.PING.RAFTZ" {
		t.Fatalf("subject = %s", got)
	}
}
