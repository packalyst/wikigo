# Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache \
    gcc \
    musl-dev \
    nodejs \
    npm \
    git

WORKDIR /build

# Copy go.mod first
COPY go.mod ./

# Install templ
RUN go install github.com/a-h/templ/cmd/templ@latest

# Copy source code
COPY . .

# Generate templ files first (creates *_templ.go files needed for imports)
RUN templ generate

# Generate go.sum and download dependencies
RUN go mod tidy && go mod download

# Build Tailwind CSS (if package.json exists)
RUN if [ -f package.json ]; then \
    npm install && \
    npm run build:css; \
    fi

# Build the binary with optimizations
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-s -w -extldflags '-static'" \
    -tags "sqlite_fts5" \
    -o gowiki \
    ./cmd/wiki

# Verify the binary exists
RUN ls -la gowiki && file gowiki || echo "Binary built"

# Runtime stage
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    su-exec \
    && rm -rf /var/cache/apk/*

# Create non-root user for security
RUN addgroup -g 1000 wiki && \
    adduser -u 1000 -G wiki -s /bin/sh -D wiki

# Create directories
RUN mkdir -p /app/data /app/uploads /app/backups /app/static && \
    chown -R wiki:wiki /app

WORKDIR /app

# Copy binary from builder (with correct ownership)
COPY --from=builder --chown=wiki:wiki /build/gowiki .

# Copy static files
COPY --from=builder --chown=wiki:wiki /build/static ./static

# Copy documentation files for auto-import
COPY --from=builder --chown=wiki:wiki /build/README.md ./README.md
COPY --from=builder --chown=wiki:wiki /build/API.md ./API.md

# Copy entrypoint script
COPY --chown=wiki:wiki --chmod=755 entrypoint.sh /app/

# Expose port
EXPOSE 9090

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget -q --spider http://localhost:9090/health || exit 1

# Volumes for persistent data
VOLUME ["/app/data", "/app/uploads", "/app/backups"]

# Environment defaults
ENV WIKI_PORT=9090 \
    WIKI_HOST=0.0.0.0 \
    WIKI_DB_PATH=/app/data/wiki.db \
    WIKI_UPLOAD_PATH=/app/uploads \
    WIKI_BACKUP_ENABLED=true \
    WIKI_BACKUP_PATH=/app/backups \
    WIKI_SITE_NAME=GoWiki \
    TZ=UTC

# Run the application (entrypoint handles permissions and drops to wiki user)
ENTRYPOINT ["./entrypoint.sh"]
