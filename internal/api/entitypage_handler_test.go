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

type scopedListAccount struct {
	api.Provider
	keys []string
	err  error
}

func (p *scopedListAccount) AccountNames(context.Context) (map[string]string, error) {
	panic("list read whole account directory")
}
func (p *scopedListAccount) AccountNamesFor(_ context.Context, keys []string) (map[string]string, error) {
	p.keys = append([]string(nil), keys...)
	return map[string]string{"APP": "Shop"}, p.err
}
func TestEntityPageAccountNamesAreScopedAndFailClosed(t *testing.T) {
	p := &scopedListAccount{Provider: fake.New(false)}
	_, c := apitest.Serve(t, p)
	for _, consumers := range []bool{false, true} {
		if consumers {
			res, err := c.ListConsumers(context.Background(), connect.NewRequest(&v1.ListConsumersRequest{Limit: 1}))
			if err != nil {
				t.Fatal(err)
			}
			if len(p.keys) != 1 || p.keys[0] != res.Msg.Consumers[0].Entity.Account || res.Msg.Consumers[0].Entity.AccountName != "Shop" {
				t.Fatal(p.keys, res.Msg)
			}
		} else {
			res, err := c.ListStreams(context.Background(), connect.NewRequest(&v1.ListStreamsRequest{Limit: 1}))
			if err != nil {
				t.Fatal(err)
			}
			if len(p.keys) != 1 || p.keys[0] != res.Msg.Streams[0].Entity.Account || res.Msg.Streams[0].Entity.AccountName != "Shop" {
				t.Fatal(p.keys, res.Msg)
			}
		}
	}
	p.err = api.ErrResourceExhausted
	if _, err := c.ListStreams(context.Background(), connect.NewRequest(&v1.ListStreamsRequest{Limit: 1})); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatal(err)
	}
	if _, err := c.ListConsumers(context.Background(), connect.NewRequest(&v1.ListConsumersRequest{Limit: 1})); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatal(err)
	}
}
