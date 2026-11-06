package store

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

// TestConfigThatDoesNotEncodeIsNotStored gives a stream a retention policy
// and a consumer an ack policy that nats-server's JSON encoding refuses.
// They have no hash, so no configuration version is stored for them and
// the epoch's errors say so; their stats are stored, and the configuration
// of another stream is stored as usual.
func TestConfigThatDoesNotEncodeIsNotStored(t *testing.T) {
	s := openMem(t)
	snap := collect.NewSnapshot(t0)
	snap.Servers["N1"] = &collect.ServerView{Info: server.ServerInfo{ID: "N1", Name: "nats-0"},
		Jsz: &server.JSInfo{AccountDetails: []*server.AccountDetail{{Id: "APP", Streams: []server.StreamDetail{
			{Name: "bad", Created: t0, Config: &server.StreamConfig{Name: "bad", Storage: server.FileStorage, Retention: server.RetentionPolicy(99)},
				Consumer: []*server.ConsumerInfo{{Stream: "bad", Name: "c", Created: t0,
					Config: &server.ConsumerConfig{Durable: "c", AckPolicy: server.AckPolicy(99)}}}},
			{Name: "good", Created: t0, Config: &server.StreamConfig{Name: "good", Storage: server.FileStorage}},
		}}}}}
	snap.FinishedAt = t0
	epoch, err := s.WriteSnapshot(context.Background(), snap)
	if err != nil {
		t.Fatal(err)
	}
	db := s.DB()
	for _, c := range []struct {
		q    string
		want int
	}{
		{`SELECT count(*) FROM stream_config WHERE stream = 'bad'`, 0},
		{`SELECT count(*) FROM consumer_config`, 0},
		{`SELECT count(*) FROM stream_config WHERE stream = 'good'`, 1},
		{`SELECT count(*) FROM stream_stats`, 2},
		{`SELECT count(*) FROM consumer_stats`, 1},
		{`SELECT len(list_filter(errors, lambda x: x LIKE '%does not encode as JSON%')) FROM epochs WHERE epoch = ` + fmt.Sprint(epoch), 2},
	} {
		if got := count(t, db, c.q); got != c.want {
			t.Errorf("%s = %d, want %d", c.q, got, c.want)
		}
	}
}

// TestConfigThatDoesNotEncodeIsLoggedOnce writes three epochs with a
// stream configuration that does not encode, and a conflicting repeated
// report of another stream, and requires one log line for each.
func TestConfigThatDoesNotEncodeIsLoggedOnce(t *testing.T) {
	s := openMem(t)
	var buf bytes.Buffer
	s.SetLogger(slog.New(slog.NewTextHandler(&buf, nil)))
	for range 3 {
		snap := collect.NewSnapshot(t0)
		other := server.StreamDetail{Name: "other", Created: t0, Config: &server.StreamConfig{Name: "other", Storage: server.FileStorage}}
		changed := other
		changed.Config = &server.StreamConfig{Name: "other", Storage: server.FileStorage, Description: "again"}
		snap.Servers["N1"] = &collect.ServerView{Info: server.ServerInfo{ID: "N1", Name: "nats-0"},
			Jsz: &server.JSInfo{AccountDetails: []*server.AccountDetail{{Id: "APP", Streams: []server.StreamDetail{
				{Name: "bad", Created: t0, Config: &server.StreamConfig{Name: "bad"}}, other, changed}}}}}
		snap.FinishedAt = t0
		if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
			t.Fatal(err)
		}
	}
	if n := strings.Count(buf.String(), "does not encode as JSON"); n != 1 {
		t.Errorf("%d log lines for the configuration that does not encode, want 1:\n%s", n, buf.String())
	}
	if n := strings.Count(buf.String(), "conflicting duplicate"); n != 1 {
		t.Errorf("%d log lines for the conflicting duplicate, want 1:\n%s", n, buf.String())
	}
}
