package storeprov_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/events"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// fixtureRange holds the fixture's three epochs, one 30-second strip
// bucket each.
func fixtureRange() *v1.TimeRange {
	return &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(90 * time.Second))}
}

func summaryOf(t *testing.T, st *store.Store, live bool, r *v1.TimeRange) *v1.SystemSummary {
	t.Helper()
	set, err := checks.Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: live, Checks: set, Clock: func() time.Time { return now }})
	res := must(c.ListSystems(ctx, connect.NewRequest(&v1.ListSystemsRequest{Range: r, StripBuckets: 3})))
	return res.GetSystems()[0]
}

func strip(s *v1.SystemSummary) string {
	var out []string
	for _, c := range s.GetStrip() {
		out = append(out, strings.TrimPrefix(c.String(), "STATUS_"))
	}
	return strings.Join(out, " ")
}

func TestSystemSummaryFromTheStore(t *testing.T) {
	st, _, _ := findingsStore(t)
	s := summaryOf(t, st, true, fixtureRange())
	if s.GetError() != "" {
		t.Fatalf("error %q", s.GetError())
	}
	// server-002 fires in epoch 2 only, consumer-010 is critical in epoch 2
	// and warning in 3, stream-005 is info throughout.
	if got := strip(s); got != "INFO CRITICAL WARNING" {
		t.Errorf("strip = %q", got)
	}
	if !s.GetStripStart().AsTime().Equal(t0) || s.GetStripBucket().AsDuration() != 30*time.Second {
		t.Errorf("strip start %v, bucket %v", s.GetStripStart(), s.GetStripBucket())
	}
	if s.GetServers() != 3 || s.GetServersAnswering() != 2 || s.GetClusters() != 1 || s.GetConnections() != 4 {
		t.Errorf("servers %d, answering %d, clusters %d, connections %d", s.GetServers(), s.GetServersAnswering(), s.GetClusters(), s.GetConnections())
	}
	var streams int32
	if err := st.DB().QueryRow(`SELECT count(DISTINCT (account, stream)) FROM stream_stats WHERE epoch = 3`).Scan(&streams); err != nil {
		t.Fatal(err)
	}
	if s.Streams == nil || s.GetStreams() != streams || streams == 0 {
		t.Errorf("streams = %v, want %d", s.Streams, streams)
	}
	if s.GetStatus() != v1.Status_STATUS_WARNING || s.GetWorstFinding().GetId() != missingID ||
		s.GetTally().GetWarning() != 1 || s.GetTally().GetInfo() != 1 || s.GetTally().GetResolved_24H() != 1 {
		t.Errorf("status %v, worst %v, tally %v", s.GetStatus(), s.GetWorstFinding().GetId(), s.GetTally())
	}
	if s.GetFreshness().GetEpoch() != 3 || s.GetCollection() != v1.CollectionState_COLLECTION_STATE_COLLECTING || s.GetCollectionError() != "" {
		t.Errorf("freshness %v, collection %v %q", s.GetFreshness(), s.GetCollection(), s.GetCollectionError())
	}
}

// A range without an epoch leaves every count unset and the status no
// data.
func TestSystemSummaryWithoutAnEpoch(t *testing.T) {
	st, _, _ := findingsStore(t)
	r := &v1.TimeRange{Start: timestamppb.New(t0.Add(-time.Hour)), End: timestamppb.New(t0.Add(-time.Hour + 90*time.Second))}
	s := summaryOf(t, st, true, r)
	if s.Servers != nil || s.ServersAnswering != nil || s.Clusters != nil || s.Streams != nil || s.Connections != nil {
		t.Errorf("counts without an epoch: %v", s)
	}
	if s.GetStatus() != v1.Status_STATUS_NO_DATA || strip(s) != "NO_DATA NO_DATA NO_DATA" || s.GetWorstFinding() != nil {
		t.Errorf("status %v, strip %q, worst %v", s.GetStatus(), strip(s), s.GetWorstFinding())
	}
}

// Without a runtime status, the collection error comes from the newest
// connection_closed or not_connected event of the collector connection
// after the newest epoch and the newest reconnect; the error began at the
// oldest such event.
func TestSystemSummaryCollectionErrorFromEvents(t *testing.T) {
	conn := func(label string, at time.Duration, kind, reason string) events.Event {
		raw, _ := json.Marshal(map[string]string{"connection": label, "reason": reason})
		return events.Event{Time: t0.Add(at), ReceivedAt: t0.Add(at), Source: events.SourceCollector, Kind: kind,
			Reason: reason, Summary: kind, Raw: raw}
	}
	collector := func(at time.Duration, kind, reason string) events.Event { return conn("collector", at, kind, reason) }
	t.Run("before the newest epoch", func(t *testing.T) {
		st, _, _ := findingsStore(t)
		if err := st.WriteEvents(ctx, []events.Event{collector(10*time.Second, "collector.connection_closed", "nats: authorization violation")}); err != nil {
			t.Fatal(err)
		}
		s := summaryOf(t, st, true, &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(now)})
		if s.GetCollection() != v1.CollectionState_COLLECTION_STATE_COLLECTING || s.GetCollectionError() != "" {
			t.Errorf("collection %v %q", s.GetCollection(), s.GetCollectionError())
		}
	})
	t.Run("after the newest epoch", func(t *testing.T) {
		st, _, _ := findingsStore(t)
		if err := st.WriteEvents(ctx, []events.Event{
			collector(70*time.Second, "collector.not_connected", "no server reachable"),
			collector(75*time.Second, "collector.connection_closed", "nats: authorization violation"),
			collector(80*time.Second, "collector.reconnected", ""),
			collector(82*time.Second, "collector.not_connected", "no server reachable"),
			collector(85*time.Second, "collector.connection_closed", "nats: authorization violation"),
			// The events connections' own losses do not stop the epochs.
			conn("sys", 88*time.Second, "collector.not_connected", "no server reachable"),
		}); err != nil {
			t.Fatal(err)
		}
		s := summaryOf(t, st, true, &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(now)})
		if s.GetCollection() != v1.CollectionState_COLLECTION_STATE_CONNECTION_CLOSED || s.GetCollectionError() != "nats: authorization violation" ||
			!s.GetCollectionErrorSince().AsTime().Equal(t0.Add(82*time.Second)) {
			t.Errorf("collection %v %q since %v", s.GetCollection(), s.GetCollectionError(), s.GetCollectionErrorSince())
		}
	})
	t.Run("without a collector", func(t *testing.T) {
		st, _, _ := findingsStore(t)
		if s := summaryOf(t, st, false, fixtureRange()); s.GetCollection() != v1.CollectionState_COLLECTION_STATE_UNSPECIFIED {
			t.Errorf("collection %v", s.GetCollection())
		}
	})
}
