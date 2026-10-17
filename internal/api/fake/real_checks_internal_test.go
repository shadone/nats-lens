package fake

import (
	"strings"
	"testing"

	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Every check the demo shows is a real one, with its real name and for an
// entity of the check's type: its findings, timeline rows, finding events,
// the findings an event raised, the transitions and the seeded mutes.
func TestDemoUsesRealChecks(t *testing.T) {
	set, err := checks.Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	entityOf := map[v1.EntityType]string{
		v1.EntityType_ENTITY_TYPE_SERVER: "server", v1.EntityType_ENTITY_TYPE_STREAM: "stream",
		v1.EntityType_ENTITY_TYPE_CONSUMER: "consumer", v1.EntityType_ENTITY_TYPE_CLIENT: "client",
		v1.EntityType_ENTITY_TYPE_SYSTEM: "system",
	}
	scenarios := map[string]*scenario{Cluster: prodScenario(), GlobalSystem: globalScenario(), StagingSystem: stagingScenario()}
	for name, sc := range scenarios {
		p := newProvider(sc, false)
		s := p.scen()
		check := func(where, code, checkName string, e *v1.EntityRef) {
			t.Helper()
			c, ok := set.Lookup(code)
			if !ok {
				t.Errorf("%s %s: no check %s", name, where, code)
				return
			}
			if checkName != "" && checkName != c.Name {
				t.Errorf("%s %s: %s named %q, the check is %q", name, where, code, checkName, c.Name)
			}
			if e != nil && entityOf[e.GetType()] != c.Entity {
				t.Errorf("%s %s: %s on a %v, the check is for a %s", name, where, code, e.GetType(), c.Entity)
			}
		}
		fs := p.findings(s)
		for _, f := range fs {
			check("finding "+f.GetId(), f.GetCheckCode(), f.GetCheckName(), f.GetEntity())
			// A value is the query's value column, named as the check's
			// header names it.
			if c, ok := set.Lookup(f.GetCheckCode()); ok && f.GetValue() != nil &&
				(c.ValueName == "" || f.GetValue().GetName() != c.ValueName || f.GetValue().GetBasis() != c.ValueBasis) {
				t.Errorf("%s finding %s: value %q (%q), the check's is %q (%q)", name, f.GetId(),
					f.GetValue().GetName(), f.GetValue().GetBasis(), c.ValueName, c.ValueBasis)
			}
			if sc.transitions != nil {
				for _, tr := range sc.transitions(s, f) {
					check("transition", strings.SplitN(tr.GetFindingId(), ":", 2)[0], "", nil)
				}
			}
		}
		for _, r := range sc.rows(sc) {
			for _, e := range r.entities {
				check("timeline row", r.code, r.name, e)
			}
		}
		for _, e := range p.events(s) {
			if e.GetSource() == srcFinding {
				code, checkName, _ := strings.Cut(e.GetType(), " ")
				check("event "+e.GetId(), code, checkName, nil)
				if e.GetFinding() == nil || e.GetTransition() == nil {
					t.Errorf("%s event %s: finding %v, transition %v", name, e.GetId(), e.GetFinding(), e.GetTransition())
				}
			}
			if f := e.GetFinding(); f != nil {
				check("event "+e.GetId()+" finding", f.GetCheckCode(), f.GetCheckName(), f.GetEntity())
			}
			if tr := e.GetTransition(); tr != nil {
				check("event "+e.GetId()+" transition", strings.SplitN(tr.GetFindingId(), ":", 2)[0], "", nil)
			}
			for _, f := range e.GetRaised() {
				check("event "+e.GetId()+" raised", f.GetCheckCode(), f.GetCheckName(), f.GetEntity())
			}
		}
		for _, m := range p.mutes {
			check("mute "+m.m.GetId(), m.m.GetCheckCode(), "", m.m.GetEntity())
		}
	}
}
