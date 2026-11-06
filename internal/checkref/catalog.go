package checkref

import (
	"sync"

	"github.com/shadone/nats-lens/docs"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

var shippedChecks = sync.OnceValues(func() (*checks.Set, error) { return checks.Load(config.Checks{}) })
var shippedReferences = sync.OnceValue(docs.CheckReferences)

// Response combines immutable shipped reference with optional runtime settings.
func Response(configured *checks.Set, state string) (*v1.ListCheckDefinitionsResponse, error) {
	defaults, err := shippedChecks()
	if err != nil {
		return nil, err
	}
	res := &v1.ListCheckDefinitionsResponse{ConfigurationState: state, DefaultRestartWindow: checks.DurationText(defaults.RestartWindow()), DefaultTimeout: checks.DurationText(defaults.Timeout())}
	if configured != nil {
		restart, timeout := checks.DurationText(configured.RestartWindow()), checks.DurationText(configured.Timeout())
		res.RestartWindow, res.Timeout = &restart, &timeout
	}

	for _, c := range defaults.Checks() {
		d := &v1.CheckDefinition{Code: c.Code, Name: c.Name, Entity: c.Entity, Category: c.Category, Severity: c.Severity, SeverityRule: c.SeverityRule,
			Kind: c.Kind, Lookback: c.Lookback, Description: c.Description, Remediation: c.Remediation, ValueName: c.ValueName, ValueBasis: c.ValueBasis,
			RelatedConfig: c.RelatedConfig, SuppressedBy: c.SuppressedBy}
		if configured != nil {
			disabled := configured.Disabled(c.Code)
			d.Disabled = &disabled
			if c.Lookback != "" {
				lb := checks.DurationText(configured.Lookback(c.Code))
				d.EffectiveLookback = &lb
			}
		}
		for _, t := range c.Thresholds {
			threshold := &v1.CheckThreshold{Name: t.Name, DefaultValue: t.Default, Unit: t.Unit, Description: t.Description}
			if configured != nil {
				if v, ok := configured.Value(c.Code, t.Name); ok {
					effective := v.String()
					threshold.EffectiveValue = &effective
				}
			}
			d.Thresholds = append(d.Thresholds, threshold)
		}
		for _, section := range shippedReferences()[c.Code] {
			d.Reference = append(d.Reference, &v1.CheckReferenceSection{Title: section.Title, Text: section.Text})
		}
		res.Definitions = append(res.Definitions, d)
	}
	return res, nil
}
