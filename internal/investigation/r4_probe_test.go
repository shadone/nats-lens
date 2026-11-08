package investigation
import("testing";"encoding/json";"strings")
func TestR4EventsEffectiveKindScope(t *testing.T){
 c,e:=ContextFromURL("https://lens.example.com/events?system=shop&kind=%20finding.resolved%20,,%20finding.firing%20&show=all","https://lens.example.com",fixtureNow());if e!=nil{t.Fatal(e)}
 req,un,e:=Prepare("ListEvents",c,nil);if e!=nil{t.Fatal(e)}
 var r struct{Kinds []string};json.Unmarshal(req,&r)
 if strings.Join(r.Kinds,"|")!="finding.resolved|finding.firing"{t.Fatalf("UI trims and drops blank kinds; request=%s unapplied=%v",req,un)}
}
func TestR4ExplicitUnrepresentableKindScope(t *testing.T){
 c,_:=ContextFromURL("https://lens.example.com/events?system=shop&show=all","https://lens.example.com",fixtureNow())
 req,_,e:=Prepare("ListEvents",c,[]byte(`{"kinds":[" finding.resolved "]}`));if e!=nil{t.Fatal(e)}
 _,limits,e:=effectiveContext("ListEvents",c,req);if e!=nil{t.Fatal(e)}
 if !strings.Contains(strings.Join(limits,";"),"kind"){t.Fatalf("UI trims kind predicates; exact explicit request needs mismatch disclosure: %s limitations=%v",req,limits)}
}

func TestR4ReturnedConsumerContextRoundTrip(t *testing.T){
 c,_:=ContextFromURL("https://lens.example.com/consumers?system=shop&stream=APP/orders","https://lens.example.com",fixtureNow())
 before,_,e:=Prepare("ListConsumers",c,nil);if e!=nil{t.Fatal(e)}
 effective,_,e:=effectiveContext("ListConsumers",c,before);if e!=nil{t.Fatal(e)}
 b,_:=json.Marshal(effective);imported,e:=ParseContext(strings.NewReader(string(b)),c.Deployment);if e!=nil{t.Fatal(e)}
 after,un,e:=Prepare("ListConsumers",imported,nil);if e!=nil{t.Fatal(e)}
 var r struct{Stream string;Account string};json.Unmarshal(after,&r)
 if r.Stream!="orders"||r.Account!="APP"{t.Fatalf("effective context lost selected scope: before=%s after=%s unapplied=%v",before,after,un)}
}
