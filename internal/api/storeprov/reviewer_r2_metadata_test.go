package storeprov

import (
	"context"
	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"testing"
)

func TestReviewerHiddenBucketMetadataBound(t *testing.T) {
	p, _, r := pageScaleProvider(t, api.MaxListRankingRows+2)
	if _, err := p.St.DB().Exec(`UPDATE stream_stats SET stream='KV_' || stream WHERE stream <> 'orders-00000'`); err != nil {
		t.Fatal(err)
	}
	reps, err := p.streamReplicas(context.Background(), 37, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.ListStreams(context.Background(), &v1.ListStreamsRequest{Range: r, Limit: 1, SummaryOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("metadata map has %d identities; returned count=%d", len(reps), res.TotalCount)
	if len(reps) > api.MaxListRankingRows {
		t.Fatalf("metadata read bypassed ranking cap: %d identities for %d match", len(reps), res.TotalCount)
	}
}
