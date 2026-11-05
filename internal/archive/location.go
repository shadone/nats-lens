package archive

import "net/url"

// LocationText is an archive location as API clients may see it: a URL
// without its user information, query and fragment, where an object
// storage location would carry credentials; a local path as it is.
func LocationText(loc string) string {
	u, err := url.Parse(loc)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return loc
	}
	u.User, u.RawQuery, u.ForceQuery, u.Fragment, u.RawFragment = nil, "", false, "", ""
	return u.String()
}
