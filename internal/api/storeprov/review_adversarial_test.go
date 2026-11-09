package storeprov_test

import (
	"connectrpc.com/connect"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"
)

func TestReviewMissingNameCollision(t *testing.T) {
	st := observationStore(t)
	for _, q := range []string{
		`UPDATE servers SET last_epoch=3 WHERE server_id='S1'`,
		`DELETE FROM stream_stats WHERE server_id='S1'`,
		`UPDATE epoch_endpoints SET state='missing' WHERE server_id='S1' AND endpoint='JSZ'`,
		`INSERT INTO servers(server_id,server_name,first_epoch,last_epoch,first_seen_at,last_seen_at) VALUES ('other-S1','nats-1',2,3,TIMESTAMP '2026-10-17',TIMESTAMP '2026-10-17')`,
		`INSERT INTO server_stats(epoch,server_id) VALUES (2,'S1'),(3,'S1'),(2,'other-S1'),(3,'other-S1')`,
		`INSERT INTO epoch_endpoints VALUES (2,'other-S1','JSZ','ok',NULL),(3,'other-S1','JSZ','ok',NULL)`,
	} {
		if _, err := st.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	c := serveSeries(t, st, t0.Add(90*time.Second))
	res := must(c.GetPlacement(ctx, connect.NewRequest(&v1.GetPlacementRequest{SelectedAsset: "APP/orders", SelectedServer: "nats-1", Range: &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(90 * time.Second))}})))
	t.Logf("matrix=%v observation=%v", cellOn(asset(res, "APP/orders"), "nats-1"), res.Observation)
	if o := res.Observation; o != nil && o.Samples > 1 {
		t.Fatalf("claimed missing continuity despite ambiguous same-name processes and failed JSZ on original peer: %v", o)
	}
}
