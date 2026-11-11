package checks

import (
	"regexp"
	"testing"
	"testing/fstest"

	"github.com/shadone/nats-lens/internal/config"
)

// severityColumn matches a query that returns a severity column. A query
// without one gives every row the header's severity.
var severityColumn = regexp.MustCompile(`(?i)\bseverity\b`)

// severityRuleProblem says how the check's header and query disagree on
// the severity: the query sets one without a severity_rule saying when,
// or the header has a rule the query never applies; "" when they agree.
func severityRuleProblem(c *Check) string {
	emits := severityColumn.MatchString(c.SQL)
	switch {
	case emits && c.SeverityRule == "":
		return "its query returns a severity column, and its header has no severity_rule"
	case !emits && c.SeverityRule != "":
		return "its header has a severity_rule, and its query returns no severity column"
	}
	return ""
}

// A check whose query sets a row's severity says in its header when, and
// a check whose query never does has no severity rule: the finding page
// gives the rule as the reason for a severity other than the usual one.
func TestSeverityRuleMatchesTheQuery(t *testing.T) {
	set, err := Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range set.Checks() {
		if c.builtin {
			continue
		}
		if p := severityRuleProblem(c); p != "" {
			t.Errorf("%s: %s", c.Code, p)
		}
	}
}

// severityRuleProblem finds a query that sets a severity without a rule,
// and a rule without a query that sets one.
func TestSeverityRuleProblem(t *testing.T) {
	file := func(rule, column string) *Check {
		fsys := fstest.MapFS{"system-999.sql": {Data: []byte("/*\ncode: system-999\nname: X\nentity: system\ncategory: health\nseverity: warning\n" +
			rule + "kind: operational\ndescription: X.\nremediation: X.\n*/\nSELECT '' AS entity_key, " + column + "'x' AS detail")}}
		files, err := load(fsys)
		if err != nil || len(files) != 1 {
			t.Fatalf("fixture: %v", err)
		}
		return files[0]
	}
	sets := "CASE WHEN 1 > 0 THEN 'critical' ELSE 'warning' END AS severity, "
	rule := "severity_rule: critical when one is above zero\n"
	for _, c := range []struct {
		name    string
		check   *Check
		problem bool
	}{
		{"sets a severity without a rule", file("", sets), true},
		{"a rule without a severity column", file(rule, ""), true},
		{"sets a severity with a rule", file(rule, sets), false},
		{"neither", file("", ""), false},
	} {
		if got := severityRuleProblem(c.check) != ""; got != c.problem {
			t.Errorf("%s: problem %v, want %v", c.name, got, c.problem)
		}
	}
}

// The built-in checks evaluated in Go state their rule as the SQL ones do.
func TestBuiltinSeverityRules(t *testing.T) {
	if c := checkFailed(); c.SeverityRule == "" {
		t.Errorf("%s turns critical after %d failed collections and states no severity_rule", c.Code, EscalateAfter)
	}
	for _, c := range []*Check{slowRouteCheck(), leafHealthCheck()} {
		if c.SeverityRule != "" {
			t.Errorf("%s: every row is %s, and it states a severity_rule", c.Code, c.Severity)
		}
	}
}
