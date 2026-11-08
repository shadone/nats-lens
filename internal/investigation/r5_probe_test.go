package investigation

import (
 "testing"
 "encoding/json"
 "strings"
)

func TestR5ExplicitEventEntityNeedsUILimitation(t *testing.T) {
 c,_:=ContextFromURL("https://lens.example.com/events?system=shop&show=all","https://lens.example.com",fixtureNow())
 req,_,err:=Prepare("ListEvents",c,[]byte(`{"entity":{"type":"ENTITY_TYPE_ACCOUNT","key":"APP","account":"APP"}}`));if err!=nil {t.Fatal(err)}
 e,limits,err:=effectiveContext("ListEvents",c,req);if err!=nil {t.Fatal(err)}
 raw,_:=json.Marshal(e);again,err:=ParseContext(strings.NewReader(string(raw)),c.Deployment);if err!=nil {t.Fatal(err)}
 req2,_,err:=Prepare("ListEvents",again,nil);if err!=nil {t.Fatal(err)}
 var a,b any;json.Unmarshal(req,&a);json.Unmarshal(req2,&b)
 if len(limits)==0 {link,_:=e.Link(c.Deployment);t.Fatalf("Events UI has only server/stream/consumer selectors; account EntityRef cannot be replayed by %s; request=%s reimport=%s",link,req,req2)}
}
func TestR5ExplicitUnrepresentableSeverityNeedsUILimitation(t *testing.T) {
 c,_:=ContextFromURL("https://lens.example.com/findings?system=shop","https://lens.example.com",fixtureNow())
 req,_,err:=Prepare("ListFindings",c,[]byte(`{"severities":["SEVERITY_UNSPECIFIED"]}`));if err!=nil {t.Fatal(err)}
 e,limits,err:=effectiveContext("ListFindings",c,req);if err!=nil {t.Fatal(err)}
 if len(limits)==0 {link,_:=e.Link(c.Deployment);t.Fatalf("Findings UI drops unspecified severity and broadens this request; link=%s request=%s",link,req)}
}
