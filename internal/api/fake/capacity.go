package fake

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/checks"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// streamLimits is a stream's configuration as its leader reports it, from
// its streamSpec or, for a KV bucket or object store, its bucketSpec.
type streamLimits struct {
	retention, storage, discard, mirror string
	sources                             int32
	// maxMsgs, maxBytes and perSubject are -1 without a limit, maxAge 0.
	maxMsgs, maxBytes, perSubject int64
	maxAge                        time.Duration
	allowMsgTTL                   bool
	// keys is a KV bucket's subjects; nil for other kinds.
	keys *int64
	// held is the age of the oldest message; zero for an empty stream.
	held time.Duration
	// declared is api.DeclaredInventory or api.DeclaredWriterTTLs when
	// stream-005 leaves the stream out by the inventory.
	declared string
}

// noLimit is a limit as the server reports none: -1.
func noLimit(v int64) int64 {
	if v <= 0 {
		return -1
	}
	return v
}

// streamLimitsOf is a stream's configuration. A bucket's TTL is its max_age, its
// history its max_msgs_per_subject, and buckets discard new.
func (sc *scenario) streamLimitsOf(sp streamSpec) streamLimits {
	l := streamLimits{
		retention: cmp.Or(sp.retention, "limits"), storage: cmp.Or(sp.storage, "file"), discard: cmp.Or(sp.discard, "old"),
		mirror: sp.mirror, sources: int32(len(sp.sources)), maxMsgs: noLimit(sp.maxMsgs), maxBytes: noLimit(sp.maxBytes),
		perSubject: noLimit(sp.perSubject), maxAge: sp.maxAge,
	}
	if isKV(sp.name) {
		var b bucketSpec
		listed := false
		for _, x := range sc.buckets {
			if x.stream == sp.name {
				b, listed = x, true
			}
		}
		l.storage, l.discard, l.maxBytes, l.maxAge, l.allowMsgTTL = b.storageOr(), "new", b.maxBytesOr(), b.ttl, b.allowMsgTTL
		l.maxMsgs, l.perSubject = -1, -1
		if strings.HasPrefix(sp.name, "KV_") {
			l.perSubject = int64(max(b.history, 1))
			l.keys = ptr(cmp.Or(b.keys, sp.msgs))
			if listed && l.allowMsgTTL && l.unbounded() {
				l.declared = api.DeclaredWriterTTLs
			}
		}
	}
	if sp.declared && l.unbounded() {
		l.declared = api.DeclaredInventory
	}
	if sp.msgs > 0 {
		l.held = cmp.Or(sp.held, l.maxAge)
	}
	return l
}

// unbounded is stream-005's condition: no max_msgs, max_bytes or max_age.
func (l streamLimits) unbounded() bool { return l.maxMsgs <= 0 && l.maxBytes <= 0 && l.maxAge == 0 }

// unboundedFinding is stream-005 for a stream, with the detail and
// related configuration the check writes from the stream's limits; since
// is when it began to fire.
func (s scen) unboundedFinding(name string, since time.Time) *v1.Finding {
	sp, _ := s.sc.findStream(Account, name)
	l := s.sc.streamLimitsOf(sp)
	ttl := checks.DurationText(l.maxAge)
	var detail string
	switch {
	case strings.HasPrefix(name, "KV_"):
		detail = "KV bucket " + strings.TrimPrefix(name, "KV_") + ": keys accumulate without bound; set a TTL or max_bytes, or mute if intended (max_bytes " +
			strconv.FormatInt(l.maxBytes, 10) + ", TTL " + ttl
		if l.perSubject > 0 {
			detail += ", history " + strconv.FormatInt(l.perSubject, 10) + " per key"
		}
		if l.allowMsgTTL {
			detail += "); it allows per-message TTLs; nats-lens cannot see whether writers set them."
		} else {
			detail += ", no per-key TTLs)."
		}
	case strings.HasPrefix(name, "OBJ_"):
		detail = "Object store " + strings.TrimPrefix(name, "OBJ_") + ": objects accumulate without bound; set a TTL or max_bytes, or mute if intended (max_bytes " +
			strconv.FormatInt(l.maxBytes, 10) + ", TTL " + ttl + ")."
	default:
		detail = "max_msgs " + strconv.FormatInt(l.maxMsgs, 10) + ", max_bytes " + strconv.FormatInt(l.maxBytes, 10) + ", max_age " + ttl +
			": nothing bounds the stream."
	}
	f := finding(api.CheckUnboundedStream, "Stream without any limit", info, firing, streamRef(name), since, since, detail)
	// The check marks every key of its related configuration.
	f.RelatedConfig = cfg([][2]string{{"allow_msg_ttl", strconv.FormatBool(l.allowMsgTTL)}, {"max_age", ttl},
		{"max_bytes", strconv.FormatInt(l.maxBytes, 10)}, {"max_msgs", strconv.FormatInt(l.maxMsgs, 10)},
		{"max_msgs_per_subject", strconv.FormatInt(l.perSubject, 10)}},
		"allow_msg_ttl", "max_age", "max_bytes", "max_msgs", "max_msgs_per_subject")
	f.RelatedConfigSource = cmp.Or(s.leader(sp), s.sc.peers(sp)[0])
	return f
}

