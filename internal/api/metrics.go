package api

import (
	"cmp"
	"slices"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Metric describes one metric GetTimeSeries serves.
type Metric struct {
	// Entity is the entity type the metric is read for.
	Entity v1.EntityType
	// Unit of the values.
	Unit v1.Unit
	// Description says what the metric measures and how a bucket combines
	// values.
	Description string
}

// MaxObservationRange bounds optional raw-observation summaries.
const MaxObservationRange = 30 * 24 * time.Hour

const (
	sys      = v1.EntityType_ENTITY_TYPE_SYSTEM
	server   = v1.EntityType_ENTITY_TYPE_SERVER
	stream   = v1.EntityType_ENTITY_TYPE_STREAM
	consumer = v1.EntityType_ENTITY_TYPE_CONSUMER
	count    = v1.Unit_UNIT_COUNT
	byteUnit = v1.Unit_UNIT_BYTES
	perSec   = v1.Unit_UNIT_PER_SECOND
	bytesSec = v1.Unit_UNIT_BYTES_PER_SECOND
)

// Metrics lists every metric ID GetTimeSeries accepts. An ID is
// <scope>.<measure>; the scope names the entity type.
var Metrics = map[string]Metric{
	"cluster.servers_up":                {sys, count, "Servers that answered in a collection; the last value of each bucket, its lowest and highest in low and high."},
	"cluster.connections":               {sys, count, "Client connections across servers; the last value of each bucket, its lowest and highest in low and high."},
	"cluster.in_msgs_rate":              {sys, perSec, "Messages received per second across servers; averaged per bucket. The first collection of a restarted server, and the first after a gap in collection, has no rate, so the sum dips there."},
	"cluster.out_msgs_rate":             {sys, perSec, "Messages sent per second across servers; averaged per bucket. The first collection of a restarted server has no rate, so the sum dips there."},
	"cluster.in_bytes_rate":             {sys, bytesSec, "Bytes received per second across servers; averaged per bucket. The first collection of a restarted server has no rate, so the sum dips there."},
	"cluster.out_bytes_rate":            {sys, bytesSec, "Bytes sent per second across servers; averaged per bucket. The first collection of a restarted server has no rate, so the sum dips there."},
	"cluster.capacity_file_store_bytes": {sys, byteUnit, "JetStream file-store usage summed across unique server names. Missing expected servers, missing storage reports, or simultaneous processes sharing a name leave the bucket unknown; complete buckets are averaged."},
	"server.capacity_file_store_bytes":  {server, byteUnit, "Server file-store usage by name across restarts. Simultaneous processes sharing the name or missing reports leave the bucket unknown; complete buckets are averaged."},
	"stream.observed_replica_bytes":     {stream, byteUnit, "Sum of logical bytes in observed replica reports, averaged per bucket. Missing replicas are not estimated; this is not physical file-store usage."},
	"cluster.js_storage_bytes":          {sys, byteUnit, "JetStream file storage in use across servers, replicas counted on each; averaged per bucket."},
	"cluster.slow_consumers":            {sys, count, "Increase of the servers' slow consumer counters; summed per bucket. A restarted server's first collection, and the first after a collection gap, add nothing."},
	"cluster.durable_consumers":         {sys, count, "Durable consumers present; the last value of each bucket, its lowest and highest in low and high."},
	"cluster.cpu_percent":               {sys, v1.Unit_UNIT_PERCENT, "CPU use across servers, percent of one core, as a band: each collection's median server is the value, the lowest and highest servers the band's edges; each averaged per bucket. Top is the server with the highest mean over the range."},
	"cluster.mem_bytes":                 {sys, byteUnit, "Resident memory across servers as a band: each collection's median server is the value, the lowest and highest servers the band's edges; each averaged per bucket. Top is the server with the highest mean over the range."},
	"cluster.streams":                   {sys, count, "Streams any server reported, KV buckets and object stores not counted; the last value of each bucket, its lowest and highest in low and high. A server that does not answer at all is not observed, so its R1 streams drop out of the count with no gap."},
	"cluster.kv_buckets":                {sys, count, "KV buckets (streams named KV_<bucket>) any server reported; the last value of each bucket, its lowest and highest in low and high. A server that does not answer at all is not observed, so its R1 buckets drop out of the count with no gap."},
	"cluster.object_stores":             {sys, count, "Object stores (streams named OBJ_<bucket>) any server reported; the last value of each bucket, its lowest and highest in low and high. A server that does not answer at all is not observed, so its R1 buckets drop out of the count with no gap."},
	"cluster.accounts":                  {sys, count, "Accounts other than the system account with a client connection, or a stream when every server answered JSZ; the last value of each bucket, its lowest and highest in low and high."},
	"cluster.meta_leader_changes":       {sys, count, "Changes of the JetStream meta leader, as more than half of the expected servers report it; a collection without a leader is not a change; summed per bucket."},
	"cluster.assets_replicated":         {sys, count, "Streams and durable consumers configured with more than one replica; a consumer with replicas 0 has its stream's count. The last value of each bucket, its lowest and highest in low and high. A server that does not answer at all is not observed, so its R1 streams drop out of the count with no gap."},
	"cluster.assets_single":             {sys, count, "Streams and durable consumers configured with one replica; a consumer with replicas 0 has its stream's count. The last value of each bucket, its lowest and highest in low and high. A server that does not answer at all is not observed, so its R1 streams drop out of the count with no gap."},
	"server.slow_consumers":             {server, count, "Increase of the server's slow consumer counter, per server process; summed per bucket. A restart's first collection, and the first after a collection gap, add nothing."},
	"server.connections":                {server, count, "Client connections on the server; the last value of each bucket, its lowest and highest in low and high."},
	"server.mem_bytes":                  {server, byteUnit, "Resident memory of the server process; averaged per bucket."},
	"server.cpu_percent":                {server, v1.Unit_UNIT_PERCENT, "CPU use of the server process, percent of one core; averaged per bucket."},
	"server.in_msgs_rate":               {server, perSec, "Messages the server received per second; averaged per bucket."},
	"server.out_msgs_rate":              {server, perSec, "Messages the server sent per second; averaged per bucket."},
	"server.in_bytes_rate":              {server, bytesSec, "Bytes the server received per second; averaged per bucket."},
	"server.out_bytes_rate":             {server, bytesSec, "Bytes the server sent per second; averaged per bucket."},
	"server.js_storage_bytes":           {server, byteUnit, "JetStream file storage in use on the server; averaged per bucket."},
	"stream.messages":                   {stream, count, "Messages stored, from the leader's view; the last value of each bucket, its lowest and highest in low and high."},
	"stream.msgs_rate":                  {stream, perSec, "Messages stored per second, from the increase of the last sequence (the leader's view, else the highest); averaged per bucket. A recreated stream has no rate in its first collection."},
	"stream.bytes":                      {stream, byteUnit, "Bytes stored, from the leader's view; averaged per bucket."},
	"consumer.num_pending":              {consumer, count, "Messages not yet delivered, from the leader's view; the last value of each bucket, its lowest and highest in low and high."},
	"consumer.num_ack_pending":          {consumer, count, "Messages delivered and not acknowledged, from the leader's view; the last value of each bucket, its lowest and highest in low and high."},
	"consumer.num_redelivered":          {consumer, count, "Messages delivered more than once, from the leader's view; the last value of each bucket, its lowest and highest in low and high."},
	"consumer.delivered_rate":           {consumer, perSec, "Messages delivered per second, from the increase of the leader's delivered consumer sequence; averaged per bucket. The first collection after a collection gap of more than three intervals, and a recreated consumer's first collection, have no rate."},
}

// MetricList returns the metrics ordered by ID.
func MetricList() []*v1.MetricInfo {
	out := make([]*v1.MetricInfo, 0, len(Metrics))
	for id, m := range Metrics {
		out = append(out, &v1.MetricInfo{Id: id, EntityType: m.Entity, Unit: m.Unit, Description: m.Description})
	}
	slices.SortFunc(out, func(a, b *v1.MetricInfo) int { return cmp.Compare(a.GetId(), b.GetId()) })
	return out
}

// Step returns the bucket width for a series: the range divided by the
// number of points, rounded up, then up to whole seconds, and never below
// min. The range must be resolved (at most MaxRange).
func Step(r *v1.TimeRange, maxPoints int32, min time.Duration) time.Duration {
	span := r.GetEnd().AsTime().Sub(r.GetStart().AsTime())
	step := time.Duration(ceilDiv(span, time.Duration(max(maxPoints, 1))))
	step = time.Duration(ceilDiv(step, time.Second)) * time.Second
	return max(step, min)
}

// BucketStart is the start of the bucket of width step (whole seconds)
// that holds t, with buckets aligned to the Unix epoch. It counts in
// seconds, because a Duration cannot hold the span from 1970 to a time
// after 2262.
func BucketStart(t time.Time, step time.Duration) time.Time {
	s := int64(step / time.Second)
	sec := t.Unix()
	b := sec / s * s
	if sec < 0 && sec%s != 0 {
		b -= s
	}
	return time.Unix(b, 0).UTC()
}
