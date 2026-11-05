package archive_test

import (
	"testing"

	"github.com/shadone/nats-lens/internal/archive"
)

// The location shown to API clients carries no credentials: an object
// storage URL loses its user information, query and fragment; a local
// path is kept as it is.
func TestLocationText(t *testing.T) {
	for in, want := range map[string]string{
		"/var/lib/natslens/archive": "/var/lib/natslens/archive",
		"archive":                   "archive",
		"s3://key:secret@bucket.example.com/archive?token=abc#x":     "s3://bucket.example.com/archive",
		"https://user@storage.example.com/a?X-Amz-Signature=deadbee": "https://storage.example.com/a",
		"gs://bucket/archive": "gs://bucket/archive",
	} {
		if got := archive.LocationText(in); got != want {
			t.Errorf("LocationText(%q) = %q, want %q", in, got, want)
		}
	}
}
