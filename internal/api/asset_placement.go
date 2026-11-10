package api

import (
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// MaxAssetPlacement is the number of entities one ListAssetPlacement call
// may ask for.
const MaxAssetPlacement = 200

// checkAssetPlacement checks a ListAssetPlacement request's entities:
// at most MaxAssetPlacement, each a stream or a consumer with a key of its
// type's parts.
func checkAssetPlacement(m *v1.ListAssetPlacementRequest) error {
	if err := maxLen("entities", len(m.GetEntities()), MaxAssetPlacement); err != nil {
		return err
	}
	for _, e := range m.GetEntities() {
		switch e.GetType() {
		case v1.EntityType_ENTITY_TYPE_STREAM, v1.EntityType_ENTITY_TYPE_CONSUMER:
		default:
			return invalid("entities: only streams and consumers have a placement, not %s", e.GetType())
		}
		if err := checkEntity("entities", e, false); err != nil {
			return err
		}
	}
	return nil
}

// DistinctAssets is the request's entities without repeats, in the order
// first asked, as references built from their type and key.
func DistinctAssets(m *v1.ListAssetPlacementRequest) []*v1.EntityRef {
	seen := map[string]bool{}
	var out []*v1.EntityRef
	for _, e := range m.GetEntities() {
		k := e.GetType().String() + "\x00" + e.GetKey()
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, EntityRefOf(e.GetType(), e.GetKey()))
	}
	return out
}
