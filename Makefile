SHELL := /bin/bash
MODULE := github.com/4jawaly/identity-gateway
# Optional build tags, e.g. TAGS=sonic for gin's SIMD JSON encoder (sonic
# supports Go <= 1.26 only; on newer toolchains it falls back to encoding/json).
TAGS ?=
MIGRATE := go run -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.3
MOCKERY := go run github.com/vektra/mockery/v2@v2.53.7
# Migrations read DB_* from .env (sourced in the shell so inline comments are handled) with dev fallbacks.
LOAD_ENV := set -a; [ -f .env ] && . ./.env; set +a
DB_URL   = postgres://$${DB_USER:-identity}:$${DB_PASSWORD:-identity}@$${DB_HOST:-localhost}:$${DB_PORT:-5434}/$${DB_NAME:-identity}?sslmode=$${DB_SSLMODE:-disable}

AIR := $(shell command -v air 2>/dev/null || echo $(shell go env GOPATH)/bin/air)

.PHONY: run-api run-worker dev dev-worker tools build test test-short lint migrate migrate-down swagger config-check mocks up down tidy

run-api: ## run the HTTP gateway
	go run -tags '$(TAGS)' ./cmd/api

run-worker: ## run the RabbitMQ consumer
	go run -tags '$(TAGS)' ./cmd/worker

dev: ## live-reload the API with air (installs air if missing)
	@test -x $(AIR) || go install github.com/air-verse/air@latest
	$(AIR) -c .air.toml

dev-worker: ## live-reload the worker with air
	@test -x $(AIR) || go install github.com/air-verse/air@latest
	$(AIR) -c .air.worker.toml

tools: ## install dev tools
	go install github.com/air-verse/air@latest
	go install github.com/swaggo/swag/cmd/swag@v1.16.4

build: ## build both binaries into ./bin
	CGO_ENABLED=0 go build -tags '$(TAGS)' -trimpath -ldflags="-s -w" -o bin/api ./cmd/api
	CGO_ENABLED=0 go build -tags '$(TAGS)' -trimpath -ldflags="-s -w" -o bin/worker ./cmd/worker

bench: ## hot-path micro benchmarks
	CGO_ENABLED=0 go test -run '^$$' -bench . -benchmem ./internal/modules/user/services/ ./pkg/cache/

test: ## full suite incl. testcontainers (needs Docker)
	go test -race -count=1 ./...

test-short: ## unit tests only
	go test -race -short -count=1 ./...

lint:
	go vet ./...
	golangci-lint run ./...

migrate: ## apply migrations (DB_* from .env)
	@$(LOAD_ENV); $(MIGRATE) -path migrations -database "$(DB_URL)" up

migrate-down: ## roll back one migration
	@$(LOAD_ENV); $(MIGRATE) -path migrations -database "$(DB_URL)" down 1

swagger: ## regenerate ./docs
	swag init -g cmd/api/main.go -o docs --parseDependency --parseInternal --outputTypes go,json,yaml
	swag fmt

mocks: ## regenerate mockery mocks (committed; CI checks the diff)
	$(MOCKERY)

config-check: ## print the redacted config as the binary sees it
	go run ./cmd/api -check-config

up:
	docker compose up -d --wait

down:
	docker compose down

tidy:
	go mod tidy
