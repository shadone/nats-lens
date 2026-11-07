package notify

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/config"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

var (
	t0 = time.Date(2026, 11, 5, 9, 30, 0, 0, time.UTC)
	// A time in another zone is written in UTC.
	tLocal = time.Date(2026, 11, 5, 1, 32, 0, 0, time.FixedZone("PST", -8*3600))
)

const uiURL = "https://nats-lens.example.com"

// base is a stream finding of the demo's shop.
func base(kind Kind) Message {
	return Message{
		Kind: kind, FindingID: "stream-001:APP/order-jobs", CheckCode: "stream-001", CheckName: "Stream without a leader",
		Category: "health", Severity: "critical", EntityType: "stream", EntityKey: "APP/order-jobs",
		EntityName: "shop/order-jobs", Detail: "no leader for 2m", Since: t0, TransitionedAt: t0,
		Link:     FindingLink(uiURL, "example-prod", "stream-001:APP/order-jobs"),
		DedupKey: DedupKey("example-prod", "stream-001:APP/order-jobs", 41230, kind, 41230),
	}
}

func value(v float64) *float64 { return &v }

// sample is one message of each kind, as T2 will build them.
func sample(kind Kind) Message {
	m := base(kind)
	switch kind {
	case KindFiring:
		m.ConditionSince = t0.Add(-2 * time.Minute)
		m.Value = value(0)
	case KindChanged:
		m.PreviousSeverity = "warning"
		m.TransitionedAt = tLocal
		m.Value = value(130)
		m.DedupKey = DedupKey("example-prod", m.FindingID, 41230, kind, 41234)
	case KindResolved:
		m.ResolvedAt = tLocal
		m.TransitionedAt = tLocal
		m.Detail = "leader elected"
		m.DedupKey = DedupKey("example-prod", m.FindingID, 41230, kind, 41234)
	case KindFlapping:
		m.Flapping = true
		m.Severity = "warning"
	case KindMuted:
		m.Mute = &Mute{By: "alice@example.com", Reason: "migration tonight", Until: t0.Add(12 * time.Hour)}
	case KindReminder:
		m.DedupKey = DedupKey("example-prod", m.FindingID, 41230, kind, 41350)
	case KindAcknowledged:
		m.Severity = "warning"
		m.Acknowledgement = &Acknowledgement{By: "alice@example.com", Note: "Checking quorum", QuietUntil: tLocal.Add(time.Hour)}
	case KindBaseline:
		return BaselineMessage("example-prod", 3, 41230).WithLink(uiURL, "example-prod")
	case KindTest:
		return TestBatch("example-prod", "Production", "1.4.0", uiURL, t0).Messages[0]
	}
	return m
}

func batch(msgs ...Message) Batch {
	return Batch{SystemID: "example-prod", SystemName: "Production", Version: "1.4.0", SentAt: tLocal,
		Key: "example-prod:7-9", UIURL: uiURL, Messages: msgs}
}

// golden compares got with testdata/name, indented, and rewrites the
// file with -update.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Indent(&buf, got, "", "  "); err != nil {
		t.Fatalf("%s: body is not JSON: %v\n%s", name, err, got)
	}
	buf.WriteByte('\n')
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to write it)", err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("%s differs from the golden file:\n--- got\n%s--- want\n%s", name, buf.Bytes(), want)
	}
}

func TestGoldenBodies(t *testing.T) {
	for _, k := range Kinds {
		b := batch(sample(k))
		wb, err := WebhookBody(b)
		if err != nil {
			t.Fatal(err)
		}
		golden(t, fmt.Sprintf("webhook-%s.json", k), wb)
		sb, err := SlackBody(b)
		if err != nil {
			t.Fatal(err)
		}
		golden(t, fmt.Sprintf("slack-%s.json", k), sb)
	}
}

// Variants the kinds alone do not show: an acceptance, a mute that ended,
// a severity that fell below the channel's range.
func TestGoldenVariants(t *testing.T) {
	accepted := sample(KindMuted)
	accepted.Mute = &Mute{Accepted: true, By: "bob@example.com", Reason: "by design", Until: t0.Add(30 * 24 * time.Hour)}
	ended := sample(KindFiring)
	ended.Reason = "acceptance ended"
	ended.Mute = &Mute{Accepted: true, By: "bob@example.com", Until: t0, EndReason: "growth above the accepted rate"}
	below := sample(KindResolved)
	below.Reason = "severity now info"
	below.Severity = "info"
	below.PreviousSeverity = "warning"
	below.ResolvedAt = time.Time{}
	b := batch(accepted, ended, below)
	wb, err := WebhookBody(b)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "webhook-variants.json", wb)
	sb, err := SlackBody(b)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "slack-variants.json", sb)
}

