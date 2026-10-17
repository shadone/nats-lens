package inventory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/store"
)

func TestLoadExampleFiles(t *testing.T) {
	for _, p := range []string{"../../inventory.example.yaml", "../../scripts/inventory.dev.yaml"} {
		rows, err := Load(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if len(rows) < 5 {
			t.Fatalf("%s: %d rows", p, len(rows))
		}
	}
	rows, _ := Load("../../inventory.example.yaml")
	byKey := map[string]store.InventoryRow{}
	for _, r := range rows {
		byKey[r.Kind+":"+r.Stream+"/"+r.Consumer] = r
	}
	consumers := 0
	for _, r := range rows {
		if r.Kind == "consumer" {
			consumers++
		}
	}
	if consumers != 11 {
		t.Errorf("example lists %d durables, want 11", consumers)
	}
	pool := byKey["consumer:media-windows/media-chunk-pool"]
	if pool.AckWait == nil || *pool.AckWait != 30*time.Second || pool.MaxAckPending == nil || *pool.MaxAckPending != 1000 {
		t.Errorf("media-chunk-pool = %+v", pool)
	}
	kv := byKey["kv:KV_worker-presence/"]
	if kv.Name != "worker-presence" || kv.MaxMsgsPerSubject == nil || *kv.MaxMsgsPerSubject != 10 || *kv.MaxAge != 120*time.Second {
		t.Errorf("worker-presence = %+v", kv)
	}
	obj := byKey["object_store:OBJ_image-input/"]
	if obj.MaxBytes == nil || *obj.MaxBytes != 8<<30 {
		t.Errorf("image-input = %+v", obj)
	}
	archive := byKey["stream:order-archive/"]
	if archive.MaxBytes == nil || *archive.MaxBytes != -1 || archive.MaxAge == nil || *archive.MaxAge != 0 {
		t.Errorf("order-archive = %+v", archive)
	}
}

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "inventory.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadRejects(t *testing.T) {
	for name, tc := range map[string]struct{ body, err string }{
		"unknown key":             {"accounts:\n  - name: A\n    streams:\n      - name: s\n        max_agee: 1h\n", "max_agee"},
		"bad retention":           {"accounts:\n  - name: A\n    streams:\n      - name: s\n        retention: forever\n", `retention "forever"`},
		"bad bytes":               {"accounts:\n  - name: A\n    kv:\n      - name: b\n        max_bytes: 8GB\n", "not a byte size"},
		"duplicate":               {"accounts:\n  - name: A\n    streams:\n      - name: s\n      - name: s\n", "listed twice"},
		"no name":                 {"accounts:\n  - streams: []\n", "no name"},
		"history on object store": {"accounts:\n  - name: A\n    object_stores:\n      - name: o\n        history: 2\n", "KV buckets only"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(write(t, tc.body)); err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Fatalf("err = %v, want %q", err, tc.err)
			}
		})
	}
}

// The rows land in the inventory table, and loading again replaces them.
func TestReplaceInventory(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	p := write(t, "accounts:\n  - name: APP\n    streams:\n      - name: jobs\n        consumers:\n          - name: w\n            filter: [b.>, a.>]\n")
	rows, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := st.ReplaceInventory(ctx, rows, p, now); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceInventory(ctx, rows, p, now); err != nil {
		t.Fatal(err)
	}
	var n int
	var filters string
	if err := st.DB().QueryRow(`SELECT count(*), max(CAST(filter_subjects AS VARCHAR)) FROM inventory`).Scan(&n, &filters); err != nil {
		t.Fatal(err)
	}
	if n != 2 || filters != "[a.>, b.>]" {
		t.Fatalf("rows %d, filters %s", n, filters)
	}
	if err := st.ReplaceInventory(ctx, nil, "", now); err != nil {
		t.Fatal(err)
	}
	if err := st.DB().QueryRow(`SELECT count(*) FROM inventory`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("after an empty load: %d %v", n, err)
	}
}
