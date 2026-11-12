package storeprov

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/collect"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// The sample store is collected every 10 minutes for about 11 hours, long
// enough for the 6-hour growth, with one collection gap of 45 minutes
// (more than three intervals) after epoch 40 and none at all in epoch 30.
const (
	sampleInterval = 10 * time.Minute
	sampleEpochs   = 66
)

var sampleT0 = time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)

func sampleAt(e int) time.Time {
	at := sampleT0.Add(time.Duration(e) * sampleInterval)
	if e > 40 {
		at = at.Add(35 * time.Minute)
	}
	return at
}

// sampleStream is a stream of the sample store. servers gives the
// servers reporting it in epoch e, leader first, nil when it does not
// exist then; leader names the leader ("" for none); seq, created and the
// sizes are per server.
type sampleStream struct {
	account, name string
	servers       func(e int) []int
	leader        func(e int, servers []int) string
	seq           func(e, si int) uint64
	created       func(e, si int) time.Time
}

var sampleCreated = sampleT0.Add(-48 * time.Hour)

func leaderFirst(_ int, servers []int) string { return fmt.Sprintf("nats-%d", servers[0]) }
func noLeader(int, []int) string              { return "" }
func createdAt(t time.Time) func(int, int) time.Time {
	return func(int, int) time.Time { return t }
}

// sampleStreams has the cases the rates and values must keep: a leader
// that moves to a server reporting another sequence; an R1 stream whose
// server misses epochs, so the previous sample is two epochs back; a
// counter reset; a stream recreated, deleted and created again; creation
// times unknown or disagreeing between replicas; no leader at all; a
// stream created inside the range; sequences near the top of BIGINT; and
// a KV bucket.
var sampleStreams = []sampleStream{
	{account: "SHOP", name: "orders", servers: func(e int) []int {
		if e >= 25 {
			return []int{1, 0, 2}
		}
		return []int{0, 1, 2}
	}, leader: leaderFirst, created: createdAt(sampleCreated),
		seq: func(e, si int) uint64 { return uint64(1000 + 600*e - 7*si) }},
	{account: "SHOP", name: "audit", servers: func(e int) []int {
		if e%5 == 3 {
			return nil
		}
		return []int{3}
	}, leader: leaderFirst, created: createdAt(sampleCreated),
		seq: func(e, _ int) uint64 { return uint64(50 * e * e) }},
	{account: "SHOP", name: "resets", servers: on(0), leader: leaderFirst, created: createdAt(sampleCreated),
		seq: func(e, _ int) uint64 {
			if e >= 12 {
				return uint64(5 * (e - 12))
			}
			return uint64(900 + 90*e)
		}},
	{account: "SHOP", name: "recreated", servers: func(e int) []int {
		if e >= 45 && e < 50 {
			return nil
		}
		return []int{2, 3}
	}, leader: leaderFirst, created: func(e, _ int) time.Time {
		switch {
		case e >= 50:
			return sampleAt(50)
		case e >= 18:
			return sampleAt(18)
		}
		return sampleCreated
	}, seq: func(e, _ int) uint64 { return uint64(10 * (e % 18)) }},
	{account: "SHOP", name: "unknown", servers: on(1, 2), leader: leaderFirst, created: func(e, si int) time.Time {
		switch {
		case e%9 == 4:
			return time.Time{}
		case e%11 == 7 && si == 2:
			return sampleCreated.Add(time.Second)
		}
		return sampleCreated
	}, seq: func(e, _ int) uint64 { return uint64(70 * e) }},
	{account: "SHOP", name: "leaderless", servers: on(0, 1, 3), leader: func(e int, servers []int) string {
		if e%4 == 1 || e >= 55 {
			return ""
		}
		return leaderFirst(e, servers)
	}, created: createdAt(sampleCreated), seq: func(e, si int) uint64 { return uint64(30*e + si) }},
	{account: "BILLING", name: "invoices", servers: func(e int) []int {
		if e < 22 {
			return nil
		}
		return []int{1}
	}, leader: leaderFirst, created: createdAt(sampleAt(22)), seq: func(e, _ int) uint64 { return uint64(3 * (e - 22)) }},
	{account: "BILLING", name: "ledger", servers: on(0, 2), leader: leaderFirst, created: createdAt(sampleCreated),
		seq: func(e, si int) uint64 { return uint64(1)<<62 + uint64(e)*(uint64(1)<<40) - uint64(si) }},
	{account: "SHOP", name: "KV_settings", servers: on(2), leader: noLeader, created: createdAt(sampleCreated),
		seq: func(e, _ int) uint64 { return uint64(e / 3) }},
	// flaky misses two epochs in six, so its previous sample is three
	// intervals back: still comparable, but before the first epoch of the
	// lookback of a range that starts at its next sample.
	{account: "SHOP", name: "flaky", servers: func(e int) []int {
		if e%6 == 2 || e%6 == 3 {
			return nil
		}
		return []int{1}
	}, leader: leaderFirst, created: createdAt(sampleCreated), seq: func(e, _ int) uint64 { return uint64(40 * e) }},
}

