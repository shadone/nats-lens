package api

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Exposition collects the Prometheus samples of one system by family;
// every sample carries system="<id>" as its first label. Families keep
// the order of their first declaration.
type Exposition struct {
	system string
	fams   families
}

// NewExposition returns an empty exposition for the system; an empty ID
// is an error, so that no sample goes without the label.
func NewExposition(system string) (*Exposition, error) {
	if system == "" {
		return nil, errors.New("metrics: empty system ID")
	}
	return &Exposition{system: system}, nil
}

// Family declares a family. A family declared again keeps its first type
// and help.
func (e *Exposition) Family(name, typ, help string) { e.fams.declare(name, typ, help) }

// Sample adds a sample to the family name; a family not declared before
// is declared untyped. Labels are name, value pairs.
func (e *Exposition) Sample(name string, value float64, labels ...string) {
	labels = append([]string{"system", e.system}, labels...)
	var b strings.Builder
	b.WriteString(name)
	b.WriteByte('{')
	for i := 0; i+1 < len(labels); i += 2 {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `%s="%s"`, labels[i], labelEscaper.Replace(labels[i+1]))
	}
	b.WriteByte('}')
	fmt.Fprintf(&b, " %g\n", value)
	e.fams.add(name, b.String())
}

// WriteTo writes the families in the text exposition format.
func (e *Exposition) WriteTo(w io.Writer) (int64, error) { return e.fams.writeTo(w) }

// families are metric families in the order of their declaration, each
// with its sample lines.
type families struct {
	list   []*family
	byName map[string]*family
}

type family struct {
	name, typ, help string
	samples         []string
}

func (f *families) declare(name, typ, help string) {
	if f.byName == nil {
		f.byName = map[string]*family{}
	}
	if _, ok := f.byName[name]; ok {
		return
	}
	fam := &family{name: name, typ: typ, help: help}
	f.list = append(f.list, fam)
	f.byName[name] = fam
}

func (f *families) add(name, line string) {
	if _, ok := f.byName[name]; !ok {
		f.declare(name, "untyped", "")
	}
	fam := f.byName[name]
	fam.samples = append(fam.samples, line)
}

// merge appends o's families and samples; a family both hold keeps f's
// type and help.
func (f *families) merge(o *families) {
	for _, fam := range o.list {
		f.declare(fam.name, fam.typ, fam.help)
		for _, s := range fam.samples {
			f.add(fam.name, s)
		}
	}
}

func (f *families) writeTo(w io.Writer) (int64, error) {
	bw := bufio.NewWriter(w)
	var n int64
	for _, fam := range f.list {
		help := helpEscaper.Replace(fam.help)
		k, _ := fmt.Fprintf(bw, "# HELP %s %s\n# TYPE %s %s\n", fam.name, help, fam.name, fam.typ)
		n += int64(k)
		for _, s := range fam.samples {
			k, _ := bw.WriteString(s)
			n += int64(k)
		}
	}
	return n, bw.Flush()
}

// The text format escapes backslash, double quote and newline in label
// values, and backslash and newline in HELP text.
var (
	labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	helpEscaper  = strings.NewReplacer(`\`, `\\`, "\n", `\n`)
)
