package api

import (
	"github.com/shadone/nats-lens/internal/checkaccept"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// MarkAcceptance sets what the check catalog says of accepting the
// finding (acceptable, growth_review) and its mute's growth_review.
func MarkAcceptance(f *v1.Finding) {
	f.Acceptable, f.GrowthReview = checkaccept.Acceptable(f.GetCheckCode()), checkaccept.GrowthReview(f.GetCheckCode())
	MarkMute(f.GetMute())
}

// MarkMute sets growth_review on an acceptance of a check with the growth
// review; a plain mute, and an acceptance of any other check, has none.
func MarkMute(m *v1.Mute) {
	if m != nil {
		m.GrowthReview = m.GetAccepted() && checkaccept.GrowthReview(m.GetCheckCode())
	}
}
