SHELL := /bin/bash
COMPOSE := docker compose -f deploy/docker-compose.yml
GOBIN := $(shell go env GOPATH)/bin
export PATH := $(GOBIN):$(PATH)

# Load .env for local targets when present.
ifneq (,$(wildcard .env))
include .env
export
endif

.PHONY: help env up down worker migrate run test test-integration lint sqlc sqlc-check tools build docker

help: ## Show targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

env: ## Create .env from .env.example with a fresh encryption key
	@test ! -f .env || { echo ".env already exists"; exit 1; }
	@sed "s|REPLACE_WITH_OUTPUT_OF_go_run_./cmd/admin_gen-key|$$(go run ./cmd/admin gen-key)|" .env.example > .env
	@echo "wrote .env"

up: ## Start Postgres, Redis, S3 (SeaweedFS), Mailpit, Jaeger
	$(COMPOSE) up -d --wait postgres redis s3 mailpit jaeger

down: ## Stop the dev stack
	$(COMPOSE) down

migrate: ## Apply migrations to the dev database
	go run ./cmd/migrate up

run: migrate ## Run the API locally
	go run ./cmd/api

worker: ## Run the background worker with the scheduler
	go run ./cmd/worker -scheduler

build: ## Build binaries into ./bin
	CGO_ENABLED=0 go build -trimpath -o bin/ ./cmd/...

test: ## Unit tests (integration tests skip without a database)
	go test -race ./...

test-integration: ## All tests against the dev Postgres/Redis (requires `make up`)
	TEST_REQUIRE_DB=1 go test -race -count=1 ./...

lint: ## go vet + golangci-lint
	go vet ./...
	golangci-lint run ./...

sqlc: ## Regenerate database code
	sqlc generate

sqlc-check: ## Fail if generated database code is stale
	sqlc diff

tools: ## Install developer tools
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.27.0
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

docker: ## Build the API image
	docker build -f deploy/Dockerfile -t shopee-automation/api:dev .

.PHONY: backup restore-drill prod-config-check

backup: ## Encrypted backup of the dev DB into tmp/backups (needs BACKUP_PASSPHRASE_FILE or BACKUP_GPG_RECIPIENT)
	PGDATABASE=shopee_automation BACKUP_DIR=$${BACKUP_DIR:-tmp/backups} \
	PG_DUMP="$(COMPOSE) exec -T -e PGUSER=app_owner postgres pg_dump" \
	deploy/backup/backup.sh

restore-drill: ## Back up the dev DB, restore into a scratch DB, compare row counts, drop it
	deploy/backup/restore-drill.sh

prod-config-check: ## Validate deploy/production (compose config + nginx -t) with placeholder values
	deploy/production/check.sh
