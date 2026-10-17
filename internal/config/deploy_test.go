package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func exampleConfig(t *testing.T, example string) (Config, string) {
	t.Helper()
	p := filepath.Join("..", "..", "k8s", "examples", example, "natslens.yaml")
	c, err := LoadFile(p, Default())
	if err != nil {
		t.Fatal(err)
	}
	return c, p
}

// The IAP example carries a placeholder audience that fails validation, and
// validates once it is replaced.
func TestIAPExampleNeedsTheAudience(t *testing.T) {
	const placeholder = "/projects/PROJECT_NUMBER/global/backendServices/BACKEND_SERVICE_ID"
	c, p := exampleConfig(t, "gke-iap")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), placeholder) {
		t.Fatalf("%s lacks the audience placeholder", p)
	}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "iap_audience") {
		t.Fatalf("placeholder accepted: %v", err)
	}
	c.IAPAudience = "/projects/123/global/backendServices/456"
	if err := c.Validate(); err != nil {
		t.Fatalf("with an audience: %v", err)
	}
}

// The port-forward example validates as it is.
func TestBasicExampleValidates(t *testing.T) {
	c, _ := exampleConfig(t, "basic")
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
