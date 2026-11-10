package api

import (
	"errors"
	"fmt"
	"testing"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func TestNormalizeAcceptMany(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	consumer := func(name string) *v1.EntityRef {
		return &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: "SHOP/order-jobs/" + name}
	}
	good := func() *v1.AcceptFindingsRequest {
		return &v1.AcceptFindingsRequest{CheckCode: "consumer-016", Reason: " one replica on purpose ", MutedBy: "op",
			Entities:  []*v1.EntityRef{consumer("a"), consumer("b")},
			ExpiresAt: tsp(now.Add(90*24*time.Hour + 500*time.Millisecond))}
	}
	m := good()
	if err := normalizeAcceptMany(m, now, "iap@example.com"); err != nil {
		t.Fatal(err)
	}
	if m.GetReason() != "one replica on purpose" || m.GetMutedBy() != "iap@example.com" || m.GetExpiresAt().AsTime() != now.Add(90*24*time.Hour) {
		t.Fatalf("normalized = %v", m)
	}
	var many []*v1.EntityRef
	for i := range MaxAcceptBatch + 1 {
		many = append(many, consumer(fmt.Sprint(i)))
	}
	m = good()
	m.Entities = many[:MaxAcceptBatch]
	if err := normalizeAcceptMany(m, now, ""); err != nil {
		t.Fatalf("%d entities: %v", MaxAcceptBatch, err)
	}
	bad := map[string]func(*v1.AcceptFindingsRequest){
		"no entity": func(m *v1.AcceptFindingsRequest) { m.Entities = nil },
		"too many":  func(m *v1.AcceptFindingsRequest) { m.Entities = many },
		"twice":     func(m *v1.AcceptFindingsRequest) { m.Entities = []*v1.EntityRef{consumer("a"), consumer("a")} },
		"system": func(m *v1.AcceptFindingsRequest) {
			m.Entities[1] = &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SYSTEM}
		},
		"empty entity":        func(m *v1.AcceptFindingsRequest) { m.Entities[1] = &v1.EntityRef{} },
		"not acceptable":      func(m *v1.AcceptFindingsRequest) { m.CheckCode = "consumer-001" },
		"review under a day":  func(m *v1.AcceptFindingsRequest) { m.ExpiresAt = tsp(now.Add(23 * time.Hour)) },
		"review past a year":  func(m *v1.AcceptFindingsRequest) { m.ExpiresAt = tsp(now.Add(MaxAcceptLength + time.Second)) },
		"no review date":      func(m *v1.AcceptFindingsRequest) { m.ExpiresAt = nil },
		"blank reason":        func(m *v1.AcceptFindingsRequest) { m.Reason = " " },
		"no name without IAP": func(m *v1.AcceptFindingsRequest) { m.MutedBy = "" },
	}
	for name, change := range bad {
		m := good()
		change(m)
		if err := normalizeAcceptMany(m, now, ""); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
