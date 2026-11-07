package storeprov_test

import (
 "testing"
 "time"
 "connectrpc.com/connect"
 v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
 "google.golang.org/protobuf/types/known/timestamppb"
)

func TestReviewR2EarlierObservedAlias(t *testing.T) {
 st:=observationStore(t)
 for _,q:=range []string{
 `INSERT INTO stream_replicas(epoch,server_id,account,stream,peer_name,current,offline,lag) SELECT epoch,server_id,account,stream,'nats-2',false,false,0 FROM stream_replicas`,
 `INSERT INTO servers(server_id,server_name,first_epoch,last_epoch,first_seen_at,last_seen_at) SELECT 'S2','nats-2',1,3,first_seen_at,last_seen_at FROM servers WHERE server_id='S1'`,
 `INSERT INTO servers(server_id,server_name,first_epoch,last_epoch,first_seen_at,last_seen_at) SELECT 'new-S2','nats-2',2,2,first_seen_at,last_seen_at FROM servers WHERE server_id='S1'`,
 `INSERT INTO server_stats(epoch,server_id) VALUES (1,'S2'),(2,'new-S2'),(3,'S2')`,
 } {if _,err:=st.DB().Exec(q);err!=nil {t.Fatal(err)}}
 c:=serveSeries(t,st,t0.Add(90*time.Second))
 res:=must(c.GetPlacement(ctx,connect.NewRequest(&v1.GetPlacementRequest{SelectedAsset:"APP/orders",SelectedServer:"nats-1",Range:&v1.TimeRange{Start:timestamppb.New(t0),End:timestamppb.New(t0.Add(90*time.Second))}})))
 t.Logf("matrix=%v observation=%v",cellOn(asset(res,"APP/orders"),"nats-1"),res.Observation)
 if o:=res.Observation;o==nil||o.Samples!=1||o.Boundary!="identity_changed" {t.Fatalf("bridged actual observed peer process change from new-S2 to earlier S2: %v",o)}
}

func TestReviewR2EarlierObservedSelectedOfflineAlias(t *testing.T) {
 st:=observationStore(t)
 for _,q:=range []string{
 `DELETE FROM stream_stats WHERE server_id='S1'`,
 `UPDATE stream_replicas SET offline=true`,
 `INSERT INTO servers(server_id,server_name,first_epoch,last_epoch,first_seen_at,last_seen_at) SELECT 'new-S1','nats-1',2,2,first_seen_at,last_seen_at FROM servers WHERE server_id='S1'`,
 `UPDATE epoch_endpoints SET server_id='new-S1' WHERE epoch=2 AND server_id='S1'`,
 `INSERT INTO server_stats(epoch,server_id) VALUES (1,'S1'),(2,'new-S1'),(3,'S1')`,
 } {if _,err:=st.DB().Exec(q);err!=nil {t.Fatal(err)}}
 c:=serveSeries(t,st,t0.Add(90*time.Second))
 res:=must(c.GetPlacement(ctx,connect.NewRequest(&v1.GetPlacementRequest{SelectedAsset:"APP/orders",SelectedServer:"nats-1",Range:&v1.TimeRange{Start:timestamppb.New(t0),End:timestamppb.New(t0.Add(90*time.Second))}})))
 t.Logf("matrix=%v observation=%v",cellOn(asset(res,"APP/orders"),"nats-1"),res.Observation)
 if o:=res.Observation;o==nil||o.Samples!=1||o.Boundary!="identity_changed" {t.Fatalf("bridged actual observed selected offline process change from new-S1 to earlier S1: %v",o)}
}
