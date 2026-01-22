#!/bin/sh
set -e

# Generate Swagger docs - search in cmd/Microvault for main and pkg for controllers
swag init --parseDependency --parseInternal --generalInfo ./cmd/Microvault/main.go --dir ./,./pkg --output ./cmd/Microvault/docs

# Build Redoc static HTML from swagger.json
if [ -f ./cmd/Microvault/docs/swagger.json ]; then
    npx @redocly/cli build-docs ./cmd/Microvault/docs/swagger.json --output ./cmd/Microvault/docs/redoc-static.html 2>/dev/null || echo "Redocly build skipped"
fi
