package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/config"
)

// The parts of a receiver URL and token that must never appear in a
// result's text.
const (
	pathSecret  = "B0SECRETPATH"
	querySecret = "Q0SECRETQUERY"
	tokenSecret = "T0SECRETTOKEN"
)

// received is one request a test receiver got.
type received struct {
	method, path string
	header       http.Header
	body         []byte
}

// receiver is an httptest server that records each request and answers
// with handle.
func receiver(t *testing.T, handle func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, chan received) {
	t.Helper()
	got := make(chan received, 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- received{method: r.Method, path: r.URL.RequestURI(), header: r.Header.Clone(), body: b}
		handle(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func status(code int) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
}

// secretURL is the server's URL with a secret path and query.
func secretURL(srv *httptest.Server) string {
	return srv.URL + "/services/" + pathSecret + "?sig=" + querySecret
}

// fileChannel is a channel whose URL and token are in files.
func fileChannel(t *testing.T, typ, url string) config.Channel {
	t.Helper()
	dir := t.TempDir()
	ch := config.DefaultChannel()
	ch.Name, ch.Type = "ops-webhook", typ
	ch.URLFile = filepath.Join(dir, "url")
	writeSecret(t, ch.URLFile, url+"\n")
	if typ == config.ChannelWebhook {
		ch.BearerTokenFile = filepath.Join(dir, "token")
		writeSecret(t, ch.BearerTokenFile, tokenSecret+"\n")
	}
	return ch
}

func writeSecret(t *testing.T, path, v string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(v), 0o600); err != nil {
		t.Fatal(err)
	}
}

// noSecret fails when the text holds a part of the URL's path or query or
// the token.
func noSecret(t *testing.T, what, text string) {
	t.Helper()
	for _, s := range []string{pathSecret, querySecret, tokenSecret, "/services/"} {
		if strings.Contains(text, s) {
			t.Errorf("%s holds %q: %s", what, s, text)
		}
	}
}

func checkResult(t *testing.T, r Result, outcome Outcome, code int) {
	t.Helper()
	if r.Outcome != outcome || r.Status != code {
		t.Errorf("result %v, want %v with HTTP %d", r, outcome, code)
	}
	noSecret(t, "result", r.String())
	if r.Err != nil {
		noSecret(t, "error", r.Err.Error())
	}
}

func TestSendWebhookDelivered(t *testing.T) {
	srv, got := receiver(t, status(http.StatusNoContent))
	s := NewSender(fileChannel(t, config.ChannelWebhook, secretURL(srv)), "1.4.0")
	b := batch(sample(KindFiring))
	r := s.Send(context.Background(), b)
	checkResult(t, r, Delivered, 204)
	if r.String() != "delivered, HTTP 204" || r.Host != srv.URL {
		t.Errorf("result %q host %q", r, r.Host)
	}
	req := <-got
	if req.method != http.MethodPost || req.path != "/services/"+pathSecret+"?sig="+querySecret {
		t.Errorf("request %s %s", req.method, req.path)
	}
	for k, want := range map[string]string{
		"Content-Type": "application/json", "User-Agent": "natslens/1.4.0",
		"Authorization": "Bearer " + tokenSecret, "Idempotency-Key": "example-prod:7-9",
	} {
		if v := req.header.Get(k); v != want {
			t.Errorf("%s = %q, want %q", k, v, want)
		}
	}
	want, _ := WebhookBody(b)
	if string(req.body) != string(want) {
		t.Errorf("body %s", req.body)
	}
}

func TestSendSlackDelivered(t *testing.T) {
	srv, got := receiver(t, status(http.StatusOK))
	ch := config.DefaultChannel()
	ch.Name, ch.Type = "ops-chat", config.ChannelSlack
	ch.URLEnv = "NATSLENS_TEST_CHAT_URL"
	t.Setenv(ch.URLEnv, secretURL(srv))
	r := NewSender(ch, "1.4.0").Send(context.Background(), batch(sample(KindResolved)))
	checkResult(t, r, Delivered, 200)
	req := <-got
	var body map[string]any
	if err := json.Unmarshal(req.body, &body); err != nil || len(body) != 1 || body["text"] == nil {
		t.Errorf("slack body %s (%v)", req.body, err)
	}
	if req.header.Get("Authorization") != "" || req.header.Get("Idempotency-Key") != "" {
		t.Errorf("slack request has webhook headers: %v", req.header)
	}
}

func TestSendStatuses(t *testing.T) {
	cases := []struct {
		name    string
		handle  func(http.ResponseWriter, *http.Request)
		outcome Outcome
		code    int
		after   time.Duration
	}{
		{"500", status(500), Retry, 500, 0},
		{"503 retry-after", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(503)
		}, Retry, 503, 2 * time.Minute},
		{"429 retry-after", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(429)
		}, Retry, 429, 30 * time.Second},
		{"408", status(408), Retry, 408, 0},
		{"400", status(400), Refused, 400, 0},
		{"404", status(404), Refused, 404, 0},
		{"redirect", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://elsewhere.example.com/collect", http.StatusTemporaryRedirect)
		}, Refused, 307, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, got := receiver(t, c.handle)
			r := NewSender(fileChannel(t, config.ChannelWebhook, secretURL(srv)), "1.4.0").Send(context.Background(), batch(sample(KindFiring)))
			checkResult(t, r, c.outcome, c.code)
			if r.RetryAfter != c.after {
				t.Errorf("retry after %s, want %s", r.RetryAfter, c.after)
			}
			<-got
			if len(got) != 0 {
				t.Errorf("%d more requests: a redirect was followed", len(got))
			}
		})
	}
}

