.PHONY: help migrate-up migrate-down migrate-version migrate-force build build-core build-migrate run test docs clean

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
	@go build -o bin/microvault cmd/Microvault/main.go
	@echo "Build complete: bin/microvault"

build-migrate:
	@echo "Building migration CLI..."
	@go build -o bin/migrate cmd/migrate/main.go
	@echo "Build complete: bin/migrate"

# Run commands
run:
	@go run cmd/Microvault/main.go

# Test commands
test:
	@echo "Running all tests..."
	@go test -v ./...

# Documentation commands
docs:
	@echo "Generating core API documentation..."
	@sh scripts/generate-core-docs.sh
	@echo "Core docs generated: cmd/Microvault/docs/"

# Clean command
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf bin/
	@rm -rf cmd/Microvault/docs/
	@echo "Clean complete"
