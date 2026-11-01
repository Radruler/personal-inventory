# Personal Inventory. Every target works on the Mac (no Go needed): when `go`
# is not on PATH, the command runs inside the dev container instead.
SHELL := /bin/bash
DC := docker compose -f .devcontainer/docker-compose.yml
IN_DEV := $(if $(shell command -v go 2>/dev/null),,$(DC) run --rm -T dev)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
IMAGE ?= ghcr.io/radruler/personal-inventory
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: help deps gen web web-placeholder build test test-go test-web lint vet run dev seed image up down clean shell

help: ## list targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

deps: ## install Go modules and web packages
	$(IN_DEV) sh -c 'go mod download && cd web && npm ci'

gen: ## regenerate the TypeScript API types from api/openapi.yaml
	$(IN_DEV) sh -c 'cd web && npm run gen'

web: ## build the SPA into internal/webui/dist
	$(IN_DEV) sh -c 'cd web && npm run build'

# go:embed needs at least one file in dist; tests do not need the real SPA.
web-placeholder:
	@mkdir -p internal/webui/dist && [ -f internal/webui/dist/index.html ] || \
	  echo '<!doctype html><title>Personal Inventory</title><p>SPA not built: run make web</p>' > internal/webui/dist/index.html

build: web ## build bin/inv (for the dev container's OS/arch)
	$(IN_DEV) sh -c 'CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/inv ./cmd/inv'

test: web-placeholder ## run all Go and web tests
	$(IN_DEV) sh -c 'go vet ./... && go test ./... && cd web && npm run check && npm test'

test-go: web-placeholder ## run Go tests only
	$(IN_DEV) go test ./...

test-web: ## run web type-check and unit tests only
	$(IN_DEV) sh -c 'cd web && npm run check && npm test'

lint: ## lint the CI workflow
	docker run --rm -v "$(CURDIR):/repo" -w /repo rhysd/actionlint:1.7.7 -color

run: web-placeholder ## run the server in the dev container on 127.0.0.1:8481
	$(DC) run --rm --service-ports dev go run ./cmd/inv serve

dev: ## vite dev server (5173) proxying to `make run`
	$(DC) run --rm --service-ports dev sh -c 'cd web && npm run dev -- --host 0.0.0.0'

seed: web-placeholder ## seed demo data into the dev database (.local/)
	$(IN_DEV) go run ./cmd/inv seed --demo

image: ## build the production image for the local platform
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):dev .

up: ## build and run the production image with compose.yml on :8480
	docker compose up --build -d

down: ## stop the compose.yml app
	docker compose down

shell: ## open a shell in the dev container
	$(DC) run --rm dev bash

clean:
	rm -rf bin internal/webui/dist web/.svelte-kit
