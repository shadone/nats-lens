package api

import (
	"net/http"

	"github.com/shadone/nats-lens/internal/investigation"
)

// investigationHandler is a method boundary for a separately restricted proxy
// identity. It does not authenticate requests: the deployment's existing access
// proxy must authenticate reads and deny this identity the ordinary /api path.
// ASVS 8.3.1: unknown and mutating procedures fail before RPC dispatch, for every
// HTTP verb and Connect/gRPC encoding. The original service keeps its own rules.
func investigationHandler(svc http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !investigation.AllowedProcedure(r.URL.Path) {
			http.Error(w, "method unavailable on read-only investigation surface", http.StatusForbidden)
			return
		}
		svc.ServeHTTP(w, r)
	})
}
