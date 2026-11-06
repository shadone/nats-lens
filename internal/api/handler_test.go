package api_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/iap"
	"github.com/shadone/nats-lens/internal/iap/iaptest"
)

const aud = "/projects/1/global/backendServices/2"

func codeOf(err error) connect.Code {
	var ce *connect.Error
	if errors.As(err, &ce) {
		return ce.Code()
	}
	return connect.CodeUnknown
}

func muteReq(token string) *connect.Request[v1.MuteFindingRequest] {
	req := connect.NewRequest(&v1.MuteFindingRequest{CheckCode: "stream-025", Reason: "r", MutedBy: "body-name",
		ExpiresAt: timestamppb.New(fake.FixedNow.Add(time.Hour))})
	req.Header().Set("X-Goog-Authenticated-User-Email", "accounts.google.com:spoofed@example.com")
	if token != "" {
		req.Header().Set(iap.Header, token)
	}
	return req
}

func TestMutesWithIAP(t *testing.T) {
	signer := iaptest.New("k1")
	claims := func(email string) map[string]any {
		return map[string]any{"iss": iap.Issuer, "aud": aud, "email": email,
			"iat": fake.FixedNow.Add(-time.Minute).Unix(), "exp": fake.FixedNow.Add(9 * time.Minute).Unix()}
	}
	v := &iap.Verifier{Audience: aud, Keys: signer, Now: func() time.Time { return fake.FixedNow }}
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: api.One(api.DefaultSystemID, api.DefaultSystemID, fake.New(false)), IAP: v})
	ctx := context.Background()

	res, err := c.MuteFinding(ctx, muteReq(signer.Sign("ES256", claims("op@example.com"))))
	if err != nil || res.Msg.GetMute().GetMutedBy() != "op@example.com" {
		t.Fatalf("verified mute: %v, %v", res, err)
	}
	for name, tok := range map[string]string{
		"no assertion":    "",
		"other audience":  signer.Sign("ES256", map[string]any{"iss": iap.Issuer, "aud": "x", "email": "a@b", "iat": fake.FixedNow.Unix(), "exp": fake.FixedNow.Add(time.Minute).Unix()}),
		"other key":       iaptest.New("k1").Sign("ES256", claims("op@example.com")),
		"garbage":         "a.b.c",
		"alg none header": signer.Sign("none", claims("op@example.com")),
	} {
		if _, err := c.MuteFinding(ctx, muteReq(tok)); codeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("mute, %s: %v", name, err)
		}
	}
	un := connect.NewRequest(&v1.UnmuteFindingRequest{MuteId: res.Msg.GetMute().GetId(), UnmutedBy: "body-name"})
	if _, err := c.UnmuteFinding(ctx, un); codeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("unmute without assertion: %v", err)
	}
	un.Header().Set(iap.Header, signer.Sign("ES256", claims("lead@example.com")))
	if r, err := c.UnmuteFinding(ctx, un); err != nil || r.Msg.GetMute().GetUnmutedBy() != "lead@example.com" {
		t.Fatalf("verified unmute: %v, %v", r, err)
	}
	// Reads need no assertion.
	if _, err := c.ListMutes(ctx, connect.NewRequest(&v1.ListMutesRequest{})); err != nil {
		t.Fatalf("read without assertion: %v", err)
	}
}

func TestMutesWithoutIAPIgnoreTheHeaders(t *testing.T) {
	_, c := apitest.Serve(t, fake.New(false))
	res, err := c.MuteFinding(context.Background(), muteReq("a.b.c"))
	if err != nil || res.Msg.GetMute().GetMutedBy() != "body-name" {
		t.Fatalf("mute = %v, %v", res, err)
	}
}

// panicky panics in ListServers.
type panicky struct{ *fake.Provider }

func (panicky) ListServers(context.Context, *v1.ListServersRequest) (*v1.ListServersResponse, error) {
	panic("boom")
}

