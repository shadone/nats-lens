package investigation
import("testing";"encoding/json";"strings")
func TestR3ConnectionsNameToolingScope(t *testing.T){
 for _,suffix:=range []string{"","&tooling=0","&tooling=1"}{
 c,e:=ContextFromURL("https://lens.example.com/connections?system=shop&name=natslens"+suffix,"https://lens.example.com",fixtureNow());if e!=nil{t.Fatal(e)}
 req,un,e:=Prepare("ListConnections",c,nil);if e!=nil{t.Fatal(e)}
 var r struct{IncludeTooling bool};json.Unmarshal(req,&r)
 if !r.IncludeTooling{t.Errorf("UI name search includes tooling, request=%s unapplied=%v",req,un)}
 }
}
func TestR3ExplicitToolingExclusionLinkDisclosure(t *testing.T){
 c,_:=ContextFromURL("https://lens.example.com/connections?system=shop&name=natslens","https://lens.example.com",fixtureNow())
 req,_,e:=Prepare("ListConnections",c,[]byte(`{"includeTooling":false}`));if e!=nil{t.Fatal(e)}
 _,limits,e:=effectiveContext("ListConnections",c,req);if e!=nil{t.Fatal(e)}
 if !strings.Contains(strings.Join(limits,";"),"tooling"){t.Fatalf("UI always includes tooling for name search; explicit exclusion needs mismatch disclosure: request=%s limitations=%v",req,limits)}
}
