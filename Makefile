BIN := bin/natslens
DEV_DATA := .dev/natslens

.PHONY: build build-full build-go build-release web-full web web-test web-dev proto test test-short test-fast verify-fast verify-full verify-release verify-compat verify-web verify-proto verify-race lint fmt demo demo-site demo-wasm demo-wasm-smoke dev-cluster dev-cluster-stop load churn run-dev image vuln clean

# Verifies/rebuilds production assets, then builds the binary that embeds them.
build: web/node_modules/.package-lock.json
	node scripts/build-web.mjs --go "$(BIN)"

# Explicit frontend rebuild, followed by the embedding binary.
build-full: web/node_modules/.package-lock.json
	node scripts/build-web.mjs --force --go "$(BIN)"

# Embeds the assets already present, or a placeholder if index.html is absent.
# It does not verify that existing assets match current frontend sources.
build-go:
	go build -o $(BIN) ./cmd/natslens

# The release build of the host platform: cgo, paths trimmed, symbols
# stripped and VERSION stamped (natslens version prints it). It embeds the
# assets already in internal/webui/dist, like build-go; the release workflow
# runs it for the macOS binaries.
VERSION ?= dev
build-release:
	@test -f internal/webui/dist/index.html || { echo "internal/webui/dist holds no built UI: run make web first"; exit 1; }
	CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $(BIN) ./cmd/natslens

# npm ci runs when package/lock/config inputs are newer than the installed marker.
web/node_modules/.package-lock.json: web/package.json web/package-lock.json web/.npmrc
	cd web && npm ci --no-audit --no-fund

web: web/node_modules/.package-lock.json
	node scripts/build-web.mjs

web-full: web/node_modules/.package-lock.json
	node scripts/build-web.mjs --force

web-test: web/node_modules/.package-lock.json
	cd web && npm test

# Vite dev server on 127.0.0.1:5173; /api goes to `make demo` or `make run-dev` on 127.0.0.1:8080.
web-dev: web/node_modules/.package-lock.json
	cd web && npm run dev

# Regenerates the Go and TypeScript code from proto/. The Go plugins run
# through the tool directives in go.mod, protoc-gen-es from web/node_modules.
proto: web/node_modules/.package-lock.json
	buf lint
	buf format --diff --exit-code
	buf generate

test:
	go test -timeout 40m ./...

test-short:
	go test -short -timeout 40m ./...

# Pure policy/decoding selection; short alone still includes DuckDB and HTTP.
test-fast:
	./scripts/test-go-fast.sh

# Aggregate checks are read-only; no dependency installation or source rewriting.
verify-fast:
	python3 -B scripts/verify.py fast

verify-full:
	python3 -B scripts/verify.py full

# The full profile with the upgrade, archive and recovery compatibility
# matrix; run before tagging a release.
verify-release:
	python3 -B scripts/verify.py release

verify-compat:
	python3 -B scripts/verify.py compat

verify-web:
	python3 -B scripts/verify.py web

verify-proto:
	python3 -B scripts/verify.py proto

verify-race:
	python3 -B scripts/verify.py race

lint:
	go vet ./...
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

fmt:
	gofmt -w .

dev-cluster:
	scripts/dev-cluster.sh start

dev-cluster-stop:
	scripts/dev-cluster.sh stop

load:
	scripts/load.sh

churn:
	scripts/load.sh churn

# Collects from the dev cluster every 10s into .dev/natslens, with events
# from the system account and the APP account. The passwords are the dev
# cluster's local values.
run-dev: build
	NATSLENS_NATS_PASSWORD=sys NATSLENS_ACCOUNT_APP_PASSWORD=app $(BIN) -config scripts/natslens.dev.yaml

# Builds the amd64 container image with buildx; IMAGE names it. The release
# workflow builds linux/amd64 and linux/arm64 from the same Dockerfile.
IMAGE ?= natslens:dev
image:
	docker buildx build --platform linux/amd64 --build-arg VERSION=$$(git rev-parse --short HEAD) -t $(IMAGE) .

# Reports known vulnerabilities in the Go code and its modules. No CI runs
# it; run it before a release.
vuln:
	@command -v govulncheck >/dev/null || { echo "govulncheck is not installed: go install golang.org/x/vuln/cmd/govulncheck@latest"; exit 1; }
	govulncheck ./...

clean:
	rm -rf bin

# Serves the API and the web UI with the fake scenario on 127.0.0.1:8080,
# without NATS.
demo: build
	$(BIN) demo

# The demo site: the web UI with the demo's API compiled to WebAssembly, as
# static files served under DEMO_BASE, the path GitHub Pages serves the
# repository's site at. See docs/features/demo-mode.md.
DEMO_SITE ?= bin/demo-site
DEMO_BASE ?= /nats-lens/
demo-site: web/node_modules/.package-lock.json
	cd web && npx tsc --noEmit
	cd web && NATSLENS_BASE=$(DEMO_BASE) VITE_NATSLENS_WASM=1 NATSLENS_WEB_OUT_DIR="$(abspath $(DEMO_SITE))" npx vite build
	$(MAKE) demo-wasm
	cp $(DEMO_SITE)/index.html $(DEMO_SITE)/404.html
	touch $(DEMO_SITE)/.nojekyll

# The WebAssembly half of the demo site, with the Go toolchain's own loader
# beside it: the two must come from the same Go release.
demo-wasm:
	mkdir -p $(DEMO_SITE)
	GOOS=js GOARCH=wasm go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $(DEMO_SITE)/natslens.wasm ./cmd/natslens-wasm
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" $(DEMO_SITE)/wasm_exec.js

# Runs one call through the built WebAssembly under Node.
demo-wasm-smoke: demo-wasm
	node cmd/natslens-wasm/smoke.mjs $(DEMO_SITE)
