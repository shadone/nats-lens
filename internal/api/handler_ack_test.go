package api_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/iap"
	"github.com/shadone/nats-lens/internal/iap/iaptest"
)

// The demo's firing finding of example-prod without an acknowledgement.
const ackFindingID = "consumer-009:" + fake.Account + "/order-jobs/resize-worker"

func ackReq(token string) *connect.Request[v1.AcknowledgeFindingRequest] {
	req := connect.NewRequest(&v1.AcknowledgeFindingRequest{FindingId: ackFindingID, Note: "on it",
		QuietFor: durationpb.New(30 * time.Minute), AckedBy: "body-name"})
	req.Header().Set("X-Goog-Authenticated-User-Email", "accounts.google.com:spoofed@example.com")
	if token != "" {
		req.Header().Set(iap.Header, token)
	}
	return req
}

// With IAP, acknowledging and handing back need a verified assertion,
// whose email is recorded; the name in the body and the unverified header
// are not.
func TestAcknowledgementsWithIAP(t *testing.T) {
	signer := iaptest.New("k1")
	claims := func(email string) map[string]any {
		return map[string]any{"iss": iap.Issuer, "aud": aud, "email": email,
			"iat": fake.FixedNow.Add(-time.Minute).Unix(), "exp": fake.FixedNow.Add(9 * time.Minute).Unix()}
	}
	v := &iap.Verifier{Audience: aud, Keys: signer, Now: func() time.Time { return fake.FixedNow }}
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: api.One(api.DefaultSystemID, api.DefaultSystemID, fake.New(false)), IAP: v})
	ctx := context.Background()
	if _, err := c.AcknowledgeFinding(ctx, ackReq("")); codeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("acknowledge without assertion: %v", err)
	}
	res, err := c.AcknowledgeFinding(ctx, ackReq(signer.Sign("ES256", claims("alice@example.com"))))
	if err != nil || res.Msg.GetAcknowledgement().GetAckedBy() != "alice@example.com" {
		t.Fatalf("verified acknowledgement: %v, %v", res, err)
	}
	end := connect.NewRequest(&v1.EndAcknowledgementRequest{Id: res.Msg.GetAcknowledgement().GetId(), EndedBy: "body-name"})
	if _, err := c.EndAcknowledgement(ctx, end); codeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("end without assertion: %v", err)
	}
	end.Header().Set(iap.Header, signer.Sign("ES256", claims("lead@example.com")))
	r, err := c.EndAcknowledgement(ctx, end)
	if err != nil || r.Msg.GetAcknowledgement().GetEndedBy() != "lead@example.com" || r.Msg.GetAcknowledgement().GetEndReason() != "released" {
		t.Fatalf("verified end: %v, %v", r, err)
	}
}

// Without IAP the request names the user, and the errors map to codes.
func TestAcknowledgementsWithoutIAP(t *testing.T) {
	_, c := apitest.Serve(t, fake.New(false))
	ctx := context.Background()
	res, err := c.AcknowledgeFinding(ctx, ackReq("a.b.c"))
	if err != nil || res.Msg.GetAcknowledgement().GetAckedBy() != "body-name" || !res.Msg.GetAcknowledgement().GetQuiet() {
		t.Fatalf("acknowledge = %v, %v", res, err)
	}
	unknown := ackReq("")
	unknown.Msg.FindingId = "consumer-009:nope"
	if _, err := c.AcknowledgeFinding(ctx, unknown); codeOf(err) != connect.CodeNotFound {
		t.Errorf("unknown finding: %v, want not_found", err)
	}
	bad := ackReq("")
	bad.Msg.QuietFor = durationpb.New(time.Minute)
	if _, err := c.AcknowledgeFinding(ctx, bad); codeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("quiet of a minute: %v, want invalid_argument", err)
	}
	end := func(id string) error {
		_, err := c.EndAcknowledgement(ctx, connect.NewRequest(&v1.EndAcknowledgementRequest{Id: id, EndedBy: "x"}))
		return err
	}
	if err := end(res.Msg.GetAcknowledgement().GetId()); err != nil {
		t.Fatal(err)
	}
	if err := end(res.Msg.GetAcknowledgement().GetId()); codeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("ending twice: %v, want failed_precondition", err)
	}
	if err := end("999"); codeOf(err) != connect.CodeNotFound {
		t.Errorf("unknown id: %v, want not_found", err)
	}
}
