// Package docs embeds the shipped product reference used by the check catalog.
package docs

import (
	"embed"
	"regexp"
	"strings"
)

//go:embed features/*-checks.md features/topology.md
var checkDocs embed.FS

// ReferenceSection is a plain paragraph with a label. Backticks mark code spans.
type ReferenceSection struct{ Title, Text string }

var checkHeading = regexp.MustCompile("^### `([a-z]+-[0-9]{3})` ")
var documentLink = regexp.MustCompile(`\[([^]]+)\]\([^)]+\)`)

var referenceBullet = regexp.MustCompile(`^- \*\*([^*]+)[:.]\*\*(?: (.*))?$`)

// CheckReferences reads every per-check paragraph, including continuation lines
// and tables that can explain prerequisites, exclusions, retention and resolution. These documents are the source of
// meaning, firing rules and limitations; no second authored catalog is maintained.
func CheckReferences() map[string][]ReferenceSection {
	out := map[string][]ReferenceSection{}
	entries, _ := checkDocs.ReadDir("features")
	for _, entry := range entries {
		b, _ := checkDocs.ReadFile("features/" + entry.Name())
		code := ""
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "##") {
				code = ""
				if m := checkHeading.FindStringSubmatch(line); m != nil {
					code = m[1]
				}
				continue
			}
			if code == "" {
				continue
			}
			m := referenceBullet.FindStringSubmatch(line)
			if m != nil {
				title := m[1]
				if title == "Problem" {
					title = "Meaning"
				}
				if title == "Left out" {
					title = "Limitations"
				}
				out[code] = append(out[code], ReferenceSection{Title: title, Text: documentLink.ReplaceAllString(m[2], "$1")})
				continue
			}
			// Keep the whole block, including unlabelled paragraphs and indented
			// continuations. A new label or heading, rather than a newline, ends
			// a section. This also retains tables inside Thresholds paragraphs.
			if len(out[code]) == 0 {
				if strings.TrimSpace(line) == "" {
					continue
				}
				out[code] = append(out[code], ReferenceSection{Title: "Additional context"})
			}
			last := &out[code][len(out[code])-1]
			last.Text += "\n" + documentLink.ReplaceAllString(strings.TrimSpace(line), "$1")
		}
	}
	// Slow routes share Topology's classifier. Include its canonical explanation
	// so the check page is complete without duplicating the authored rule.
	topology, _ := checkDocs.ReadFile("features/topology.md")
	const slowPrefix = "- **Slow routes.** "
	for _, line := range strings.Split(string(topology), "\n") {
		if text, ok := strings.CutPrefix(line, slowPrefix); ok {
			out["server-025"] = append(out["server-025"], ReferenceSection{Title: "Route classification", Text: documentLink.ReplaceAllString(text, "$1")})
		}
	}
	for code, sections := range out {
		for i := range sections {
			sections[i].Text = strings.TrimSpace(sections[i].Text)
		}
		out[code] = sections
	}
	return out
}
