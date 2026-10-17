package storeprov_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
	"github.com/shadone/nats-lens/internal/inventory"
	"github.com/shadone/nats-lens/internal/store"
)

// bucketInventory lists the fixture's KV_stock-levels, a KV bucket and an
// object store that no server reports, and the object store the test adds.
const bucketInventory = `accounts:
  - name: APP
    kv:
      - name: stock-levels
        history: 1
      - name: label-templates
        replicas: 1
    object_stores:
      - name: image-input
      - name: image-output
`

// bucketsClient is the fixture with the bucket inventory, two more watchers on
// KV_stock-levels, an object store OBJ_image-output on nats-0, a stream
// KVX_foo that only looks like a bucket, and stream-011 firing for the
// missing KV bucket.
func bucketsClient(t *testing.T) natslensv1connect.LensServiceClient {
	t.Helper()
	st := fixtureStore(t)
	path := filepath.Join(t.TempDir(), "inventory.yaml")
	if err := os.WriteFile(path, []byte(bucketInventory), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := inventory.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceInventory(ctx, rows, path, t0); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		// The same watcher reported by both servers counts once.
		`INSERT INTO consumer_stats (epoch, server_id, account, stream, consumer) VALUES
			(3, 'NFAKESERVER0ID', '` + account + `', 'KV_stock-levels', 'oc_w1'),
			(3, 'NFAKESERVER1NEW', '` + account + `', 'KV_stock-levels', 'oc_w1'),
			(3, 'NFAKESERVER0ID', '` + account + `', 'KV_stock-levels', 'oc_w2')`,
		`INSERT INTO stream_stats (epoch, server_id, account, stream, messages, bytes, num_subjects, is_leader, leader) VALUES
			(3, 'NFAKESERVER0ID', '` + account + `', 'OBJ_image-output', 40, 5242880, 4, true, 'nats-0'),
			(3, 'NFAKESERVER0ID', '` + account + `', 'KVX_foo', 1, 10, 1, true, 'nats-0')`,
		// A bucket whose server counts 4 consumers, none of them in the
		// detail: its watchers were not observed.
		`INSERT INTO stream_stats (epoch, server_id, account, stream, messages, bytes, num_subjects, is_leader, leader, consumer_count) VALUES
			(3, 'NFAKESERVER0ID', '` + account + `', 'KV_sessions', 2, 20, 2, true, 'nats-0', 4)`,
		`INSERT INTO stream_config (server_id, account, stream, first_epoch, last_epoch, config_hash, description, storage,
			replicas, max_bytes, max_age_ns, sealed) VALUES
			('NFAKESERVER0ID', '` + account + `', 'OBJ_image-output', 1, 3, 'h1', 'resized images', 'file', 1, 1073741824,
			 172800000000000, true)`,
	} {
		if _, err := st.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for epoch := int64(1); epoch <= 3; epoch++ {
		run := store.CheckRun{Code: "stream-011", EntityType: "stream", Status: store.RunOK, Rows: []store.FindingRow{{
			EntityKey: account + "/KV_label-templates", Severity: "critical",
			Detail: "KV bucket label-templates (stream KV_label-templates) is in the inventory and no server reports it.",
		}}}
		if _, err := st.ApplyCheckRuns(ctx, epoch, []store.CheckRun{run}); err != nil {
			t.Fatal(err)
		}
	}
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Clock: func() time.Time { return now }})
	return c
}

func placementOf(ps []*v1.ReplicaPlacement) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.GetServer()+":"+p.GetState().String())
	}
	return out
}

