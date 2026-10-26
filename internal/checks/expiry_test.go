package checks

import "testing"

// The checks that warn of an expiry, and only they, have the category
// expiry; the limit checks do not.
func TestIsExpiry(t *testing.T) {
	for code, want := range map[string]bool{
		"account-003": true, "account-004": true, "server-016": true,
		"account-001": false, "account-002": false, "server-001": false, "nope-001": false,
	} {
		if IsExpiry(code) != want {
			t.Errorf("IsExpiry(%s) = %v, want %v", code, !want, want)
		}
	}
}
