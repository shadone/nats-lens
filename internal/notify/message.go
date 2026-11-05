// Package notify builds the messages nats-lens sends to notification
// channels, writes them in each channel type's body and sends them.
package notify

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Kind is what happened to a finding, as far as one channel is told.
type Kind string

// The kinds of message. Every kind but KindTest can be selected with a
// channel's kinds (config.ChannelKinds).
const (
	KindFiring       Kind = "firing"
	KindChanged      Kind = "changed"
	KindResolved     Kind = "resolved"
	KindFlapping     Kind = "flapping"
	KindMuted        Kind = "muted"
	KindReminder     Kind = "reminder"
	KindAcknowledged Kind = "acknowledged"
	// KindTest is the message of natslens notify test; it is about no
	// finding.
	KindTest Kind = "test"
)

// Kinds lists every kind, in the order the documentation lists them.
var Kinds = []Kind{KindFiring, KindChanged, KindResolved, KindFlapping, KindMuted, KindReminder, KindAcknowledged, KindTest}

// Mute is the mute or acceptance a muted message, or a firing message
// whose mute ended, refers to.
type Mute struct {
	// Accepted marks an acceptance rather than a mute.
	Accepted bool
	By       string
	Reason   string
	// Until is when the mute ends, or the acceptance's review date.
	Until time.Time
	// EndReason says why nats-lens ended an acceptance; empty otherwise.
	EndReason string
}

// Message is one notification about one finding. A zero field is unset
// and left out of the webhook body.
type Message struct {
	Kind Kind
	// Reason says why the message was sent when the kind alone does not
	// ("mute ended", "severity now info").
	Reason    string
	FindingID string
	CheckCode string
	CheckName string
	Category  string
	Severity  string
	// PreviousSeverity is the severity the channel was last sent, for a
	// changed message.
	PreviousSeverity string
	EntityType       string
	EntityKey        string
	// EntityName is the entity's readable name, such as an account's,
	// when it differs from the key.
	EntityName string
	Detail     string
	// Since is when the finding's episode began.
	Since time.Time
	// ConditionSince is when the condition began, when the check knows.
	ConditionSince time.Time
	TransitionedAt time.Time
	ResolvedAt     time.Time
	Flapping       bool
	Value          *float64
	Mute           *Mute
	// Link is the finding's page in the UI; empty without ui_url.
	Link string
	// DedupKey is the same each time this message is sent.
	DedupKey string
}

// Batch is the messages of one request to one channel.
type Batch struct {
	SystemID   string
	SystemName string
	// Version is nats-lens's version.
	Version string
	SentAt  time.Time
	// Key is sent as Idempotency-Key: the same for every attempt at the
	// same batch.
	Key string
	// UIURL is notifications.ui_url, for the Slack body's link to
	// Findings; empty means no link.
	UIURL    string
	Messages []Message
}

// FindingLink is the address of a finding's page, as the UI's own links
// write it; empty without a ui_url.
func FindingLink(uiURL, systemID, findingID string) string {
	if uiURL == "" {
		return ""
	}
	return strings.TrimRight(uiURL, "/") + "/findings/" + url.PathEscape(findingID) + "?system=" + url.QueryEscape(systemID)
}

// FindingsLink is the address of the Findings page of a system; empty
// without a ui_url.
func FindingsLink(uiURL, systemID string) string {
	if uiURL == "" {
		return ""
	}
	return strings.TrimRight(uiURL, "/") + "/findings?system=" + url.QueryEscape(systemID)
}

// DedupKey identifies a message: the system, the finding, the epoch its
// episode started at, the kind and the epoch it was decided at.
func DedupKey(systemID, findingID string, sinceEpoch int64, kind Kind, epoch int64) string {
	return systemID + ":" + findingID + ":" + strconv.FormatInt(sinceEpoch, 10) + ":" + string(kind) + ":" + strconv.FormatInt(epoch, 10)
}

// TestBatch is the batch natslens notify test sends: one message of kind
// test, about no finding.
func TestBatch(systemID, systemName, version, uiURL string, now time.Time) Batch {
	now = now.UTC()
	return Batch{
		SystemID: systemID, SystemName: systemName, Version: version, SentAt: now, UIURL: uiURL,
		Key: systemID + ":test:" + strconv.FormatInt(now.UnixMicro(), 10),
		Messages: []Message{{
			Kind:     KindTest,
			Detail:   "nats-lens test message from " + systemID,
			Link:     FindingsLink(uiURL, systemID),
			DedupKey: systemID + ":test:" + strconv.FormatInt(now.UnixMicro(), 10),
		}},
	}
}
