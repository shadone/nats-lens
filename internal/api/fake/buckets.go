package fake

import (
	"context"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// bucketSpec is the configuration of a KV bucket or object store that the
// scenario's inventory lists. A present bucket's numbers come from the
// stream of the same name among the scenario's streams; a missing one is
// in no stream and has no numbers.
type bucketSpec struct {
	stream      string
	description string
	storage     string // "" is file
	history     int32  // KV only; 0 is 1
	ttl         time.Duration
	maxBytes    int64 // 0 is -1, unlimited
	compression string
	allowMsgTTL bool
	sealed      bool
	// keys is the KV bucket's subjects; 0 means its messages.
	keys int64
	// missing is a bucket the inventory lists that no server reports.
	missing bool
	// listed is set by bucketSpecs for a bucket the scenario lists.
	listed bool
}

// bucketSpecs are the scenario's buckets of one prefix: one per KV_ or
// OBJ_ stream, with its listed spec where the scenario gives one, and then
// the missing ones.
func (sc *scenario) bucketSpecs(prefix string) []bucketSpec {
	given := map[string]bucketSpec{}
	for _, b := range sc.buckets {
		b.listed = true
		given[b.stream] = b
	}
	var out []bucketSpec
	for _, sp := range sc.streams {
		if !strings.HasPrefix(sp.name, prefix) {
			continue
		}
		b, ok := given[sp.name]
		if !ok {
			b = bucketSpec{stream: sp.name}
		}
		out = append(out, b)
	}
	for _, b := range sc.buckets {
		if b.missing && strings.HasPrefix(b.stream, prefix) {
			b.listed = true
			out = append(out, b)
		}
	}
	return out
}

// bucketView is what both kinds of bucket share.
type bucketView struct {
	spec      bucketSpec
	ref       *v1.EntityRef
	bucket    string
	sp        streamSpec
	status    v1.Status
	codes     []string
	placement []*v1.ReplicaPlacement
	growth    *float64
}

func (v bucketView) present() bool { return !v.spec.missing }

// bucketViews lists the buckets of one prefix in the account (all for ""),
// worst first.
func (p *Provider) bucketViews(r *v1.TimeRange, account, prefix string) []bucketView {
	if account != "" && account != Account {
		return nil
	}
	s := p.scen()
	cur := current(p.findings(s), r)
	var out []bucketView
	for _, b := range p.sc.bucketSpecs(prefix) {
		v := bucketView{spec: b, ref: streamRef(b.stream), bucket: strings.TrimPrefix(b.stream, prefix)}
		v.status, v.codes = worstStatus(cur, v.ref)
		if v.present() {
			v.sp, _ = p.sc.findStream(Account, b.stream)
			v.placement = s.placement(v.sp)
			v.growth = s.growth(v.ref, r)
		} else if len(v.codes) == 0 {
			v.status = v1.Status_STATUS_NO_DATA
		}
		out = append(out, v)
	}
	sortByStatus(out, func(v bucketView) v1.Status { return v.status }, func(v bucketView) string { return v.ref.GetKey() })
	return out
}

func (b bucketSpec) storageOr() string {
	if b.storage == "" {
		return "file"
	}
	return b.storage
}

func (b bucketSpec) maxBytesOr() int64 {
	if b.maxBytes == 0 {
		return -1
	}
	return b.maxBytes
}

// ListKeyValueStores returns the KV buckets, worst first.
func (p *Provider) ListKeyValueStores(_ context.Context, req *v1.ListKeyValueStoresRequest) (*v1.ListKeyValueStoresResponse, error) {
	res := &v1.ListKeyValueStoresResponse{}
	for _, v := range p.bucketViews(req.GetRange(), req.GetAccount(), "KV_") {
		b := v.spec
		kv := &v1.KeyValueStore{
			Entity: v.ref, Bucket: v.bucket, Present: v.present(), Expected: b.listed,
			Status: v.status, FindingCodes: v.codes, Placement: v.placement,
		}
		if v.present() {
			keys := b.keys
			if keys == 0 {
				keys = v.sp.msgs
			}
			kv.History, kv.Ttl, kv.MaxBytes = ptr(max(b.history, 1)), durationpb.New(b.ttl), ptr(b.maxBytesOr())
			kv.Replicas, kv.Storage, kv.Compression = ptr(int32(len(p.sc.peers(v.sp)))), b.storageOr(), b.compression
			kv.Keys, kv.Messages, kv.Bytes = ptr(keys), ptr(v.sp.msgs), ptr(int64(v.sp.bytes))
			kv.Watchers, kv.BytesPerDay = ptr(int32(v.sp.ephemeral)), v.growth
			kv.AllowMsgTtl, kv.Description = b.allowMsgTTL, b.description
		}
		res.KeyValueStores = append(res.KeyValueStores, kv)
	}
	return res, nil
}

// ListObjectStores returns the object stores, worst first.
func (p *Provider) ListObjectStores(_ context.Context, req *v1.ListObjectStoresRequest) (*v1.ListObjectStoresResponse, error) {
	res := &v1.ListObjectStoresResponse{}
	for _, v := range p.bucketViews(req.GetRange(), req.GetAccount(), "OBJ_") {
		b := v.spec
		o := &v1.ObjectStore{
			Entity: v.ref, Bucket: v.bucket, Present: v.present(), Expected: b.listed,
			Status: v.status, FindingCodes: v.codes, Placement: v.placement,
		}
		if v.present() {
			o.Ttl, o.MaxBytes, o.Replicas, o.Storage = durationpb.New(b.ttl), ptr(b.maxBytesOr()), ptr(int32(len(p.sc.peers(v.sp)))), b.storageOr()
			o.Chunks, o.Bytes, o.BytesPerDay = ptr(v.sp.msgs), ptr(int64(v.sp.bytes)), v.growth
			o.Sealed, o.Description = b.sealed, b.description
		}
		res.ObjectStores = append(res.ObjectStores, o)
	}
	return res, nil
}
