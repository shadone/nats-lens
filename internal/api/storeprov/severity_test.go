package storeprov

import (
	"testing"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The server deletes and recreates the internal consumers of mirrors and
// sources on its own; their deletes are routine, not critical.
func TestInternalConsumerDeleteIsRoutine(t *testing.T) {
	for _, c := range []struct {
		consumer string
		want     v1.Severity
	}{
		{"resize-worker", v1.Severity_SEVERITY_CRITICAL},
		{"JS_MIRROR_order-archive_1a2b", v1.Severity_SEVERITY_UNSPECIFIED},
		{"JS_SOURCE_x_9z", v1.Severity_SEVERITY_UNSPECIFIED},
	} {
		if got := severityOf("consumer.deleted", "", c.consumer); got != c.want {
			t.Errorf("%s: %s, want %s", c.consumer, got, c.want)
		}
	}
}
