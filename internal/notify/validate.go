package notify

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
)

// CheckFilters refuses a channel whose checks or exclude_checks name an
// unknown check code, or whose categories name a category no check has.
// It runs once the check files are loaded, as checks.disabled is checked.
func CheckFilters(n config.Notifications, set *checks.Set) error {
	var cats []string
	for _, c := range set.Checks() {
		if !slices.Contains(cats, c.Category) {
			cats = append(cats, c.Category)
		}
	}
	sort.Strings(cats)
	var errs []error
	for i, ch := range n.Channels {
		p := fmt.Sprintf("notifications.channels[%d] (%s)", i, ch.Name)
		for key, codes := range map[string][]string{"checks": ch.Checks, "exclude_checks": ch.ExcludeChecks} {
			for _, code := range codes {
				if _, ok := set.Lookup(code); !ok {
					errs = append(errs, fmt.Errorf("%s: %s: unknown check %q", p, key, code))
				}
			}
		}
		for _, c := range ch.Categories {
			if !slices.Contains(cats, c) {
				errs = append(errs, fmt.Errorf("%s: categories: unknown category %q; categories are %s", p, c, strings.Join(cats, ", ")))
			}
		}
	}
	sortErrors(errs)
	return errors.Join(errs...)
}

// sortErrors orders errors by their text, so that a map's order does not
// show.
func sortErrors(errs []error) {
	sort.Slice(errs, func(i, j int) bool { return errs[i].Error() < errs[j].Error() })
}
