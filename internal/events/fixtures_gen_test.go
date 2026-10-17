package events_test

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/shadone/nats-lens/internal/events"
	"github.com/shadone/nats-lens/internal/testcluster"
)

var update = flag.Bool("update", false, "record event fixtures from an in-process cluster into testdata/")

// fixture is one recorded message: the subject, the account label it was
// received under (empty for the system account) and the payload.
type fixture struct {
	Subject string          `json:"subject"`
	Account string          `json:"account,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

// recorder keeps the first message seen per fixture name.
type recorder struct {
	mu   sync.Mutex
	seen map[string]fixture
}

func (r *recorder) add(name string, f fixture) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.seen[name]; !ok {
		r.seen[name] = f
	}
}

// fixtureName names a message by its type, or its subject's last tokens for
// payloads without one; auth errors and disconnects share a type.
func fixtureName(subject string, payload []byte) string {
	var h struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(payload, &h)
	toks := strings.Split(subject, ".")
	switch {
	case strings.HasSuffix(subject, ".CLIENT.AUTH.ERR"):
		return "auth_error"
	case h.Type == "":
		return strings.ToLower(toks[len(toks)-1])
	}
	name := h.Type[strings.LastIndex(h.Type, ".")+1:]
	if name == "api_audit" {
		var a struct {
			Subject string `json:"subject"`
		}
		_ = json.Unmarshal(payload, &a)
		if at := strings.Split(a.Subject, "."); len(at) >= 4 {
			name += "_" + strings.ToLower(at[2]+"_"+at[3])
		}
	}
	if name == "consumer_action" || name == "stream_action" {
		var a struct {
			Action string `json:"action"`
		}
		_ = json.Unmarshal(payload, &a)
		name += "_" + a.Action
	}
	return name
}

// TestGenerateFixtures records real payloads for the decoder tests. Run it
// with: go test ./internal/events -run TestGenerateFixtures -update
func TestGenerateFixtures(t *testing.T) {
	if !*update {
		t.Skip("run with -update to record fixtures")
	}
	ctx := context.Background()
	c := testcluster.Start(t, 3)
	rec := &recorder{seen: map[string]fixture{}}
	sys := c.Connect(0, testcluster.SysUser, testcluster.SysPass)
	for _, s := range events.SysSubjects(true) {
		if _, err := sys.Subscribe(s, func(m *nats.Msg) {
			rec.add(fixtureName(m.Subject, m.Data), fixture{Subject: m.Subject, Payload: m.Data})
		}); err != nil {
			t.Fatal(err)
		}
	}
	app := c.Connect(0, testcluster.AppUser, testcluster.AppPass)
	for _, s := range events.JSSubjects {
		if _, err := app.Subscribe(s, func(m *nats.Msg) {
			rec.add(fixtureName(m.Subject, m.Data), fixture{Subject: m.Subject, Account: "APP", Payload: m.Data})
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := errors(sys.Flush(), app.Flush()); err != nil {
		t.Fatal(err)
	}
	js, err := jetstream.New(app)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: "order-jobs", Subjects: []string{"orders.>"}, Replicas: 3})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := js.UpdateStream(ctx, jetstream.StreamConfig{Name: "order-jobs", Subjects: []string{"orders.>"}, Replicas: 3, Description: "jobs"}); err != nil {
		t.Fatal(err)
	}
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable: "resize-worker", AckPolicy: jetstream.AckExplicitPolicy, MaxDeliver: 2,
		AckWait: 200 * time.Millisecond, SampleFrequency: "100%",
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if _, err := js.Publish(ctx, "orders.p1.j1.resize.created", []byte("job")); err != nil {
			t.Fatal(err)
		}
	}
	// One message each: acked (ack metric), negatively acknowledged,
	// terminated, and left alone until max deliveries.
	msgs, err := cons.Fetch(4, jetstream.FetchMaxWait(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	i := 0
	for m := range msgs.Messages() {
		switch i {
		case 0:
			_ = m.Ack()
		case 1:
			_ = m.Nak()
		case 2:
			_ = m.TermWithReason("bad payload")
		}
		i++
	}
	time.Sleep(300 * time.Millisecond)
	// Second delivery of the unacknowledged ones reaches max_deliver.
	if again, err := cons.Fetch(4, jetstream.FetchMaxWait(time.Second)); err == nil {
		for range again.Messages() {
		}
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := js.PauseConsumer(ctx, "order-jobs", "resize-worker", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	// A client that connects and leaves, and a failed login.
	victim, err := nats.Connect(c.URL(1), nats.UserInfo(testcluster.AppUser, testcluster.AppPass), nats.Name("victim"))
	if err != nil {
		t.Fatal(err)
	}
	victim.Close()
	if bad, err := nats.Connect(c.URL(1), nats.UserInfo("app", "wrong"), nats.MaxReconnects(0)); err == nil {
		bad.Close()
	}

	// Lame duck the stream leader: leader elections for the stream and
	// consumer follow, plus lame duck and shutdown events. A meta leader
	// step-down gives a meta leader election.
	moveLeaders(t, app, "order-jobs", "resize-worker", "nats-2")
	if _, err := sys.Request("$JS.API.META.LEADER.STEPDOWN", nil, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	c.LameDuck(2)
	time.Sleep(5 * time.Second)

	deleter, err := nats.Connect(c.URL(0), nats.UserInfo(testcluster.AppUser, testcluster.AppPass), nats.Name("test-deleter"))
	if err != nil {
		t.Fatal(err)
	}
	defer deleter.Close()
	djs, _ := jetstream.New(deleter)
	if err := djs.DeleteConsumer(ctx, "order-jobs", "resize-worker"); err != nil {
		t.Fatal(err)
	}
	if err := djs.DeleteStream(ctx, "order-jobs"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	// A plain stop, for the shutdown event.
	c.Stop(1)
	time.Sleep(2 * time.Second)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	dir := filepath.Join("testdata", "events")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	for name, f := range rec.seen {
		b, err := json.MarshalIndent(f, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".json"), append(b, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("recorded %s from %s", name, f.Subject)
	}
}

// moveLeaders asks the stream's and the consumer's leaders to step down in
// favour of server preferred, and waits until both lead there.
func moveLeaders(t *testing.T, nc *nats.Conn, stream, consumer, preferred string) {
	t.Helper()
	body := []byte(`{"placement":{"preferred":"` + preferred + `"}}`)
	subjects := map[string]string{
		"$JS.API.STREAM.LEADER.STEPDOWN." + stream:                    "$JS.API.STREAM.INFO." + stream,
		"$JS.API.CONSUMER.LEADER.STEPDOWN." + stream + "." + consumer: "$JS.API.CONSUMER.INFO." + stream + "." + consumer,
	}
	for stepdown, info := range subjects {
		deadline := time.Now().Add(10 * time.Second)
		for {
			var r struct {
				Cluster struct {
					Leader string `json:"leader"`
				} `json:"cluster"`
			}
			if m, err := nc.Request(info, nil, 2*time.Second); err == nil && json.Unmarshal(m.Data, &r) == nil && r.Cluster.Leader == preferred {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: leader did not move to %s", stepdown, preferred)
			}
			_, _ = nc.Request(stepdown, body, 2*time.Second)
			time.Sleep(500 * time.Millisecond)
		}
	}
}

func errors(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
