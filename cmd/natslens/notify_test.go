package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/config"
)

const (
	notifyPathSecret  = "B0SECRETPATH"
	notifyQuerySecret = "Q0SECRETQUERY"
	notifyTokenSecret = "T0SECRETTOKEN"
)

// notifyReceiver answers each path with its status: /ok 204, /chat 200,
// /gone 404, /broken 500, /slow after the client gave up. It records the
// bodies by path.
func notifyReceiver(t *testing.T) (*httptest.Server, func(path string) []map[string]any) {
	t.Helper()
	got := map[string][]map[string]any{}
	done := make(chan struct{})
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		body["_auth"] = r.Header.Get("Authorization")
		mu.Lock()
		got[r.URL.Path] = append(got[r.URL.Path], body)
		mu.Unlock()
		switch r.URL.Path {
		case "/services/" + notifyPathSecret + "/ok":
			w.WriteHeader(http.StatusNoContent)
		case "/services/" + notifyPathSecret + "/chat":
			w.WriteHeader(http.StatusOK)
		case "/services/" + notifyPathSecret + "/gone":
			w.WriteHeader(http.StatusNotFound)
		case "/services/" + notifyPathSecret + "/broken":
			w.WriteHeader(http.StatusInternalServerError)
		case "/services/" + notifyPathSecret + "/slow":
			select {
			case <-done:
			case <-r.Context().Done():
			}
		}
	}))
	t.Cleanup(func() { close(done); srv.Close() })
	return srv, func(path string) []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return got["/services/"+notifyPathSecret+path]
	}
}

func writeNotifyFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// notifyTestConfig has one channel per outcome; each URL holds a secret
// path and query, the webhook's token is in a file.
func notifyTestConfig(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	dir := t.TempDir()
	u := func(p string) string {
		return srv.URL + "/services/" + notifyPathSecret + p + "?sig=" + notifyQuerySecret
	}
	token := writeNotifyFile(t, dir, "token", notifyTokenSecret+"\n")
	okURL := writeNotifyFile(t, dir, "ok-url", u("/ok")+"\n")
	t.Setenv("NATSLENS_TEST_CHAT_URL", u("/chat"))
	return writeNotifyFile(t, dir, "natslens.yaml", fmt.Sprintf(`
systems:
  - id: example-prod
    name: Production
    nats: {url: "nats://127.0.0.1:4222"}
  - id: example-staging
    nats: {url: "nats://127.0.0.1:4223"}
notifications:
  ui_url: https://nats-lens.example.com
  channels:
    - name: ops-webhook
      type: webhook
      url_file: %s
      bearer_token_file: %s
    - name: ops-chat
      type: slack
      url_env: NATSLENS_TEST_CHAT_URL
      systems: [example-staging]
    - name: gone
      type: webhook
      url: %s
    - name: broken
      type: webhook
      url: %s
    - name: slow
      type: webhook
      url: %s
      timeout: 300ms
    - name: missing
      type: webhook
      url_file: %s
`, okURL, token, u("/gone"), u("/broken"), u("/slow"), filepath.Join(dir, "absent")))
}

func noNotifySecret(t *testing.T, what, text string) {
	t.Helper()
	for _, s := range []string{notifyPathSecret, notifyQuerySecret, notifyTokenSecret, "/services/"} {
		if strings.Contains(text, s) {
			t.Errorf("%s holds %q:\n%s", what, s, text)
		}
	}
}

