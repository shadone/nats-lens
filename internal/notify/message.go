// Package notify builds the messages nats-lens sends to notification
// channels, writes them in each channel type's body and sends them.
package notify

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Kind is what happened to a finding, as far as one channel is told.
type Kind string

// The kinds of message. Every kind but KindBaseline and KindTest can be
// selected with a channel's kinds (config.ChannelKinds).
const (
	KindFiring       Kind = "firing"
	KindChanged      Kind = "changed"
	KindResolved     Kind = "resolved"
	KindFlapping     Kind = "flapping"
	KindMuted        Kind = "muted"
	KindReminder     Kind = "reminder"
	KindAcknowledged Kind = "acknowledged"
	// KindBaseline is the one message a channel gets when nats-lens starts
	// notifying it, or after its database was rebuilt: how many findings
	// fire, instead of one message each. It is about no finding, and a
	// channel's kinds cannot leave it out.
	KindBaseline Kind = "baseline"
	// KindTest is the message of natslens notify test; it is about no
	// finding.
	KindTest Kind = "test"
)

// Kinds lists every kind, in the order the documentation lists them.
var Kinds = []Kind{KindFiring, KindChanged, KindResolved, KindFlapping, KindMuted, KindReminder, KindAcknowledged, KindBaseline, KindTest}

// Mute is the mute or acceptance a muted message, or a firing message
// whose mute ended, refers to.
type Mute struct {
	// Accepted marks an acceptance rather than a mute.
	Accepted bool   `json:"accepted,omitempty"`
	By       string `json:"by,omitempty"`
	Reason   string `json:"reason,omitempty"`
	// Until is when the mute ends, or the acceptance's review date.
	Until time.Time `json:"until,omitzero"`
	// EndReason says why nats-lens ended an acceptance; empty otherwise.
	EndReason string `json:"end_reason,omitempty"`
}

// Acknowledgement is the API-stored actor, note and quiet time of an acknowledgement.
// A zero QuietUntil means until resolved.
type Acknowledgement struct {
	By            string    `json:"by"`
	Note          string    `json:"note,omitempty"`
	QuietUntil    time.Time `json:"quiet_until,omitzero"`
	UntilResolved bool      `json:"until_resolved,omitempty"`
}

// Message is one notification about one finding. A zero field is unset
// and left out of the webhook body. The JSON tags are the form an outbox
// row stores it in (Encode, Decode), not the webhook body.
type Message struct {
	Kind Kind `json:"kind"`
	// Reason says why the message was sent when the kind alone does not
	// ("mute ended", "severity now info").
	Reason    string `json:"reason,omitempty"`
	FindingID string `json:"finding_id,omitempty"`
	CheckCode string `json:"check_code,omitempty"`
	CheckName string `json:"check_name,omitempty"`
	Category  string `json:"category,omitempty"`
	Severity  string `json:"severity,omitempty"`
	// PreviousSeverity is the severity the channel was last sent, for a
	// changed message.
	PreviousSeverity string `json:"previous_severity,omitempty"`
	EntityType       string `json:"entity_type,omitempty"`
	EntityKey        string `json:"entity_key,omitempty"`
	// EntityName is the entity's readable name, such as an account's,
	// when it differs from the key.
	EntityName string `json:"entity_name,omitempty"`
	Detail     string `json:"detail,omitempty"`
	// Since is when the finding's episode began.
	Since time.Time `json:"since,omitzero"`
	// ConditionSince is when the condition began, when the check knows.
	ConditionSince  time.Time        `json:"condition_since,omitzero"`
	TransitionedAt  time.Time        `json:"transitioned_at,omitzero"`
	ResolvedAt      time.Time        `json:"resolved_at,omitzero"`
	Flapping        bool             `json:"flapping,omitempty"`
	Value           *float64         `json:"value,omitempty"`
	Acknowledgement *Acknowledgement `json:"acknowledgement,omitempty"`
	Mute            *Mute            `json:"mute,omitempty"`
	// FiringCount is, on a baseline message, how many findings the
	// channel's view holds.
	FiringCount *int `json:"firing_count,omitempty"`
	// Link is the finding's page in the UI; empty without ui_url. An
	// outbox row stores no link: WithLink builds it when it is sent.
	Link string `json:"link,omitempty"`
	// DedupKey is the same each time this message is sent.
	DedupKey string `json:"dedup_key"`
}

// Encode is the message as an outbox row stores it, without its link.
func (m Message) Encode() ([]byte, error) {
	m.Link = ""
	return marshal(m)
}

// Decode reads a message Encode wrote.
func Decode(b []byte) (Message, error) {
	var m Message
	err := json.Unmarshal(b, &m)
	return m, err
}

// WithLink is the message with its link for uiURL: the finding's page, or
// for a message about no finding (baseline, test) the system's Findings
// page; none without a ui_url.
func (m Message) WithLink(uiURL, systemID string) Message {
	if m.FindingID == "" {
		m.Link = FindingsLink(uiURL, systemID)
	} else {
		m.Link = FindingLink(uiURL, systemID, m.FindingID)
	}
	return m
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

// BaselineMessage is the one message a channel gets when nats-lens starts
// notifying it: how many findings its view holds at the epoch.
func BaselineMessage(systemID string, firing int, epoch int64) Message {
	return Message{
		Kind: KindBaseline, Detail: BaselineText(firing), FiringCount: &firing,
		DedupKey: systemID + ":baseline:" + strconv.FormatInt(epoch, 10),
	}
}

// BaselineText words a baseline: "nats-lens now notifies this channel: 3
// findings firing".
func BaselineText(firing int) string {
	switch firing {
	case 0:
		return "nats-lens now notifies this channel: no findings firing"
	case 1:
		return "nats-lens now notifies this channel: 1 finding firing"
	}
	return "nats-lens now notifies this channel: " + strconv.Itoa(firing) + " findings firing"
}