func TestPanicBecomesInternal(t *testing.T) {
	var logs syncBuffer
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: api.One(api.DefaultSystemID, api.DefaultSystemID, panicky{fake.New(false)}), Log: slog.New(slog.NewTextHandler(&logs, nil))})
	_, err := c.ListServers(context.Background(), connect.NewRequest(&v1.ListServersRequest{}))
	if codeOf(err) != connect.CodeInternal {
		t.Fatalf("panic: %v", err)
	}
	if out := logs.String(); !strings.Contains(out, "api call panicked") || !strings.Contains(out, "panicky.ListServers") {
		t.Fatalf("the log has no stack of the panic: %s", out)
	}
	// The server still answers.
	if _, err := c.GetOverview(context.Background(), connect.NewRequest(&v1.GetOverviewRequest{})); err != nil {
		t.Fatal(err)
	}
}

func TestListMetrics(t *testing.T) {
	_, c := apitest.Serve(t, fake.New(false))
	res, err := c.ListMetrics(context.Background(), connect.NewRequest(&v1.ListMetricsRequest{}))
	if err != nil || len(res.Msg.GetMetrics()) != len(api.Metrics) {
		t.Fatalf("metrics: %v, %v", res, err)
	}
	for i, m := range res.Msg.GetMetrics() {
		if m.GetDescription() == "" || m.GetUnit() == v1.Unit_UNIT_UNSPECIFIED || (i > 0 && m.GetId() <= res.Msg.GetMetrics()[i-1].GetId()) {
			t.Errorf("metric %d: %v", i, m)
		}
	}
}

// syncBuffer is a bytes.Buffer safe for the server's goroutines.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestLensInfo(t *testing.T) {
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: api.One(api.DefaultSystemID, api.DefaultSystemID, fake.New(false)), Version: "v1.2.3"})
	info, err := c.GetLensInfo(context.Background(), connect.NewRequest(&v1.GetLensInfoRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	m := info.Msg
	if m.GetIdentityMode() != v1.IdentityMode_IDENTITY_MODE_REQUEST || m.GetVersion() != "v1.2.3" || m.GetCluster() != fake.Cluster ||
		m.GetCollectInterval().AsDuration() != 30*time.Second || m.GetRetention().AsDuration() != 30*24*time.Hour || !m.GetDemo() {
		t.Fatalf("info without IAP = %v", m)
	}
	// The UI's live ranges end at this now, so -fixed reports its own time.
	if !m.GetNow().AsTime().Equal(fake.FixedNow) {
		t.Fatalf("now = %v, want the fixed demo's %v", m.GetNow(), fake.FixedNow)
	}
	if !m.GetNowFixed() {
		t.Fatal("now_fixed = false under -fixed, want true: the UI's clock would move past the fixed now")
	}
	_, c = apitest.ServeHandler(t, &api.Handler{Systems: api.One(api.DefaultSystemID, api.DefaultSystemID, fake.New(true))})
	info, err = c.GetLensInfo(context.Background(), connect.NewRequest(&v1.GetLensInfoRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if info.Msg.GetNowFixed() || time.Since(info.Msg.GetNow().AsTime()) > time.Minute {
		t.Fatalf("live demo: now_fixed = %v, now = %v; want false and the wall clock", info.Msg.GetNowFixed(), info.Msg.GetNow())
	}
	v := &iap.Verifier{Audience: aud, Keys: iaptest.New("k1")}
	_, c = apitest.ServeHandler(t, &api.Handler{Systems: api.One(api.DefaultSystemID, api.DefaultSystemID, fake.New(false)), IAP: v})
	// Reading the info needs no assertion, so the UI can ask before a mute.
	info, err = c.GetLensInfo(context.Background(), connect.NewRequest(&v1.GetLensInfoRequest{}))
	if err != nil || info.Msg.GetIdentityMode() != v1.IdentityMode_IDENTITY_MODE_IAP {
		t.Fatalf("info with IAP = %v, %v", info, err)
	}
}

func TestOnCallReadWithIAP(t *testing.T) {
	v := &iap.Verifier{Audience: aud, Keys: iaptest.New("k1")}
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: fake.Demo(false), IAP: v})
	req := connect.NewRequest(&v1.GetOnCallRequest{FindingId: "consumer-010:APP/order-archive/analytics-aggregator"})
	req.Header().Set(iap.Header, "invalid assertion")
	got, err := c.GetOnCall(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Msg.Finding.Acknowledgement.GetAckedBy() != "alice@example.com" {
		t.Fatal("read changed recorded identity")
	}
}
