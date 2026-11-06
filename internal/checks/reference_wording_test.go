package checks

import (
	"github.com/shadone/nats-lens/internal/config"
	"strings"
	"testing"
)

func TestCreationReferenceDoesNotInferCauseOrPromiseRepair(t *testing.T) {
	s, err := Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := s.Lookup("consumer-009")
	if strings.Contains(c.Description, "a replica was rebuilt") || strings.Contains(c.Remediation, "so the replicas resynchronise") {
		t.Fatal("creation timestamps cannot establish a rebuild or guarantee step-down repairs it")
	}
	if !strings.Contains(c.Description, "position_ahead_tolerance_ratio of the leader's position") {
		t.Error("ahead ratio must be a share of the leader position")
	}
	for _, word := range []string{"currentness", "configuration", "captured"} {
		if !strings.Contains(c.Remediation, word) {
			t.Errorf("remediation lacks %q evidence", word)
		}
	}
}
