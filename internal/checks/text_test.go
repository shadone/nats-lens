package checks

import (
	"regexp"
	"strings"
	"testing"

	"github.com/shadone/nats-lens/internal/config"
)

var (
	epochWord = regexp.MustCompile(`\bepochs?\b`)
	codeSpan  = regexp.MustCompile("`[^`]*`")
)

// userText reports whether s says "epoch" outside code spans and
// identifiers such as min_epochs.
func saysEpoch(s string) bool { return epochWord.MatchString(codeSpan.ReplaceAllString(s, "")) }

// Every text a check shows a user says "collection", as the UI does.
func TestCheckTextSaysCollection(t *testing.T) {
	set, err := Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	checks := set.Checks()
	if c, ok := set.Lookup(CheckFailedCode); ok {
		checks = append(checks, c)
	}
	for _, c := range checks {
		texts := map[string]string{"name": c.Name, "description": c.Description, "remediation": c.Remediation, "value_name": c.ValueName, "value_basis": c.ValueBasis}
		for _, th := range c.Thresholds {
			texts["threshold "+th.Name] = th.Description
		}
		for what, s := range texts {
			if saysEpoch(s) {
				t.Errorf("%s %s: %q", c.Code, what, s)
			}
		}
	}
}

// The detail of an escalated system-001 says collections.
func TestEscalationDetailSaysCollection(t *testing.T) {
	if s := escalationText([]string{"stream-001"}); saysEpoch(s) || !strings.Contains(s, "collections") {
		t.Errorf("%q", s)
	}
}
