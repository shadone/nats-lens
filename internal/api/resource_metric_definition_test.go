package api_test

import (
	"github.com/shadone/nats-lens/internal/api"
	"strings"
	"testing"
)

func TestResourceRateDefinitionsExplainSequenceEvidence(t *testing.T) {
	for metric, terms := range map[string][]string{"stream.msgs_rate": {"sequence", "skipped", "volume", "creation"}, "consumer.delivered_rate": {"sequence", "redeliver", "unique", "creation"}} {
		s := strings.ToLower(api.Metrics[metric].Description)
		for _, term := range terms {
			if !strings.Contains(s, term) {
				t.Errorf("%s does not explain %q: %s", metric, term, s)
			}
		}
	}
}
