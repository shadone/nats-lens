package storeprov_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPlacementObservationNameIdentity(t *testing.T) {
	for _, tc := range []struct {
		name     string
		sql      []string
		samples  int32
		boundary string
	}{
		{"selected collision now", []string{
			`INSERT INTO servers(server_id,server_name,first_epoch,last_epoch,first_seen_at,last_seen_at) SELECT 'other-S1','nats-1',2,3,first_seen_at,last_seen_at FROM servers WHERE server_id='S1'`,
			`INSERT INTO server_stats(epoch,server_id) VALUES (2,'S1'),(3,'S1'),(2,'other-S1'),(3,'other-S1')`,
		}, 1, "evidence_unknown"},
		{"selected collision only in history", []string{
			`INSERT INTO servers(server_id,server_name,first_epoch,last_epoch,first_seen_at,last_seen_at) SELECT 'other-S1','nats-1',2,2,first_seen_at,last_seen_at FROM servers WHERE server_id='S1'`,
			`INSERT INTO server_stats(epoch,server_id) VALUES (2,'S1'),(2,'other-S1')`,
		}, 1, "evidence_unknown"},
		{"reporter collision without another asset report", []string{
			`INSERT INTO servers(server_id,server_name,first_epoch,last_epoch,first_seen_at,last_seen_at) SELECT 'other-S0','nats-0',2,2,first_seen_at,last_seen_at FROM servers WHERE server_id='S0'`,
			`INSERT INTO server_stats(epoch,server_id) VALUES (2,'S0'),(2,'other-S0')`,
		}, 1, "evidence_unknown"},
		{"nonselected peer collision without own asset report", []string{
			`INSERT INTO stream_replicas(epoch,server_id,account,stream,peer_name,current,offline,lag) SELECT epoch,server_id,account,stream,'nats-2',false,false,0 FROM stream_replicas`,
			`INSERT INTO servers(server_id,server_name,first_epoch,last_epoch,first_seen_at,last_seen_at) SELECT 'S2','nats-2',1,3,first_seen_at,last_seen_at FROM servers WHERE server_id='S1'`,
			`INSERT INTO servers(server_id,server_name,first_epoch,last_epoch,first_seen_at,last_seen_at) SELECT 'other-S2','nats-2',2,2,first_seen_at,last_seen_at FROM servers WHERE server_id='S1'`,
			`INSERT INTO server_stats(epoch,server_id) VALUES (2,'S2'),(2,'other-S2')`,
		}, 1, "evidence_unknown"},
		{"nonselected peer handover cuts identity", []string{
			`INSERT INTO stream_replicas(epoch,server_id,account,stream,peer_name,current,offline,lag) SELECT epoch,server_id,account,stream,'nats-2',false,false,0 FROM stream_replicas`,
			`INSERT INTO servers(server_id,server_name,first_epoch,last_epoch,first_seen_at,last_seen_at) SELECT 'S2','nats-2',1,2,first_seen_at,last_seen_at FROM servers WHERE server_id='S1'`,
			`INSERT INTO servers(server_id,server_name,first_epoch,last_epoch,first_seen_at,last_seen_at) SELECT 'new-S2','nats-2',3,3,first_seen_at,last_seen_at FROM servers WHERE server_id='S1'`,
		}, 1, "identity_changed"},
		{"endpoint success alone reveals collision", []string{
			`INSERT INTO servers(server_id,server_name,first_epoch,last_epoch,first_seen_at,last_seen_at) SELECT 'other-S1','nats-1',2,2,first_seen_at,last_seen_at FROM servers WHERE server_id='S1'`,
			`INSERT INTO epoch_endpoints VALUES (2,'other-S1','VARZ','ok',NULL)`,
		}, 1, "evidence_unknown"},
		{"unrelated name collision", []string{
			`INSERT INTO servers(server_id,server_name,first_epoch,last_epoch,first_seen_at,last_seen_at) SELECT 'other-S1','unrelated',2,3,first_seen_at,last_seen_at FROM servers WHERE server_id='S1'`,
			`INSERT INTO server_stats(epoch,server_id) VALUES (2,'S1'),(3,'S1'),(2,'other-S1'),(3,'other-S1')`,
		}, 2, "state_changed"},
		{"future process cannot leak backwards", []string{
			`INSERT INTO servers(server_id,server_name,first_epoch,last_epoch,first_seen_at,last_seen_at) SELECT 'other-S1','nats-1',4,4,first_seen_at,last_seen_at FROM servers WHERE server_id='S1'`,
		}, 2, "state_changed"},
		{"clean process handover cuts identity", []string{
			`INSERT INTO servers(server_id,server_name,first_epoch,last_epoch,first_seen_at,last_seen_at) SELECT 'new-S1','nats-1',3,3,first_seen_at,last_seen_at FROM servers WHERE server_id='S1'`,
			`UPDATE stream_stats SET server_id='new-S1' WHERE server_id='S1' AND epoch=3`,
			`UPDATE epoch_endpoints SET server_id='new-S1' WHERE server_id='S1' AND epoch=3`,
		}, 1, "identity_changed"},
		{"retired process does not poison new run", []string{
			`UPDATE stream_replicas SET current=false`,
			`INSERT INTO servers(server_id,server_name,first_epoch,last_epoch,first_seen_at,last_seen_at) SELECT 'new-S1','nats-1',2,3,first_seen_at,last_seen_at FROM servers WHERE server_id='S1'`,
			`UPDATE stream_stats SET server_id='new-S1' WHERE server_id='S1' AND epoch>=2`,
			`UPDATE epoch_endpoints SET server_id='new-S1' WHERE server_id='S1' AND epoch>=2`,
		}, 2, "identity_changed"},
	} {
		for _, kind := range []string{"stream", "consumer"} {
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				st := observationStore(t)
				for _, q := range tc.sql {
					if _, err := st.DB().Exec(q); err != nil {
						t.Fatal(err)
					}
				}
				selected := "APP/orders"
				if kind == "consumer" {
					for _, q := range []string{
						`INSERT INTO consumer_stats(epoch,server_id,account,stream,consumer,is_leader,created) SELECT epoch,server_id,account,stream,'shipper',is_leader,created FROM stream_stats`,
						`INSERT INTO consumer_config(server_id,account,stream,consumer,first_epoch,last_epoch,durable_name,replicas,config_hash) SELECT server_id,account,stream,'shipper',first_epoch,last_epoch,'shipper',replicas,config_hash FROM stream_config`,
						`INSERT INTO consumer_replicas(epoch,server_id,account,stream,consumer,peer_name,current,offline,lag) SELECT epoch,server_id,account,stream,'shipper',peer_name,current,offline,lag FROM stream_replicas`,
					} {
						if _, err := st.DB().Exec(q); err != nil {
							t.Fatal(err)
						}
					}
					selected += "/shipper"
				}
				c := serveSeries(t, st, t0.Add(90*time.Second))
				res := must(c.GetPlacement(ctx, connect.NewRequest(&v1.GetPlacementRequest{SelectedAsset: selected, SelectedServer: "nats-1", Range: &v1.TimeRange{Start: timestamppb.New(t0.Add(-time.Hour)), End: timestamppb.New(t0.Add(90 * time.Second))}})))
				o := res.Observation
				if o == nil || o.Samples != tc.samples || o.Boundary != tc.boundary || o.Truncated {
					t.Fatalf("observation=%v, want %d / %s", o, tc.samples, tc.boundary)
				}
			})
		}
	}
}
