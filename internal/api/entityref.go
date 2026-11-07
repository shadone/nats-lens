package api

import (
	"strings"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// EntityRefOf is the reference of an entity of type t with key, as every
// response carries it: a server, stream or consumer with its parts, an
// account with the account set (the handler then fills account_name), the
// system without a key, anything else named by its key. Findings, mutes
// and the timeline build their references with it, in the store and the
// demo alike.
func EntityRefOf(t v1.EntityType, key string) *v1.EntityRef {
	parts := strings.Split(key, "/")
	r := &v1.EntityRef{Type: t, Key: key, Name: key}
	switch {
	case t == v1.EntityType_ENTITY_TYPE_SYSTEM:
		return &v1.EntityRef{Type: t}
	case t == v1.EntityType_ENTITY_TYPE_SERVER:
		r.Server = key
	case t == v1.EntityType_ENTITY_TYPE_ACCOUNT:
		r.Account = key
	case t == v1.EntityType_ENTITY_TYPE_STREAM && len(parts) == 2:
		r.Account, r.Stream, r.Name = parts[0], parts[1], parts[1]
	case t == v1.EntityType_ENTITY_TYPE_CONSUMER && len(parts) == 3:
		r.Account, r.Stream, r.Consumer, r.Name = parts[0], parts[1], parts[2], parts[2]
	}
	return r
}