// Without ui_url a message carries no link, in either body.
func TestBodiesWithoutLink(t *testing.T) {
	m := sample(KindFiring)
	m.Link = FindingLink("", "example-prod", m.FindingID)
	b := batch(m)
	b.UIURL = ""
	wb, _ := WebhookBody(b)
	sb, _ := SlackBody(b)
	for _, body := range [][]byte{wb, sb} {
		if bytes.Contains(body, []byte("http")) || bytes.Contains(body, []byte(`"link"`)) {
			t.Errorf("body without ui_url has a link: %s", body)
		}
	}
	golden(t, "webhook-no-link.json", wb)
	golden(t, "slack-no-link.json", sb)
}

// A Slack text lists at most 10 messages and counts the rest, with a link
// to Findings when there is a ui_url.
func TestSlackCut(t *testing.T) {
	var msgs []Message
	for i := range 14 {
		m := sample(KindFiring)
		m.EntityKey = fmt.Sprintf("APP/orders-%02d", i)
		m.EntityName = ""
		msgs = append(msgs, m)
	}
	sb, err := SlackBody(batch(msgs...))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "slack-cut.json", sb)
	var body struct{ Text string }
	if err := json.Unmarshal(sb, &body); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(body.Text, "\n")
	if len(lines) != 1+SlackMaxLines+1 {
		t.Fatalf("%d lines:\n%s", len(lines), body.Text)
	}
	if last := lines[len(lines)-1]; last != "and 4 more: <"+uiURL+"/findings?system=example-prod|Findings>" {
		t.Errorf("last line %q", last)
	}
	b := batch(msgs...)
	b.UIURL = ""
	sb, _ = SlackBody(b)
	if !bytes.HasSuffix(sb, []byte(`and 4 more"}`)) {
		t.Errorf("cut without ui_url: %s", sb)
	}
	sb, _ = SlackBody(batch(msgs[:SlackMaxLines]...))
	if bytes.Contains(sb, []byte("more")) {
		t.Errorf("10 messages cut: %s", sb)
	}
}

// Slack's three special characters in a detail are escaped, so a detail
// cannot make a link.
func TestSlackEscapes(t *testing.T) {
	m := sample(KindFiring)
	m.Detail = "lag > 100 & <https://evil.example.com|click>"
	sb, _ := SlackBody(batch(m))
	var body struct{ Text string }
	_ = json.Unmarshal(sb, &body)
	if !strings.Contains(body.Text, "lag &gt; 100 &amp; &lt;https://evil.example.com|click&gt;") {
		t.Errorf("detail not escaped: %s", body.Text)
	}
}

// Every time in the webhook body is RFC 3339 UTC; an unset value is left
// out, while a value of 0 is written.
func TestWebhookTimesAndUnset(t *testing.T) {
	wb, _ := WebhookBody(batch(sample(KindFiring), sample(KindTest)))
	var body struct {
		SentAt        string           `json:"sent_at"`
		Notifications []map[string]any `json:"notifications"`
	}
	if err := json.Unmarshal(wb, &body); err != nil {
		t.Fatal(err)
	}
	if body.SentAt != "2026-11-05T09:32:00Z" {
		t.Errorf("sent_at %q", body.SentAt)
	}
	if v, ok := body.Notifications[0]["value"]; !ok || v != 0.0 {
		t.Errorf("value 0 not written: %v", body.Notifications[0])
	}
	test := body.Notifications[1]
	for _, k := range []string{"value", "since", "resolved_at", "severity", "mute", "flapping", "finding_id"} {
		if _, ok := test[k]; ok {
			t.Errorf("unset %s written: %v", k, test)
		}
	}
}

// The kinds a channel can select are the message kinds but test.
func TestKindsMatchConfig(t *testing.T) {
	var got []string
	for _, k := range Kinds {
		if k != KindTest && k != KindBaseline {
			got = append(got, string(k))
		}
	}
	if !slices.Equal(got, config.ChannelKinds) {
		t.Fatalf("kinds %v, config %v", got, config.ChannelKinds)
	}
}

func TestLinks(t *testing.T) {
	if got := FindingLink(uiURL+"/", "example-prod", "stream-001:APP/order jobs"); got !=
		"https://nats-lens.example.com/findings/stream-001:APP%2Forder%20jobs?system=example-prod" {
		t.Errorf("finding link %s", got)
	}
	if got := DedupKey("example-prod", "stream-001:APP/order-jobs", 41230, KindFiring, 41230); got !=
		"example-prod:stream-001:APP/order-jobs:41230:firing:41230" {
		t.Errorf("dedup key %s", got)
	}
}

func slackText(t *testing.T, b Batch) string {
	t.Helper()
	sb, err := SlackBody(b)
	if err != nil {
		t.Fatal(err)
	}
	var body struct{ Text string }
	if err := json.Unmarshal(sb, &body); err != nil {
		t.Fatal(err)
	}
	return body.Text
}

