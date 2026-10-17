package storeprov

import (
	"testing"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// A stream finding sets the tile of its kind of stream only: a KV
// bucket's the KV tile, a plain stream's the streams tile.
func TestStreamFindingsGoToTheirKindsTile(t *testing.T) {
	tiles := []*v1.Tile{{Id: "streams", Value: ptr(3.0)}, {Id: "kv", Value: ptr(1.0)}, {Id: "object_stores", Value: ptr(1.0)},
		{Id: "accounts", Value: ptr(2.0)}}
	cur := []*v1.Finding{{CheckCode: "stream-003", Severity: v1.Severity_SEVERITY_WARNING,
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Stream: "KV_customer-config"}}}
	tileStatus(tiles, cur, func(...string) bool { return true })
	want := []v1.Status{v1.Status_STATUS_OK, v1.Status_STATUS_WARNING, v1.Status_STATUS_OK, v1.Status_STATUS_NO_DATA}
	for i, tile := range tiles {
		if tile.GetStatus() != want[i] {
			t.Errorf("tile %s = %v, want %v", tile.GetId(), tile.GetStatus(), want[i])
		}
	}
}
