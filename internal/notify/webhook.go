package notify

import (
	"time"
)

// WebhookVersion is the version of the webhook body. A change a receiver
// could trip over (a field removed, renamed or retyped) takes a new
// version; a new field does not.
const WebhookVersion = 1

type webhookBody struct {
	Version         int             `json:"version"`
	SentAt          string          `json:"sent_at"`
	System          webhookSystem   `json:"system"`
	NatsLensVersion string          `json:"nats_lens_version"`
	Notifications   []webhookNotice `json:"notifications"`
}

type webhookSystem struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type webhookNotice struct {
	Kind             Kind         `json:"kind"`
	Reason           string       `json:"reason,omitempty"`
	FindingID        string       `json:"finding_id,omitempty"`
	CheckCode        string       `json:"check_code,omitempty"`
	CheckName        string       `json:"check_name,omitempty"`
	Category         string       `json:"category,omitempty"`
	Severity         string       `json:"severity,omitempty"`
	PreviousSeverity string       `json:"previous_severity,omitempty"`
	EntityType       string       `json:"entity_type,omitempty"`
	EntityKey        string       `json:"entity_key,omitempty"`
	EntityName       string       `json:"entity_name,omitempty"`
	Detail           string       `json:"detail,omitempty"`
	Since            string       `json:"since,omitempty"`
	ConditionSince   string       `json:"condition_since,omitempty"`
	TransitionedAt   string       `json:"transitioned_at,omitempty"`
	ResolvedAt       string       `json:"resolved_at,omitempty"`
	Flapping         bool         `json:"flapping,omitempty"`
	Value            *float64     `json:"value,omitempty"`
	Mute             *webhookMute `json:"mute,omitempty"`
	FiringCount      *int         `json:"firing_count,omitempty"`
	Link             string       `json:"link,omitempty"`
	DedupKey         string       `json:"dedup_key"`
}

type webhookMute struct {
	Accepted  bool   `json:"accepted,omitempty"`
	By        string `json:"by,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Until     string `json:"until,omitempty"`
	EndReason string `json:"end_reason,omitempty"`
}

// timeText is t in RFC 3339 UTC, or empty when t is zero.
func timeText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// WebhookBody is the batch as the webhook channel's JSON body, version 1.
func WebhookBody(b Batch) ([]byte, error) {
	body := webhookBody{
		Version:         WebhookVersion,
		SentAt:          timeText(b.SentAt),
		System:          webhookSystem{ID: b.SystemID, Name: b.SystemName},
		NatsLensVersion: b.Version,
		Notifications:   make([]webhookNotice, 0, len(b.Messages)),
	}
	for _, m := range b.Messages {
		n := webhookNotice{
			Kind: m.Kind, Reason: m.Reason, FindingID: m.FindingID, CheckCode: m.CheckCode, CheckName: m.CheckName,
			Category: m.Category, Severity: m.Severity, PreviousSeverity: m.PreviousSeverity,
			EntityType: m.EntityType, EntityKey: m.EntityKey, EntityName: m.EntityName, Detail: m.Detail,
			Since: timeText(m.Since), ConditionSince: timeText(m.ConditionSince),
			TransitionedAt: timeText(m.TransitionedAt), ResolvedAt: timeText(m.ResolvedAt),
			Flapping: m.Flapping, Value: m.Value, FiringCount: m.FiringCount, Link: m.Link, DedupKey: m.DedupKey,
		}
		if mu := m.Mute; mu != nil {
			n.Mute = &webhookMute{Accepted: mu.Accepted, By: mu.By, Reason: mu.Reason, Until: timeText(mu.Until), EndReason: mu.EndReason}
		}
		body.Notifications = append(body.Notifications, n)
	}
	return marshal(body)
}
