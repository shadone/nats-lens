package notify

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/shadone/nats-lens/internal/config"
)

// destination is a channel's URL and token as read for one attempt.
type destination struct {
	url, token string
}

// readDestination reads the channel's URL and token from where the
// configuration says, at each call, so that a rotated file applies without
// a restart. An error names the key and the file or variable, never the
// value.
func readDestination(ch config.Channel) (destination, error) {
	var d destination
	var err error
	switch {
	case ch.URL != "":
		d.url = ch.URL
	case ch.URLFile != "":
		d.url, err = readSecretFile("url_file", ch.URLFile)
	case ch.URLEnv != "":
		d.url, err = readSecretEnv("url_env", ch.URLEnv)
	default:
		err = errors.New("no url, url_file or url_env")
	}
	if err != nil {
		return d, err
	}
	if err := config.CheckChannelURL(d.url); err != nil {
		return d, fmt.Errorf("%s: %w", urlKey(ch), err)
	}
	switch {
	case ch.BearerTokenFile != "":
		d.token, err = readSecretFile("bearer_token_file", ch.BearerTokenFile)
	case ch.BearerTokenEnv != "":
		d.token, err = readSecretEnv("bearer_token_env", ch.BearerTokenEnv)
	}
	return d, err
}

func urlKey(ch config.Channel) string {
	switch {
	case ch.URLFile != "":
		return "url_file " + ch.URLFile
	case ch.URLEnv != "":
		return "url_env " + ch.URLEnv
	}
	return "url"
}

func readSecretFile(key, path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		var pe *os.PathError
		if errors.As(err, &pe) {
			err = pe.Err
		}
		return "", fmt.Errorf("%s %s cannot be read: %w", key, path, err)
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", fmt.Errorf("%s %s is empty", key, path)
	}
	return v, nil
}

func readSecretEnv(key, name string) (string, error) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return "", fmt.Errorf("%s %s is not set", key, name)
	}
	return v, nil
}

// SecretProblem says why a channel's URL or token cannot be read now.
func SecretProblem(ch config.Channel) error {
	_, err := readDestination(ch)
	return err
}

// secretParts are the parts of a destination that must not appear in a
// text: the whole URL, its user information, path and query, and the
// token.
func (d destination) secretParts() []string {
	parts := []string{d.url, d.token}
	if u, err := url.Parse(d.url); err == nil {
		parts = append(parts, u.RawQuery, u.Fragment, u.EscapedPath(), u.Path)
		if u.User != nil {
			parts = append(parts, u.User.String())
		}
		// Each path segment too: a library may print one alone.
		for _, seg := range strings.Split(u.Path, "/") {
			if len(seg) >= 4 && !strings.Contains(u.Host, seg) {
				parts = append(parts, seg)
			}
		}
	}
	var out []string
	for _, p := range parts {
		if len(p) > 1 {
			out = append(out, p)
		}
	}
	return out
}

// scrub removes the destination's secret parts from a text, longest
// first.
func (d destination) scrub(s string) string {
	parts := d.secretParts()
	for changed := true; changed; {
		changed = false
		longest := -1
		for i, p := range parts {
			if strings.Contains(s, p) && (longest < 0 || len(p) > len(parts[longest])) {
				longest = i
			}
		}
		if longest >= 0 {
			s = strings.ReplaceAll(s, parts[longest], "[redacted]")
			changed = true
		}
	}
	return s
}
