package fake_test

import (
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func TestFakeKeyValueStores(t *testing.T) {
	c := client(t)
	res := must(c.ListKeyValueStores(ctx, connect.NewRequest(&v1.ListKeyValueStoresRequest{})))
	byName := map[string]*v1.KeyValueStore{}
	for _, kv := range res.GetKeyValueStores() {
		byName[kv.GetBucket()] = kv
	}
	cc := byName["customer-config"]
	if cc == nil || cc.GetEntity().GetStream() != "KV_customer-config" || cc.GetEntity().GetKey() != fake.Account+"/KV_customer-config" ||
		!cc.GetPresent() || cc.GetWatchers() != 35 || cc.GetReplicas() != 3 || len(cc.GetPlacement()) != 3 ||
		cc.GetPlacement()[0].GetState() != v1.PlacementState_PLACEMENT_STATE_LEADER || cc.Keys == nil {
		t.Fatalf("customer-config = %v", cc)
	}
	r1 := byName["worker-presence"]
	if r1.GetReplicas() != 1 || len(r1.GetPlacement()) != 1 || r1.GetStorage() != "memory" || r1.GetTtl().AsDuration() != 2*time.Minute {
		t.Errorf("worker-presence = %v", r1)
	}
	if !byName["order-jobs-cache"].GetAllowMsgTtl() {
		t.Errorf("order-jobs-cache = %v", byName["order-jobs-cache"])
	}
	// Every bucket's stream answers GetStream, so its row can link there.
	for _, kv := range res.GetKeyValueStores() {
		must(c.GetStream(ctx, connect.NewRequest(&v1.GetStreamRequest{Account: kv.GetEntity().GetAccount(), Stream: kv.GetEntity().GetStream()})))
	}
	objs := must(c.ListObjectStores(ctx, connect.NewRequest(&v1.ListObjectStoresRequest{}))).GetObjectStores()
	var names []string
	for _, o := range objs {
		names = append(names, o.GetBucket())
		if o.Chunks == nil || o.GetTtl().AsDuration() != 48*time.Hour || o.GetMaxBytes() != 8<<30 {
			t.Errorf("object store %v", o)
		}
	}
	if !slices.Equal(names, []string{"image-input", "image-output"}) {
		t.Errorf("object stores = %v", names)
	}
}

func TestFakeBucketsAccountScope(t *testing.T) {
	c := client(t)
	if kvs := must(c.ListKeyValueStores(ctx, connect.NewRequest(&v1.ListKeyValueStoresRequest{Account: "OPS"}))).GetKeyValueStores(); len(kvs) != 0 {
		t.Errorf("kv in OPS = %v", kvs)
	}
	if objs := must(c.ListObjectStores(ctx, connect.NewRequest(&v1.ListObjectStoresRequest{Account: "OPS"}))).GetObjectStores(); len(objs) != 0 {
		t.Errorf("object stores in OPS = %v", objs)
	}
	if n := len(must(c.ListKeyValueStores(ctx, connect.NewRequest(&v1.ListKeyValueStoresRequest{Account: fake.Account}))).GetKeyValueStores()); n != 3 {
		t.Errorf("kv in %s = %d, want 3", fake.Account, n)
	}
}

// example-global's R1 bucket lived on nats-us-4, which is down: it is
// missing, first, with its finding and no numbers.
func TestFakeGlobalMissingBucket(t *testing.T) {
	c := demo(t)
	kvs := must(c.ListKeyValueStores(ctx, connect.NewRequest(&v1.ListKeyValueStoresRequest{System: fake.GlobalSystem}))).GetKeyValueStores()
	if len(kvs) != 3 {
		t.Fatalf("global kv = %v", kvs)
	}
	m := kvs[0]
	if m.GetBucket() != "label-templates" || m.GetPresent() || !m.GetExpected() || m.GetStatus() != v1.Status_STATUS_WARNING ||
		!slices.Equal(m.GetFindingCodes(), []string{"stream-011"}) || m.Keys != nil || m.Watchers != nil || len(m.GetPlacement()) != 0 {
		t.Fatalf("missing bucket = %v", m)
	}
	fs := must(c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{System: fake.GlobalSystem}))).GetFindings()
	if !slices.ContainsFunc(fs, func(f *v1.Finding) bool { return f.GetId() == "stream-011:APP/KV_label-templates" }) {
		t.Errorf("no stream-011 finding among %d", len(fs))
	}
	for _, kv := range kvs[1:] {
		if kv.GetBucket() == "session-cache" && !kv.GetAllowMsgTtl() {
			t.Errorf("session-cache = %v", kv)
		}
	}
}

// The demo's missing bucket finding reads as the real stream-011 writes it.
func TestFakeMissingBucketFindingMatchesTheCheck(t *testing.T) {
	c := demo(t)
	f := must(c.GetFinding(ctx, connect.NewRequest(&v1.GetFindingRequest{System: fake.GlobalSystem, Id: "stream-011:APP/KV_label-templates"}))).GetFinding()
	want := "KV bucket label-templates (stream KV_label-templates) is in the inventory and no server reports it; " +
		"last reported by nats-us-4 at 2026-10-17 09:11:35 UTC (may be on nats-us-4, which did not answer JSZ). No delete event recorded."
	if f.GetDetail() != want || f.GetCheckName() != "Expected stream or bucket missing" {
		t.Errorf("detail = %q (%s)", f.GetDetail(), f.GetCheckName())
	}
	var keys []string
	for _, c := range f.GetRelatedConfig() {
		keys = append(keys, c.GetKey())
	}
	if !slices.Equal(keys, []string{"kind", "replicas", "max_age", "max_bytes", "max_msgs"}) || f.GetRelatedConfigSource() != "inventory" {
		t.Errorf("related config = %v", f.GetRelatedConfig())
	}
}

// A timeline row of stream-011 carries the check's own name.
func TestFakeTimelineNamesStream011(t *testing.T) {
	c := client(t)
	rows := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{}))).GetRows()
	seen := false
	for _, r := range rows {
		if r.GetCheckCode() == "stream-011" {
			seen = true
			if r.GetCheckName() != "Expected stream or bucket missing" {
				t.Errorf("stream-011 row named %q", r.GetCheckName())
			}
		}
	}
	if !seen {
		t.Fatal("no stream-011 row")
	}
}
