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

// Every authored check paragraph can carry behavior, even a Thresholds paragraph.
// Compare with the embedded source so future summarisation cannot silently lose it.
func TestReferenceRetainsEveryAuthoredParagraph(t *testing.T) {
	refs := CheckReferences()
	entries, err := checkDocs.ReadDir("features")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, entry := range entries {
		b, err := checkDocs.ReadFile("features/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
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
			if m == nil {
				continue
			}
			want := documentLink.ReplaceAllString(m[2], "$1")
			found := false
			for _, section := range refs[code] {
				if section.Text == want {
					found = true
				}
			}
			if !found {
				t.Errorf("%s lost authored %s paragraph: %s", code, m[1], want)
			}
			checked++
		}
	}
	if checked < 400 {
		t.Fatalf("only checked %d paragraphs", checked)
	}
}

func TestReferenceClient007RetentionAndResolution(t *testing.T) {
	var text string
	for _, section := range CheckReferences()["client-007"] {
		text += section.Text + "\n"
	}
	for _, rule := range []string{"however young", "CONNZ failed", "connz_limit", "10th such collection in a row", "resolves at once in a whole collection"} {
		if !strings.Contains(text, rule) {
			t.Errorf("client-007 lost behavior %q", rule)
		}
	}
}

func TestReferenceStream008DoesNotInferHistory(t *testing.T) {
	var text string
	for _, section := range CheckReferences()["stream-008"] {
		text += section.Text + "\n"
	}
	for _, claim := range []string{"a replica was rebuilt", "holds the same messages"} {
		if strings.Contains(text, claim) {
			t.Errorf("stream-008 claims %q from metadata", claim)
		}
	}
	if !strings.Contains(text, "do not prove identical messages") {
		t.Error("reference must explain the identity limitation")
	}
}