// sampleSnapshot builds epoch e: nats-3 does not answer in every seventh
// epoch, and in epoch 30 no server answers.
func sampleSnapshot(e int) *collect.Snapshot {
	at := sampleAt(e)
	snap := collect.NewSnapshot(at)
	snap.Expected = []string{"nats-0", "nats-1", "nats-2", "nats-3"}
	snap.FinishedAt = at.Add(300 * time.Millisecond)
	if e == 30 {
		return snap
	}
	started := sampleT0.Add(-time.Hour)
	for si := range 4 {
		if si == 3 && e%7 == 6 {
			continue
		}
		name, id := fmt.Sprintf("nats-%d", si), fmt.Sprintf("NSAMPLESERVER%dID", si)
		info := server.ServerInfo{Name: name, ID: id, Cluster: "shop-east", Version: "2.15.2", JetStream: true}
		v := &collect.ServerView{Info: info, Status: map[collect.Endpoint]collect.EndpointStatus{
			collect.Varz: {State: collect.StateOK}, collect.Jsz: {State: collect.StateOK}}}
		v.Varz = &server.Varz{ID: id, Name: name, Version: info.Version, Start: started, Now: at, Mem: 1 << 20, Cores: 2, MaxProcs: 2}
		accs := map[string]*server.AccountDetail{}
		var order []string
		for _, s := range sampleStreams {
			servers := s.servers(e)
			self := false
			for _, o := range servers {
				self = self || o == si
			}
			if !self {
				continue
			}
			leader := s.leader(e, servers)
			ci := &server.ClusterInfo{Name: "shop-east", RaftGroup: s.name, Leader: leader}
			for _, o := range servers {
				if o != si {
					ci.Replicas = append(ci.Replicas, &server.PeerInfo{Name: fmt.Sprintf("nats-%d", o), Current: true})
				}
			}
			cfg := &server.StreamConfig{Name: s.name, Subjects: []string{s.name + ".>"}, Retention: server.LimitsPolicy,
				Storage: server.FileStorage, Replicas: len(servers), MaxConsumers: -1, MaxMsgs: -1, MaxBytes: -1, MaxMsgsPer: -1,
				MaxMsgSize: -1, Discard: server.DiscardOld}
			seq := s.seq(e, si)
			sd := server.StreamDetail{Name: s.name, Created: s.created(e, si), Config: cfg, Cluster: ci,
				State: server.StreamState{Msgs: seq/2 + uint64(si), Bytes: 4096 + 512*uint64(e) + 1000*uint64(si)*uint64(e%3),
					FirstSeq: 1, LastSeq: seq, LastTime: at}}
			acc, ok := accs[s.account]
			if !ok {
				acc = &server.AccountDetail{Name: s.account, Id: s.account}
				accs[s.account] = acc
				order = append(order, s.account)
			}
			acc.Streams = append(acc.Streams, sd)
		}
		jsz := &server.JSInfo{ID: id, Now: at, Meta: &server.MetaClusterInfo{Name: "shop-east", Leader: "nats-0", Size: 4}}
		for _, a := range order {
			jsz.AccountDetails = append(jsz.AccountDetails, accs[a])
		}
		v.Jsz = jsz
		snap.Servers[id] = v
	}
	return snap
}

