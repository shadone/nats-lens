# Check reference pages

Base: 345f405c178307d7187c23bd5d85edd31760ef2d

Design proposed, not yet confirmed: expose the embedded definition catalog through Connect, supplement it with the existing per-check feature reference, and keep selected-system configuration separate. Default reference never includes private remediation extensions. No NATS writes or live deployment access.

- [x] Inspect sources and routes
- [x] Runtime RED for catalog and metadata correction
- [x] Definition API, all 88 references, configuration isolation
- [x] Catalog/detail routes and contextual links
- [ ] Focused RED/GREEN, browser and full verification
- [ ] Feature docs audit and focused commits

Parent owns fresh read-only review. No reviewer spawned here.

Runtime RED evidence:
- `GOCACHE=/tmp/nats-lens-checkref-go-cache go test ./internal/api -run '^TestCheckCatalogHTTP$' -count=1`: ran, failed catalog HTTP 404 (before API).
- `cd web && npm test -- src/pages/Checks.test.tsx`: ran, failed missing Checks heading (before route).
- `GOCACHE=/tmp/nats-lens-checkref-go-cache go test ./internal/checks -run '^TestCreationReferenceDoesNotInferCauseOrPromiseRepair$' -count=1`: ran, failed inferred cause/repair claim.

Focused GREEN: API/catalog and wording commands pass; TypeScript passes; 65 tests across Checks, FindingDetail, EventRow, placementView and globalQuery pass.

Frontend dependencies were copied from the existing main checkout (no package install). Temporary Go cache avoids sandbox restrictions. HTTP tests use an in-process recorder and require no local listener or database.
