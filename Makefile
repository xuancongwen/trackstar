VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOFLAGS := -trimpath
SQLC    := go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
PLATFORMS ?= linux/amd64 linux/arm64

.PHONY: dev backend frontend build test e2e lint migrate release sqlc docker clean

# Not ./data: docker compose bind-mounts that one and its container writes as root.
DEV_DATA_DIR ?= ./data-dev
DEV_PORT     ?= 5173
DEV_API_PORT ?= 3000

## dev: API on :3000 and Vite (hot reload) on :5173 — open http://localhost:5173
## Either process exiting stops the other, so a broken backend is an error, not a
## silently 502-ing UI. Override DEV_DATA_DIR / DEV_PORT / DEV_API_PORT as needed.
dev: web/node_modules
	@if [ -e "$(DEV_DATA_DIR)" ] && [ ! -w "$(DEV_DATA_DIR)" ]; then \
		echo "error: $(DEV_DATA_DIR) is not writable by $$(id -un) (owned by $$(stat -c %U "$(DEV_DATA_DIR)" 2>/dev/null || stat -f %Su "$(DEV_DATA_DIR)"))."; \
		echo "       Fix with: sudo chown -R $$(id -un): $(DEV_DATA_DIR)   # or: make dev DEV_DATA_DIR=<other dir>"; \
		exit 1; \
	fi
	@trap 'kill 0 2>/dev/null' INT TERM EXIT; \
	( TRACKSTAR_ADDR=127.0.0.1:$(DEV_API_PORT) TRACKSTAR_DATA_DIR=$(DEV_DATA_DIR) TRACKSTAR_PUBLIC_URL=http://localhost:$(DEV_PORT)/ TRACKSTAR_LOG_LEVEL=debug \
	    go run ./cmd/trackstar; echo "backend exited; stopping"; kill 0 ) & \
	( TRACKSTAR_DEV_PORT=$(DEV_PORT) TRACKSTAR_DEV_BACKEND=http://127.0.0.1:$(DEV_API_PORT) npm --prefix web run dev; echo "vite exited; stopping"; kill 0 ) & \
	wait

## backend: compile the Go binary with whatever is in web/dist
backend:
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/trackstar ./cmd/trackstar

## frontend: compile the Svelte app into web/dist
frontend: web/node_modules
	npm --prefix web run build

## build: the production artifact — one binary with the frontend embedded
build: frontend backend

web/node_modules: web/package.json web/package-lock.json
	npm --prefix web ci
	@touch $@

## test: backend and frontend tests
test: web/node_modules
	go test ./...
	npm --prefix web test

## e2e: headless-Chromium tests against the real binary (needs `make build`; CHROME_PATH overrides /usr/bin/chromium)
e2e: web/node_modules
	npm --prefix web run e2e

## lint: static checks
lint: web/node_modules
	go vet ./...
	@test -z "$$(gofmt -l cmd internal db web/*.go)" || { echo "gofmt needed:"; gofmt -l cmd internal db web/*.go; exit 1; }
	npm --prefix web run check
	@if command -v shellcheck >/dev/null; then shellcheck --severity=warning scripts/*.sh; else npx --yes shellcheck --severity=warning scripts/*.sh; fi

## migrate: apply migrations to the local development database
migrate:
	TRACKSTAR_DATA_DIR=$(DEV_DATA_DIR) go run ./cmd/trackstar migrate

## sqlc: regenerate internal/database/dbgen from db/queries
sqlc:
	cd db && $(SQLC) generate

## release: dist/trackstar-<version>-<os>-<arch>.tar.gz (+ SHA256SUMS)
release: frontend
	@rm -rf dist && mkdir -p dist
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; name=trackstar-$(VERSION)-$$os-$$arch; \
		echo "building $$name"; \
		mkdir -p dist/$$name && \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/$$name/trackstar ./cmd/trackstar && \
		cp -r scripts deploy README.md dist/$$name/ && \
		tar -C dist --owner=0 --group=0 -czf dist/$$name.tar.gz $$name && rm -rf dist/$$name || exit 1; \
	done
	@cd dist && sha256sum *.tar.gz > SHA256SUMS && cat SHA256SUMS

## docker: build the container image
docker:
	docker build --build-arg VERSION=$(VERSION) -t trackstar:latest .

clean:
	rm -rf bin dist data-dev
	find web/dist -mindepth 1 ! -name .gitkeep -delete
