package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/shadone/nats-lens/internal/collect"
)

func TestSourceHealthStored(t *testing.T) {
	s := openMem(t)
	a := &server.StreamSource{Name: "orders", External: &server.ExternalStream{ApiPrefix: "$JS.eu.API"}, FilterSubject: "orders.eu.>"}
	b := &server.StreamSource{Name: "orders", SubjectTransforms: []server.SubjectTransformConfig{{Source: "orders.us.>", Destination: "us.>"}}}
	write := func(n int, configs []*server.StreamSource, reports []*server.StreamSourceInfo, leader string) {
		snap := collect.NewSnapshot(t0.Add(time.Duration(n) * time.Minute))
		mustApply(t, snap, collect.Jsz, envelope(t, "nats-0", "S0", server.JSInfo{AccountDetails: []*server.AccountDetail{{Id: "APP", Streams: []server.StreamDetail{{Name: "combined", Created: t0.Add(-time.Hour), Config: &server.StreamConfig{Name: "combined", Storage: server.FileStorage, Sources: configs}, Cluster: &server.ClusterInfo{Leader: leader}, Sources: reports}}}}}))
		snap.FinishedAt = t0.Add(time.Duration(n) * time.Minute)
		if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
			t.Fatal(err)
		}
	}
	reports := []*server.StreamSourceInfo{{Name: "orders", External: a.External, FilterSubject: a.FilterSubject, Active: -1, Lag: 9007199254740993, Error: &server.ApiError{Code: 404, ErrCode: 10059, Description: "stream 'orders' not found\x00detail"}}, {Name: "orders", SubjectTransforms: b.SubjectTransforms, Active: time.Duration(9007199254740993)}}
	write(0, []*server.StreamSource{a, b, nil}, reports, "nats-0")
	write(1, []*server.StreamSource{b, a}, reports, "nats-0")
	write(2, []*server.StreamSource{a, b}, nil, "nats-0")
	write(3, []*server.StreamSource{a, b}, reports, "nats-1")
	var active, lag int64
	var code, errcode int
	var desc string
	if err := s.DB().QueryRow(`SELECT active_ns,lag,error_code,error_id,error FROM stream_source_stats WHERE epoch=1 AND filter_subject<>''`).Scan(&active, &lag, &code, &errcode, &desc); err != nil {
		t.Fatal(err)
	}
	if active != -1 || lag != 9007199254740993 || code != 404 || errcode != 10059 || desc != "stream 'orders' not found\x00detail" {
		t.Fatalf("lost source state: %d %d %d %d %q", active, lag, code, errcode, desc)
	}
	if n := count(t, s.DB(), `SELECT count(DISTINCT source_id) FROM stream_source_stats`); n != 2 {
		t.Fatalf("identities after reorder: %d", n)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM stream_source_stats WHERE epoch IN (3,4) AND NOT reported AND active_ns IS NULL AND lag IS NULL AND error IS NULL`); n != 4 {
		t.Fatalf("unknown reports: %d", n)
	}
	var clean sql.NullString
	if err := s.DB().QueryRow(`SELECT error FROM stream_source_stats WHERE epoch=1 AND filter_subject=''`).Scan(&clean); err != nil || clean.Valid {
		t.Fatalf("clean error: %v %v", clean, err)
	}
	a.OptStartSeq = 12
	write(4, []*server.StreamSource{a}, reports, "nats-0")
	if n := count(t, s.DB(), `SELECT count(DISTINCT source_id) FROM stream_source_stats`); n != 3 {
		t.Fatalf("changed start identity: %d", n)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM stream_source_stats WHERE epoch=5`); n != 1 {
		t.Fatalf("removed source: %d", n)
	}
}
