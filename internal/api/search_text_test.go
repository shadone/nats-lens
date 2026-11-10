package api

import (
	"testing"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func TestEventHasText(t *testing.T) {
	e := &v1.Event{Kind: "k8s.pod.not_ready", Reason: "Unhealthy", Account: "ADKEY", AccountName: "Suppliers"}
	for text, want := range map[string]bool{"UNHEALTHY": true, "suppl": true, "shop/resize": true, "not_ready": true, "other": false} {
		if got := EventHasText(e, text, "shop/resize-worker-x"); got != want {
			t.Errorf("%q: %v, want %v", text, got, want)
		}
	}
}
