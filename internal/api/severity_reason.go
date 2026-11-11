package api

import (
	"fmt"
	"strings"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// UsualSeverityFunc returns a check's usual severity ("info", "warning" or
// "critical") and its severity rule; ok is false for an unknown check.
// checks.UsualSeverity is the one both providers pass.
type UsualSeverityFunc func(code string) (severity, rule string, ok bool)

// MarkSeverityReason sets the finding's severity_reason from its check's
// usual severity and severity rule.
func MarkSeverityReason(f *v1.Finding, usual UsualSeverityFunc) {
	sev, rule, ok := usual(f.GetCheckCode())
	if !ok {
		f.SeverityReason = ""
		return
	}
	f.SeverityReason = SeverityReason(f.GetCheckCode(), f.GetSeverity(), f.GetState() == v1.FindingState_FINDING_STATE_RESOLVED, sev, rule)
}

// severityWord is the severity as prose; empty for one that is unset.
func severityWord(s v1.Severity) string {
	switch s {
	case v1.Severity_SEVERITY_INFO:
		return "info"
	case v1.Severity_SEVERITY_WARNING:
		return "warning"
	case v1.Severity_SEVERITY_CRITICAL:
		return "critical"
	}
	return ""
}

// severityRank orders info < warning < critical; -1 for anything else.
func severityRank(s string) int {
	switch s {
	case "info":
		return 0
	case "warning":
		return 1
	case "critical":
		return 2
	}
	return -1
}

// asNoun is the severity after "is": "a warning", "info", "critical".
func asNoun(s string) string {
	if s == "warning" {
		return "a warning"
	}
	return s
}

// SeverityReason says why a finding of the check has the severity it has:
// every finding of a check without a severity rule has the usual
// severity; with a rule, the finding is at the usual severity, or the rule
// raised or lowered it, and the rule follows in the check's own words. A
// resolved finding is described at the severity it had when it resolved.
// It returns "" when sev or usual is unknown.
func SeverityReason(code string, sev v1.Severity, resolved bool, usual, rule string) string {
	at := severityWord(sev)
	if at == "" || severityRank(usual) < 0 {
		return ""
	}
	rule = strings.TrimSpace(rule)
	if rule != "" && !strings.HasSuffix(rule, ".") {
		rule += "."
	}
	if rule == "" && at == usual {
		return fmt.Sprintf("Every %s finding is %s; its severity does not change.", code, asNoun(usual))
	}
	if at == usual {
		lead := fmt.Sprintf("%s is the usual severity of %s.", upperFirst(asNoun(usual)), code)
		if resolved {
			lead = fmt.Sprintf("It was %s when it resolved, the usual severity of %s.", asNoun(at), code)
		}
		return lead + " Its severity rule says when it differs: " + rule
	}
	if rule == "" {
		// Only a finding stored under an older header gets here: the
		// header gives no reason, so nothing claims a rule moved it.
		lead := "It is " + asNoun(at)
		if resolved {
			lead = fmt.Sprintf("It was %s when it resolved", asNoun(at))
		}
		return fmt.Sprintf("%s, though the usual severity of %s is now %s and the check states no rule for another severity.", lead, code, usual)
	}
	moved := "Raised"
	if severityRank(at) < severityRank(usual) {
		moved = "Lowered"
	}
	how := "by its severity rule: " + rule
	if resolved {
		return fmt.Sprintf("It was %s when it resolved: %s from %s, the usual severity of %s, %s", asNoun(at), strings.ToLower(moved), usual, code, how)
	}
	return fmt.Sprintf("%s to %s from %s, the usual severity of %s, %s", moved, at, usual, code, how)
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// DropSeverityReason empties the reason of a past occurrence's finding:
// an occurrence is read without its historical check definition, and
// today's header would misdescribe the severity it had then.
func DropSeverityReason(f *v1.Finding) {
	if f != nil {
		f.SeverityReason = ""
	}
}
