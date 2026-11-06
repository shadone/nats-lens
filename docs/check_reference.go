// Package docs embeds the shipped product reference used by the check catalog.
package docs

import (
	"embed"
	"regexp"
	"strings"
)

//go:embed features/*-checks.md
var checkDocs embed.FS

// ReferenceSection is a plain paragraph with a label. Backticks mark code spans.
type ReferenceSection struct{ Title, Text string }

var checkHeading = regexp.MustCompile("^### `([a-z]+-[0-9]{3})` ")
var referenceBullet = regexp.MustCompile(`^- \*\*([^*]+):\*\* (.*)$`)

// CheckReferences reads only the per-check sections, excluding duplicated
// descriptions and threshold lists. These documents remain the source of
// firing rules and limitations; no second authored catalog is maintained.
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
			if m == nil || m[1] == "Problem" || m[1] == "Thresholds" {
				continue
			}
			title := m[1]
			if title == "Left out" {
				title = "Limitations"
			}
			out[code] = append(out[code], ReferenceSection{Title: title, Text: m[2]})
		}
	}
	return out
}