func TestRetryAfterDate(t *testing.T) {
	now := time.Date(2026, 11, 5, 9, 30, 0, 0, time.UTC)
	if d := retryAfter(now.Add(90*time.Second).Format(http.TimeFormat), now); d != 90*time.Second {
		t.Errorf("date: %s", d)
	}
	for _, v := range []string{"", "soon", "-5", now.Add(-time.Minute).Format(http.TimeFormat)} {
		if d := retryAfter(v, now); d != 0 {
			t.Errorf("%q: %s", v, d)
		}
	}
}

func TestSendTimeout(t *testing.T) {
	release := make(chan struct{})
	srv, _ := receiver(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	ch := fileChannel(t, config.ChannelWebhook, secretURL(srv))
	ch.Timeout = config.Duration(200 * time.Millisecond)
	start := time.Now()
	r := NewSender(ch, "1.4.0").Send(context.Background(), batch(sample(KindFiring)))
	checkResult(t, r, Retry, 0)
	if r.Err == nil || r.Err.Error() != "no answer within 200ms" {
		t.Errorf("err %v", r.Err)
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Errorf("took %s", el)
	}
}

// A receiver that does not listen is a network error, retried, and its
// text names no secret part.
func TestSendNetworkError(t *testing.T) {
	srv, _ := receiver(t, status(204))
	u := secretURL(srv)
	srv.Close()
	r := NewSender(fileChannel(t, config.ChannelWebhook, u), "1.4.0").Send(context.Background(), batch(sample(KindFiring)))
	checkResult(t, r, Retry, 0)
	if r.Err == nil {
		t.Fatal("no error")
	}
}

// A missing or empty secret file, an unset variable and a file holding a
// URL that is not https are retried failures that name the key and the
// file, not the value.
func TestSendSecretProblems(t *testing.T) {
	ch := fileChannel(t, config.ChannelWebhook, "https://hooks.example.com/services/"+pathSecret)
	missing := ch
	missing.URLFile = filepath.Join(t.TempDir(), "absent")
	r := NewSender(missing, "1.4.0").Send(context.Background(), batch(sample(KindFiring)))
	checkResult(t, r, Retry, 0)
	if r.Err == nil || !strings.Contains(r.Err.Error(), "url_file "+missing.URLFile+" cannot be read") {
		t.Errorf("missing file: %v", r.Err)
	}

	noToken := ch
	noToken.BearerTokenFile = filepath.Join(t.TempDir(), "token")
	writeSecret(t, noToken.BearerTokenFile, "\n")
	r = NewSender(noToken, "1.4.0").Send(context.Background(), batch(sample(KindFiring)))
	if r.Outcome != Retry || r.Err == nil || !strings.Contains(r.Err.Error(), "bearer_token_file "+noToken.BearerTokenFile+" is empty") {
		t.Errorf("empty token file: %v", r)
	}

	env := ch
	env.URLFile, env.URLEnv = "", "NATSLENS_TEST_UNSET_URL"
	t.Setenv(env.URLEnv, "")
	if err := SecretProblem(env); err == nil || err.Error() != "url_env NATSLENS_TEST_UNSET_URL is not set" {
		t.Errorf("unset variable: %v", err)
	}

	plain := fileChannel(t, config.ChannelWebhook, "http://hooks.example.com/services/"+pathSecret+"?sig="+querySecret)
	err := SecretProblem(plain)
	if err == nil || !strings.Contains(err.Error(), "url_file "+plain.URLFile+": an http URL") {
		t.Errorf("http URL in file: %v", err)
	}
	noSecret(t, "error", err.Error())
	if SecretProblem(ch) != nil {
		t.Errorf("good channel: %v", SecretProblem(ch))
	}
}

// A rotated file applies at the next attempt, without a new sender.
func TestSendRereadsFiles(t *testing.T) {
	srv, got := receiver(t, status(204))
	ch := fileChannel(t, config.ChannelWebhook, secretURL(srv))
	s := NewSender(ch, "1.4.0")
	s.Send(context.Background(), batch(sample(KindFiring)))
	writeSecret(t, ch.BearerTokenFile, "rotated\n")
	s.Send(context.Background(), batch(sample(KindFiring)))
	if a, b := (<-got).header.Get("Authorization"), (<-got).header.Get("Authorization"); a != "Bearer "+tokenSecret || b != "Bearer rotated" {
		t.Errorf("tokens %q then %q", a, b)
	}
}

// scrub takes every secret part out of a text, such as a *url.Error's,
// which holds the whole URL.
func TestScrub(t *testing.T) {
	d := destination{url: "https://user:pw@hooks.example.com/services/" + pathSecret + "?sig=" + querySecret, token: tokenSecret}
	text := `Post "` + d.url + `": token ` + tokenSecret + " path /services/" + pathSecret + " segment " + pathSecret + " pw user:pw"
	got := d.scrub(text)
	noSecret(t, "scrubbed text", got)
	if strings.Contains(got, "user:pw") {
		t.Errorf("user info kept: %s", got)
	}
	if !strings.Contains(d.scrub("dial tcp: lookup hooks.example.com"), "hooks.example.com") {
		t.Error("host scrubbed")
	}
	if err := unwrapURLError(&urlErr); err.Error() != "connection refused" {
		t.Errorf("unwrapped %v", err)
	}
}

var urlErr = url.Error{Op: "Post", URL: "https://hooks.example.com/services/" + pathSecret, Err: errors.New("connection refused")}
