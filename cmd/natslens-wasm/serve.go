package main

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing/fstest"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/fake"
)

// version is what GetLensInfo reports; the build sets it with -X.
var version = "dev"

// request is one HTTP request the page hands to the API: the path with its
// query, below the API's root ("/api/..."), and single-valued headers.
type request struct {
	Method string
	URL    string
	Header map[string]string
	Body   []byte
}

// response is the API's answer, with repeated headers joined by ", ".
type response struct {
	Status int
	Header map[string]string
	Body   []byte
}

// newMux serves the demo's systems through the mux natslens demo serves,
// live (shifted to the wall clock) or at the scenario's own time. It serves
// no UI: the page itself is static files.
func newMux(live bool, log *slog.Logger) (http.Handler, error) {
	systems := fake.Demo(live)
	if err := systems.Validate(); err != nil {
		return nil, err
	}
	h := &api.Handler{Systems: systems, Log: log, Version: version}
	return api.NewMux(h, fstest.MapFS{}), nil
}

// serve runs one request through h. A panic outside the Connect handlers,
// which recover their own, becomes an error rather than ending the program
// that every later request needs.
func serve(h http.Handler, r request) (res response, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("natslens-wasm: %s %s panicked: %v", r.Method, r.URL, p)
		}
	}()
	var body io.Reader = http.NoBody
	if r.Body != nil {
		body = bytes.NewReader(r.Body)
	}
	req := httptest.NewRequest(r.Method, r.URL, body)
	for k, v := range r.Header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	out := rec.Result()
	defer out.Body.Close()
	b, err := io.ReadAll(out.Body)
	if err != nil {
		return response{}, err
	}
	hdr := make(map[string]string, len(out.Header))
	for k, v := range out.Header {
		hdr[k] = strings.Join(v, ", ")
	}
	return response{Status: out.StatusCode, Header: hdr, Body: b}, nil
}
