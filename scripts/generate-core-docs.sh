#!/bin/sh
set -e

# Generate Swagger docs - search in cmd/microvault for main and pkg for controllers
swag init --parseDependency --parseInternal --generalInfo ./cmd/microvault/main.go --dir ./,./pkg --output ./cmd/microvault/docs

# Build Redoc static HTML from swagger.json
if [ -f ./cmd/microvault/docs/swagger.json ]; then
    npx @redocly/cli build-docs ./cmd/microvault/docs/swagger.json --output ./cmd/microvault/docs/redoc-static.html 2>/dev/null || echo "Redocly build skipped"
fi
