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
		// URLs Go cannot parse, or without a host, lose everything up to the
		// last @ and from the query on.
		"s3://us%er:p%40ss@b/p": "s3://b/p",
		"s3://key:sec ret@b/p":  "s3://b/p",
		"s3://k:s@/p":           "s3:///p",
		"s3://b/p%zz?secret=1":  "s3://b/p%zz",
		"s3:bucket?secret=1":    "s3:bucket",
		"file:///tmp/a?x=1":     "file:///tmp/a",
		"s3://b:9000/p;k=v":     "s3://b:9000/p;k=v",
		`C:\data\a`:             `C:\data\a`,
		"/data/a?b#c":           "/data/a?b#c",
		"relative/dir":          "relative/dir",
	} {
		if got := archive.LocationText(in); got != want {
			t.Errorf("LocationText(%q) = %q, want %q", in, got, want)
		}
	}
}