func TestNotifyTest(t *testing.T) {
	srv, got := notifyReceiver(t)
	cfgPath := notifyTestConfig(t, srv)
	host := strings.TrimPrefix(srv.URL, "http://")
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	var out bytes.Buffer
	err := runNotifyCmd(context.Background(), []string{"test", "-config", cfgPath}, &out)
	if err == nil || err.Error() != "4 of 6 channels failed" {
		t.Fatalf("err %v\n%s", err, out.String())
	}
	want := []string{
		"ops-webhook (" + host + "): delivered, HTTP 204",
		"ops-chat (" + host + "): delivered, HTTP 200",
		"gone (" + host + "): refused, HTTP 404",
		"broken (" + host + "): failed, HTTP 500",
		"slow (" + host + "): failed: no answer within 300ms",
		"missing: failed: url_file ",
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != len(want) {
		t.Fatalf("lines:\n%s", out.String())
	}
	for i, w := range want {
		if !strings.HasPrefix(lines[i], w) {
			t.Errorf("line %d %q, want %q", i, lines[i], w)
		}
	}
	if !strings.HasSuffix(lines[5], "absent cannot be read: no such file or directory") {
		t.Errorf("missing file line %q", lines[5])
	}
	noNotifySecret(t, "output", out.String())
	noNotifySecret(t, "error", err.Error())
	noNotifySecret(t, "log", logs.String())

	// The webhook body is version 1 with one test message; the token is
	// sent; the Slack body is one text.
	hook := got("/ok")
	if len(hook) != 1 {
		t.Fatalf("webhook requests %v", hook)
	}
	if hook[0]["version"] != 1.0 || hook[0]["_auth"] != "Bearer "+notifyTokenSecret {
		t.Errorf("webhook body %v", hook[0])
	}
	items, _ := hook[0]["notifications"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["kind"] != "test" ||
		items[0].(map[string]any)["detail"] != "nats-lens test message from example-prod" {
		t.Errorf("webhook notifications %v", items)
	}
	chat := got("/chat")
	if len(chat) != 1 || !strings.Contains(chat[0]["text"].(string), "TEST nats-lens test message from example-staging") {
		t.Errorf("slack body %v", chat)
	}
}

// -channel tests one channel, and a run where every channel delivers
// exits 0; -system names the system in the message.
func TestNotifyTestOneChannel(t *testing.T) {
	srv, got := notifyReceiver(t)
	cfgPath := notifyTestConfig(t, srv)
	var out bytes.Buffer
	if err := runNotifyCmd(context.Background(), []string{"test", "-config", cfgPath, "-channel", "ops-chat", "-system", "example-prod"}, &out); err != nil {
		t.Fatalf("err %v\n%s", err, out.String())
	}
	if n := strings.Count(out.String(), "\n"); n != 1 || !strings.Contains(out.String(), "ops-chat") {
		t.Errorf("output %q", out.String())
	}
	if chat := got("/chat"); len(chat) != 1 || !strings.Contains(chat[0]["text"].(string), "Production (example-prod)") {
		t.Errorf("slack body %v", chat)
	}
	if len(got("/ok")) != 0 {
		t.Error("another channel was sent to")
	}
	err := runNotifyCmd(context.Background(), []string{"test", "-config", cfgPath, "-channel", "ops-mail"}, &out)
	if err == nil || !strings.Contains(err.Error(), `unknown channel "ops-mail"; configured channels: ops-webhook, ops-chat`) {
		t.Errorf("unknown channel: %v", err)
	}
	err = runNotifyCmd(context.Background(), []string{"test", "-config", cfgPath, "-system", "example-dev"}, &out)
	if err == nil || !strings.Contains(err.Error(), `unknown system "example-dev"`) {
		t.Errorf("unknown system: %v", err)
	}
}

func TestNotifyTestRefusals(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	for _, c := range []struct {
		args []string
		want string
	}{
		{nil, "usage: natslens notify test"},
		{[]string{"send"}, "usage: natslens notify test"},
		{[]string{"test"}, "-config is required"},
		{[]string{"test", "-config", writeNotifyFile(t, dir, "empty.yaml", "data_dir: /tmp/x\n")}, "no notifications.channels"},
		{[]string{"test", "-config", writeNotifyFile(t, dir, "code.yaml",
			"notifications:\n  channels:\n    - {name: ops, type: webhook, url_env: X, exclude_checks: [stream-999]}\n")}, `unknown check "stream-999"`},
	} {
		err := runNotifyCmd(context.Background(), c.args, &out)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: err %v, want %q", c.args, err, c.want)
		}
	}
}

// natslens run warns of a secret it cannot read and of a missing ui_url,
// without the secret, and refuses an unknown check code; once does not
// warn.
func TestCheckNotificationsWarns(t *testing.T) {
	dir := t.TempDir()
	set, err := loadChecks(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	bad := config.DefaultChannel()
	bad.Name, bad.Type, bad.URLFile = "ops-webhook", config.ChannelWebhook, filepath.Join(dir, "absent")
	plain := config.DefaultChannel()
	plain.Name, plain.Type = "ops-chat", config.ChannelSlack
	plain.URLFile = writeNotifyFile(t, dir, "chat-url", "http://hooks.example.com/services/"+notifyPathSecret+"?sig="+notifyQuerySecret)
	good := config.DefaultChannel()
	good.Name, good.Type, good.URL = "ok", config.ChannelWebhook, "https://hooks.example.com/in"
	cfg.Notifications.Channels = []config.Channel{bad, plain, good}

	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	if err := checkNotifications(cfg, set, "run", log); err != nil {
		t.Fatal(err)
	}
	text := logs.String()
	for _, want := range []string{`"channel":"ops-webhook"`, "url_file " + bad.URLFile + " cannot be read", `"channel":"ops-chat"`,
		"an http URL is accepted only for a loopback host", "notifications.ui_url is not set"} {
		if !strings.Contains(text, want) {
			t.Errorf("log lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, `"channel":"ok"`) {
		t.Errorf("a readable channel warned:\n%s", text)
	}
	noNotifySecret(t, "log", text)

	logs.Reset()
	if err := checkNotifications(cfg, set, "once", log); err != nil || logs.Len() != 0 {
		t.Errorf("once: %v, logged %s", err, logs.String())
	}
	cfg.Notifications.Channels[2].Checks = []string{"stream-999"}
	if err := checkNotifications(cfg, set, "once", log); err == nil || !strings.Contains(err.Error(), "stream-999") {
		t.Errorf("unknown code: %v", err)
	}
}

// The starting line of natslens run prints the channels without the URL's
// path or query.
func TestStartingLineHidesChannelSecrets(t *testing.T) {
	cfg := config.Default()
	ch := config.DefaultChannel()
	ch.Name, ch.Type = "ops-chat", config.ChannelSlack
	ch.URL = "https://hooks.example.com/services/" + notifyPathSecret + "?sig=" + notifyQuerySecret
	cfg.Notifications.Channels = []config.Channel{ch}
	var logs bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg.Checks.Disabled = []string{"stream-999"} // stops runConfig right after the starting line
	_ = runConfig(ctx, "run", cfg, "", slog.New(slog.NewJSONHandler(&logs, nil)), nil)
	if !strings.Contains(logs.String(), `"msg":"starting"`) || !strings.Contains(logs.String(), "hooks.example.com") {
		t.Fatalf("no starting line with the channel:\n%s", logs.String())
	}
	noNotifySecret(t, "starting line", logs.String())
}
