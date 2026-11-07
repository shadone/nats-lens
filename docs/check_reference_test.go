package docs

import (
	"strings"
	"testing"
)

func TestReferenceSelfContainedPlainText(t *testing.T) {
	refs := CheckReferences()
	if len(refs) != 89 {
		t.Fatalf("references=%d, want 89", len(refs))
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

// Audit every source line within a check block. This oracle deliberately uses
// Markdown delimiters, not the production heading/bullet regexes. It includes
// unlabelled paragraphs, table rows and continuations as well as labelled text.
func TestReferenceRetainsEveryAuthoredParagraph(t *testing.T) {
	refs := CheckReferences()
	entries, _ := checkDocs.ReadDir("features")
	codes, lines := map[string]bool{}, 0
	for _, entry := range entries {
		b, err := checkDocs.ReadFile("features/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		code := ""
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "##") {
				code = ""
				parts := strings.Split(line, "`")
				if strings.HasPrefix(line, "### `") && len(parts) >= 3 {
					if _, ok := refs[parts[1]]; ok {
						code = parts[1]
						codes[code] = true
					}
				}
				continue
			}
			if code == "" || strings.TrimSpace(line) == "" {
				continue
			}
			want := strings.TrimSpace(line)
			if strings.HasPrefix(want, "- **") {
				_, rest, ok := strings.Cut(want[4:], "**")
				if !ok {
					t.Fatalf("unclosed source label: %s", line)
				}
				want = strings.TrimSpace(rest)
			}
			// Strip document destinations independently; the UI keeps their label.
			for strings.Contains(want, "](") {
				start := strings.Index(want, "[")
				mid := strings.Index(want[start:], "](") + start
				end := strings.Index(want[mid:], ")") + mid
				want = want[:start] + want[start+1:mid] + want[end+1:]
			}
			var got string
			for _, section := range refs[code] {
				got += section.Text + " "
			}
			if !strings.Contains(strings.Join(strings.Fields(got), " "), strings.Join(strings.Fields(want), " ")) {
				t.Errorf("%s lost source content: %s", code, want)
			}
			lines++
		}
	}
	if len(codes) != 89 || lines < 400 {
		t.Fatalf("audit covered %d checks / %d lines", len(codes), lines)
	}
	t.Logf("audited %d checks / %d substantive source lines", len(codes), lines)
}

func TestReferenceLiteralPrerequisitesAndExclusions(t *testing.T) {
	for code, rules := range map[string][]string{
		"client-006":   {"mute set on the old key no longer applies, so mute the new key again", "Without it nats-lens stores no `client.auth_error` event", "cannot tell \"no failed login\" from \"not captured\""},
		"client-007":   {"row still counts MQTT clients"},
		"client-008":   {"So is nats.py's `0.0.0`", "unknown and not judged", "A minimum of 0 turns a language off", "`min_python` | `python3` | nats.py | 2.9.0"},
		"consumer-015": {"nats-server refuses an update of `max_waiting`", "A queue that is full by design is muted with that reason"},
	} {
		var text string
		for _, section := range CheckReferences()[code] {
			text += section.Text + "\n"
		}
		for _, rule := range rules {
			if !strings.Contains(text, rule) {
				t.Errorf("%s lost %q", code, rule)
			}
		}
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
