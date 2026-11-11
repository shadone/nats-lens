package checks

import (
	"bytes"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
)

// severityColumn matches a column named severity in a query whose
// comments are gone: an alias, with AS, of a select list. A query without
// one gives every row the header's severity.
//
// It is a text match, so it does not see a single-quoted alias (AS
// 'severity'), an alias without AS ('critical' severity), a severity
// column passed through from a table or a CTE (SELECT *, or f.severity in
// the select list), or a -- inside a string literal, whose rest of line
// sqlComment removes; and it counts an alias in a CTE that the final
// SELECT leaves out.
var severityColumn = regexp.MustCompile(`(?i)\bAS\s+"?severity"?(\W|$)`)

// sqlComment matches a -- comment to the end of its line and a /* */ one.
var sqlComment = regexp.MustCompile(`(?s)--[^\n]*|/\*.*?\*/`)

// severityRuleProblem says how the check's header and query disagree on
// the severity: the query sets one without a severity_rule saying when,
// or the header has a rule the query never applies; "" when they agree.
func severityRuleProblem(c *Check) string {
	emits := severityColumn.MatchString(sqlComment.ReplaceAllString(c.SQL, " "))
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
	file := func(rule, sql string) *Check {
		fsys := fstest.MapFS{"system-999.sql": {Data: []byte("/*\ncode: system-999\nname: X\nentity: system\ncategory: health\nseverity: warning\n" +
			rule + "kind: operational\ndescription: X.\nremediation: X.\n*/\n" + sql)}}
		files, err := load(fsys)
		if err != nil || len(files) != 1 {
			t.Fatalf("fixture: %v", err)
		}
		return files[0]
	}
	query := "SELECT '' AS entity_key, CASE WHEN 1 > 0 THEN 'critical' ELSE 'warning' END AS severity, 'x' AS detail"
	plain := "SELECT '' AS entity_key, 'x' AS detail"
	// A comment that names the severity, and a filter on another table's
	// severity column, are no result column.
	commented := "-- every row has the header's severity\n/* no severity column */\n" + plain
	filtered := "SELECT '' AS entity_key, 'x' AS detail FROM findings f WHERE f.severity = 'critical'"
	quoted := "SELECT '' AS entity_key, 'critical' AS \"severity\", 'x' AS detail"
	rule := "severity_rule: critical when one is above zero\n"
	for _, c := range []struct {
		name    string
		check   *Check
		problem bool
	}{
		{"sets a severity without a rule", file("", query), true},
		{"a rule without a severity column", file(rule, plain), true},
		{"a comment that names the severity", file("", commented), false},
		{"a rule, and a filter on a severity", file(rule, filtered), true},
		{"sets a quoted severity without a rule", file("", quoted), true},
		{"sets a severity with a rule", file(rule, query), false},
		{"neither", file("", plain), false},
	} {
		if got := severityRuleProblem(c.check) != ""; got != c.problem {
			t.Errorf("%s: problem %v, want %v", c.name, got, c.problem)
		}
	}
}

// Every built-in check evaluated in Go declares the severities its rows
// get, its usual one among them, and has a severity rule when it declares
// more than one, as the SQL checks do.
func TestBuiltinSeverityRules(t *testing.T) {
	set, err := Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range set.Checks() {
		if !c.builtin {
			continue
		}
		n++
		switch {
		case !slices.Contains(c.severities, c.Severity):
			t.Errorf("%s declares %v, without its usual %s", c.Code, c.severities, c.Severity)
		case len(c.severities) > 1 && c.SeverityRule == "":
			t.Errorf("%s declares %v and states no severity_rule", c.Code, c.severities)
		case len(c.severities) == 1 && c.SeverityRule != "":
			t.Errorf("%s: every row is %s, and it states a severity_rule", c.Code, c.Severity)
		}
	}
	if n == 0 {
		t.Error("no built-in check")
	}
}

// A built-in check's row of a severity it does not declare is kept, as
// the epoch's findings matter more than the label, and the engine logs a
// warning that names the check and the severity.
func TestDeclaredSeverities(t *testing.T) {
	c := slowRouteCheck()
	var logged bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logged, nil))
	rows, err := declared(c, log)([]store.FindingRow{{EntityKey: "nats-0", Severity: "warning"}}, nil)
	if err != nil || len(rows) != 1 || logged.Len() != 0 {
		t.Errorf("warning: %d rows, %v, logged %q", len(rows), err, logged.String())
	}
	rows, err = declared(c, log)([]store.FindingRow{{EntityKey: "nats-0", Severity: "critical"}, {EntityKey: "nats-1", Severity: "warning"}}, nil)
	if err != nil || len(rows) != 2 {
		t.Errorf("critical: %d rows, %v; want both rows kept", len(rows), err)
	}
	if s := logged.String(); !strings.Contains(s, "level=WARN") || !strings.Contains(s, "check="+c.Code) || !strings.Contains(s, "severity=critical") {
		t.Errorf("logged %q; want a warning naming %s and critical", s, c.Code)
	}
}
