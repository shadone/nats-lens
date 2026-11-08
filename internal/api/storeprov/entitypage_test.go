package storeprov_test

import (
	"github.com/shadone/nats-lens/internal/api/apitest"
	"testing"
)

func TestStoreEntityListContract(t *testing.T) { apitest.RunEntityListContract(t, client(t)) }
