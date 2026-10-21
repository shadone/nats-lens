package fake

import (
	"strings"
	"testing"
)

// Every demo finding says what to do; one without a story of its own says
// what its check says, as stream-005 on order-archive does.
func TestDemoFindingsCarryRemediation(t *testing.T) {
	scenarios := map[string]*scenario{Cluster: prodScenario(), GlobalSystem: globalScenario(), StagingSystem: stagingScenario()}
	for name, sc := range scenarios {
		p := newProvider(sc, false)
		for _, f := range p.findings(p.scen()) {
			if f.GetRemediation() == "" {
				t.Errorf("%s %s: no remediation", name, f.GetId())
			}
			if f.GetId() == "stream-005:APP/order-archive" && !strings.Contains(f.GetRemediation(), "accept it as by design with a review date on the Capacity page") {
				t.Errorf("%s %s: remediation %q is not the check's", name, f.GetId(), f.GetRemediation())
			}
		}
	}
}
