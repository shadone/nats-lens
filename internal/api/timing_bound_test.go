package api

import (
	"fmt"
	"testing"

	"connectrpc.com/connect"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/timing"
)

// Several systems, every procedure and several codes each keep their own
// series: none of them reaches the per-system bound.
func TestAPITimingKeepsEverySystem(t *testing.T) {
	methods := v1.File_natslens_v1_service_proto.Services().ByName("LensService").Methods()
	codes := []string{"ok", connect.CodeCanceled.String(), connect.CodeDeadlineExceeded.String(), connect.CodeInvalidArgument.String(),
		connect.CodeNotFound.String(), connect.CodeUnavailable.String(), connect.CodeInternal.String(), connect.CodeFailedPrecondition.String()}
	systems := []string{timing.AllSystems, timing.UnknownSystem}
	for i := range 6 {
		systems = append(systems, fmt.Sprintf("bound-test-%d", i))
	}
	for _, sys := range systems {
		for i := range methods.Len() {
			for _, c := range codes {
				timing.APIDuration.With(sys, string(methods.Get(i).Name()), c).Observe(0.01)
			}
		}
	}
	for _, sys := range systems[2:] {
		for i := range methods.Len() {
			for _, c := range codes {
				if n := timing.APIDuration.Count(sys, string(methods.Get(i).Name()), c); n != 1 {
					t.Fatalf("%s %s %s counted %d, want 1", sys, methods.Get(i).Name(), c, n)
				}
			}
		}
	}
	for _, s := range timing.APIDuration.Series() {
		if s.Labels[0] == timing.Overflow {
			t.Fatalf("a system collapsed to other: %v", s.Labels)
		}
	}
}
