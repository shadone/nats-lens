package storeprov_test

import (
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// An entity without a finding is ok only when every check of its type ran
// ok in the epoch: one stream check that ran does not vouch for a stream
// whose other checks failed.
func TestNoDataWhileChecksOfTheTypeFail(t *testing.T) {
	st := fixtureStore(t)
	set, err := checks.Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	var runs []store.CheckRun
	for _, c := range set.Checks() {
		r := store.CheckRun{Code: c.Code, EntityType: c.Entity, Status: store.RunError, Error: "boom"}
		if c.Code == "stream-005" || strings.HasPrefix(c.Code, "server-") {
			r.Status, r.Error = store.RunOK, ""
		}
		runs = append(runs, r)
	}
	// Epoch 3 is the fixture's newest, with its streams and consumers.
	if _, err := st.ApplyCheckRuns(ctx, 3, runs); err != nil {
		t.Fatal(err)
	}
	c := serveAt(t, st, set, now)
	streams := must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{Range: hour()}))).GetStreams()
	consumers := must(c.ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{Range: hour()}))).GetConsumers()
	if len(streams) == 0 || len(consumers) == 0 {
		t.Fatalf("%d streams, %d consumers", len(streams), len(consumers))
	}
	for _, s := range streams {
		if s.GetStatus() == v1.Status_STATUS_OK {
			t.Errorf("stream %s is ok while most stream checks failed", s.GetEntity().GetKey())
		}
	}
	for _, s := range consumers {
		if s.GetStatus() == v1.Status_STATUS_OK {
			t.Errorf("consumer %s is ok while every consumer check failed", s.GetEntity().GetKey())
		}
	}
	// The server checks ran, so a server without a finding is ok.
	for _, s := range must(c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{Range: hour()}))).GetServers() {
		if s.GetStatus() != v1.Status_STATUS_OK {
			t.Errorf("server %s = %v while its checks ran", s.GetName(), s.GetStatus())
		}
	}
}
