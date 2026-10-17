package api

import (
	"strconv"
	"strings"
	"unicode/utf8"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// keyParts is the number of slash-separated parts of each entity type's
// key; zero means the key is one free-form part.
var keyParts = map[v1.EntityType]int{
	v1.EntityType_ENTITY_TYPE_SERVER:     1,
	v1.EntityType_ENTITY_TYPE_ACCOUNT:    1,
	v1.EntityType_ENTITY_TYPE_CLIENT:     0,
	v1.EntityType_ENTITY_TYPE_STREAM:     2,
	v1.EntityType_ENTITY_TYPE_CONSUMER:   3,
	v1.EntityType_ENTITY_TYPE_CONNECTION: 2,
}

// checkEntity checks an entity reference used as a filter or a mute
// target. An unset reference passes when optional is set. The type must be
// a known one, the system entity has no key, and other keys have the parts
// of their type (a connection key is server/cid with a numeric cid).
func checkEntity(field string, e *v1.EntityRef, optional bool) error {
	if e == nil || (optional && e.GetType() == v1.EntityType_ENTITY_TYPE_UNSPECIFIED && e.GetKey() == "") {
		if optional {
			return nil
		}
		return invalid("%s is required", field)
	}
	t, key := e.GetType(), e.GetKey()
	if t == v1.EntityType_ENTITY_TYPE_SYSTEM {
		if key != "" {
			return invalid("%s: the system entity has no key", field)
		}
		return nil
	}
	parts, ok := keyParts[t]
	if !ok {
		return invalid("%s: unsupported entity type %d", field, int32(t))
	}
	if key == "" || utf8.RuneCountInString(key) > MaxNameLength {
		return invalid("%s: key must have 1 to %d characters", field, MaxNameLength)
	}
	if parts == 0 {
		return nil
	}
	split := strings.Split(key, "/")
	if len(split) != parts || containsEmpty(split) {
		return invalid("%s: a %s key has %d non-empty parts separated by /", field, t, parts)
	}
	if t == v1.EntityType_ENTITY_TYPE_CONNECTION {
		if _, err := strconv.ParseUint(split[1], 10, 64); err != nil {
			return invalid("%s: a connection key is server/cid with a numeric cid", field)
		}
	}
	return nil
}

func containsEmpty(s []string) bool {
	for _, x := range s {
		if x == "" {
			return true
		}
	}
	return false
}
