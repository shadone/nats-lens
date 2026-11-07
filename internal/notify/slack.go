package notify

import (
	"fmt"
	"strings"
	"time"
)

// SlackMaxLines is the most messages one Slack text lists; the rest are
// counted on a last line.
const SlackMaxLines = 10

// SlackMaxField is the most runes of one text taken from a finding or a
// mute (a detail, an entity, a reason); a longer one is cut and ends in
// "…".
const SlackMaxField = 300

// SlackMaxText is the most bytes of the whole text, well under the
// 40,000 characters Slack takes; message lines beyond it are counted on
// the last line instead.
const SlackMaxText = 30000

// slackEscape escapes the three characters Slack's text format gives a
// meaning to.
var slackEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// slackOneLine turns line breaks and tabs into spaces, so that a text
// taken from NATS cannot start a line of its own.
var slackOneLine = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ", "\t", " ", " ", " ", " ", " ")

// field is s on one line, cut to SlackMaxField runes. It is escaped when
// the line is put together.
func field(s string) string {
	s = slackOneLine.Replace(s)
	if r := []rune(s); len(r) > SlackMaxField {
		s = string(r[:SlackMaxField-1]) + "…"
	}
	return s
}

// SlackBody is the batch as a Slack-compatible incoming webhook body: a
// text with a header line naming the system, then one line per message,
// at most SlackMaxLines and SlackMaxText bytes.
func SlackBody(b Batch) ([]byte, error) {
	header := slackHeader(b)
	var lines []string
	size := len(header)
	for i, m := range b.Messages {
		if i == SlackMaxLines {
			break
		}
		l := slackLine(m)
		// Room is kept for the line that counts the rest.
		if len(lines) > 0 && size+1+len(l) > SlackMaxText-200 {
			break
		}
		lines = append(lines, l)
		size += 1 + len(l)
	}
	if rest := len(b.Messages) - len(lines); rest > 0 {
		more := fmt.Sprintf("and %d more", rest)
		if l := FindingsLink(b.UIURL, b.SystemID); l != "" {
			more += ": <" + l + "|Findings>"
		}
		lines = append(lines, more)
	}
	return marshal(struct {
		Text string `json:"text"`
	}{strings.Join(append([]string{header}, lines...), "\n")})
}

func slackHeader(b Batch) string {
	name := field(b.SystemID)
	if b.SystemName != "" && b.SystemName != b.SystemID {
		name = field(b.SystemName) + " (" + name + ")"
	}
	return "nats-lens, " + slackEscape.Replace(name) + ":"
}

// slackLine is one message: the severity in capitals, what happened, the
// check, the entity, the detail and the link.
func slackLine(m Message) string {
	if m.Kind == KindTest {
		line := "TEST " + slackEscape.Replace(field(m.Detail))
		if m.Link != "" {
			line += " <" + m.Link + "|open>"
		}
		return line
	}
	if m.Kind == KindBaseline {
		line := slackEscape.Replace(field(m.Detail))
		if m.Link != "" {
			line += " <" + m.Link + "|Findings>"
		}
		return line
	}
	var sb strings.Builder
	sb.WriteString(slackEscape.Replace(strings.ToUpper(field(m.Severity))))
	sb.WriteString(" ")
	sb.WriteString(slackEscape.Replace(slackWhat(m)))
	sb.WriteString(": ")
	sb.WriteString(slackEscape.Replace(field(m.CheckCode) + " " + field(m.CheckName) + ": " + entityText(m)))
	if m.Detail != "" {
		sb.WriteString(" (" + slackEscape.Replace(field(m.Detail)) + ")")
	}
	if m.Link != "" {
		sb.WriteString(" <" + m.Link + "|open>")
	}
	return sb.String()
}

// slackWhat words the kind, with the reason after a comma.
func slackWhat(m Message) string {
	what := field(string(m.Kind))
	switch m.Kind {
	case KindChanged:
		if m.PreviousSeverity != "" {
			what = "changed from " + field(m.PreviousSeverity)
		}
	case KindMuted:
		if mu := m.Mute; mu != nil {
			what = "muted"
			if mu.Accepted {
				what = "accepted"
			}
			if mu.By != "" {
				what += " by " + field(mu.By)
			}
			if !mu.Until.IsZero() {
				what += " until " + slackTime(mu.Until)
			}
			if mu.Reason != "" {
				what += " (" + field(mu.Reason) + ")"
			}
		}
	case KindAcknowledged:
		if a := m.Acknowledgement; a != nil {
			// ASVS 1.2.3: actor and note use the same bounded, escaped text path as findings.
			what = "acknowledged by " + field(a.By)
			if a.UntilResolved {
				what += ", quiet until resolved"
			} else if !a.QuietUntil.IsZero() {
				what += ", quiet until " + slackTime(a.QuietUntil)
			}
			if a.Note != "" {
				what += " (" + field(a.Note) + ")"
			}
		}
	case KindReminder:
		what = "still firing"
		if !m.Since.IsZero() {
			what += " since " + slackTime(m.Since)
		}
	}
	if m.Reason != "" {
		what += ", " + field(m.Reason)
	}
	return what
}

func slackTime(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }

// entityText is the entity's readable name, or its key without one.
func entityText(m Message) string {
	if m.EntityName != "" {
		return field(m.EntityName)
	}
	return field(m.EntityKey)
}
