package api

import (
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
)

// inlineElement matches an inline script or style element and its text.
var inlineElement = map[string]*regexp.Regexp{
	"script": regexp.MustCompile(`(?is)<script([^>]*)>(.*?)</script>`),
	"style":  regexp.MustCompile(`(?is)<style([^>]*)>(.*?)</style>`),
}

// srcAttr matches a src attribute, which makes a script element external.
var srcAttr = regexp.MustCompile(`(?i)\ssrc\s*=`)

// inlineHashes returns the CSP source of every inline element of tag
// ("script" or "style") in page: 'sha256-<base64>' of its exact text.
func inlineHashes(page []byte, tag string) []string {
	var out []string
	for _, m := range inlineElement[tag].FindAllSubmatch(page, -1) {
		if srcAttr.Match(m[1]) {
			continue
		}
		sum := sha256.Sum256(m[2])
		out = append(out, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
	}
	return out
}

// contentSecurityPolicy allows the page's own files, the inline theme
// script and styles of index.html by hash, data: images (the favicon), and
// no framing. The hashes are taken from the embedded index.html, so they
// follow every build.
func contentSecurityPolicy(ui fs.FS) string {
	page, _ := fs.ReadFile(ui, "index.html")
	src := func(tag string) string {
		return strings.Join(append([]string{"'self'"}, inlineHashes(page, tag)...), " ")
	}
	return strings.Join([]string{
		"default-src 'self'",
		"script-src " + src("script"),
		"style-src " + src("style"),
		"img-src 'self' data:",
		"font-src 'self'",
		"connect-src 'self'",
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}

// withSecurityHeaders sets the security headers on every response.
func withSecurityHeaders(csp string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
