#!/bin/sh
set -e

# Generate Swagger docs - search in cmd/microvault for main and pkg for controllers
swag init --parseDependency --parseInternal --generalInfo ./cmd/microvault/main.go --dir ./,./pkg --output ./cmd/microvault/docs

# Build the themed Redoc static HTML from swagger.json. Not
# `@redocly/cli build-docs --theme` — see scripts/render-redoc.js for why.
if [ -f ./cmd/microvault/docs/swagger.json ]; then
    node ./scripts/render-redoc.js ./cmd/microvault/docs/swagger.json ./cmd/microvault/docs/redoc-static.html "microvault API" || echo "Redoc render skipped"
fi
