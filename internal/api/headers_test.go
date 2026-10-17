package api

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func cspHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

func TestSecurityHeaders(t *testing.T) {
	script := "\ntry { document.documentElement.dataset.x = 1 } catch (e) {}\n"
	style := "\nbody{margin:0}\n"
	index := "<!doctype html><html><head><script>" + script + "</script>" +
		`<script type="module" src="/assets/app.js"></script><style>` + style + "</style></head></html>"
	ui := fstest.MapFS{"index.html": {Data: []byte(index)}, "assets/app.js": {Data: []byte("js")}}
	h := NewMux(&Handler{}, ui)
	for _, path := range []string{"/", "/streams/x", "/assets/app.js", "/healthz", "/api/natslens.v1.LensService/GetLensInfo", "/api/nothing"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		hd := w.Header()
		if got := hd.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options %q", path, got)
		}
		if got := hd.Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("%s: Referrer-Policy %q", path, got)
		}
		csp := hd.Get("Content-Security-Policy")
		for _, want := range []string{"default-src 'self'", "frame-ancestors 'none'", "object-src 'none'", "base-uri 'none'",
			"script-src 'self' " + cspHash(script), "style-src 'self' " + cspHash(style)} {
			if !strings.Contains(csp, want) {
				t.Errorf("%s: CSP %q lacks %q", path, csp, want)
			}
		}
		if strings.Contains(csp, "unsafe-inline") {
			t.Errorf("%s: CSP allows inline code: %q", path, csp)
		}
	}
}

// The module script with a src needs no hash; only inline code does.
func TestInlineHashesSkipExternalScripts(t *testing.T) {
	got := inlineHashes([]byte(`<script src="/a.js"></script><script type="module" crossorigin src="/b.js"></script><script>x()</script>`), "script")
	if len(got) != 1 || got[0] != cspHash("x()") {
		t.Fatalf("hashes = %v", got)
	}
}