// sampleProvider writes the sample store through WriteSnapshot in the time
// zone Pacific/Honolulu, so that a time shifted by the session zone shows.
func sampleProvider(t *testing.T) *Provider {
	t.Helper()
	t.Setenv("TZ", "Pacific/Honolulu")
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.DB().Exec(`SET GLOBAL TimeZone = 'Pacific/Honolulu'`); err != nil {
		t.Fatal(err)
	}
	for e := range sampleEpochs {
		if _, err := st.WriteSnapshot(ctx, sampleSnapshot(e)); err != nil {
			t.Fatalf("epoch %d: %v", e, err)
		}
	}
	last := sampleAt(sampleEpochs)
	return &Provider{St: st, Interval: sampleInterval, Clock: func() time.Time { return last }}
}

// sampleRanges cover the whole store; its middle; a start just after an
// epoch, so that the lookback decides whether the first rate counts;
// starts at an epoch whose flaky sample compares with one before the
// lookback; ranges across the collection gap and the empty epoch; one
// epoch; and one before any stream exists.
func sampleRanges() []*v1.TimeRange {
	at := func(d time.Duration) *timestamppb.Timestamp { return timestamppb.New(sampleT0.Add(d)) }
	return []*v1.TimeRange{
		{Start: at(-time.Hour), End: at(13 * time.Hour)},
		{Start: at(2*time.Hour + 5*time.Minute), End: at(9 * time.Hour)},
		{Start: at(90*time.Minute + time.Second), End: at(8*time.Hour + 20*time.Minute)},
		{Start: at(160 * time.Minute), End: at(5 * time.Hour)},
		{Start: at(100 * time.Minute), End: at(11 * time.Hour)},
		{Start: at(4*time.Hour + 30*time.Minute), End: at(7*time.Hour + 30*time.Minute)},
		{Start: at(6*time.Hour + 30*time.Minute), End: at(6*time.Hour + 40*time.Minute)},
		{Start: at(-6 * time.Hour), End: at(-time.Hour)},
	}
}

var sampleMetrics = []string{"stream.msgs_rate", "stream.bytes", "stream.messages", "stream.observed_replica_bytes"}

// sampleReads is everything that reads stream_samples, read once.
type sampleReads struct {
	series       map[string]*v1.Series
	obs          map[string]*v1.ObservationSummary
	contributors map[string]*v1.ListMetricContributorsResponse
	requests     map[string]*v1.ListMetricContributorsRequest
	capacity     map[string]*api.CapacityFacts
	findings     map[string][]*v1.Finding
	streams      map[string]*v1.ListStreamsResponse
	stream       map[string]*v1.GetStreamResponse
}

