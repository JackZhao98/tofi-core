# Tofi Sandbox — ephemeral container image for running user tasks
# Used by DockerExecutor when sandbox-mode=docker
FROM debian:bookworm-slim

# Install runtime tools needed by user tasks
RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 python3-pip python3-venv \
    curl wget jq git \
    nodejs npm \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user for task execution
RUN useradd -m -s /bin/bash -u 1000 sandbox

# Create writable workspace (will be tmpfs in production)
RUN mkdir -p /workspace && chown sandbox:sandbox /workspace

USER sandbox
WORKDIR /workspace

CMD ["sleep", "infinity"]
