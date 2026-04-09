# Agentic Coop DB Makefile — single dev entrypoint
#
# Most users only need:
#   make up-local        # bring the stack up locally
#   make test            # all tests (unit + integration + python e2e)
#   make down            # tear it down
#
# Profiles map to the compose files under deploy/.
#
# Override variables on the command line, e.g.:
#   make up-cloud DOMAIN=db.example.com EMAIL=ops@example.com

SHELL          := /usr/bin/env bash
.ONESHELL:
.SHELLFLAGS    := -eu -o pipefail -c
.DEFAULT_GOAL  := help

# ---- variables ---------------------------------------------------------------

MODULE         := github.com/fheinfling/agentic-coop-db
VERSION        ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0-dev")
COMMIT         ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE     := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS        := -s -w \
                  -X $(MODULE)/internal/version.Version=$(VERSION) \
                  -X $(MODULE)/internal/version.Commit=$(COMMIT) \
                  -X $(MODULE)/internal/version.BuildDate=$(BUILD_DATE)

GO             ?= go
GO_BUILD_FLAGS := -trimpath -ldflags "$(LDFLAGS)"

PROJECT        := agentcoopdb
COMPOSE        := docker compose -p $(PROJECT)
COMPOSE_BASE   := -f deploy/compose.yml
COMPOSE_LOCAL  := $(COMPOSE_BASE) -f deploy/compose.local.yml
COMPOSE_PI     := $(COMPOSE_BASE) -f deploy/compose.pi-lite.yml
COMPOSE_CLOUD  := $(COMPOSE_BASE) -f deploy/compose.cloud.yml

# ---- help --------------------------------------------------------------------

help: ## show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_.-]+:.*?## / {printf "  \033[1m%-22s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# ---- build -------------------------------------------------------------------

build: build-server build-migrate build-lint-migrations ## build all Go binaries

build-server: ## build the API server binary
	$(GO) build $(GO_BUILD_FLAGS) -o bin/agentic-coop-db-server ./cmd/server

build-migrate: ## build the standalone migrator
	$(GO) build $(GO_BUILD_FLAGS) -o bin/agentic-coop-db-migrate ./cmd/migrate

build-lint-migrations: ## build the migration linter
	$(GO) build -o bin/lint-migrations ./scripts/lint-migrations

build-python: ## build the Python sdist + wheel
	cd clients/python && python -m build

# ---- lint --------------------------------------------------------------------

lint: lint-go lint-python lint-migrations ## run all linters

lint-go: ## golangci-lint
	golangci-lint run ./...

lint-python: ## ruff + mypy
	cd clients/python && ruff check . && mypy src/agentcoopdb

lint-migrations: build-lint-migrations ## fail if tenant tables are missing RLS policies
	./bin/lint-migrations migrations

# ---- test --------------------------------------------------------------------

test: test-unit test-integration test-e2e ## all tests

test-unit: ## fast unit tests (no docker)
	$(GO) test -short ./...

test-integration: ## testcontainers-backed integration suite
	$(GO) test -tags=integration ./test/integration/... ./test/security/...

test-e2e: ## python end-to-end (offline queue) — requires up-local
	cd clients/python && RUN_E2E=1 pytest -q

# ---- compose targets ---------------------------------------------------------

up-local: ## start the local dev stack on http://localhost:8080
	./scripts/dev-up.sh
	$(COMPOSE) $(COMPOSE_LOCAL) up -d --build
	@echo "API:    http://localhost:8080"
	@echo "Health: http://localhost:8080/healthz"
	@echo "Mint a key with: ./scripts/gen-key.sh default dbadmin"

up-pi: ## start the pi-lite (ARM64, low-mem) stack
	./scripts/dev-up.sh
	$(COMPOSE) $(COMPOSE_PI) up -d --build

up-cloud: ## start the cloud (caddy auto-tls + backups) stack
	./scripts/dev-up.sh
	$(COMPOSE) $(COMPOSE_CLOUD) up -d --build

swarm-deploy: ## deploy stack.swarm.yml to a docker swarm
	docker stack deploy -c deploy/stack.swarm.yml $(PROJECT)

down: ## tear down the dev stack and remove its volumes
	-$(COMPOSE) $(COMPOSE_LOCAL) down -v
	-$(COMPOSE) $(COMPOSE_PI)    down -v
	-$(COMPOSE) $(COMPOSE_CLOUD) down -v

logs: ## tail logs from the dev stack
	$(COMPOSE) $(COMPOSE_LOCAL) logs -f --tail=200

# ---- developer convenience ---------------------------------------------------

fmt: ## gofmt + goimports + ruff format
	gofmt -w -s .
	cd clients/python && ruff format .

tidy: ## go mod tidy
	$(GO) mod tidy

mod-verify: ## verify module checksums
	$(GO) mod verify

verify-acs: ## run scripts/verify-acs.sh end to end
	./scripts/verify-acs.sh

clean: ## remove build artifacts
	rm -rf bin/ dist/ build/ coverage.*
	cd clients/python && rm -rf dist/ build/ *.egg-info/

.PHONY: help build build-server build-migrate build-lint-migrations build-python \
        lint lint-go lint-python lint-migrations \
        test test-unit test-integration test-e2e \
        up-local up-pi up-cloud swarm-deploy down logs \
        fmt tidy mod-verify verify-acs clean
