# Dockerfile for agent sandboxed execution
# Each agent runs in its own container with persistent state
#
# Build from workspace root (golang2/):
#   docker build -t gowild-agent -f apps/agent/Dockerfile .
#
# Or use: ./agent (auto-builds the image, sandbox is default)

FROM golang:1.25-bookworm AS builder

WORKDIR /build

# Build in module mode (not workspace mode). The repo dropped `replace`
# directives in favor of pseudo-versions resolved against the private
# github.com/original-david-knight/go_wild repo. The Docker builder has no
# credentials for that remote, and workspace mode (go.work) still triggers
# VCS lookups for indirect deps pinned to those pseudo-versions. So instead
# we inject local-path `replace` directives into every module we copy —
# `replace` bypasses VCS resolution entirely.
ENV GOWORK=off
ENV GOTOOLCHAIN=auto

# Cache-bust: any value change here invalidates source copy + build layers
# while keeping the runtime apt layer cached.
ARG CACHE_BUST=0

# Copy all source code
COPY apps/agent/ ./apps/agent/
COPY agent_auth/ ./agent_auth/
COPY agent_data/ ./agent_data/
COPY agentic_loop/ ./agentic_loop/
COPY claudellm/ ./claudellm/
COPY crypto/ ./crypto/
COPY data/ ./data/
COPY knowledge_graph/ ./knowledge_graph/
COPY my/ ./my/
COPY tools/ ./tools/

# Inject replace directives into every copied go.mod so sibling modules
# resolve to local source regardless of the pseudo-version they reference.
COPY apps/agent/Dockerfile.inject-replaces.sh /tmp/inject-replaces.sh
RUN sh /tmp/inject-replaces.sh

# Build the agent binary
WORKDIR /build/apps/agent
RUN CGO_ENABLED=1 go build -o /agent .

# Runtime image
FROM debian:bookworm-slim

# Build metadata labels (set via build args)
ARG BUILD_SHA=unknown
ARG BUILD_DIRTY=unknown
ARG BUILD_ID=unknown
ARG BUILD_TIME=unknown
LABEL org.gowild.agent.build_sha=$BUILD_SHA \
    org.gowild.agent.build_dirty=$BUILD_DIRTY \
    org.gowild.agent.build_id=$BUILD_ID \
    org.gowild.agent.build_time=$BUILD_TIME

# Install runtime dependencies:
# - ca-certificates: for HTTPS requests
# - python3, pip: for Python tools (runs directly, no Docker needed in container)
# - curl, wget: for network requests
# - git: for version control
# - jq: for JSON processing
# - file, tree: for file inspection
# - wl-clipboard, xclip: for clipboard support (optional)
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    python3 \
    python3-pip \
    python3-venv \
    curl \
    wget \
    git \
    jq \
    file \
    tree \
    wl-clipboard \
    xclip \
    && rm -rf /var/lib/apt/lists/*

# Create a symlink so 'python' works
RUN ln -sf /usr/bin/python3 /usr/bin/python

# Copy the binary
COPY --from=builder /agent /usr/local/bin/agent

# Working directory for agent data (mounted as a volume)
WORKDIR /data

# Environment defaults
ENV TERM=xterm-256color
ENV GOWILD_IN_CONTAINER=1

# The entrypoint runs the agent
# Agent ID and other flags are passed at runtime
ENTRYPOINT ["/usr/local/bin/agent"]
