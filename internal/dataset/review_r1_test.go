package dataset

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReviewUnpublishedCanonicalAlias(t *testing.T) {
	path, _ := acquireFixture(t)
	stage := filepath.Join(filepath.Dir(path), ".dataset-partial-review")
	if err := os.Rename(path, stage); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "/.", "/../.dataset-partial-review/."} {
		d, err := Open(context.Background(), stage+suffix)
		if err == nil {
			d.Close()
			t.Errorf("unpublished artifact accepted via %q", stage+suffix)
		}
	}
}
func TestReviewMissingCaptureDisclosures(t *testing.T) {
	path, m := acquireFixture(t)
	b, _ := json.Marshal(m)
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"application_version", "pending_stages", "exported_facts", "live_only"} {
		delete(raw, key)
	}
	b, _ = json.Marshal(raw)
	if err := os.WriteFile(filepath.Join(path, ManifestFile), b, 0600); err != nil {
		t.Fatal(err)
	}
	d, err := Open(context.Background(), path)
	if err == nil {
		d.Close()
		t.Fatal("manifest without application version or capture completeness disclosures accepted")
	}
}

func TestReviewStageAliasesAndPublishedControls(t *testing.T) {
	path, _ := acquireFixture(t)
	parent := filepath.Dir(path)
	for _, alias := range []string{path + "/", path + "/.", path + "/../" + filepath.Base(path) + "/."} {
		d, err := Open(context.Background(), alias)
		if err != nil {
			t.Fatalf("published alias rejected %q: %v", alias, err)
		}
		d.Close()
	}
	stage := filepath.Join(parent, ".dataset-partial-aliases")
	if err := os.Rename(path, stage); err != nil {
		t.Fatal(err)
	}
	parentLink := filepath.Join(privateRoot(t), "parent")
	if err := os.Symlink(parent, parentLink); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(".", stage)
	if err != nil {
		t.Fatal(err)
	}
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		t.Fatal(err)
	}
	aliases := []string{stage + "/", stage + "/.", stage + "//./", stage + "/../" + filepath.Base(stage) + "/.", relative + "/.", filepath.Join(parentLink, filepath.Base(stage)) + "/.", filepath.Join(resolvedParent, filepath.Base(stage)) + "/."}
	for _, alias := range aliases {
		d, err := Open(context.Background(), alias)
		if err == nil {
			d.Close()
			t.Errorf("stage alias accepted %q", alias)
		}
	}
	stageLink := filepath.Join(parent, "stage-link")
	if err := os.Symlink(stage, stageLink); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{stageLink, stageLink + "/.", stageLink + "/"} {
		if d, err := Open(context.Background(), alias); err == nil {
			d.Close()
			t.Errorf("final symlink accepted %q", alias)
		}
	}
}
func TestReviewDisclosureSemantics(t *testing.T) {
	path, m := acquireFixture(t)
	for _, key := range []string{"application_version", "pending_stages", "exported_facts", "live_only"} {
		for _, mutation := range []string{"omitted", "null", "empty", "invalid", "missing_token", "duplicate", "additional_token"} {
			t.Run(key+"/"+mutation, func(t *testing.T) {
				b, _ := json.Marshal(m)
				var raw map[string]any
				if err := json.Unmarshal(b, &raw); err != nil {
					t.Fatal(err)
				}
				switch mutation {
				case "omitted":
					delete(raw, key)
				case "null":
					raw[key] = nil
				case "empty":
					if key == "application_version" {
						raw[key] = ""
					} else {
						raw[key] = []string{}
					}
				case "invalid":
					if key == "application_version" {
						raw[key] = "https://example.com/private?token=invalid"
					} else {
						raw[key] = []string{"pipeline_complete"}
					}
				default:
					if key == "application_version" {
						return
					}
					values := raw[key].([]any)
					switch mutation {
					case "missing_token":
						raw[key] = values[1:]
					case "duplicate":
						raw[key] = append(values, values[0])
					case "additional_token":
						raw[key] = append(values, "pipeline_complete")
					}
				}
				b, _ = json.Marshal(raw)
				if err := os.WriteFile(filepath.Join(path, ManifestFile), b, 0600); err != nil {
					t.Fatal(err)
				}
				if d, err := Open(context.Background(), path); err == nil {
					d.Close()
					t.Fatal("invalid disclosure accepted")
				}
			})
		}
	}
}
