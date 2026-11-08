package api_test

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

type oversizedListAccount struct{ api.Provider }

func (*oversizedListAccount) AccountNames(context.Context) (map[string]string, error) {
	return map[string]string{"APP": strings.Repeat("x", 600000)}, nil
}
func TestEntityPageBudgetIncludesAccountDecoration(t *testing.T) {
	_, c := apitest.Serve(t, &oversizedListAccount{Provider: fake.New(false)})
	if _, err := c.ListStreams(context.Background(), connect.NewRequest(&v1.ListStreamsRequest{Limit: 1})); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatal(err)
	}
	if _, err := c.ListConsumers(context.Background(), connect.NewRequest(&v1.ListConsumersRequest{Limit: 1})); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatal(err)
	}
}
