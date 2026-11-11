package timing

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Family is one metric family in the Prometheus text format: its
// samples' lines, each ending in a newline.
type Family struct {
	Name, Type, Help string
	Lines            []string
}

type collector interface{ collect() Family }

var (
	regMu    sync.Mutex
	registry []collector
)

func register(c collector) {
	regMu.Lock()
	defer regMu.Unlock()
	registry = append(registry, c)
}

// Families returns every registered family in the order of declaration,
// each series sorted by its label values. A family without samples is
// returned with no lines.
func Families() []Family {
	regMu.Lock()
	cs := slices.Clone(registry)
	regMu.Unlock()
	out := make([]Family, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.collect())
	}
	return out
}

func (f *family) sortedKeys() []string {
	ks := make([]string, 0, len(f.keys))
	for k := range f.keys {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

func (v *HistogramVec) collect() Family {
	fam := Family{Name: v.name, Type: "histogram", Help: v.help}
	v.mu.RLock()
	defer v.mu.RUnlock()
	for _, k := range v.sortedKeys() {
		vals := v.keys[k]
		cum, sum, count := v.series[k].snapshot()
		for i, b := range v.bounds {
			fam.Lines = append(fam.Lines, line(v.name+"_bucket", float64(cum[i]), v.labels, vals, "le", strconv.FormatFloat(b, 'g', -1, 64)))
		}
		fam.Lines = append(fam.Lines, line(v.name+"_bucket", float64(count), v.labels, vals, "le", "+Inf"))
		fam.Lines = append(fam.Lines, line(v.name+"_sum", sum, v.labels, vals))
		fam.Lines = append(fam.Lines, line(v.name+"_count", float64(count), v.labels, vals))
	}
	return fam
}

func (g *GaugeVec) collect() Family {
	fam := Family{Name: g.name, Type: "gauge", Help: g.help}
	g.mu.RLock()
	defer g.mu.RUnlock()
	for _, k := range g.sortedKeys() {
		fam.Lines = append(fam.Lines, line(g.name, float64(g.series[k].Load()), g.labels, g.keys[k]))
	}
	return fam
}

// line formats one sample; extra is a trailing name, value pair.
func line(name string, value float64, labels, values []string, extra ...string) string {
	var b strings.Builder
	b.WriteString(name)
	n := 0
	write := func(k, v string) {
		if n == 0 {
			b.WriteByte('{')
		} else {
			b.WriteByte(',')
		}
		n++
		fmt.Fprintf(&b, `%s="%s"`, k, escaper.Replace(v))
	}
	for i, l := range labels {
		write(l, values[i])
	}
	for i := 0; i+1 < len(extra); i += 2 {
		write(extra[i], extra[i+1])
	}
	if n > 0 {
		b.WriteByte('}')
	}
	fmt.Fprintf(&b, " %g\n", value)
	return b.String()
}

var escaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
