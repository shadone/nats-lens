package investigation

import (
	"slices"
	"strings"
)

// connectionsIncludeTooling is the Connections page's effective visibility
// policy, not an API default. A name search includes matching tooling even
// with tooling=0. Both import and link-equivalence checks use this policy;
// explicit request fields are applied afterward. Cross-language contract
// fixtures are exercised against the actual UI's outgoing requests.
func connectionsIncludeTooling(name string, tooling bool) bool {
	return tooling || name != ""
}

// ECMAScript String.trim WhiteSpace + LineTerminator, not Go's different
// Unicode White_Space set (notably FEFF versus 0085). Identity fields are never
// passed here. ASCII controls are still rejected by the context boundary.
func uiTrim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return r >= 0x9 && r <= 0xd || r == 0x20 || r == 0xa0 || r == 0x1680 || r >= 0x2000 && r <= 0x200a || r == 0x2028 || r == 0x2029 || r == 0x202f || r == 0x205f || r == 0x3000 || r == 0xfeff
	})
}
func eventKinds(raw string) []string {
	out := []string{}
	for _, s := range strings.Split(raw, ",") {
		if s = uiTrim(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
func eventSources(raw string) []string {
	ids := strings.Split(raw, ",")
	out := []string{}
	for _, p := range []struct{ id, value string }{{"nats", "EVENT_SOURCE_NATS"}, {"kubernetes", "EVENT_SOURCE_KUBERNETES"}, {"findings", "EVENT_SOURCE_FINDING"}, {"collector", "EVENT_SOURCE_COLLECTOR"}} {
		if slices.Contains(ids, p.id) {
			out = append(out, p.value)
		}
	}
	return out
}
func findingSeverities(raw string) []string {
	out := []string{}
	for _, s := range strings.Split(raw, ",") {
		if slices.Contains([]string{"critical", "warning", "info"}, s) {
			out = append(out, "SEVERITY_"+strings.ToUpper(s))
		}
	}
	return out
}
