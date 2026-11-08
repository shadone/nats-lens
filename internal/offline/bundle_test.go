package offline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/dataset"
	"github.com/shadone/nats-lens/internal/investigation"
	"github.com/shadone/nats-lens/internal/store"
)

func TestOfflineCanonicalBundleParity(t *testing.T) {
	ctx := context.Background()
	path := closedFixture(t)
	st, err := store.OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err = os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(root, "capture")
	manifest, err := dataset.Acquire(ctx, st, bundle, dataset.System{ID: "shop", CollectionIntervalNS: int64(30 * time.Second)}, "fixture", dataset.Options{MaxBytes: 64 << 20})
	st.Close()
	if err != nil {
		t.Fatal(err)
	}
	bare, err := Open(ctx, investigation.OfflineSelection{DB: path, System: "shop", Interval: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer bare.Close()
	captured, err := Open(ctx, investigation.OfflineSelection{Bundle: bundle})
	if err != nil {
		t.Fatal(err)
	}
	defer captured.Close()
	for _, c := range []struct{ method, request string }{
		{"ListSystems", `{"stripBuckets":2}`}, {"ListMetrics", `{}`}, {"ListCheckDefinitions", `{}`}, {"GetOverview", `{}`},
		{"GetTimeSeries", `{"metric":"server.cpu_percent","entity":{"type":"ENTITY_TYPE_SERVER","key":"nats-0"}}`},
		{"ListEvents", `{"pageSize":1}`}, {"GetEvent", `{"id":"9007199254740993"}`}, {"ListFindings", `{}`}, {"GetFinding", `{"id":"server-001:nats-0"}`},
		{"GetCapacity", `{}`}, {"GetPlacement", `{}`}, {"GetDataHistory", `{}`},
	} {
		t.Run(c.method, func(t *testing.T) {
			a, ae := bare.Source.Query(ctx, c.method, []byte(c.request))
			b, be := captured.Source.Query(ctx, c.method, []byte(c.request))
			if ae != nil || be != nil || !reflect.DeepEqual(withoutRuntime(t, a), withoutRuntime(t, b)) {
				t.Fatalf("bare %s %v\nbundle %s %v", a, ae, b, be)
			}
		})
	}
	info, err := captured.Source.Query(ctx, "GetLensInfo", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var clock struct {
		Now     string `json:"now"`
		Version string `json:"version"`
	}
	if err = json.Unmarshal(info, &clock); err != nil {
		t.Fatal(err)
	}
	if clock.Now != "2026-10-17T14:29:01Z" || clock.Version != "fixture" || manifest.ExportedAt.Equal(*manifest.Boundaries.DataReference) {
		t.Fatalf("capture/export confused: %s %+v", info, manifest)
	}
	if _, err = captured.Source.Query(ctx, "GetLensInfo", []byte(`{"system":"other"}`)); err == nil {
		t.Fatal("bundle implies other system")
	}
}
