package main

import (
	"fmt"
	"strings"
	"testing"
)

// microRefusalEvidence validates the evidence before interpreting absent events.
func microRefusalEvidence(epochs, refused, held, unwanted int) error {
	if epochs < 2 {
		return fmt.Errorf("only %d epochs: want repeated collection", epochs)
	}
	if refused == 0 {
		return fmt.Errorf("no $SRV permission refusals recorded")
	}
	if held == 0 {
		return fmt.Errorf("no held $SRV requests after refusal")
	}
	if unwanted > 0 {
		return fmt.Errorf("%d collector.subscription_error events for refused $SRV requests", unwanted)
	}
	return nil
}

func TestMicroRefusalEvidence(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		epochs, refused, held, unwanted int
		want                            string
	}{
		{name: "no activity", want: "epochs"},
		{name: "no refusals", epochs: 2, want: "refusals"},
		{name: "no held requests", epochs: 2, refused: 2, want: "held"},
		{name: "only one epoch", epochs: 1, refused: 2, held: 2, want: "epochs"},
		{name: "forbidden events", epochs: 2, refused: 2, held: 2, unwanted: 1, want: "subscription_error"},
		{name: "active and clean", epochs: 2, refused: 2, held: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := microRefusalEvidence(tc.epochs, tc.refused, tc.held, tc.unwanted)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("evidence (%d epochs, %d refusals, %d held, %d unwanted): got %v, want %q", tc.epochs, tc.refused, tc.held, tc.unwanted, err, tc.want)
			}
		})
	}
}
