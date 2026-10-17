package checks

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// RemediationSeparator stands between a check's own remediation and the
// text a remediation file adds to it.
const RemediationSeparator = "\n\n---\n"

// loadRemediation reads checks.remediation_file: a YAML mapping of check
// code to the text to append to that check's remediation.
func loadRemediation(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("checks.remediation_file: %w", err)
	}
	var extra map[string]string
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
func (s *Set) addRemediation(path string, extra map[string]string) []error {
	codes := make([]string, 0, len(extra))
	for code := range extra {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	var errs []error
	for _, code := range codes {
		c := s.byCode[code]
		text := strings.TrimSpace(extra[code])
		switch {
		case c == nil:
			errs = append(errs, fmt.Errorf("checks.remediation_file %s: unknown check %q", path, code))
			continue
		case text == "":
			errs = append(errs, fmt.Errorf("checks.remediation_file %s: %s: empty text", path, code))
			continue
		}
		cp := *c
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
