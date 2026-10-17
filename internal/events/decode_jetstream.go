package events

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

// JSSubjects are the subjects captured in each JetStream account.
var JSSubjects = []string{"$JS.EVENT.ADVISORY.>", "$JS.EVENT.METRIC.>", authErrAccountSubject}

// KindAPIAudit is the kind of a JetStream API audit advisory.
const KindAPIAudit = "api.audit"

// DecodeJS turns a JetStream advisory or metric into an event. account is
// the account the message was received in; an advisory that names its own
// account overrides it. Unknown types are kept with kind "jetstream.other".
func DecodeJS(account, subject string, data []byte, received time.Time) (Event, error) {
	raw, err := cleanRaw(data)
	if err != nil {
		return Event{}, fmt.Errorf("%s: %w", subject, err)
	}
	var head server.TypedEvent
	if err := json.Unmarshal(data, &head); err != nil {
		return Event{}, fmt.Errorf("%s: %w", subject, err)
	}
	ev := Event{ReceivedAt: received.UTC(), Source: SourceJetStream, Type: head.Type, Account: account, Raw: raw, id: head.ID}
	ev.setTime(head.Time)
	d := jsDecoders[head.Type]
	if d == nil {
		ev.Kind = "jetstream.other"
		ev.Summary = fmt.Sprintf("%s on %s", orDash(head.Type), subject)
		return ev, nil
	}
	if err := d(&ev, data); err != nil {
		return Event{}, fmt.Errorf("%s (%s): %w", subject, head.Type, err)
	}
	return ev, nil
}

