package storeprov

import (
	"testing"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func TestStatusOfListsEachCodeOnce(t *testing.T) {
	stream := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "A/s", Account: "A", Stream: "s"}
	f := func(c string) *v1.Finding {
		return &v1.Finding{CheckCode: "consumer-003", Severity: v1.Severity_SEVERITY_WARNING,
			Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: "A/s/" + c, Account: "A", Stream: "s", Consumer: c}}
	}
	_, codes := statusOf([]*v1.Finding{f("a"), f("b")}, stream)
	if len(codes) != 1 || codes[0] != "consumer-003" {
		t.Errorf("codes = %v, want [consumer-003]", codes)
	}
}
