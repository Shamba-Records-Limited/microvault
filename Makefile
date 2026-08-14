.PHONY: help migrate-up migrate-down migrate-version migrate-force build build-core build-migrate run up up-build down test test-coverage test-coverage-html test-integration docs clean

COMPOSE := docker compose

help:
	@echo "Microvault Makefile Commands"
	@echo ""
	@echo "Migration Commands:"
	@echo "  make migrate-up          - Run all pending database migrations"
	@echo "  make migrate-down        - Rollback the last migration"
	@echo "  make migrate-version     - Show current migration version"
	@echo "  make migrate-force V=N   - Force migration to version N"
	@echo ""
	@echo "Build Commands:"
	@echo "  make build               - Build all applications (core + migrate)"
	@echo "  make build-core          - Build core application"
	@echo "  make build-migrate       - Build migration CLI"
	@echo ""
	@echo "Run Commands:"
	@echo "  make run                 - Run core application"
	@echo ""
	@echo "Docker Commands:"
	@echo "  make up                  - Start the dev stack in the background"
	@echo "  make up-build            - Rebuild images, then start the dev stack"
	@echo "  make down                - Stop the dev stack"
	@echo ""
	@echo "Test Commands:"
	@echo "  make test                - Run all tests"
	@echo ""
	@echo "Documentation Commands:"
	@echo "  make docs                - Generate API documentation"
	@echo ""
	@echo "Other Commands:"
	@echo "  make clean               - Remove built binaries"
	@echo ""

# Migration commands
migrate-up:
	@go run cmd/migrate/main.go up

migrate-down:
	@go run cmd/migrate/main.go down

migrate-version:
	@go run cmd/migrate/main.go version

migrate-force:
ifndef V
	@echo "Error: Version number required. Usage: make migrate-force V=3"
	@exit 1
endif
	@go run cmd/migrate/main.go force $(V)

# Build commands
build: build-core build-migrate
	@echo "All applications built successfully"

build-core:
	@echo "Building core application..."
	@go build -o bin/microvault cmd/microvault/main.go
	@echo "Build complete: bin/microvault"

build-migrate:
	@echo "Building migration CLI..."
	@go build -o bin/migrate cmd/migrate/main.go
	@echo "Build complete: bin/migrate"

# Run commands
run:
	@go run cmd/microvault/main.go

# Docker commands
up:
	@$(COMPOSE) up -d

up-build:
	@$(COMPOSE) up -d --build

down:
	@$(COMPOSE) down

# Test commands
test:
	@echo "Running all tests..."
	@go test -v ./...

# Minimum acceptable total statement coverage. Ratchet upward as coverage
# improves; CI fails if the total drops below this floor.
COVERAGE_MIN ?= 29

test-coverage:
	@go test ./... -coverprofile=coverage.out -covermode=atomic
	@echo ""
	@echo "Per-package coverage:"
	@go tool cover -func=coverage.out | awk '$$3 != "" {print}' | grep -vE "\.go:" | tail -1 >/dev/null; \
		go test ./... -cover 2>/dev/null | grep -E "coverage: [0-9]" | sed -E 's#github.com/Shamba-Records-Limited/microvault/##' | sort -t: -k2 -rn || true
	@total=$$(go tool cover -func=coverage.out | tail -1 | awk '{print $$NF}' | tr -d '%'); \
		echo ""; \
		echo "Total coverage: $$total% (floor: $(COVERAGE_MIN)%)"; \
		awk -v t="$$total" -v m="$(COVERAGE_MIN)" 'BEGIN{ if (t+0 < m+0) { print "FAIL: coverage below floor"; exit 1 } }'

test-coverage-html: test-coverage
	@go tool cover -html=coverage.out -o coverage.html
	@echo "HTML report: coverage.html"

# Integration tests (build tag `integration`) run against a real Postgres. They
# self-skip when no DB is reachable. Point them at a migrated database via the
# DB_* env vars; the defaults match microvault-credit/docker-compose.test.yml's
# postgres-test (localhost:5435, microvault_test). Bring that DB up first, e.g.:
#   docker compose -f ../microvault-credit/docker-compose.test.yml up -d postgres-test
test-integration:
	@echo "Running integration tests (build tag: integration)..."
	@go test -tags=integration ./...

# Documentation commands
docs:
	@echo "Generating core API documentation..."
	@sh scripts/generate-core-docs.sh
	@echo "Core docs generated: cmd/microvault/docs/"

# Clean command
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf bin/
	@rm -rf cmd/microvault/docs/
	@echo "Clean complete"