// jsDecoders fill the event from a payload, keyed by the payload type.
var jsDecoders = map[string]func(*Event, []byte) error{
	server.JSStreamActionAdvisoryType: func(e *Event, b []byte) error {
		var m server.JSStreamActionAdvisory
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		e.Stream = m.Stream
		e.Kind = "stream." + actionVerb(m.Action)
		e.Summary = fmt.Sprintf("stream %s %s", assetLabel(e), actionVerb(m.Action))
		return nil
	},
	server.JSConsumerActionAdvisoryType: func(e *Event, b []byte) error {
		var m server.JSConsumerActionAdvisory
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		e.Stream, e.Consumer = m.Stream, m.Consumer
		e.Kind = "consumer." + actionVerb(m.Action)
		e.Summary = fmt.Sprintf("consumer %s %s", assetLabel(e), actionVerb(m.Action))
		return nil
	},
	server.JSConsumerPauseAdvisoryType: func(e *Event, b []byte) error {
		var m server.JSConsumerPauseAdvisory
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		e.Stream, e.Consumer = m.Stream, m.Consumer
		if m.Paused {
			e.Kind = "consumer.paused"
			e.Summary = fmt.Sprintf("consumer %s paused until %s", assetLabel(e), m.PauseUntil.UTC().Format(time.RFC3339))
		} else {
			e.Kind = "consumer.resumed"
			e.Summary = fmt.Sprintf("consumer %s resumed", assetLabel(e))
		}
		return nil
	},
	server.JSStreamLeaderElectedAdvisoryType: func(e *Event, b []byte) error {
		var m server.JSStreamLeaderElectedAdvisory
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		e.Account = firstNonEmpty(m.Account, e.Account)
		e.Stream, e.ServerName, e.Kind = m.Stream, m.Leader, "stream.leader_elected"
		e.Summary = fmt.Sprintf("stream %s elected leader %s; replicas %s", assetLabel(e), orDash(m.Leader), peers(m.Replicas))
		return nil
	},
	server.JSConsumerLeaderElectedAdvisoryType: func(e *Event, b []byte) error {
		var m server.JSConsumerLeaderElectedAdvisory
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		e.Account = firstNonEmpty(m.Account, e.Account)
		e.Stream, e.Consumer, e.ServerName, e.Kind = m.Stream, m.Consumer, m.Leader, "consumer.leader_elected"
		e.Summary = fmt.Sprintf("consumer %s elected leader %s; replicas %s", assetLabel(e), orDash(m.Leader), peers(m.Replicas))
		return nil
	},
	server.JSStreamQuorumLostAdvisoryType: func(e *Event, b []byte) error {
		var m server.JSStreamQuorumLostAdvisory
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		e.Account = firstNonEmpty(m.Account, e.Account)
		e.Stream, e.Kind = m.Stream, "stream.quorum_lost"
		e.Summary = fmt.Sprintf("stream %s lost quorum; replicas %s", assetLabel(e), peers(m.Replicas))
		return nil
	},
	server.JSConsumerQuorumLostAdvisoryType: func(e *Event, b []byte) error {
		var m server.JSConsumerQuorumLostAdvisory
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		e.Account = firstNonEmpty(m.Account, e.Account)
		e.Stream, e.Consumer, e.Kind = m.Stream, m.Consumer, "consumer.quorum_lost"
		e.Summary = fmt.Sprintf("consumer %s lost quorum; replicas %s", assetLabel(e), peers(m.Replicas))
		return nil
	},
	server.JSConsumerDeliveryExceededAdvisoryType: func(e *Event, b []byte) error {
		var m server.JSConsumerDeliveryExceededAdvisory
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		e.Stream, e.Consumer, e.Kind = m.Stream, m.Consumer, "consumer.max_deliveries"
		e.Summary = fmt.Sprintf("consumer %s gave up on stream sequence %d after %d deliveries", assetLabel(e), m.StreamSeq, m.Deliveries)
		return nil
	},
	server.JSConsumerDeliveryNakAdvisoryType: func(e *Event, b []byte) error {
		var m server.JSConsumerDeliveryNakAdvisory
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		e.Stream, e.Consumer, e.Kind = m.Stream, m.Consumer, "consumer.nak"
		e.Summary = fmt.Sprintf("consumer %s: stream sequence %d negatively acknowledged on delivery %d", assetLabel(e), m.StreamSeq, m.Deliveries)
		return nil
	},
	server.JSConsumerDeliveryTerminatedAdvisoryType: func(e *Event, b []byte) error {
		var m server.JSConsumerDeliveryTerminatedAdvisory
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		e.Stream, e.Consumer, e.Kind, e.Reason = m.Stream, m.Consumer, "consumer.terminated", m.Reason
		e.Summary = fmt.Sprintf("consumer %s: stream sequence %d terminated on delivery %d: %s", assetLabel(e), m.StreamSeq, m.Deliveries, orDash(m.Reason))
		return nil
	},
	server.JSConsumerAckMetricType: func(e *Event, b []byte) error {
		var m server.JSConsumerAckMetric
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		e.Stream, e.Consumer, e.Kind = m.Stream, m.Consumer, "consumer.ack_sampled"
		e.Summary = fmt.Sprintf("consumer %s: stream sequence %d acknowledged %s after delivery %d",
			assetLabel(e), m.StreamSeq, time.Duration(m.Delay), m.Deliveries)
		return nil
	},
	server.JSConsumerGroupPinnedAdvisoryType: func(e *Event, b []byte) error {
		var m server.JSConsumerGroupPinnedAdvisory
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		e.Account = firstNonEmpty(m.Account, e.Account)
		e.Stream, e.Consumer, e.Kind = m.Stream, m.Consumer, "consumer.pinned"
		e.Summary = fmt.Sprintf("consumer %s group %s pinned to %s", assetLabel(e), m.Group, m.PinnedClientId)
		return nil
	},
	server.JSConsumerGroupUnpinnedAdvisoryType: func(e *Event, b []byte) error {
		var m server.JSConsumerGroupUnpinnedAdvisory
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		e.Account = firstNonEmpty(m.Account, e.Account)
		e.Stream, e.Consumer, e.Kind, e.Reason = m.Stream, m.Consumer, "consumer.unpinned", m.Reason
		e.Summary = fmt.Sprintf("consumer %s group %s unpinned: %s", assetLabel(e), m.Group, orDash(m.Reason))
		return nil
	},
	server.JSStreamBatchAbandonedAdvisoryType: func(e *Event, b []byte) error {
		var m server.JSStreamBatchAbandonedAdvisory
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		e.Account = firstNonEmpty(m.Account, e.Account)
		e.Stream, e.Kind, e.Reason = m.Stream, "stream.batch_abandoned", string(m.Reason)
		e.Summary = fmt.Sprintf("stream %s abandoned batch %s: %s", assetLabel(e), m.BatchId, orDash(string(m.Reason)))
		return nil
	},
	server.JSSnapshotCreatedAdvisoryType:  streamClientDecoder("stream.snapshot_started", "snapshot started"),
	server.JSSnapshotCompleteAdvisoryType: streamClientDecoder("stream.snapshot_completed", "snapshot completed"),
	server.JSRestoreCreateAdvisoryType:    streamClientDecoder("stream.restore_started", "restore started"),
	server.JSRestoreCompleteAdvisoryType:  streamClientDecoder("stream.restore_completed", "restore completed"),
	server.JSDomainLeaderElectedAdvisoryType: func(e *Event, b []byte) error {
		var m server.JSDomainLeaderElectedAdvisory
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		// The meta leader is named by its raft peer ID; the replicas map
		// peer IDs to server names.
		e.ServerName, e.Kind = peerName(m.Leader, m.Replicas), "meta.leader_elected"
		e.Summary = fmt.Sprintf("JetStream meta group of cluster %s elected leader %s; replicas %s", orDash(m.Cluster), orDash(e.ServerName), peers(m.Replicas))
		return nil
	},
	server.JSServerOutOfStorageAdvisoryType: func(e *Event, b []byte) error {
		var m server.JSServerOutOfSpaceAdvisory
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		e.ServerName, e.Stream, e.Kind = firstNonEmpty(m.Server, m.ServerID), m.Stream, "server.out_of_storage"
		e.Summary = fmt.Sprintf("server %s is out of JetStream storage (stream %s)", orDash(e.ServerName), orDash(m.Stream))
		return nil
	},
	server.JSServerRemovedAdvisoryType: func(e *Event, b []byte) error {
		var m server.JSServerRemovedAdvisory
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		e.ServerName, e.Kind = firstNonEmpty(m.Server, m.ServerID), "server.removed"
		e.Summary = fmt.Sprintf("server %s was removed from the JetStream cluster %s", orDash(e.ServerName), orDash(m.Cluster))
		return nil
	},
	server.JSMetaRescueAdvisoryType: func(e *Event, b []byte) error {
		var m server.JSMetaRescueAdvisory
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		e.ServerName, e.Kind = firstNonEmpty(m.Server, m.ServerID), "meta.rescue"
		e.Summary = fmt.Sprintf("server %s lowered the meta group quorum from %d to %d", orDash(e.ServerName), m.PrevQuorum, m.NewQuorum)
		return nil
	},
	server.JSAPILimitReachedAdvisoryType: func(e *Event, b []byte) error {
		var m server.JSAPILimitReachedAdvisory
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		e.ServerName, e.Kind = m.Server, "jetstream.api_limit_reached"
		e.Summary = fmt.Sprintf("server %s reached the JetStream API queue limit and dropped %d requests", orDash(m.Server), m.Dropped)
		return nil
	},
	server.JSAPIAuditType: func(e *Event, b []byte) error {
		var m server.JSAPIAudit
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		e.ServerName, e.Kind = m.Server, KindAPIAudit
		e.setClient(m.Client, "")
		if a, ok := auditAction(&m); ok {
			e.Stream, e.Consumer = a.stream, a.consumer
		}
		e.Reason = apiError(m.Response)
		e.Summary = fmt.Sprintf("%s by %s", m.Subject, auditClient(m.Client))
		if e.Reason != "" {
			e.Summary += ": " + e.Reason
		}
		return nil
	},
}

