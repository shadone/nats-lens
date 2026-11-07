package storeprov_test

import (
 "strings"
 "testing"

 "github.com/shadone/nats-lens/internal/api"
)

func TestReplayObservedClientLibraries(t *testing.T) {
 for _, tc := range []struct{name, mutation string; change bool}{
  {"rolling versions", "", true},
  {"unknown version", "UPDATE connections SET version='0.0.0' WHERE epoch=40", false},
  {"new application", "DELETE FROM connections WHERE epoch=3", false},
  {"renamed application", "UPDATE connections SET name='renamed@pod' WHERE epoch=40", false},
  {"different account", "UPDATE connections SET account='OTHER' WHERE epoch=40", false},
  {"unnamed", "UPDATE connections SET name=''", false},
  {"same versions restart", "UPDATE connections SET version='1.0'", false},
  {"cut list", "UPDATE server_stats SET connz_returned=0 WHERE epoch=40", false},
  {"missing connz", "DELETE FROM epoch_endpoints WHERE epoch=40 AND endpoint='CONNZ'", false},
 } { t.Run(tc.name, func(t *testing.T) {
  p,r:=replayStore(t)
  // Supply complete CONNZ coverage at both comparison epochs; server restarts
  // change process IDs, but application/account identity remains stable.
  for _, e:= range []struct{n int; ids []string}{{3,[]string{"S0","S1OLD","S2"}}, {40,[]string{"S0","S1NEW"}}} {
   for _,id:=range e.ids {
    r.add("epoch_endpoints",map[string]any{"epoch":e.n,"server_id":id,"endpoint":"CONNZ","state":"ok"})
   }
  }
  if _,err:=r.st.DB().Exec(`UPDATE epochs SET expected_servers=observed_servers, expected_names=observed_names WHERE epoch IN (3,40); UPDATE server_stats SET connz_total=1,connz_returned=1 WHERE epoch IN (3,40)`);err!=nil {t.Fatal(err)}
  for i,e:=range []int{3,40,40} {
   version:="1.0"; if i==2 {version="1.1"}
   r.add("connections",map[string]any{"epoch":e,"server_id":"S0","cid":i+1,"name":"checkout@pod","account":rAcc,"lang":"go","version":version,"ip":"10.0.0.1"})
  }
  if tc.mutation!="" {if _,err:=r.st.DB().Exec(tc.mutation);err!=nil {t.Fatal(err)}}
  facts,err:=p.Replay(t.Context(),replayReq(rEpoch(3),rEpoch(40)));if err!=nil {t.Fatal(err)}
  res:=api.FinishReplay(facts)
  var body string
  for _,s:=range res.Steps {if strings.Contains(s.Title,"checkout client-library set differs") {body=s.Body; if len(s.Evidence)<2 {t.Fatal("missing collection evidence")}}}
  if (body!="")!=tc.change {t.Fatalf("change=%v, body=%q",tc.change,body)}
  if tc.change {for _,want:=range []string{"go 1.0 (1)","go 1.1 (1)",rAcc,"not evidence of an application deployment","system-wide","not a causal link"} {if !strings.Contains(body,want) {t.Errorf("missing %q in %s",want,body)}}}
 }) }
}
