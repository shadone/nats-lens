package fake

import (
	"github.com/shadone/nats-lens/internal/checkref"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"sync"
)

// CheckDefinitions keeps shipped reference separate from this system's settings.
func (p *Provider) CheckDefinitions() (*v1.ListCheckDefinitionsResponse, error) {
	return checkref.Response(nil, "demo")
}

// Shipped guidance is shared with reference pages, never site remediation.
var shippedCheckGuidance = sync.OnceValue(func() map[string]string {
	response, err := checkref.Response(nil, "demo")
	if err != nil {
		panic(err)
	} // Invalid embedded definitions are a build defect.
	out := make(map[string]string, len(response.Definitions))
	for _, definition := range response.Definitions {
		out[definition.Code] = definition.Remediation
	}
	return out
})
