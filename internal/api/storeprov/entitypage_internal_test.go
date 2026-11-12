package storeprov

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type entityPageDB struct {
	*sql.DB
	queries     int
	historyKeys [][]string
}

func (d *entityPageDB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	d.queries++
	if strings.Contains(q, "WITH per AS") && strings.Contains(q, "SELECT unnest(?::VARCHAR[])") {
		found := false
		for _, a := range args {
			if keys, ok := a.([]string); ok {
				d.historyKeys = append(d.historyKeys, append([]string{}, keys...))
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("unbounded page history query")
		}
	}
	return d.DB.QueryContext(ctx, q, args...)
}
func (d *entityPageDB) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	d.queries++
	return d.DB.QueryRowContext(ctx, q, args...)
}

func pageScaleProvider(t testing.TB, n int) (*Provider, *entityPageDB, *v1.TimeRange) {
	t.Helper()
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	at := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	for _, q := range []string{
		`INSERT INTO epochs(epoch,started_at,finished_at,duration_ms,expected_servers,observed_servers,expected_names,observed_names,missing_names,endpoint_failures,partial,errors) SELECT e+29, TIMESTAMP '2026-10-17 06:00:00' + e * INTERVAL 1 HOUR,TIMESTAMP '2026-10-17 06:00:00' + e * INTERVAL 1 HOUR,0,1,1,['nats-0'],['nats-0'],[],0,false,[] FROM range(1,9) t(e)`,
		`INSERT INTO epochs(epoch,started_at,finished_at,duration_ms,expected_servers,observed_servers,expected_names,observed_names,missing_names,endpoint_failures,partial,errors) SELECT e, TIMESTAMP '2026-10-09 18:00:00' + e * INTERVAL 6 HOUR,TIMESTAMP '2026-10-09 18:00:00' + e * INTERVAL 6 HOUR,0,1,1,['nats-0'],['nats-0'],[],0,false,[] FROM range(1,30) t(e)`,
		`INSERT INTO stream_stats(epoch,server_id,account,stream,messages,bytes,is_leader,leader) SELECT e,'S0','APP','orders-' || lpad(i::VARCHAR,5,'0'),i,1000*i+e*10,true,'nats-0' FROM range(1,30) t(e) CROSS JOIN range(0,?) s(i)`,
		`INSERT INTO servers(server_id,server_name,first_epoch,last_epoch,first_seen_at,last_seen_at) VALUES ('S0','nats-0',1,37,TIMESTAMP '2026-10-17 07:00:00',TIMESTAMP '2026-10-17 14:00:00')`,
		`INSERT INTO stream_stats(epoch,server_id,account,stream,messages,bytes,is_leader,leader) SELECT e+29,'S0','APP','orders-' || lpad(i::VARCHAR,5,'0'),i,1000*i+e*100,true,'nats-0' FROM range(1,9) t(e) CROSS JOIN range(0,?) s(i)`,
		`INSERT INTO consumer_stats(epoch,server_id,account,stream,consumer,created,is_leader,delivered_consumer_seq) SELECT e+29,'S0','APP','orders-00000','worker-' || lpad(i::VARCHAR,5,'0'),TIMESTAMP '2026-10-17 06:00:00',true,i*e FROM range(1,9) t(e) CROSS JOIN range(0,?) s(i)`,
		`INSERT INTO consumer_config(server_id,account,stream,consumer,first_epoch,last_epoch,config_hash,durable_name) SELECT 'S0','APP','orders-00000','worker-' || lpad(i::VARCHAR,5,'0'),1,37,'h','worker-' || i FROM range(0,?) t(i)`,
	} {
		var args []any
		if strings.Contains(q, "?") {
			args = []any{n}
		}
		if _, err = st.DB().ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	spy := &entityPageDB{DB: st.DB()}
	p := &Provider{St: st, Interval: 30 * time.Second, Clock: func() time.Time { return at.Add(time.Minute) }, readDB: spy}
	return p, spy, &v1.TimeRange{Start: ts(at.Add(-8 * time.Hour)), End: ts(at.Add(time.Minute))}
}

func TestEntityListScaleAndEnrichmentScope(t *testing.T) {
	p, spy, r := pageScaleProvider(t, 1200)
	first, err := p.ListStreams(context.Background(), &v1.ListStreamsRequest{Range: r, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	firstQueries := spy.queries
	if len(first.Streams) != 10 || first.TotalCount != 1200 || first.NextPageToken == "" {
		t.Fatal(first)
	}
	if len(spy.historyKeys) != 2 {
		t.Fatalf("history reads %d, want two batched reads", len(spy.historyKeys))
	}
	for _, keys := range spy.historyKeys {
		if len(keys) != 10 {
			t.Fatalf("enriched %d entities for page of 10", len(keys))
		}
		for i, key := range keys {
			if key != first.Streams[i].Entity.Key {
				t.Fatal(keys)
			}
		}
	}
	for _, s := range first.Streams {
		if s.BytesPerDay == nil || len(s.GetBytes_7D().GetPoints()) != 28 {
			t.Fatalf("growth/series missing: %v", s)
		}
	}
	b, _ := protojson.Marshal(first)
	if len(b) > api.MaxEntityPageBytes || proto.Size(first) > api.MaxEntityPageBytes {
		t.Fatal("response exceeds budget")
	}
	e, _, err := p.latestEpoch(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	legacy, totals, err := p.streamRows(context.Background(), e, r, "", "")
	if err != nil {
		t.Fatal(err)
	}
	legacyJSON, _ := protojson.Marshal(&v1.ListStreamsResponse{Streams: legacy, Totals: totals})
	if len(legacyJSON) <= 1<<20 {
		t.Fatalf("scale fixture no longer crosses the existing 1 MiB send cap: %d", len(legacyJSON))
	}
	t.Logf("legacy unbounded response: %d JSON bytes for 1200 streams with 28 observed sparkline buckets", len(legacyJSON))
	seen := map[string]bool{}
	token := ""
	var totalBytes int64
	for {
		res, err := p.ListStreams(context.Background(), &v1.ListStreamsRequest{Range: r, Limit: 1000, PageToken: token})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Streams) > 100 || res.TotalCount != 1200 {
			t.Fatal("unbounded page or false count")
		}
		for _, s := range res.Streams {
			if seen[s.Entity.Key] {
				t.Fatal("duplicate", s.Entity.Key)
			}
			seen[s.Entity.Key] = true
			totalBytes += s.GetBytes()
		}
		if res.NextPageToken == "" {
			if totalBytes != res.Totals.Bytes {
				t.Fatalf("loaded sum %d != all-match total %d", totalBytes, res.Totals.Bytes)
			}
			break
		}
		token = res.NextPageToken
	}
	if len(seen) != 1200 {
		t.Fatal(len(seen))
	}
	// New live writes cannot reorder a retained context or alter its totals.
	for _, q := range []string{
		`INSERT INTO epochs SELECT * REPLACE (38 AS epoch, TIMESTAMP '2026-10-17 14:00:30' AS started_at, TIMESTAMP '2026-10-17 14:00:30' AS finished_at) FROM epochs WHERE epoch=37`,
		`INSERT INTO stream_stats SELECT * REPLACE (38 AS epoch, 0 AS bytes) FROM stream_stats WHERE epoch=37`,
		`INSERT INTO stream_stats(epoch,server_id,account,stream,messages,bytes,is_leader,leader) VALUES (38,'S0','APP','orders-00000-new',1,7,true,'nats-0')`,
	} {
		if _, err = p.St.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	before := spy.queries
	next, err := p.ListStreams(context.Background(), &v1.ListStreamsRequest{Range: r, Limit: 10, PageToken: first.NextPageToken})
	if err != nil {
		t.Fatal(err)
	}
	if next.Epoch != first.Epoch || next.Totals.Bytes != first.Totals.Bytes || next.Streams[0].Entity.Key != "APP/orders-00010" {
		t.Fatal("live update shifted traversal")
	}
	if spy.queries-before != 5 {
		t.Fatalf("continuation queries %d, want epoch check + two history batches, each with its samples check", spy.queries-before)
	}
	fresh, err := p.ListStreams(context.Background(), &v1.ListStreamsRequest{Range: r, SummaryOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Epoch != 38 || fresh.TotalCount != 1201 {
		t.Fatal("fresh traversal missed the new collection")
	}
	if _, err = p.St.DB().Exec(`DELETE FROM epochs WHERE epoch=37`); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ListStreams(context.Background(), &v1.ListStreamsRequest{Range: r, PageToken: first.NextPageToken}); !errors.Is(err, api.ErrFailedPrecondition) {
		t.Fatal(err)
	}
	t.Logf("1200 matches: first page 10 entities, %d counted queries (2 history), %d JSON bytes, %d protobuf bytes; continuation 3 queries", firstQueries, len(b), proto.Size(first))
}

func TestConsumerListScaleTraversalAndCancellation(t *testing.T) {
	p, spy, r := pageScaleProvider(t, 1200)
	seen := map[string]bool{}
	token := ""
	for {
		before := spy.queries
		res, err := p.ListConsumers(context.Background(), &v1.ListConsumersRequest{Range: r, PageToken: token})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Consumers) > 100 || res.TotalCount != 1200 {
			t.Fatalf("%d/%d", len(res.Consumers), res.TotalCount)
		}
		b, _ := protojson.Marshal(res)
		if len(b) > api.MaxEntityPageBytes {
			t.Fatal(len(b))
		}
		if token != "" && spy.queries-before != 1 {
			t.Fatal("continuation rebuilt consumer ranking")
		}
		for _, c := range res.Consumers {
			if seen[c.Entity.Key] {
				t.Fatal("duplicate")
			}
			seen[c.Entity.Key] = true
		}
		if res.NextPageToken == "" {
			break
		}
		token = res.NextPageToken
	}
	if len(seen) != 1200 {
		t.Fatal(len(seen))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := spy.queries
	if _, err := p.ListStreams(ctx, &v1.ListStreamsRequest{Range: r}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := p.ListConsumers(ctx, &v1.ListConsumersRequest{Range: r}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if spy.queries != before {
		t.Fatal("canceled request queried")
	}
}

func TestEntityListSerializedBudgetAndReadLimit(t *testing.T) {
	p, _, r := pageScaleProvider(t, 200)
	if _, err := p.St.DB().Exec(`UPDATE stream_stats SET leader=repeat('x',7000) WHERE epoch=37 AND stream < 'orders-00100'`); err != nil {
		t.Fatal(err)
	}
	res, err := p.ListStreams(context.Background(), &v1.ListStreamsRequest{Range: r})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := protojson.Marshal(res)
	if len(res.Streams) >= 100 || len(res.Streams) == 0 || res.TotalCount != 200 || res.NextPageToken == "" || len(b) > api.MaxEntityPageBytes {
		t.Fatalf("%d rows, %d matches, %d bytes", len(res.Streams), res.TotalCount, len(b))
	}
	next, err := p.ListStreams(context.Background(), &v1.ListStreamsRequest{Range: r, PageToken: res.NextPageToken})
	if err != nil {
		t.Fatal(err)
	}
	if next.Streams[0].Entity.Key == res.Streams[len(res.Streams)-1].Entity.Key {
		t.Fatal("byte-bound page repeated boundary")
	}
	if _, err = p.St.DB().Exec(`UPDATE stream_stats SET leader=repeat('x',600000) WHERE epoch=37 AND stream='orders-00000'`); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ListStreams(context.Background(), &v1.ListStreamsRequest{Range: r, Limit: 1}); !errors.Is(err, api.ErrResourceExhausted) {
		t.Fatal("oversized entity", err)
	}
}

// Run only in a coordinated timing window. Compare work and allocations as well
// as wall time: the legacy path builds history for every match before limiting.
func BenchmarkStreamListPageEnrichment(b *testing.B) {
	for _, legacy := range []bool{true, false} {
		name := "page10"
		if legacy {
			name = "legacy1200"
		}
		b.Run(name, func(b *testing.B) {
			p, _, r := pageScaleProvider(b, 1200)
			r.Start = ts(r.End.AsTime().Add(-7 * 24 * time.Hour))
			e, _, err := p.latestEpoch(context.Background(), r)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			if legacy {
				b.ReportMetric(1200, "enriched_entities/op")
			} else {
				b.ReportMetric(10, "enriched_entities/op")
			}
			b.ResetTimer()
			for range b.N {
				if legacy {
					ss, totals, err := p.streamRows(context.Background(), e, r, "", "")
					if err != nil {
						b.Fatal(err)
					}
					if _, err = p.streamStatus(context.Background(), r, e, ss); err != nil {
						b.Fatal(err)
					}
					api.LimitStreams(&v1.ListStreamsResponse{Streams: ss, Totals: totals}, 10)
				} else {
					if _, err := p.ListStreams(context.Background(), &v1.ListStreamsRequest{Range: r, Limit: 10}); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

type cancelHistoryDB struct {
	queryReader
	cancel context.CancelFunc
}

func (d *cancelHistoryDB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	if strings.Contains(q, "WITH per AS") && strings.Contains(q, "SELECT unnest(?::VARCHAR[])") {
		d.cancel()
	}
	return d.queryReader.QueryContext(ctx, q, args...)
}
func TestStreamPageCancelsHistoryRead(t *testing.T) {
	p, spy, r := pageScaleProvider(t, 100)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.readDB = &cancelHistoryDB{queryReader: spy, cancel: cancel}
	if _, err := p.ListStreams(ctx, &v1.ListStreamsRequest{Range: r, Limit: 10}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(spy.historyKeys) != 1 {
		t.Fatal("canceled first history read still attempted the second batch")
	}
}
func TestEntityListRankingReadLimit(t *testing.T) {
	p, _, r := pageScaleProvider(t, api.MaxListRankingRows+1)
	if _, err := p.ListStreams(context.Background(), &v1.ListStreamsRequest{Range: r, Limit: 1}); !errors.Is(err, api.ErrResourceExhausted) {
		t.Fatal("unbounded stream ranking", err)
	}
	if _, err := p.ListConsumers(context.Background(), &v1.ListConsumersRequest{Range: r, Limit: 1}); !errors.Is(err, api.ErrResourceExhausted) {
		t.Fatal("unbounded consumer ranking", err)
	}
}

func TestStreamNameDiscoverySkipsHistory(t *testing.T) {
	p, spy, r := pageScaleProvider(t, 1200)
	res, err := p.ListStreams(context.Background(), &v1.ListStreamsRequest{Range: r, SummaryOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Streams) != 100 || res.TotalCount != 1200 || len(spy.historyKeys) != 0 {
		t.Fatalf("%d/%d, %d history reads", len(res.Streams), res.TotalCount, len(spy.historyKeys))
	}
	for _, s := range res.Streams {
		if s.BytesPerDay != nil || s.Bytes_7D != nil {
			t.Fatal("summary-only list read history")
		}
	}
}
