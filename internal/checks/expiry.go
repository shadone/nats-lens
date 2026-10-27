package checks

import (
	"fmt"
	"io/fs"
	"log/slog"
	"sync"

	checkfiles "github.com/shadone/nats-lens/checks"
)

// CategoryExpiry is the category of the checks that warn of something that
// will expire, a certificate or a JWT. Among warnings the API ranks their
// findings after every other warning, so they never head an outage.
const CategoryExpiry = "expiry"

// expiryCodesIn are the checks of CategoryExpiry among the check files of
// fsys.
func expiryCodesIn(fsys fs.FS) (map[string]bool, error) {
	cs, err := load(fsys)
	if err != nil {
		return nil, fmt.Errorf("expiry checks: %w", err)
	}
	out := map[string]bool{}
	for _, c := range cs {
		if c.Category == CategoryExpiry {
			out[c.Code] = true
		}
	}
	return out, nil
}

// expiryCodes are the built-in checks of CategoryExpiry. The built-in
// files load whenever natslens starts (Load fails the start otherwise), so
// an error here is a fault of the binary; it is logged once, and no
// finding ranks as an expiry.
var expiryCodes = sync.OnceValue(func() map[string]bool {
	out, err := expiryCodesIn(checkfiles.FS)
	if err != nil {
		slog.Error("built-in checks do not load; no finding ranks as an expiry", "err", err)
		return map[string]bool{}
	}
	return out
})

// IsExpiry reports whether the built-in check code warns of an expiry.
func IsExpiry(code string) bool { return expiryCodes()[code] }
