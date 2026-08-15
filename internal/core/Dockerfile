# --- Builder stage ---
FROM golang:1.26-alpine AS builder

# Install build dependencies
RUN apk add --no-cache npm git ca-certificates tzdata && \
    npm i -g @redocly/cli@latest && \
    go install github.com/swaggo/swag/cmd/swag@latest

# Set the working directory
WORKDIR /build

# Copy dependency files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code (SHARED)
COPY ./pkg ./pkg
COPY ./platform ./platform
COPY ./static ./static

# Copy source code (CORE)
COPY ./cmd/microvault ./cmd/microvault
COPY ./internal/core ./internal/core

# Generate documentation
WORKDIR /build/internal/core
RUN swag init && \
    redocly build-docs ./docs/swagger.yaml --output ./docs/redoc-static.html 2>/dev/null || true

# Build the application
WORKDIR /build
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN go build -ldflags="-s -w" -o core ./cmd/microvault/main.go

# --- Release stage ---
FROM alpine:latest AS release

# Install runtime dependencies and create user
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S appgroup && \
    adduser -S appuser -G appgroup

WORKDIR /app

# Copy binary and necessary files from builder
COPY --from=builder /build/internal/core ./core
COPY --from=builder /build/internal/core/docs/redoc-static.html ./docs/redoc-static.html
COPY --from=builder /build/static ./static
COPY --from=builder /build/internal/core/entrypoint.sh ./entrypoint.sh

# Set permissions before switching user
RUN chmod +x ./entrypoint.sh ./core && \
    chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Expose application port
EXPOSE 8080

# Command to run when starting the container
ENTRYPOINT ["./entrypoint.sh"]