func readSamples(t *testing.T, st *store.Store) sampleReads {
	t.Helper()
	ctx := context.Background()
	out := sampleReads{series: map[string]*v1.Series{}, obs: map[string]*v1.ObservationSummary{},
		contributors: map[string]*v1.ListMetricContributorsResponse{}, requests: map[string]*v1.ListMetricContributorsRequest{}, capacity: map[string]*api.CapacityFacts{},
		findings: map[string][]*v1.Finding{}, streams: map[string]*v1.ListStreamsResponse{}, stream: map[string]*v1.GetStreamResponse{}}
	for ri, r := range sampleRanges() {
		// A provider of its own per read, so that no page is reused.
		p := &Provider{St: st, Interval: sampleInterval, Clock: func() time.Time { return r.End.AsTime() }}
		for _, metric := range sampleMetrics {
			for _, s := range sampleStreams {
				e := refOf(v1.EntityType_ENTITY_TYPE_STREAM, []any{s.account, s.name})
				for _, mp := range []int32{0, 3, 500} {
					res, err := p.GetTimeSeries(ctx, &v1.GetTimeSeriesRequest{Metric: metric, Entity: e, Range: r, MaxPoints: mp,
						IncludeObservations: mp == 3})
					if err != nil {
						t.Fatalf("%s %s: %v", metric, e.GetKey(), err)
					}
					out.series[fmt.Sprintf("%s %s %d %d", metric, e.GetKey(), ri, mp)] = res.GetSeries()
				}
				o, err := p.observationSummary(ctx, metric, e, r)
				if err != nil {
					t.Fatal(err)
				}
				out.obs[fmt.Sprintf("%s %s %d", metric, e.GetKey(), ri)] = o
			}
			for rank := v1.MetricRanking_METRIC_RANKING_NAME; rank <= v1.MetricRanking_METRIC_RANKING_COUNTER_INCREASE; rank++ {
				for _, desc := range []bool{false, true} {
					for _, account := range []string{"", "SHOP"} {
						req := &v1.ListMetricContributorsRequest{Metric: metric, Range: r, Ranking: rank, Descending: desc, Account: account, Limit: 100}
						res, err := p.ListMetricContributors(ctx, req)
						key := fmt.Sprintf("%s %d %v %v %q", metric, ri, rank, desc, account)
						out.requests[key] = req
						if err != nil {
							out.contributors[key] = &v1.ListMetricContributorsResponse{Metric: "error: " + err.Error()}
							continue
						}
						res.ReadAt = nil
						out.contributors[key] = res
					}
				}
			}
		}
		f, err := p.Capacity(ctx, &v1.GetCapacityRequest{Range: r})
		if err != nil {
			t.Fatal(err)
		}
		out.findings[fmt.Sprint(ri)] = f.Findings
		f.Findings = nil
		out.capacity[fmt.Sprint(ri)] = f
		ls, err := p.ListStreams(ctx, &v1.ListStreamsRequest{Range: r, IncludeKv: true})
		if err != nil {
			t.Fatal(err)
		}
		out.streams[fmt.Sprint(ri)] = ls
		for _, s := range sampleStreams {
			gs, err := p.GetStream(ctx, &v1.GetStreamRequest{Range: r, Account: s.account, Stream: s.name})
			if err != nil {
				gs = &v1.GetStreamResponse{Stream: &v1.StreamSummary{Leader: "error: " + err.Error()}}
			}
			out.stream[fmt.Sprintf("%s/%s %d", s.account, s.name, ri)] = gs
		}
	}
	return out
}

// meanTolerance is how far apart, relative to their size, two averages
// of the same rates may be. An average of rates is a sum of doubles
// divided by their number, and DuckDB adds them in the order its plan and
// threads deliver them, which the plan over the samples and the one over
// the reports do not share, and which SQL leaves open. Averages of the
// integer values (bytes, messages) are exact and must be equal, as must
// every other value.
const meanTolerance = 1e-12

// rounded counts the averages align had to align.
var rounded int

// align sets *got to want when both are set and differ by no more than
// meanTolerance.
func align(got **float64, want *float64) {
	if *got == nil || want == nil || **got == *want {
		return
	}
	if math.Abs(**got-*want) <= meanTolerance*math.Max(math.Abs(**got), math.Abs(*want)) {
		*got = ptr(*want)
		rounded++
	}
}

// sameMean aligns got's mean with want's.
func sameMean(got, want *v1.ObservationSummary) {
	if got != nil && want != nil {
		align(&got.Mean, want.Mean)
	}
}

