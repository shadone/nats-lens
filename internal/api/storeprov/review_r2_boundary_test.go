package storeprov

import (
 "testing"
 "time"
 v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func TestReviewR2GapThreshold(t *testing.T) {
 for _,tc:=range []struct{name string;gap time.Duration;want int32;boundary string}{
  {"exactly twice interval",60*time.Second,3,"range_limit"},
  {"over twice interval",60*time.Second+time.Nanosecond,1,"collection_gap"},
  {"equal timestamps",0,1,"collection_gap"},
  {"reversed timestamps",-time.Second,1,"collection_gap"},
 } {t.Run(tc.name,func(t *testing.T){
  ss:=observedSamples(v1.PlacementState_PLACEMENT_STATE_BEHIND)
  ss[1].at=ss[0].at.Add(-tc.gap);ss[2].at=ss[1].at.Add(-30*time.Second)
  o:=observePlacement(ss,"APP/orders","nats-1","range_limit",30*time.Second)
  if o==nil||o.Samples!=tc.want||o.Boundary!=tc.boundary {t.Fatalf("observation=%v",o)}
 })}
}
