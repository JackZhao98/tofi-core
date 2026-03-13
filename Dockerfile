# ── Stage 1: Build Go binary ──
FROM golang:1.24-bookworm AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o tofi-server ./cmd/tofi

# ── Stage 2: Runtime ──
FROM debian:bookworm-slim

# Install runtime tools needed by sandbox (direct mode in container)
RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 python3-pip python3-venv \
    curl wget jq git \
    nodejs npm \
    ca-certificates \
    chromium \
    && rm -rf /var/lib/apt/lists/*

# Create app user (non-root)
RUN useradd -m -s /bin/bash -u 1000 tofi

WORKDIR /app
COPY --from=builder /build/tofi-server .

# Create data directory with proper ownership
RUN mkdir -p /app/.tofi && chown -R tofi:tofi /app

# Switch to non-root user
USER tofi

EXPOSE 8080

ENTRYPOINT ["./tofi-server", "server", "-port", "8080", "-home", "/app/.tofi"]