// sameAverages returns a copy of got whose averaged values (the points of
// a series that averages its buckets, its smallest and largest, and the
// mean of its observations) are want's where they differ by no more than
// meanTolerance.
func sameAverages(got, want *v1.Series) *v1.Series {
	g := proto.Clone(got).(*v1.Series)
	if len(g.GetPoints()) == len(want.GetPoints()) {
		for i, pt := range g.GetPoints() {
			v := ptr(pt.GetValue())
			align(&v, ptr(want.GetPoints()[i].GetValue()))
			pt.Value = *v
		}
	}
	align(&g.Min, want.Min)
	align(&g.Max, want.Max)
	sameMean(g.GetObservations(), want.GetObservations())
	return g
}

func compareSampleReads(t *testing.T, got, want sampleReads) {
	t.Helper()
	rounded = 0
	points, ranked, rated := 0, 0, 0
	for k, w := range want.series {
		if !proto.Equal(sameAverages(got.series[k], w), w) {
			t.Errorf("series %s:\nsamples %v\nreports %v", k, got.series[k], w)
		}
		points += len(w.GetPoints())
	}
	for k, w := range want.obs {
		g := proto.Clone(got.obs[k]).(*v1.ObservationSummary)
		sameMean(g, w)
		if !proto.Equal(g, w) {
			t.Errorf("observations %s:\nsamples %v\nreports %v", k, g, w)
		}
		if w.GetKnownCount() > 0 {
			rated++
		}
	}
	for k, w := range want.contributors {
		// With the means aligned, entities whose means tie within the
		// tolerance rank as the reports rank them.
		g := proto.Clone(got.contributors[k]).(*v1.ListMetricContributorsResponse)
		byKey := map[string]*v1.MetricContributor{}
		for _, c := range w.GetContributors() {
			byKey[c.GetEntity().GetKey()] = c
		}
		for _, c := range g.GetContributors() {
			sameMean(c.GetObservations(), byKey[c.GetEntity().GetKey()].GetObservations())
		}
		api.SortMetricContributors(g.Contributors, want.requests[k])
		if !proto.Equal(g, w) {
			t.Errorf("contributors %s:\nsamples %v\nreports %v", k, got.contributors[k], w)
		}
		ranked += len(w.GetContributors())
	}
	for k, w := range want.capacity {
		if !reflect.DeepEqual(got.capacity[k], w) {
			t.Errorf("capacity %s:\nsamples %+v\nreports %+v", k, got.capacity[k], w)
		}
	}
	for k, w := range want.findings {
		g := got.findings[k]
		if len(g) != len(w) {
			t.Errorf("capacity findings %s: %d, want %d", k, len(g), len(w))
			continue
		}
		for i := range w {
			if !proto.Equal(g[i], w[i]) {
				t.Errorf("capacity finding %s %d:\nsamples %v\nreports %v", k, i, g[i], w[i])
			}
		}
	}
	for k, w := range want.streams {
		if !proto.Equal(got.streams[k], w) {
			t.Errorf("streams %s:\nsamples %v\nreports %v", k, got.streams[k], w)
		}
	}
	for k, w := range want.stream {
		if !proto.Equal(got.stream[k], w) {
			t.Errorf("stream %s:\nsamples %v\nreports %v", k, got.stream[k], w)
		}
	}
	if rounded > 0 {
		t.Logf("%d means differ in the last bits of a double sum", rounded)
	}
	if points == 0 || ranked == 0 || rated == 0 {
		t.Fatalf("the reads are empty: %d points, %d ranked rows, %d summaries with a value", points, ranked, rated)
	}
}

