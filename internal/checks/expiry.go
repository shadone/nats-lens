package checks

import (
	"sync"

	checkfiles "github.com/shadone/nats-lens/checks"
)

// CategoryExpiry is the category of the checks that warn of something that
// will expire, a certificate or a JWT. Within a severity the API ranks
// their findings after every other finding, so they never head an outage.
const CategoryExpiry = "expiry"

// expiryCodes are the built-in checks of CategoryExpiry.
var expiryCodes = sync.OnceValue(func() map[string]bool {
	out := map[string]bool{}
	cs, err := load(checkfiles.FS)
	if err != nil {
		return out
	}
	for _, c := range cs {
		if c.Category == CategoryExpiry {
			out[c.Code] = true
		}
	}
	return out
})

// IsExpiry reports whether the built-in check code warns of an expiry.
func IsExpiry(code string) bool { return expiryCodes()[code] }
