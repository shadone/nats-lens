package docs

import (
	"strings"
	"testing"
)

func TestReferenceSelfContainedPlainText(t *testing.T) {
	refs := CheckReferences()
	if len(refs) != 88 {
		t.Fatalf("references=%d, want 88", len(refs))
	}
	for code, sections := range refs {
		for _, section := range sections {
			if strings.Contains(section.Text, "](") {
				t.Errorf("%s contains an unresolved document link: %s", code, section.Text)
			}
		}
	}
	var slow string
	for _, section := range refs["server-025"] {
		slow += section.Text
	}
	for _, rule := range []string{"2 ms", "3 times", "more than half"} {
		if !strings.Contains(slow, rule) {
			t.Errorf("slow route reference lacks %q classification rule", rule)
		}
	}
}
