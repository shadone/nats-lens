package fake_test

import (
	"github.com/shadone/nats-lens/internal/api/apitest"
	"testing"
)

func TestDemoEntityListContract(t *testing.T) { apitest.RunEntityListContract(t, client(t)) }