// unsample removes the stream samples of the epochs from the first one
// starting at or after from on, as a store whose fill has not reached
// them: their samples, their marks, and the heads, which go back to each
// stream's newest sample before them.
func unsample(t *testing.T, st *store.Store, from time.Time) {
	t.Helper()
	for _, q := range []string{
		`DELETE FROM stream_samples WHERE epoch >= (SELECT min(epoch) FROM epochs WHERE started_at >= ?)`,
		`DELETE FROM stream_sample_epochs WHERE epoch >= (SELECT min(epoch) FROM epochs WHERE started_at >= ?)`,
	} {
		if _, err := st.DB().Exec(q, from); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	for _, q := range []string{`DELETE FROM stream_sample_heads`, `INSERT INTO stream_sample_heads
SELECT account, stream, epoch, seq, created FROM stream_samples QUALIFY row_number() OVER (PARTITION BY account, stream ORDER BY epoch DESC) = 1`} {
		if _, err := st.DB().Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

// Every read of stream_samples gives what reading the reports gives: the
// per-stream series with and without observations, the ranking of every
// stream metric by every ranking, the Capacity page's facts, the Streams
// list with its growth and sparklines, and each stream, over several
// ranges. The reports are read where the samples are missing, so removing
// them all reads the old queries.
func TestStreamSamplesMatchReports(t *testing.T) {
	p := sampleProvider(t)
	ctx := context.Background()
	var n int
	if err := p.St.DB().QueryRow(`SELECT count(*) FROM stream_sample_epochs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != sampleEpochs {
		t.Fatalf("%d epochs have samples, want every one (%d)", n, sampleEpochs)
	}
	for _, metric := range sampleMetrics {
		m := metricQueries[metric]
		for _, r := range sampleRanges() {
			got, err := p.resolveMetric(ctx, m, p.seriesLookback(metric, r.Start.AsTime()), r.End.AsTime())
			if err != nil {
				t.Fatal(err)
			}
			if got.query != m.query || m.scan == "" {
				t.Fatalf("%s over %v does not read the samples", metric, r)
			}
		}
	}
	got := readSamples(t, p.St)
	unsample(t, p.St, sampleT0.Add(-24*time.Hour))
	if err := p.St.DB().QueryRow(`SELECT count(*) FROM stream_samples`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d samples left (%v)", n, err)
	}
	want := readSamples(t, p.St)
	compareSampleReads(t, got, want)
}

// Where only the later epochs of the store have no samples, a range over
// them reads the reports and one before them the samples, with the same
// answers; filling them gives the samples the epoch writes gave.
func TestStreamSamplesPartlyMissing(t *testing.T) {
	p := sampleProvider(t)
	ctx := context.Background()
	const samples = `SELECT count(*), coalesce(bit_xor(hash(epoch, account, stream, bytes, messages, replica_bytes, seq, created,
  prev_epoch, prev_seq, prev_created)), 0) FROM stream_samples`
	var rows int
	var sum, after uint64
	if err := p.St.DB().QueryRow(samples).Scan(&rows, &sum); err != nil {
		t.Fatal(err)
	}
	want := readSamples(t, p.St)
	unsample(t, p.St, sampleAt(33))
	cover := map[bool]bool{}
	for _, r := range sampleRanges() {
		ok, err := p.streamSamplesCover(ctx, p.seriesLookback("stream.msgs_rate", r.Start.AsTime()), r.End.AsTime())
		if err != nil {
			t.Fatal(err)
		}
		cover[ok] = true
	}
	if !cover[true] || !cover[false] {
		t.Fatalf("the ranges do not include both a sampled and an unsampled one: %v", cover)
	}
	compareSampleReads(t, readSamples(t, p.St), want)
	filled, err := p.St.FillStreamSamples(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if filled != sampleEpochs-33 {
		t.Errorf("filled %d epochs, want %d", filled, sampleEpochs-33)
	}
	var rowsAfter int
	if err := p.St.DB().QueryRow(samples).Scan(&rowsAfter, &after); err != nil {
		t.Fatal(err)
	}
	if rowsAfter != rows || after != sum {
		t.Errorf("after the fill %d samples (hash %d), want the written %d (hash %d)", rowsAfter, after, rows, sum)
	}
	compareSampleReads(t, readSamples(t, p.St), want)
}
