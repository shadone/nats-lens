package config

import (
	"strings"
	"testing"
)

func TestUnauthenticatedListenRefused(t *testing.T) {
	cases := []struct {
		name, listen, audience string
		allow                  bool
		want                   string
	}{
		{"loopback", "127.0.0.1:8080", "", false, ""},
		{"localhost", "localhost:8080", "", false, ""},
		{"disabled", "", "", false, ""},
		{"open without IAP", "0.0.0.0:8080", "", false, "allow_unauthenticated"},
		{"open, allowed", "0.0.0.0:8080", "", true, ""},
		{"open with IAP", "0.0.0.0:8080", "/projects/123/global/backendServices/456", false, ""},
		{"placeholder audience", "0.0.0.0:8080", "/projects/PROJECT_NUMBER/global/backendServices/BACKEND_SERVICE_ID", false, "iap_audience"},
		{"bad audience on loopback", "127.0.0.1:8080", "123", false, "iap_audience"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Default()
			c.Listen, c.IAPAudience, c.AllowUnauthenticated = tc.listen, tc.audience, tc.allow
			err := c.Validate()
			if tc.want == "" && err != nil {
				t.Fatalf("unexpected %v", err)
			}
			if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}