// streamClientDecoder decodes snapshot and restore advisories, which name a
// stream and the client that asked.
func streamClientDecoder(kind, what string) func(*Event, []byte) error {
	return func(e *Event, b []byte) error {
		var m struct {
			Stream string             `json:"stream"`
			Client *server.ClientInfo `json:"client"`
			Error  string             `json:"error"`
		}
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		e.Stream, e.Kind, e.Reason = m.Stream, kind, m.Error
		e.setClient(m.Client, "")
		e.Summary = fmt.Sprintf("stream %s %s by %s", assetLabel(e), what, auditClient(m.Client))
		return nil
	}
}

func actionVerb(a server.ActionAdvisoryType) string {
	switch a {
	case server.CreateEvent:
		return "created"
	case server.DeleteEvent:
		return "deleted"
	case server.ModifyEvent:
		return "updated"
	}
	return string(a)
}

// assetLabel names a stream or consumer as account > stream > consumer.
func assetLabel(e *Event) string {
	parts := []string{}
	if e.Account != "" {
		parts = append(parts, e.Account)
	}
	parts = append(parts, orDash(e.Stream))
	if e.Consumer != "" {
		parts = append(parts, e.Consumer)
	}
	return strings.Join(parts, " > ")
}

// peers lists replicas as "name (state)", sorted by name.
func peers(ps []*server.PeerInfo) string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		if p == nil {
			continue
		}
		state := "behind"
		switch {
		case p.Offline:
			state = "offline"
		case p.Current:
			state = "current"
		}
		out = append(out, p.Name+" "+state)
	}
	if len(out) == 0 {
		return "none listed"
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// peerName returns the server name of the replica with raft peer ID id, or
// id itself when no replica has it.
func peerName(id string, ps []*server.PeerInfo) string {
	for _, p := range ps {
		if p != nil && p.Peer == id && p.Name != "" {
			return p.Name
		}
	}
	return id
}

func auditClient(c *server.ClientInfo) string {
	if c == nil {
		return "unknown client"
	}
	return clientLabel(c) + " from " + orDash(c.Host)
}

// apiError returns the error description of a JetStream API response, or
// empty when the call succeeded.
func apiError(response string) string {
	var r struct {
		Error *struct {
			Code        int    `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(response), &r) != nil || r.Error == nil {
		return ""
	}
	return fmt.Sprintf("%d %s", r.Error.Code, r.Error.Description)
}
