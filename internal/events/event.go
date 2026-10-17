// Package events captures what a snapshot misses: client connects and
// disconnects, server shutdowns, and JetStream advisories such as a consumer
// deleted between two polls, with the client that deleted it.
package events

import (
	"context"
	"encoding/json"
	"time"
)

// Sources of events.
const (
	SourceSys       = "sys"
	SourceJetStream = "jetstream"
	SourceCollector = "collector"
	// SourceK8s is a Kubernetes Event or a change of a watched pod or node.
	SourceK8s = "k8s"
)

// Event is one row of the events table.
type Event struct {
	// Time is when the event happened: the payload's timestamp when it has
	// one, otherwise ReceivedAt.
	Time time.Time
	// ReceivedAt is when the collector received the message.
	ReceivedAt time.Time
	// Source is sys, jetstream, k8s or collector.
	Source string
	// Type is the payload's type string; empty for payloads without one
	// (lame duck and shutdown) and for the collector's own events.
	Type string
	// Kind is a short normalized name such as consumer.deleted.
	Kind       string
	ServerName string
	Account    string
	// AccountName is the account's readable name (its name tag) when the
	// payload gives it; not stored with the event, it names the account.
	AccountName string
	Stream      string
	Consumer    string
	ClientName  string
	ClientHost  string
	ClientUser  string
	ClientLang  string
	// Reason is the disconnect, termination or other reason when given.
	Reason string
	// Summary is one human-readable line.
	Summary string
	// Raw is the payload as JSON, with user JWTs removed.
	Raw json.RawMessage

	// id is the payload's event ID. The server publishes cluster
	// advisories in the asset's account and in the system account with the
	// same ID; Capture stores the first copy only.
	id string
	// clientID is the client's connection ID on its server, for pairing a
	// connect with its disconnect.
	clientID uint64
}

// Writer stores batches of events.
type Writer interface {
	WriteEvents(ctx context.Context, evs []Event) error
}
