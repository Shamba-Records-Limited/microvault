#!/bin/sh
set -e

# Generate Swagger docs: cmd/microvault for main and pkg for controllers. swag
# searches each --dir recursively, so the dirs must not overlap or every route
# under both is declared twice; the general info file is named relative to the
# first dir.
swag init --parseDependency --parseInternal --generalInfo main.go --dir ./cmd/microvault,./pkg --output ./cmd/microvault/docs

# Build the themed Redoc static HTML from swagger.json. Not
# `@redocly/cli build-docs --theme` — see scripts/render-redoc.js for why.
if [ -f ./cmd/microvault/docs/swagger.json ]; then
    node ./scripts/render-redoc.js ./cmd/microvault/docs/swagger.json ./cmd/microvault/docs/redoc-static.html "microvault API" || echo "Redoc render skipped"
fi
