package storeprov
import (
 "context"
 "testing"
 "errors"
 "github.com/shadone/nats-lens/internal/api"
 v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)
func TestReviewerHistoricalRankingReadLimit(t *testing.T) {
 p, _, r := pageScaleProvider(t, api.MaxListRankingRows+1)
 for _, q := range []string{
 `DELETE FROM consumer_stats WHERE epoch=37 AND consumer <> 'worker-00000'`,
 `UPDATE epochs SET started_at=TIMESTAMP '2026-10-17 13:58:00' WHERE epoch=35`,
 `UPDATE epochs SET started_at=TIMESTAMP '2026-10-17 13:59:00' WHERE epoch=36`,
 } { if _,err:=p.St.DB().Exec(q);err!=nil {t.Fatal(err)} }
 rates, err:=p.deliveredRates(context.Background(),r,"","","")
 if err!=nil {t.Fatal(err)}
 t.Logf("materialized %d rate-ranking entries for one current consumer",len(rates))
 res,err:=p.ListConsumers(context.Background(),&v1.ListConsumersRequest{Range:r,Limit:1})
 if !errors.Is(err,api.ErrResourceExhausted) {t.Fatalf("expected bounded ranking rejection, got total=%d error=%v",res.GetTotalCount(),err)}
}
