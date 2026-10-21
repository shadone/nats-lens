package fake

import (
	"sync"

	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// checkRemediation is each built-in check's remediation by code, read once
// from checks/*.sql, so a demo finding without a story of its own says what
// the real check says.
var checkRemediation = sync.OnceValue(func() map[string]string {
	out := map[string]string{}
	set, err := checks.Load(config.Checks{})
	if err != nil {
		return out
	}
	for _, c := range set.Checks() {
		out[c.Code] = c.Remediation
	}
	return out
})

// withRemediation gives each finding without a remediation its check's.
func withRemediation(fs []*v1.Finding) {
	for _, f := range fs {
		if f.GetRemediation() == "" {
			f.Remediation = checkRemediation()[f.GetCheckCode()]
		}
	}
}
