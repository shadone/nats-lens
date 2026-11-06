package checks

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"
)

// RemediationSeparator stands between a check's own remediation and the
// text a remediation file adds to it.
const RemediationSeparator = "\n\n---\n"

type remediationEntry struct {
	Text     string `yaml:"text"`
	Playbook string `yaml:"playbook"`
}

func (r *remediationEntry) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		return n.Decode(&r.Text)
	}
	type entry remediationEntry
	return n.Decode((*entry)(r))
}

// loadRemediation reads plain text or a text/playbook entry per check.
func loadRemediation(path string) (map[string]remediationEntry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("checks.remediation_file: %w", err)
	}
	var extra map[string]remediationEntry
	if err := yaml.Unmarshal(b, &extra); err != nil {
		return nil, fmt.Errorf("checks.remediation_file %s: %w", path, err)
	}
	return extra, nil
}

// RemediationExtended is the number of checks the remediation file added
// text to.
func (s *Set) RemediationExtended() int { return s.remediationExtended }

// addRemediation replaces each check the file names by a copy whose
// remediation ends with the file's text, so that checks loaded elsewhere
// keep theirs. An unknown code or an empty text is an error.
func (s *Set) addRemediation(path string, extra map[string]remediationEntry) []error {
	codes := make([]string, 0, len(extra))
	for code := range extra {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	var errs []error
	for _, code := range codes {
		c := s.byCode[code]
		text := strings.TrimSpace(extra[code].Text)
		switch {
		case c == nil:
			errs = append(errs, fmt.Errorf("checks.remediation_file %s: unknown check %q", path, code))
			continue
		case text == "":
			errs = append(errs, fmt.Errorf("checks.remediation_file %s: %s: empty text", path, code))
			continue
		}
		playbook := strings.TrimSpace(extra[code].Playbook)
		if extra[code].Playbook != "" {
			// ASVS 1.2.2: only absolute HTTPS links may reach the card.
			u, err := url.Parse(playbook)
			if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || strings.ContainsFunc(extra[code].Playbook, unicode.IsControl) {
				errs = append(errs, fmt.Errorf("checks.remediation_file %s: %s: playbook must be an absolute https URL without credentials or control characters", path, code))
				continue
			}
		}
		cp := *c
		cp.Playbook = playbook
		cp.Remediation = c.Remediation + RemediationSeparator + text
		s.byCode[code] = &cp
		s.remediationExtended++
		for i, old := range s.checks {
			if old == c {
				s.checks[i] = &cp
			}
		}
	}
	return errs
}
