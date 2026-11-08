BIN := bin/natslens
DEV_DATA := .dev/natslens

.PHONY: build build-go web web-test web-dev proto test test-short test-fast lint fmt demo dev-cluster dev-cluster-stop load churn run-dev image vuln clean

# Builds the web UI into internal/webui/dist, then the binary that embeds it.
build: web
	go build -o $(BIN) ./cmd/natslens

# Builds the binary alone; without `make web` it embeds a placeholder page.
build-go:
	go build -o $(BIN) ./cmd/natslens

# npm ci runs only when the lockfile is newer than the installed tree.
web/node_modules/.package-lock.json: web/package-lock.json
	cd web && npm ci --no-audit --no-fund

web: web/node_modules/.package-lock.json
	cd web && npm run build

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

# Builds the container image for GKE (amd64) with buildx; IMAGE names it.
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
