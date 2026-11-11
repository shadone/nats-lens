package collect

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

// fuzzTargets are the endpoints a reply is applied for, with the type each
// one's reply decodes into.
var fuzzTargets = []struct {
	e      Endpoint
	target func() any
}{
	{Statsz, func() any { return new(server.ServerStatsMsg) }},
	{Varz, func() any { return new(reply[*server.Varz]) }},
	{Connz, func() any { return new(reply[*server.Connz]) }},
	{Routez, func() any { return new(reply[*server.Routez]) }},
	{Gatewayz, func() any { return new(reply[*server.Gatewayz]) }},
	{Leafz, func() any { return new(reply[*server.Leafz]) }},
	{Accountz, func() any { return new(reply[*server.Accountz]) }},
	{AccountStatz, func() any { return new(reply[*server.AccountStatz]) }},
	{Jsz, func() any { return new(reply[*server.JSInfo]) }},
	{Healthz, func() any { return new(reply[*server.HealthStatus]) }},
	{Raftz, func() any { return new(reply[RaftzStatus]) }},
	{AccountRaftz("APP"), func() any { return new(reply[RaftzStatus]) }},
	{Ipqueuesz, func() any { return new(reply[server.IpqueueszStatus]) }},
	{Subsz, func() any { return new(reply[*server.Subsz]) }},
	{Endpoint("BOGUS"), nil},
}

var listPosition = regexp.MustCompile(`\[[0-9]+\]`)

// FuzzSnapshotApply applies monitoring replies: a reply without server
// info is an error and leaves no server; any other reply records the
// endpoint as ok or as an error on its server's view, the same way each
// time. A reply that decodes plainly reports no unknown values, and each
// unknown value names a path and holds the JSON the reply sent.
func FuzzSnapshotApply(f *testing.F) {
	for _, name := range []string{"jsz-nats-0.json", "jsz-nats-1.json"} {
		b, err := os.ReadFile("testdata/" + name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(uint8(8), b)
	}
	const srv = `"server":{"name":"nats-0","id":"NFAKESERVER0ID","ver":"2.15.0"}`
	for _, s := range []struct {
		target uint8
		body   string
	}{
		{0, `{` + srv + `,"statsz":{"mem":1,"cores":2,"cpu":0.5,"connections":3}}`},
		{0, `{"server":{"id":""},"statsz":{}}`},
		{1, `{` + srv + `,"data":{"server_id":"NFAKESERVER0ID","max_payload":1048576,"now":"2026-10-17T14:30:00Z"}}`},
		{2, `{` + srv + `,"data":{"num_connections":1,"connections":[{"cid":1,"name":"app","jwt":"x"}]}}`},
		{6, `{` + srv + `,"data":{"account_detail":{"account_name":"APP"}}}`},
		{8, `{` + srv + `,"data":{"account_details":[{"name":"APP","stream_detail":[{"name":"orders","config":{"retention":"future","storage":7,"discard":null}}]}]}}`},
		{8, `{` + srv + `,"data":{"account_details":[{"name":"APP","stream_detail":[{"name":"orders","config":{"retention":"future","max_msgs":"lots"}}]}]}}`},
		{8, `{` + srv + `,"data":{"account_details":[{"name":"APP","stream_detail":[{"name":"orders","consumer_detail":[{"name":"w","config":{"ack_policy":"maybe","deliver_policy":"later","replay_policy":-1}}]}]}]}}`},
		{8, `{` + srv + `,"data":{"account_details":[{"name":"APP","stream_detail":[{"name":"orders","config":{"Retention":"future","retention":"limits"}}]}]}}`},
		{8, `{` + srv + `,"error":{"code":503,"description":"JetStream not enabled"}}`},
		{9, `{` + srv + `,"data":{"status":"ok","errors":[{"type":"FUTURE","error":"x"}]}}`},
		{10, `{` + srv + `,"data":{"$G":{"S-R3":{"wal_error":{"x":1},"state":"LEADER"}}}}`},
		{11, `{` + srv + `,"data":{"APP":{"C-R3":{"wal_error":"disk"}}}}`},
		{12, `{` + srv + `,"data":{"q":{"pending":1}}}`},
		{13, `{` + srv + `,"data":{"num_subscriptions":1,"subscriptions_list":[{"subject":"a.>","sid":"1"}]}}`},
		{14, `{` + srv + `,"data":{}}`},
		{8, `{"data":{}}`},
		{8, `null`},
		{8, `[]`},
		{8, `{` + srv + `,"data":{"account_details":[{"name":"\ud800","stream_detail":[[[[[[[]]]]]]]}]}}`},
	} {
		f.Add(s.target, []byte(s.body))
	}
	f.Fuzz(func(t *testing.T, pick uint8, data []byte) {
		if len(data) > 64<<10 {
			return
		}
		ft := fuzzTargets[int(pick)%len(fuzzTargets)]
		snap := NewSnapshot(time.Time{})
		err := snap.Apply(ft.e, data)
		again := NewSnapshot(time.Time{})
		err2 := again.Apply(ft.e, data)
		if (err == nil) != (err2 == nil) || (err != nil && err.Error() != err2.Error()) || !reflect.DeepEqual(snap, again) {
			t.Fatalf("Apply(%s) is not deterministic: %v / %v", ft.e, err, err2)
		}
		if err != nil {
			if len(snap.Servers) != 0 {
				t.Fatalf("Apply(%s) failed with %v but kept servers %v", ft.e, err, snap.Servers)
			}
			return
		}
		if len(snap.Servers) != 1 {
			t.Fatalf("Apply(%s) succeeded with %d servers", ft.e, len(snap.Servers))
		}
		for id, v := range snap.Servers {
			st, ok := v.Status[ft.e]
			if id == "" || v.Info.ID != id || !ok || (st.State != StateOK && st.State != StateError) {
				t.Fatalf("Apply(%s) left server %q with status %+v", ft.e, id, v.Status)
			}
			if st.State == StateError && (st.Error == "" || len(st.Unknown) != 0) {
				t.Fatalf("Apply(%s) recorded an error without a message or with unknown values: %+v", ft.e, st)
			}
			if len(st.Unknown) > 0 && json.Unmarshal(data, ft.target()) == nil {
				t.Fatalf("Apply(%s) reports unknown values %+v in a reply that decodes plainly", ft.e, st.Unknown)
			}
			for _, u := range st.Unknown {
				if u.Path == "" || !json.Valid([]byte(u.Value)) || listPosition.MatchString(u.Field()) {
					t.Fatalf("Apply(%s) reports unknown value %+v with field %q", ft.e, u, u.Field())
				}
			}
		}
	})
}
