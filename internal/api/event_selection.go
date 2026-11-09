package api

import (
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"strconv"
)

// EventSelection validates explicit identities before they reach the native
// driver. Boundaries are navigation data, never authorization or a full snapshot.
func EventSelection(ids []string, boundary *int64) ([]int64, error) {
	if boundary != nil && *boundary < 0 {
		return nil, invalid("event membership boundary must not be negative")
	}
	if len(ids) > 1000 {
		return nil, invalid("at most 1000 selected event IDs are allowed")
	}
	out := make([]int64, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil || n <= 0 || strconv.FormatInt(n, 10) != id || seen[n] {
			return nil, invalid("selected event IDs must be unique positive decimal int64 values")
		}
		seen[n] = true
		out = append(out, n)
	}
	return out, nil
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
