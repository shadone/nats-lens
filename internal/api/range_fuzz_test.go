package api

import (
	"errors"
	"strings"
	"testing"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// checkResolvedRange checks a range a request may read: start before end,
// at most MaxRange apart.
func checkResolvedRange(t *testing.T, start, end time.Time) {
	t.Helper()
	if !start.Before(end) || end.Sub(start) > MaxRange || start.Before(end.Add(-MaxRange)) {
		t.Fatalf("resolved range %v to %v is empty, reversed or longer than %v", start, end, MaxRange)
	}
}

// FuzzTimelineBuckets resolves a status timeline's range and bucket: a
// request is refused as invalid, or its range is ordered and bounded, its
// bucket at least a second, and it holds 1 to MaxBuckets buckets, every
// instant of the range falling in one of them.
func FuzzTimelineBuckets(f *testing.F) {
	const at = int64(1792247400) // 2026-10-17 14:30 UTC
	f.Add(true, at-3600, int32(0), true, at, int32(0), true, int64(60), int32(0), at, int64(1800))
	f.Add(false, int64(0), int32(0), false, int64(0), int32(0), false, int64(0), int32(0), at, int64(0))
	f.Add(true, at-3600, int32(0), true, at, int32(0), true, int64(1), int32(0), at, int64(3599))
	f.Add(true, at-3601, int32(0), true, at, int32(0), true, int64(1), int32(999999999), at, int64(0))
	f.Add(true, at-int64(MaxRange/time.Second), int32(0), true, at, int32(0), true, int64(17280), int32(0), at, int64(0))
	f.Add(true, at-int64(MaxRange/time.Second)-1, int32(0), true, at, int32(0), true, int64(86400), int32(0), at, int64(0))
	f.Add(true, int64(-62135596800), int32(0), true, int64(253402300799), int32(999999999), true, int64(315576000000), int32(0), at, int64(0))
	f.Add(true, int64(-62135596801), int32(0), true, at, int32(0), false, int64(0), int32(0), at, int64(0))
	f.Add(true, at, int32(-1), true, at, int32(1000000000), false, int64(0), int32(0), at, int64(0))
	f.Add(true, at, int32(0), true, at, int32(0), false, int64(0), int32(0), at, int64(0))
	f.Add(false, int64(0), int32(0), true, int64(-62135596800), int32(0), false, int64(0), int32(0), at, int64(0))
	f.Add(true, at-60, int32(0), true, at, int32(0), true, int64(0), int32(999999999), at, int64(0))
	f.Add(true, at-60, int32(0), true, at, int32(0), true, int64(-1), int32(0), at, int64(0))
	f.Add(true, at-60, int32(0), true, at, int32(0), true, int64(9223372036854775807), int32(0), at, int64(0))
	f.Fuzz(func(t *testing.T, hasStart bool, startS int64, startN int32, hasEnd bool, endS int64, endN int32,
		hasBucket bool, bucketS int64, bucketN int32, nowS int64, probe int64) {
		if nowS < -62135596800 || nowS > 253402300799 {
			return // the server's clock, not request input
		}
		req := &v1.GetStatusTimelineRequest{Range: &v1.TimeRange{}}
		if hasStart {
			req.Range.Start = &timestamppb.Timestamp{Seconds: startS, Nanos: startN}
		}
		if hasEnd {
			req.Range.End = &timestamppb.Timestamp{Seconds: endS, Nanos: endN}
		}
		if hasBucket {
			req.Bucket = &durationpb.Duration{Seconds: bucketS, Nanos: bucketN}
		}
		again := proto.Clone(req).(*v1.GetStatusTimelineRequest)
		now := time.Unix(nowS, 0).UTC()
		err := normalizeTimeline(req, now)
		if err2 := normalizeTimeline(again, now); (err == nil) != (err2 == nil) || !proto.Equal(req, again) {
			t.Fatalf("normalizeTimeline is not deterministic: %v / %v", err, err2)
		}
		if err != nil {
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("normalizeTimeline = %v; want an invalid-argument error", err)
			}
			return
		}
		start, end := req.GetRange().GetStart().AsTime(), req.GetRange().GetEnd().AsTime()
		checkResolvedRange(t, start, end)
		bucket := req.GetBucket().AsDuration()
		if bucket < time.Second {
			t.Fatalf("bucket %v is under a second", bucket)
		}
		n := BucketCount(end.Sub(start), bucket)
		if n < 1 || n > MaxBuckets {
			t.Fatalf("%v to %v by %v holds %d buckets", start, end, bucket, n)
		}
		span := end.Sub(start)
		for _, at := range []time.Time{start, end.Add(-1), start.Add(time.Duration(uint64(probe) % uint64(span)))} {
			if i := BucketIndex(start, bucket, at); i < 0 || i >= n {
				t.Fatalf("instant %v of %v to %v falls in bucket %d of %d", at, start, end, i, n)
			}
		}
	})
}

