package api

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Exposition collects Prometheus samples by family and writes them in
// the text format, each family's HELP and TYPE once, followed by its
// samples from every system. Families keep the order of their first
// declaration.
type Exposition struct {
	system   string
	families []*family
	byName   map[string]*family
}

type family struct {
	name, typ, help string
	samples         []string
}

// NewExposition returns an empty exposition.
func NewExposition() *Exposition {
	return &Exposition{byName: map[string]*family{}}
}

// SetSystem sets the system ID that labels the samples added next, as
// the first label system="<id>"; empty adds no label.
func (e *Exposition) SetSystem(id string) { e.system = id }

// Family declares a family. A family declared again keeps its first type
// and help.
func (e *Exposition) Family(name, typ, help string) {
	if _, ok := e.byName[name]; ok {
		return
	}
	f := &family{name: name, typ: typ, help: help}
	e.families = append(e.families, f)
	e.byName[name] = f
}

// Sample adds a sample to the family name; a family not declared before
// is declared untyped. Labels are name, value pairs.
func (e *Exposition) Sample(name string, value float64, labels ...string) {
	if _, ok := e.byName[name]; !ok {
		e.Family(name, "untyped", "")
	}
	f := e.byName[name]
	if e.system != "" {
		labels = append([]string{"system", e.system}, labels...)
	}
	var b strings.Builder
	b.WriteString(name)
	if len(labels) > 0 {
		b.WriteByte('{')
		for i := 0; i+1 < len(labels); i += 2 {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `%s="%s"`, labels[i], promLabel(labels[i+1]))
		}
		b.WriteByte('}')
	}
	fmt.Fprintf(&b, " %g\n", value)
	f.samples = append(f.samples, b.String())
}

// WriteTo writes the families in the text exposition format.
func (e *Exposition) WriteTo(w io.Writer) (int64, error) {
	bw := bufio.NewWriter(w)
	var n int64
	for _, f := range e.families {
		k, _ := fmt.Fprintf(bw, "# HELP %s %s\n# TYPE %s %s\n", f.name, f.help, f.name, f.typ)
		n += int64(k)
		for _, s := range f.samples {
			k, _ := bw.WriteString(s)
			n += int64(k)
		}
	}
	return n, bw.Flush()
}

var labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

// promLabel escapes a label value for the Prometheus text format.
func promLabel(s string) string { return labelEscaper.Replace(s) }
