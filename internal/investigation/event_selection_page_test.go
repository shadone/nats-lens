package investigation

import (
 "testing"
 v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
 "google.golang.org/protobuf/proto"
)
func TestCapturedSelectionPageAdmissionAndAcknowledgement(t *testing.T){
 if Method("ListEventSelection")==nil{t.Fatal("bounded event selection unavailable to read-only agents")}
 boundary:=int64(3)
 request:=&v1.ListEventSelectionRequest{Ids:[]string{"1","2","3"},EventReadId:&boundary,Offset:1,PageSize:1}
 valid:=&v1.ListEventSelectionResponse{SelectedIds:request.Ids,EventReadId:&boundary,Events:[]*v1.Event{{Id:"2"}},TotalCount:1,SelectedTotalCount:3,Offset:1,PageSize:1,NextOffset:2}
 if err:=checkEventSelectionReply("ListEventSelection",request.ProtoReflect(),valid.ProtoReflect());err!=nil{t.Fatal(err)}
 for name,change:=range map[string]func(*v1.ListEventSelectionResponse){
 "ignored":func(r *v1.ListEventSelectionResponse){r.SelectedIds=nil},
 "wrong page":func(r *v1.ListEventSelectionResponse){r.Events[0].Id="1"},
 "duplicate":func(r *v1.ListEventSelectionResponse){r.Events=append(r.Events,r.Events[0]);r.TotalCount=2},
 "wrong offset":func(r *v1.ListEventSelectionResponse){r.Offset=0},
 "wrong size":func(r *v1.ListEventSelectionResponse){r.PageSize=50},
 "wrong next":func(r *v1.ListEventSelectionResponse){r.NextOffset=0},
 "wrong total":func(r *v1.ListEventSelectionResponse){r.SelectedTotalCount=2},
 "wrong count":func(r *v1.ListEventSelectionResponse){r.TotalCount=0},
 "missing boundary":func(r *v1.ListEventSelectionResponse){r.EventReadId=nil},
 }{t.Run(name,func(t *testing.T){r:=proto.Clone(valid).(*v1.ListEventSelectionResponse);change(r);if checkEventSelectionReply("ListEventSelection",request.ProtoReflect(),r.ProtoReflect())==nil{t.Fatal("broader or unacknowledged page accepted")}})}
}
