package checks

import "testing"

func TestUsualSeverity(t *testing.T) {
	for _, c := range []struct {
		code, severity string
		rule           bool
	}{
		{"stream-001", "critical", false},
		{"account-001", "warning", true},
		{"system-010", "warning", true},
		{CheckFailedCode, "warning", true},
		{SlowRouteCode, "warning", false},
	} {
		sev, rule, ok := UsualSeverity(c.code)
		if !ok || sev != c.severity || (rule != "") != c.rule {
			t.Errorf("%s: %q, rule %q, ok %v; want %q, a rule %v", c.code, sev, rule, ok, c.severity, c.rule)
		}
	}
	if _, _, ok := UsualSeverity("no-such-999"); ok {
		t.Error("no-such-999 is known")
	}
}
