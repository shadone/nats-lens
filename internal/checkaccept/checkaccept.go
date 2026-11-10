// Package checkaccept says which built-in checks may be accepted as by
// design and which acceptances end early on a growth change, from the
// acceptable and accept_review keys of the check files' headers.
// internal/checks validates those keys when it loads the files; this
// package reads only them, so that the API can apply the rule without
// importing the check engine.
package checkaccept

import (
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"

	checkfiles "github.com/shadone/nats-lens/checks"
)

// ReviewGrowth is the accept_review of a check whose acceptance ends early
// when the entity's week's growth changes.
const ReviewGrowth = "growth"

type header struct {
	Code         string `yaml:"code"`
	Acceptable   bool   `yaml:"acceptable"`
	AcceptReview string `yaml:"accept_review"`
}

type catalog struct {
	acceptable, growth map[string]bool
}

func read(fsys fs.FS) (catalog, error) {
	out := catalog{acceptable: map[string]bool{}, growth: map[string]bool{}}
	files, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return out, err
	}
	for _, name := range files {
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			return out, err
		}
		text := strings.TrimSpace(string(b))
		end := strings.Index(text, "*/")
		if !strings.HasPrefix(text, "/*") || end < 0 {
			return out, fmt.Errorf("%s: no header", name)
		}
		var h header
		if err := yaml.Unmarshal([]byte(text[2:end]), &h); err != nil {
			return out, fmt.Errorf("%s: header: %w", name, err)
		}
		out.acceptable[h.Code] = h.Acceptable
		out.growth[h.Code] = h.Acceptable && h.AcceptReview == ReviewGrowth
	}
	return out, nil
}

// builtin is read once. The built-in files load whenever natslens starts,
// so an error is a fault of the binary; it is logged, and no check is
// acceptable.
var builtin = sync.OnceValue(func() catalog {
	c, err := read(checkfiles.FS)
	if err != nil {
		slog.Error("built-in check headers do not read; no check can be accepted", "err", err)
		return catalog{}
	}
	return c
})

// Acceptable reports whether a finding of the built-in check code may be
// accepted as by design.
func Acceptable(code string) bool { return builtin().acceptable[code] }

// GrowthReview reports whether an acceptance of the built-in check code
// ends early when the entity's growth changes.
func GrowthReview(code string) bool { return builtin().growth[code] }
