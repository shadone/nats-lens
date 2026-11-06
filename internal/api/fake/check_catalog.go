package fake

import (
	"github.com/shadone/nats-lens/internal/checkref"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// CheckDefinitions keeps shipped reference separate from this system's settings.
func (p *Provider) CheckDefinitions() (*v1.ListCheckDefinitionsResponse, error) {
	return checkref.Response(nil, "demo")
}