func TestListKeyValueStores(t *testing.T) {
	c := bucketsClient(t)
	res := must(c.ListKeyValueStores(ctx, connect.NewRequest(&v1.ListKeyValueStoresRequest{Range: hour()})))
	byName := map[string]*v1.KeyValueStore{}
	var names []string
	for _, kv := range res.GetKeyValueStores() {
		byName[kv.GetBucket()] = kv
		names = append(names, kv.GetBucket())
	}
	// The missing bucket's critical finding puts it first; KVX_foo is not a
	// bucket.
	if want := []string{"label-templates", "sessions", "stock-levels"}; !slices.Equal(names, want) {
		t.Fatalf("buckets = %v, want %v", names, want)
	}

	kv := byName["stock-levels"]
	if kv.GetEntity().GetKey() != account+"/KV_stock-levels" || kv.GetEntity().GetStream() != "KV_stock-levels" {
		t.Errorf("entity = %v", kv.GetEntity())
	}
	if !kv.GetPresent() || !kv.GetExpected() || kv.GetHistory() != 1 || kv.GetReplicas() != 3 || kv.GetStorage() != "file" ||
		kv.GetKeys() != 6 || kv.GetMessages() != 6 || kv.GetBytes() != 853 || kv.GetMaxBytes() != -1 || kv.GetTtl().AsDuration() != 0 ||
		kv.Ttl == nil || kv.GetCompression() != "none" || kv.GetAllowMsgTtl() || kv.GetDescription() == "" {
		t.Errorf("stock-levels = %v", kv)
	}
	// The fixture's ordered consumer on nats-1 and the two added watchers.
	if kv.Watchers == nil || kv.GetWatchers() != 3 {
		t.Errorf("watchers = %d, want 3", kv.GetWatchers())
	}
	// nats-2 is listed by the leader but reports nothing.
	if got, want := placementOf(kv.GetPlacement()), []string{
		"nats-0:PLACEMENT_STATE_LEADER", "nats-1:PLACEMENT_STATE_IN_SYNC", "nats-2:PLACEMENT_STATE_MISSING",
	}; !slices.Equal(got, want) {
		t.Errorf("placement = %v, want %v", got, want)
	}

	if w := byName["sessions"].Watchers; w != nil {
		t.Errorf("sessions watchers = %d, want unset", *w)
	}

	missing := byName["label-templates"]
	if missing.GetPresent() || !missing.GetExpected() || missing.GetStatus() != v1.Status_STATUS_CRITICAL ||
		!slices.Equal(missing.GetFindingCodes(), []string{"stream-011"}) || missing.GetEntity().GetKey() != account+"/KV_label-templates" {
		t.Errorf("label-templates = %v", missing)
	}
	// Nothing about a missing bucket was observed.
	if missing.Keys != nil || missing.Bytes != nil || missing.Watchers != nil || missing.Replicas != nil || missing.Ttl != nil || len(missing.GetPlacement()) != 0 {
		t.Errorf("label-templates has numbers: %v", missing)
	}
}

func TestListObjectStores(t *testing.T) {
	c := bucketsClient(t)
	res := must(c.ListObjectStores(ctx, connect.NewRequest(&v1.ListObjectStoresRequest{Range: hour()})))
	byName := map[string]*v1.ObjectStore{}
	for _, o := range res.GetObjectStores() {
		byName[o.GetBucket()] = o
	}
	if len(byName) != 2 {
		t.Fatalf("object stores = %v", res.GetObjectStores())
	}
	o := byName["image-output"]
	if !o.GetPresent() || !o.GetExpected() || o.GetChunks() != 40 || o.GetBytes() != 5242880 || o.GetReplicas() != 1 ||
		o.GetStorage() != "file" || o.GetMaxBytes() != 1<<30 || o.GetTtl().AsDuration() != 48*time.Hour || !o.GetSealed() ||
		o.GetDescription() != "resized images" {
		t.Errorf("image-output = %v", o)
	}
	if got := placementOf(o.GetPlacement()); !slices.Equal(got, []string{"nats-0:PLACEMENT_STATE_LEADER"}) {
		t.Errorf("placement = %v", got)
	}
	// No finding names the missing image-input, so its status is unknown.
	in := byName["image-input"]
	if in.GetPresent() || !in.GetExpected() || in.GetStatus() != v1.Status_STATUS_NO_DATA || in.Chunks != nil {
		t.Errorf("image-input = %v", in)
	}
}

func TestBucketsAccountScope(t *testing.T) {
	c := bucketsClient(t)
	if n := len(must(c.ListKeyValueStores(ctx, connect.NewRequest(&v1.ListKeyValueStoresRequest{Range: hour(), Account: account}))).GetKeyValueStores()); n != 3 {
		t.Errorf("kv in %s: %d, want 3", account, n)
	}
	if kvs := must(c.ListKeyValueStores(ctx, connect.NewRequest(&v1.ListKeyValueStoresRequest{Range: hour(), Account: "OTHER"}))).GetKeyValueStores(); len(kvs) != 0 {
		t.Errorf("kv in OTHER: %v", kvs)
	}
	if objs := must(c.ListObjectStores(ctx, connect.NewRequest(&v1.ListObjectStoresRequest{Range: hour(), Account: "OTHER"}))).GetObjectStores(); len(objs) != 0 {
		t.Errorf("object stores in OTHER: %v", objs)
	}
}
