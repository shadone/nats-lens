package api

import (
	"github.com/shadone/nats-lens/internal/eventselection"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// EventSelection validates explicit identities before they reach the native
// driver. Boundaries are navigation data, never authorization or a full snapshot.
func EventSelection(ids []string, boundary *int64) ([]int64, error) {
	values, err := eventselection.ValidateIDs(ids, boundary, 1000)
	if err != nil {
		return nil, invalid("%s", err)
	}
	return values, nil
}

// EventEntity reports only identities the event actually names. Kubernetes node
// identities have no NATS entity route; they stay in the recorded event payload.
func EventEntity(e *v1.Event) *v1.EntityRef {
	switch {
	case e.GetAccount() != "" && e.GetStream() != "" && e.GetConsumer() != "":
		return EntityRefOf(v1.EntityType_ENTITY_TYPE_CONSUMER, e.GetAccount()+"/"+e.GetStream()+"/"+e.GetConsumer())
	case e.GetAccount() != "" && e.GetStream() != "":
		return EntityRefOf(v1.EntityType_ENTITY_TYPE_STREAM, e.GetAccount()+"/"+e.GetStream())
	case e.GetAccount() != "":
		return EntityRefOf(v1.EntityType_ENTITY_TYPE_ACCOUNT, e.GetAccount())
	case e.GetServer() != "":
		return EntityRefOf(v1.EntityType_ENTITY_TYPE_SERVER, e.GetServer())
	case e.GetClientName() != "":
		return EntityRefOf(v1.EntityType_ENTITY_TYPE_CLIENT, e.GetClientName())
	}
	return nil
}

// EventBatchSelection bounds identity-only lookup before any database read.
func EventBatchSelection(ids []string, boundary *int64) error {
	if len(ids) == 0 || len(ids) > 50 {
		return invalid("select between 1 and 50 event identities")
	}
	_, err := EventSelection(ids, boundary)
	return err
}
