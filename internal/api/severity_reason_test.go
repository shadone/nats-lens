package api_test

import (
	"testing"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func TestSeverityReason(t *testing.T) {
	rule := "critical at or above crit_ratio of the limit"
	for _, c := range []struct {
		name     string
		sev      v1.Severity
		resolved bool
		usual    string
		rule     string
		want     string
	}{
		{"no rule", warning, false, "warning", "", "Every stream-001 finding is a warning; its severity does not change."},
		{"no rule, info", info, false, "info", "", "Every stream-001 finding is info; its severity does not change."},
		{"no rule, resolved", critical, true, "critical", "", "Every stream-001 finding is critical; its severity does not change."},
		{"usual", warning, false, "warning", rule,
			"A warning is the usual severity of stream-001. Its severity rule says when it differs: critical at or above crit_ratio of the limit."},
		{"usual, rule ends with a full stop", info, false, "info", "warning when it grows.",
			"Info is the usual severity of stream-001. Its severity rule says when it differs: warning when it grows."},
		{"usual, resolved", warning, true, "warning", rule,
			"It was a warning when it resolved, the usual severity of stream-001. Its severity rule says when it differs: critical at or above crit_ratio of the limit."},
		{"raised", critical, false, "warning", rule,
			"Raised to critical from warning, the usual severity of stream-001, by its severity rule: critical at or above crit_ratio of the limit."},
		{"lowered", info, false, "critical", "info while the server restarts",
			"Lowered to info from critical, the usual severity of stream-001, by its severity rule: info while the server restarts."},
		{"raised, resolved", critical, true, "warning", rule,
			"It was critical when it resolved: raised from warning, the usual severity of stream-001, by its severity rule: critical at or above crit_ratio of the limit."},
		{"lowered to warning, resolved", warning, true, "critical", "warning while the server restarts",
			"It was a warning when it resolved: lowered from critical, the usual severity of stream-001, by its severity rule: warning while the server restarts."},
		{"raised from info, resolved", warning, true, "info", "warning when it grows",
			"It was a warning when it resolved: raised from info, the usual severity of stream-001, by its severity rule: warning when it grows."},
		{"lowered to info, resolved", info, true, "warning", "info while the server restarts",
			"It was info when it resolved: lowered from warning, the usual severity of stream-001, by its severity rule: info while the server restarts."},
		{"another severity without a rule", critical, false, "warning", "",
			"It is critical, though the usual severity of stream-001 is now warning and the check states no rule for another severity."},
		{"a warning without a rule", warning, false, "info", "",
			"It is a warning, though the usual severity of stream-001 is now info and the check states no rule for another severity."},
		{"another severity without a rule, resolved", info, true, "warning", "",
			"It was info when it resolved, though the usual severity of stream-001 is now warning and the check states no rule for another severity."},
		{"unset severity", v1.Severity_SEVERITY_UNSPECIFIED, false, "warning", rule, ""},
		{"unknown usual severity", warning, false, "", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := api.SeverityReason("stream-001", c.sev, c.resolved, c.usual, c.rule); got != c.want {
				t.Errorf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}

// MarkSeverityReason reads the check's usual severity and rule, and sets
// nothing for a check it does not know.
func TestMarkSeverityReason(t *testing.T) {
	usual := func(code string) (string, string, bool) {
		if code == "stream-001" {
			return "warning", "", true
		}
		return "", "", false
	}
	f := &v1.Finding{CheckCode: "stream-001", Severity: warning, State: v1.FindingState_FINDING_STATE_FIRING}
	api.MarkSeverityReason(f, usual)
	if want := "Every stream-001 finding is a warning; its severity does not change."; f.GetSeverityReason() != want {
		t.Errorf("got %q, want %q", f.GetSeverityReason(), want)
	}
	f = &v1.Finding{CheckCode: "custom-001", Severity: warning, SeverityReason: "stale"}
	api.MarkSeverityReason(f, usual)
	if f.GetSeverityReason() != "" {
		t.Errorf("unknown check: got %q, want none", f.GetSeverityReason())
	}
}
