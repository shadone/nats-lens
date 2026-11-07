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

func TestStreamCreationReferenceDoesNotInferCauseOrPromiseRepair(t *testing.T) {
	s, err := Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := s.Lookup("stream-008")
	for _, claim := range []string{"was rebuilt", "holds the same messages", "so the replicas resynchronise", "no action is needed"} {
		if strings.Contains(c.Description+c.Remediation, claim) {
			t.Errorf("unsupported creation metadata claim %q", claim)
		}
	}
	for _, evidence := range []string{"currentness", "configuration", "captured", "do not prove identical messages", "does not guarantee repair"} {
		if !strings.Contains(c.Description+c.Remediation, evidence) {
			t.Errorf("stream guidance lacks %q", evidence)
		}
	}
}

func TestRaftMetadataSamplingQualifications(t *testing.T) {
	s, err := Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"server-027", "server-028"} {
		c, _ := s.Lookup(code)
		for _, phrase := range []string{"not proof of continuous capture", "Entirely absent collections within max_gap can bridge", "retained collection", "gaps over max_gap", "proposed, not yet confirmed"} {
			if !strings.Contains(c.Description, phrase) {
				t.Errorf("%s metadata lacks %q", code, phrase)
			}
		}
		text := c.Description + c.ValueBasis
		for _, threshold := range c.Thresholds {
			text += threshold.Description
		}
		for _, claim := range []string{"uninterrupted successful", "uninterrupted observed duration", "continuously observed", "throughout the grace period"} {
			if strings.Contains(text, claim) {
				t.Errorf("%s unsupported metadata claim %q", code, claim)
			}
		}
	}
}
