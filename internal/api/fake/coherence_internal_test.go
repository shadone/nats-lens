package fake

import (
	"math"
	"slices"
	"strconv"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The demo's storage findings, mutes and acceptances agree with its stream
// and bucket specs, as the real checks and the growth review would have
// them: stream-005 fires on exactly the unbounded streams the inventory
// does not declare, with the related configuration the check writes;
// stream-006 names the stream's byte limit; every seeded mute names a
// finding; an acceptance recorded the growth of its entity's series then,
// and one nats-lens ended has its finding firing again and an end reason
// from the series.
func TestDemoStorageStoryAgreesWithSpecs(t *testing.T) {
	for name, sc := range map[string]*scenario{Cluster: prodScenario(), GlobalSystem: globalScenario(), StagingSystem: stagingScenario()} {
		p := newProvider(sc, false)
		s := p.scen()
		fs := sc.findings(s)
		firing := map[string]*v1.Finding{}
		seen := map[string]bool{}
		for _, f := range fs {
			seen[f.GetCheckCode()+":"+f.GetEntity().GetKey()] = true
			if f.GetState() != resolved {
				firing[f.GetCheckCode()+":"+f.GetEntity().GetKey()] = f
			}
		}
		for _, sp := range sc.streams {
			l := sc.streamLimitsOf(sp)
			f := firing["stream-005:"+Account+"/"+sp.name]
			if want := l.unbounded() && l.declared == ""; want != (f != nil) {
				t.Errorf("%s %s: unbounded and not declared %v, stream-005 firing %v", name, sp.name, want, f != nil)
			}
			if f != nil {
				w := s.unboundedFinding(sp.name, f.GetSince().AsTime())
				if f.GetDetail() != w.GetDetail() || !slices.EqualFunc(f.GetRelatedConfig(), w.GetRelatedConfig(), func(a, b *v1.ConfigValue) bool {
					return a.GetKey() == b.GetKey() && a.GetValue() == b.GetValue()
				}) {
					t.Errorf("%s %s: stream-005 says %q %v, the check would write %q %v", name, sp.name, f.GetDetail(), f.GetRelatedConfig(),
						w.GetDetail(), w.GetRelatedConfig())
				}
			}
			if f := firing["stream-006:"+Account+"/"+sp.name]; f != nil {
				for _, c := range f.GetRelatedConfig() {
					if (c.GetKey() == "max_bytes" && c.GetValue() != strconv.FormatInt(l.maxBytes, 10)) || (c.GetKey() == "discard" && c.GetValue() != l.discard) {
						t.Errorf("%s %s: stream-006 says %s %s, the stream has max_bytes %d discard %s", name, sp.name, c.GetKey(), c.GetValue(),
							l.maxBytes, l.discard)
					}
				}
				if l.maxBytes <= 0 {
					t.Errorf("%s %s: stream-006 without max_bytes", name, sp.name)
				}
			}
		}
		if sc.mutes == nil {
			continue
		}
		for _, sm := range sc.mutes() {
			m := sm.view(s)
			if m.GetEntity() == nil {
				continue
			}
			key := m.GetCheckCode() + ":" + m.GetEntity().GetKey()
			if !seen[key] {
				t.Errorf("%s mute %s: no finding %s", name, m.GetId(), key)
			}
			if !m.GetAccepted() {
				continue
			}
			before := func(at time.Time) *float64 {
				return s.growth(m.GetEntity(), &v1.TimeRange{Start: timestamppb.New(at.Add(-api.AcceptGrowthWindow)), End: timestamppb.New(at)})
			}
			if g := before(m.GetCreatedAt().AsTime()); g == nil || m.GrowthPerDay == nil || math.Abs(*g-m.GetGrowthPerDay()) > 0.02*math.Abs(*g)+1 {
				t.Errorf("%s acceptance %s recorded %s, its series grew %s", name, m.GetId(), growthText(m.GrowthPerDay), growthText(g))
			}
			if m.GetUnmutedBy() != api.AcceptEndedBy {
				continue
			}
			// The review's rule over the series: changed at the end, at no
			// collection of the day before, and worded as the review words it.
			series := func(c time.Time) (float64, bool) { return sc.value("stream.bytes", m.GetEntity(), c) }
			end := m.GetUnmutedAt().AsTime()
			g := reviewGrowth(series, end)
			if g == nil || !api.GrowthChanged(m.GetGrowthPerDay(), g.PerDay) || m.GetEndReason() != g.EndReason(m.GetGrowthPerDay()) {
				t.Errorf("%s acceptance %s ended %q, the review reads %v then", name, m.GetId(), m.GetEndReason(), g)
			}
			// Every collection of the day before the end leaves it in force.
			for at := end.Add(-24 * time.Hour); at.Before(end); at = at.Add(sampleEvery) {
				if e := reviewGrowth(series, at); e != nil && api.GrowthChanged(m.GetGrowthPerDay(), e.PerDay) {
					t.Errorf("%s acceptance %s: the review would have ended it at %v, before %v", name, m.GetId(), at, end)
					break
				}
			}
			if firing[key] == nil {
				t.Errorf("%s acceptance %s ended, and %s does not fire", name, m.GetId(), key)
			}
		}
	}
}

// growthText writes a growth that may be unset.
func growthText(v *float64) string {
	if v == nil {
		return "nothing"
	}
	return api.FmtGrowth(*v)
}
