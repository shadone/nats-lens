package api_test

import (
	"connectrpc.com/connect"
	"context"
	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/investigation"
	"google.golang.org/protobuf/types/known/timestamppb"
	"slices"
	"testing"
	"time"
)

type storageProviderStub struct {
	*fake.Provider
	called int
	left   time.Duration
}

func (p *storageProviderStub) StorageChange(ctx context.Context, req *v1.GetStorageChangeRequest) (*api.StorageChangeFacts, error) {
	p.called++
	d, ok := ctx.Deadline()
	if ok {
		p.left = time.Until(d)
	}
	_, f := storageFacts()
	return f, nil
}
func TestStorageChangeHandler(t *testing.T) {
	p := &storageProviderStub{Provider: fake.New(false)}
	_, c := apitest.Serve(t, p)
	req, _ := storageFacts()
	req.System = ""
	res, err := c.GetStorageChange(context.Background(), connect.NewRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	if p.called != 1 || p.left <= 0 || p.left > api.QueryTimeout || res.Msg.TotalCount != 4 {
		t.Fatalf("shared timeout/finishing missing: calls=%d left=%v res=%v", p.called, p.left, res.Msg)
	}
	for name, change := range map[string]func(*v1.GetStorageChangeRequest){"future": func(r *v1.GetStorageChangeRequest) { r.B = timestamppb.New(fake.FixedNow.Add(time.Second)) }, "reversed": func(r *v1.GetStorageChangeRequest) { r.B = r.A }, "missing": func(r *v1.GetStorageChangeRequest) { r.A = nil }, "invalid_timestamp": func(r *v1.GetStorageChangeRequest) { r.A = &timestamppb.Timestamp{Nanos: -1} }, "page": func(r *v1.GetStorageChangeRequest) { r.PageSize = 101 }, "negative_pin": func(r *v1.GetStorageChangeRequest) { r.EpochA = -1 }, "token": func(r *v1.GetStorageChangeRequest) { r.PageToken = string(make([]byte, 1025)) }, "system": func(r *v1.GetStorageChangeRequest) { r.System = "unknown" }} {
		t.Run(name, func(t *testing.T) {
			r, _ := storageFacts()
			r.System = ""
			change(r)
			if _, err := c.GetStorageChange(context.Background(), connect.NewRequest(r)); connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Fatalf("validation=%v", err)
			}
		})
	}
	if p.called != 1 {
		t.Fatal("invalid request reached provider")
	}
}
func TestStorageChangeReadOnlyAdmission(t *testing.T) {
	if !slices.Contains(investigation.Methods(), "GetStorageChange") || investigation.Method("GetStorageChange") == nil || !investigation.AllowedProcedure("/natslens.v1.LensService/GetStorageChange") {
		t.Fatal("shared read-only source cannot discover storage comparison")
	}
}
