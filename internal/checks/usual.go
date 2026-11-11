package checks

import (
	"log/slog"
	"sync"

	"github.com/shadone/nats-lens/internal/config"
)

// usual maps each built-in check code to its header's severity and
// severity rule. The built-in files load whenever natslens starts, so an
// error here is a fault of the binary; it is logged once, and no check is
// known.
var usual = sync.OnceValue(func() map[string][2]string {
	set, err := Load(config.Checks{})
	if err != nil {
		slog.Error("built-in checks do not load; no finding says why it has its severity", "err", err)
		return map[string][2]string{}
	}
	out := make(map[string][2]string, len(set.Checks()))
	for _, c := range set.Checks() {
		out[c.Code] = [2]string{c.Severity, c.SeverityRule}
	}
	return out
})

// UsualSeverity returns the built-in check's usual severity and its
// severity rule, empty when every finding of the check has the usual
// severity; ok is false for a code that is no built-in check.
func UsualSeverity(code string) (severity, rule string, ok bool) {
	v, ok := usual()[code]
	return v[0], v[1], ok
}
