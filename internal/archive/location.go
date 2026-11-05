package archive

import (
	"net/url"
	"strings"
)

// LocationText is an archive location as API clients may see it: a URL
// without its user information, query and fragment, where an object
// storage location would carry credentials; a local path as it is. A URL
// that cannot be parsed, or has no host, loses everything between :// and
// the last @ and everything from ? or # on, so that no credential is shown.
func LocationText(loc string) string {
	u, err := url.Parse(loc)
	if err == nil && u.Scheme != "" && u.Host != "" {
		u.User, u.RawQuery, u.ForceQuery, u.Fragment, u.RawFragment = nil, "", false, "", ""
		return u.String()
	}
	if !hasScheme(loc) {
		return loc
	}
	s := loc
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.Index(s, "://"); i >= 0 {
		if j := strings.LastIndex(s, "@"); j > i {
			s = s[:i+3] + s[j+1:]
		}
	}
	return s
}

// hasScheme reports whether loc starts with a URL scheme of two characters
// or more followed by a colon; a single letter is a Windows drive.
func hasScheme(loc string) bool {
	i := strings.Index(loc, ":")
	if i < 2 {
		return false
	}
	for k, c := range loc[:i] {
		letter := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		if !letter && (k == 0 || !(c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.')) {
			return false
		}
	}
	return true
}
