package checks

import (
	"testing"
	"testing/fstest"

	checkfiles "github.com/shadone/nats-lens/checks"
)

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

// Check files that do not load are an error, not an empty set.
func TestExpiryCodesInBadFiles(t *testing.T) {
	if _, err := expiryCodesIn(fstest.MapFS{"bad-001.sql": {Data: []byte("SELECT 1")}}); err == nil {
		t.Fatal("no error for a check file without a header")
	}
	got, err := expiryCodesIn(checkfiles.FS)
	if err != nil || !got["server-016"] || got["account-001"] {
		t.Fatalf("built-in expiry checks %v, %v", got, err)
	}
}
