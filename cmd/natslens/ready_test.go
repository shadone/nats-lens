package main

import (
	"testing"
	"time"
)

// Readiness means the store is open and the API serves. It must not depend
// on collection: with NATS down no epoch is stored, and a readiness tied to
// epochs takes the only replica out of the load balancer, so the UI that
// shows "not collecting" becomes unreachable exactly when it is needed.
func TestReadinessIgnoresCollection(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	r := newReadiness(func() time.Time { return now })
	if r.ready() {
		t.Fatal("ready before the API serves")
	}
	r.serving()
	if !r.ready() {
		t.Fatal("not ready once the store is open and the API serves, with no epoch stored")
	}
	now = now.Add(24 * time.Hour)
	if !r.ready() {
		t.Fatal("not ready after a day without a stored epoch (NATS down)")
	}
}
