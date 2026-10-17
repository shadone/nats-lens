package events

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/shadone/nats-lens/internal/testcluster"
)

// The scoped system and account users of the README subscribe to every
// subject the capture needs and resolve the account name.
func TestCaptureWithScopedUsers(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a NATS server")
	}
	c := testcluster.Start(t, 1)
	w := &flakyWriter{}
	capture := New(Options{Writer: w, FlushInterval: 50 * time.Millisecond, Log: quiet()})
	stop := runCapture(t, capture)
	connect := func(label, user, pass string) *nats.Conn {
		nc, err := nats.Connect(c.URL(0), append(capture.ConnHandlers(label), nats.UserInfo(user, pass))...)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(nc.Close)
		return nc
	}
	if err := capture.SubscribeSys(connect("sys", testcluster.ScopedSysUser, testcluster.ScopedSysPass)); err != nil {
		t.Fatal(err)
	}
	if err := capture.SubscribeAccount("app-label", connect("app-label", testcluster.ScopedAppUser, testcluster.ScopedAppPass)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for capture.resolveRequests("app-label") == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	js, err := jetstream.New(c.Connect(0, testcluster.AppUser, testcluster.AppPass))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := js.CreateStream(context.Background(), jetstream.StreamConfig{Name: "jobs", Subjects: []string{"jobs.>"}}); err != nil {
		t.Fatal(err)
	}
	var created *Event
	for deadline = time.Now().Add(5 * time.Second); created == nil && time.Now().Before(deadline); {
		time.Sleep(50 * time.Millisecond)
		w.mu.Lock()
		for i := range w.evs {
			if w.evs[i].Kind == "stream.created" {
				created = &w.evs[i]
			}
		}
		w.mu.Unlock()
	}
	stop()
	if capture.resolveDenied("app-label") {
		t.Error("account name lookup refused for the scoped account user")
	}
	if created == nil {
		t.Fatalf("no stream.created event; kinds %v", kinds(w))
	}
	if created.Account != "APP" {
		t.Errorf("stream.created account = %q, want the resolved name APP", created.Account)
	}
	for _, k := range kinds(w) {
		if k == "collector.subscription_error" || k == "collector.connection_closed" {
			t.Fatalf("permission problem stored: %v", kinds(w))
		}
	}
}