// FuzzEntityKey checks entity references used as filters and mute
// targets, and the reference a key gives back: a key of a compound type is
// refused unless it has its type's number of non-empty parts, and an
// accepted one splits into parts that join to the key again.
func FuzzEntityKey(f *testing.F) {
	for _, s := range []struct {
		typ int32
		key string
	}{
		{int32(v1.EntityType_ENTITY_TYPE_STREAM), "APP/orders"},
		{int32(v1.EntityType_ENTITY_TYPE_CONSUMER), "APP/orders/worker"},
		{int32(v1.EntityType_ENTITY_TYPE_CONNECTION), "nats-0/42"},
		{int32(v1.EntityType_ENTITY_TYPE_CONNECTION), "nats-0/-1"},
		{int32(v1.EntityType_ENTITY_TYPE_CONNECTION), "nats-0/18446744073709551616"},
		{int32(v1.EntityType_ENTITY_TYPE_SERVER), "nats-0"},
		{int32(v1.EntityType_ENTITY_TYPE_SERVER), "nats/0"},
		{int32(v1.EntityType_ENTITY_TYPE_ACCOUNT), "ACDE"},
		{int32(v1.EntityType_ENTITY_TYPE_CLIENT), "a/b/c/d"},
		{int32(v1.EntityType_ENTITY_TYPE_SYSTEM), ""},
		{int32(v1.EntityType_ENTITY_TYPE_SYSTEM), "x"},
		{int32(v1.EntityType_ENTITY_TYPE_UNSPECIFIED), ""},
		{999, "x"},
		{int32(v1.EntityType_ENTITY_TYPE_STREAM), "APP//orders"},
		{int32(v1.EntityType_ENTITY_TYPE_STREAM), "/orders"},
		{int32(v1.EntityType_ENTITY_TYPE_STREAM), "APP/orders/"},
		{int32(v1.EntityType_ENTITY_TYPE_STREAM), "APP∕orders"},
		{int32(v1.EntityType_ENTITY_TYPE_STREAM), "АPP/ördérs‍"},
		{int32(v1.EntityType_ENTITY_TYPE_STREAM), "APP/" + strings.Repeat("é", 300)},
		{int32(v1.EntityType_ENTITY_TYPE_STREAM), "APP/\xff"},
	} {
		f.Add(s.typ, s.key, false)
	}
	f.Fuzz(func(t *testing.T, typ int32, key string, optional bool) {
		if len(key) > 4096 {
			return
		}
		ref := &v1.EntityRef{Type: v1.EntityType(typ), Key: key}
		err := checkEntity("entity", ref, optional)
		if err != nil {
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("checkEntity(%d, %q) = %v; want an invalid-argument error", typ, key, err)
			}
			return
		}
		if ref.GetType() == v1.EntityType_ENTITY_TYPE_UNSPECIFIED {
			if !optional || key != "" {
				t.Fatalf("checkEntity accepted an unspecified type with key %q", key)
			}
			return
		}
		got := EntityRefOf(ref.GetType(), key)
		if got.GetType() != ref.GetType() {
			t.Fatalf("EntityRefOf(%v, %q) has type %v", ref.GetType(), key, got.GetType())
		}
		switch ref.GetType() {
		case v1.EntityType_ENTITY_TYPE_SYSTEM:
			if got.GetKey() != "" {
				t.Fatalf("system reference has key %q", got.GetKey())
			}
		case v1.EntityType_ENTITY_TYPE_STREAM:
			if got.GetAccount()+"/"+got.GetStream() != key || got.GetName() != got.GetStream() || got.GetAccount() == "" || got.GetStream() == "" {
				t.Fatalf("stream key %q gives %v", key, got)
			}
		case v1.EntityType_ENTITY_TYPE_CONSUMER:
			if got.GetAccount()+"/"+got.GetStream()+"/"+got.GetConsumer() != key || got.GetName() != got.GetConsumer() || got.GetConsumer() == "" {
				t.Fatalf("consumer key %q gives %v", key, got)
			}
		default:
			if got.GetKey() != key {
				t.Fatalf("key %q gives %v", key, got)
			}
		}
	})
}
