package fake

import (
	"strings"
	"testing"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// A system whose servers report no storage limit has no limit and no
// share used: never "NaN% used".
func TestStorageTileWithoutLimits(t *testing.T) {
	sc := prodScenario()
	for i := range sc.servers {
		sc.servers[i].maxStore = 0
	}
	s := scen{now: FixedNow, sc: sc}
	r := &v1.TimeRange{Start: ts(FixedNow.Add(-24 * time.Hour)), End: ts(FixedNow)}
	for _, tile := range s.overviewTiles(r, 96, nil, nil) {
		if tile.GetId() != "storage" {
			continue
		}
		if tile.Limit != nil || tile.GetValueSuffix() != "" || strings.Contains(tile.GetSubtitle(), "NaN") ||
			!strings.HasPrefix(tile.GetSubtitle(), "all replicas") {
			t.Fatalf("storage = limit %v, suffix %q, subtitle %q", tile.Limit, tile.GetValueSuffix(), tile.GetSubtitle())
		}
		return
	}
	t.Fatal("no storage tile")
}
