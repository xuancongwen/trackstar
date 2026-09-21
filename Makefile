VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOFLAGS := -trimpath
SQLC    := go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0
PLATFORMS ?= linux/amd64 linux/arm64

.PHONY: dev backend frontend build test lint migrate release sqlc docker clean

## dev: API on :3000 and Vite (hot reload) on :5173 — open http://localhost:5173
dev: web/node_modules
	@trap 'kill 0' INT TERM EXIT; \
	TRACKER_DATA_DIR=./data TRACKER_PUBLIC_URL=http://localhost:5173/ TRACKER_LOG_LEVEL=debug go run ./cmd/tracker & \
	npm --prefix web run dev & \
	wait

## backend: compile the Go binary with whatever is in web/dist
backend:
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/tracker ./cmd/tracker

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

## lint: static checks
lint: web/node_modules
	go vet ./...
	@test -z "$$(gofmt -l cmd internal db web/*.go)" || { echo "gofmt needed:"; gofmt -l cmd internal db web/*.go; exit 1; }
	npm --prefix web run check
	@if command -v shellcheck >/dev/null; then shellcheck scripts/*.sh; else echo "shellcheck not installed; skipping"; fi

## migrate: apply migrations to the local development database
migrate:
	TRACKER_DATA_DIR=./data go run ./cmd/tracker migrate

## sqlc: regenerate internal/database/dbgen from db/queries
sqlc:
	cd db && $(SQLC) generate

## release: dist/tracker-<version>-<os>-<arch>.tar.gz (+ SHA256SUMS)
release: frontend
	@rm -rf dist && mkdir -p dist
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; name=tracker-$(VERSION)-$$os-$$arch; \
		echo "building $$name"; \
		mkdir -p dist/$$name && \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/$$name/tracker ./cmd/tracker && \
		cp -r scripts deploy README.md dist/$$name/ && \
		tar -C dist -czf dist/$$name.tar.gz $$name && rm -rf dist/$$name || exit 1; \
	done
	@cd dist && sha256sum *.tar.gz > SHA256SUMS && cat SHA256SUMS

## docker: build the container image
docker:
	docker build --build-arg VERSION=$(VERSION) -t tracker:latest .

clean:
	rm -rf bin dist data
	find web/dist -mindepth 1 ! -name .gitkeep -delete