// A line break in any text from NATS or a mute becomes a space, so that
// it cannot forge a line of its own.
func TestSlackOneLinePerMessage(t *testing.T) {
	m := sample(KindMuted)
	m.Detail = "lag 5\nCRITICAL firing: stream-002 fake: APP/x <!here>"
	m.EntityName = "shop\r\norder-jobs"
	m.CheckName = "Stream\rwithout a leader"
	m.Reason = "mute ended"
	m.Mute.Reason = "migration\ttonight\nCRITICAL firing"
	m.Mute.By = "alice\n@example.com"
	b := batch(m)
	b.SystemName = "Prod\nCRITICAL"
	text := slackText(t, b)
	if n := strings.Count(text, "\n"); n != 1 {
		t.Errorf("%d line breaks, want 1:\n%s", n, text)
	}
	if strings.Contains(text, "<!") {
		t.Errorf("mention not escaped: %s", text)
	}
	golden(t, "slack-one-line.json", []byte(`{"text":`+strconvQuote(text)+`}`))
}

func strconvQuote(s string) string {
	b, _ := marshal(s)
	return string(b)
}

// Each text is cut to SlackMaxField runes, and the whole text stays under
// SlackMaxText bytes: lines beyond it are counted on the last line.
func TestSlackLimits(t *testing.T) {
	m := sample(KindFiring)
	m.Detail = strings.Repeat("é", 200000)
	text := slackText(t, batch(m))
	if want := "(" + strings.Repeat("é", SlackMaxField-1) + "…)"; !strings.Contains(text, want) {
		t.Errorf("detail not cut to %d runes: %d bytes", SlackMaxField, len(text))
	}
	var msgs []Message
	for range 10 {
		m := sample(KindMuted)
		long := strings.Repeat("\U0001F600", 1000)
		m.Detail, m.EntityName, m.CheckName, m.Reason = long, long, long, long
		m.Mute.Reason, m.Mute.By = long, long
		msgs = append(msgs, m)
	}
	text = slackText(t, batch(msgs...))
	if len(text) > SlackMaxText {
		t.Errorf("text of %d bytes", len(text))
	}
	lines := strings.Split(text, "\n")
	last := lines[len(lines)-1]
	if shown := len(lines) - 2; !strings.HasPrefix(last, fmt.Sprintf("and %d more: <", 10-shown)) || shown < 1 || shown == 10 {
		t.Errorf("%d lines shown, last %q", shown, last)
	}
}

// An outbox row stores a message without its link; decoding it and adding
// the link for a ui_url gives the message back, times in UTC.
func TestEncodeDecode(t *testing.T) {
	for _, k := range Kinds {
		m := sample(k)
		m.Since = m.Since.In(time.FixedZone("IST", 5*3600+1800))
		b, err := m.Encode()
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte("nats-lens.example.com")) {
			t.Errorf("%s: encoded message has a link: %s", k, b)
		}
		got, err := Decode(b)
		if err != nil {
			t.Fatal(err)
		}
		got = got.WithLink(uiURL, "example-prod")
		wb, _ := WebhookBody(batch(got))
		want, _ := WebhookBody(batch(m))
		if !bytes.Equal(wb, want) {
			t.Errorf("%s: after encode and decode\n%s\nwant\n%s", k, wb, want)
		}
	}
}

func TestAcknowledgementBodiesUntilResolved(t *testing.T) {
	m := sample(KindAcknowledged)
	m.Acknowledgement = &Acknowledgement{By: "operator\n<!here>", Note: "Investigating\n<https://example.com|link> & quorum", UntilResolved: true}
	wb, err := WebhookBody(batch(m))
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Notifications []struct{ Acknowledgement map[string]any }
	}
	if err := json.Unmarshal(wb, &body); err != nil {
		t.Fatal(err)
	}
	a := body.Notifications[0].Acknowledgement
	if a["until_resolved"] != true || a["by"] != m.Acknowledgement.By || a["note"] != m.Acknowledgement.Note {
		t.Fatalf("ack: %v", a)
	}
	if _, ok := a["quiet_until"]; ok {
		t.Fatalf("zero quiet_until: %s", wb)
	}
	text := slackText(t, batch(m))
	if strings.Count(text, "\n") != 1 || strings.Contains(text, "<!") || !strings.Contains(text, "quiet until resolved") || !strings.Contains(text, "&lt;https://example.com|link&gt; &amp; quorum") {
		t.Fatal(text)
	}
	golden(t, "webhook-acknowledged-until-resolved.json", wb)
	sb, _ := SlackBody(batch(m))
	golden(t, "slack-acknowledged-until-resolved.json", sb)
}