// Capacity builds the facts of the range's newest epoch from the
// scenario: every server's file store and every stream's limits, with
// their growth over the range by the store's rule, and the findings that
// fire at the range's end with their mutes.
func (p *Provider) Capacity(_ context.Context, req *v1.GetCapacityRequest) (*api.CapacityFacts, error) {
	return p.capacityAt(p.scen(), req)
}

func (p *Provider) capacityAt(s scen, req *v1.GetCapacityRequest) (*api.CapacityFacts, error) {
	r := req.GetRange()
	at, n, ok := s.epochBefore(r)
	if !ok {
		return &api.CapacityFacts{}, nil
	}
	f := &api.CapacityFacts{Epoch: n, EpochAt: at, GrowthFrom: latest(api.GrowthStart(r.GetStart().AsTime(), r.GetEnd().AsTime(), sampleEvery), s.dataStart()),
		Findings: current(p.findings(s), r)}
	for _, sv := range s.sc.servers {
		x := api.ServerStoreFact{Name: sv.name, Cluster: sv.cluster, PerDay: s.growthOf("server.js_storage_bytes", serverRef(sv.name), r),
			MemPerDay: s.growthOf(fakeServerMemStore, serverRef(sv.name), r)}
		if !s.offline(sv.name, at) {
			x.Reported, x.Used, x.Limit = true, ptr(sv.store), ptr(sv.maxStore)
			x.MemUsed, x.MemLimit = ptr(s.sc.memStore(sv.name)), ptr(sv.maxMemStore)
		}
		f.Servers = append(f.Servers, x)
	}
	for _, sp := range s.sc.streams {
		l := s.sc.streamLimitsOf(sp)
		e := streamRef(sp.name)
		x := api.CapacityStreamFact{
			Account: Account, Stream: sp.name, Retention: l.retention, Storage: l.storage, Discard: l.discard, Mirror: l.mirror,
			Sources: l.sources, Replicas: int32(len(s.sc.peers(sp))), Messages: ptr(sp.msgs), Bytes: ptr(int64(sp.bytes)), Keys: l.keys,
			MaxMsgs: ptr(l.maxMsgs), MaxBytes: ptr(l.maxBytes), MaxMsgsPerSubject: ptr(l.perSubject), MaxAge: ptr(l.maxAge),
			AllowMsgTTL: l.allowMsgTTL, BytesPerDay: s.growth(e, r), MsgsPerDay: s.growthOf("stream.messages", e, r), Declared: l.declared,
		}
		if sp.msgs > 0 {
			x.FirstTS = ptr(at.Add(-l.held))
		}
		f.Streams = append(f.Streams, x)
	}
	slices.SortFunc(f.Streams, func(a, b api.CapacityStreamFact) int { return cmp.Compare(a.Stream, b.Stream) })
	// The accounts as the Accounts page reads them; Account's use grows
	// with its streams.
	for _, a := range s.accounts(at) {
		x := api.AccountStoreFact{Account: a.Account, JSStorage: a.JSStorage, JSMemory: a.JSMemory, Details: a.Details}
		if a.Account == Account {
			x.StoragePerDay = s.growthOf(fakeAccountFileStore, accountRef(Account), r)
			x.MemoryPerDay = s.growthOf(fakeAccountMemStore, accountRef(Account), r)
		}
		f.Accounts = append(f.Accounts, x)
	}
	return f, nil
}
