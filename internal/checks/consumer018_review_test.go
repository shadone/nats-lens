package checks

import (
 "context"
 "testing"
 "time"
 "github.com/shadone/nats-lens/internal/config"
)

func TestReview018HeldKnownQuorumLoss(t *testing.T) {
 f := degradedFixture(t, 4)
 set, err := Load(config.Checks{Timeout: config.Duration(10*time.Second)})
 if err != nil { t.Fatal(err) }
 eng := &Engine{Set:set, Interval:30*time.Second, only:map[string]bool{"consumer-018":true,"consumer-001":true}}
 if _,err := eng.Run(context.Background(),f.st,3);err!=nil {t.Fatal(err)}
 f.exec(`UPDATE consumer_replicas SET offline=true WHERE epoch=4 AND consumer='bad'`)
 f.exec(`UPDATE consumer_stats SET replicas_offline=2 WHERE epoch=4 AND consumer='bad'`)
 f.exec(`DELETE FROM consumer_stats WHERE epoch=4 AND consumer='bad' AND server_id<>'S0'`)
 runs,err := eng.Evaluate(context.Background(),f.st.DB(),4)
 if err != nil {t.Fatal(err)}
 for _,r := range runs {
  if r.Code=="consumer-018" && len(r.Rows)>0 { t.Errorf("known quorum loss retained warning: %+v",r.Rows) }
  if r.Code=="consumer-001" { t.Logf("quorum finding rows at first loss epoch: %d",len(r.Rows)) }
 }
}

func TestReview018Controls(t *testing.T) {
 for _,tc := range []struct{name,sql string; fire bool}{
  {"follower views",`INSERT INTO consumer_replicas SELECT * REPLACE ('S1' AS server_id, false AS current, 0 AS lag) FROM consumer_replicas WHERE server_id='S0' AND consumer='good'`,true},
  {"lag unknown",`UPDATE consumer_replicas SET lag=NULL WHERE consumer='bad'`,false},
  {"offline unknown",`UPDATE consumer_replicas SET offline=NULL WHERE consumer='bad'`,false},
  {"time backward",`UPDATE epochs SET started_at=TIMESTAMP '2026-10-17 08:00:00' WHERE epoch=2`,false},
  {"max individual gap",`UPDATE epochs SET started_at=started_at+INTERVAL 80 SECOND WHERE epoch=2`,false},
 } {
  t.Run(tc.name,func(t *testing.T){ f:=degradedFixture(t,3);f.exec(tc.sql);if tc.fire {f.fires("consumer-018",3,"APP/jobs/bad")}else{f.fires("consumer-018",3)} })
 }
}

func TestReview018RecreatedLeaderless(t *testing.T) {
 f:=degradedFixture(t,4)
 set,err:=Load(config.Checks{Timeout:config.Duration(10*time.Second)});if err!=nil {t.Fatal(err)}
 eng:=&Engine{Set:set,only:map[string]bool{"consumer-018":true},Interval:30*time.Second}
 if _,err:=eng.Run(context.Background(),f.st,3);err!=nil {t.Fatal(err)}
 f.exec(`UPDATE consumer_stats SET created=TIMESTAMP '2026-10-17 09:01:00', leader=NULL, is_leader=false WHERE epoch=4 AND consumer='bad'`)
 f.exec(`UPDATE consumer_config SET last_epoch=3 WHERE consumer='bad'`)
 f.exec(`INSERT INTO consumer_config SELECT * REPLACE (4 AS first_epoch,4 AS last_epoch,TIMESTAMP '2026-10-17 09:01:00' AS created) FROM consumer_config WHERE consumer='bad'`)
 f.fires("consumer-018",4)
}

func TestReview018HeldControls(t *testing.T) {
 for _,tc:=range []struct{name,sql string; hold bool}{
  {"unknown peers",`UPDATE consumer_replicas SET current=NULL WHERE epoch=4 AND consumer='bad'`,true},
  {"known healthy",`UPDATE consumer_replicas SET current=true,lag=0 WHERE epoch=4 AND consumer='bad'`,false},
  {"known incarnation leader",`UPDATE consumer_stats SET created=TIMESTAMP '2026-10-17 09:01:00' WHERE epoch=4 AND consumer='bad'`,false},
  {"missing config",`DELETE FROM consumer_config WHERE consumer='bad'`,true},
  {"known single",`UPDATE consumer_config SET replicas=1 WHERE consumer='bad'`,false},
 } {
  t.Run(tc.name,func(t *testing.T){
   f:=degradedFixture(t,4);set,err:=Load(config.Checks{});if err!=nil{t.Fatal(err)}
   eng:=&Engine{Set:set,only:map[string]bool{"consumer-018":true}}
   if _,err:=eng.Run(context.Background(),f.st,3);err!=nil{t.Fatal(err)}
   f.exec(tc.sql);if tc.hold {r:=f.fires("consumer-018",4,"APP/jobs/bad");if r["APP/jobs/bad"].Value!=nil{t.Fatal("unknown has value")}}else{f.fires("consumer-018",4)}
  })
 }
}
