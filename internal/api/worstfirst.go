package api

import (
	"cmp"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// CompareWorst orders findings worst first, as every list and headline
// ranks them: firing before resolved, unmuted before muted, then by
// severity; among warnings a finding that is not an expiry warning (its
// check's category is expiry, Finding.expiry) before one that is, so a
// credential that expires soon never heads an outage; then the longest
// firing first, then by ID. A critical expiry (expired, or within days)
// ranks as any critical finding.
func CompareWorst(a, b *v1.Finding) int {
	return cmp.Or(
		cmp.Compare(rankOf(a.GetState() == v1.FindingState_FINDING_STATE_RESOLVED), rankOf(b.GetState() == v1.FindingState_FINDING_STATE_RESOLVED)),
		cmp.Compare(rankOf(a.GetMute() != nil), rankOf(b.GetMute() != nil)),
		cmp.Compare(severityOrder(a.GetSeverity()), severityOrder(b.GetSeverity())),
		cmp.Compare(rankOf(expiryWarning(a)), rankOf(expiryWarning(b))),
		sinceOf(a).Compare(sinceOf(b)),
		cmp.Compare(a.GetId(), b.GetId()),
	)
}

func sinceOf(f *v1.Finding) time.Time { return f.GetSince().AsTime() }

func rankOf(b bool) int {
	if b {
		return 1
	}
	return 0
}

func severityOrder(s v1.Severity) int {
	switch s {
	case v1.Severity_SEVERITY_CRITICAL:
		return 0
	case v1.Severity_SEVERITY_WARNING:
		return 1
	case v1.Severity_SEVERITY_INFO:
		return 2
	}
	return 3
}

// expiryWarning reports whether f is an expiry finding at warning, which
// ranks after the other warnings.
func expiryWarning(f *v1.Finding) bool {
	return f.GetExpiry() && f.GetSeverity() == v1.Severity_SEVERITY_WARNING
}
