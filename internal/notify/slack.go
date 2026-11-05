package notify

import (
	"fmt"
	"strings"
	"time"
)

// SlackMaxLines is the most messages one Slack text lists; the rest are
// counted on a last line.
const SlackMaxLines = 10

// slackEscape escapes the three characters Slack's text format gives a
// meaning to.
var slackEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// SlackBody is the batch as a Slack-compatible incoming webhook body: a
// text with a header line naming the system, then one line per message,
// at most SlackMaxLines.
func SlackBody(b Batch) ([]byte, error) {
	lines := []string{slackHeader(b)}
	for i, m := range b.Messages {
		if i == SlackMaxLines {
			more := fmt.Sprintf("and %d more", len(b.Messages)-SlackMaxLines)
			if l := FindingsLink(b.UIURL, b.SystemID); l != "" {
				more += ": <" + l + "|Findings>"
			}
			lines = append(lines, more)
			break
		}
		lines = append(lines, slackLine(m))
	}
	return marshal(struct {
		Text string `json:"text"`
	}{strings.Join(lines, "\n")})
}

func slackHeader(b Batch) string {
	name := b.SystemID
	if b.SystemName != "" && b.SystemName != b.SystemID {
		name = b.SystemName + " (" + b.SystemID + ")"
	}
	return "nats-lens, " + slackEscape.Replace(name) + ":"
}

// slackLine is one message: the severity in capitals, what happened, the
// check, the entity, the detail and the link.
func slackLine(m Message) string {
	if m.Kind == KindTest {
		line := "TEST " + slackEscape.Replace(m.Detail)
		if m.Link != "" {
			line += " <" + m.Link + "|open>"
		}
		return line
	}
	var sb strings.Builder
	sb.WriteString(strings.ToUpper(m.Severity))
	sb.WriteString(" ")
	sb.WriteString(slackEscape.Replace(slackWhat(m)))
	sb.WriteString(": ")
	sb.WriteString(slackEscape.Replace(m.CheckCode + " " + m.CheckName + ": " + entityText(m)))
	if m.Detail != "" {
		sb.WriteString(" (" + slackEscape.Replace(m.Detail) + ")")
	}
	if m.Link != "" {
		sb.WriteString(" <" + m.Link + "|open>")
	}
	return sb.String()
}

// slackWhat words the kind, with the reason after a comma.
func slackWhat(m Message) string {
	what := string(m.Kind)
	switch m.Kind {
	case KindChanged:
		if m.PreviousSeverity != "" {
			what = "changed from " + m.PreviousSeverity
		}
	case KindMuted:
		if mu := m.Mute; mu != nil {
			what = "muted"
			if mu.Accepted {
				what = "accepted"
			}
			if mu.By != "" {
				what += " by " + mu.By
			}
			if !mu.Until.IsZero() {
				what += " until " + slackTime(mu.Until)
			}
			if mu.Reason != "" {
				what += " (" + mu.Reason + ")"
			}
		}
	case KindReminder:
		what = "still firing"
		if !m.Since.IsZero() {
			what += " since " + slackTime(m.Since)
		}
	}
	if m.Reason != "" {
		what += ", " + m.Reason
	}
	return what
}

func slackTime(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }

// entityText is the entity's readable name, or its key without one.
func entityText(m Message) string {
	if m.EntityName != "" {
		return m.EntityName
	}
	return m.EntityKey
}
