# Dockerfile for Agent Network Server
# Build context should be the workspace root (golang2/)

# Build stage
FROM golang:1.25.5-alpine AS builder

WORKDIR /app

# Build in module mode. Sibling modules are referenced via pseudo-versions
# that resolve against the private github.com/original-david-knight/go_wild
# repo, which the Docker builder has no credentials for. Inject local-path
# `replace` directives into every copied go.mod so those lookups are
# bypassed. (Workspace mode doesn't help — it still triggers VCS resolution
# for indirect deps pinned to those pseudo-versions.)
ENV GOWORK=off

# Copy required packages (transitive closure of apps/agent_net_server's
# sibling-module dependencies)
COPY my/ ./my/
COPY data/ ./data/
COPY agentic_loop/ ./agentic_loop/
COPY knowledge_graph/ ./knowledge_graph/
COPY agent_data/ ./agent_data/
COPY agent_net/ ./agent_net/
COPY apps/agent_net_server/ ./apps/agent_net_server/

# Inject replace directives so sibling modules resolve to local source.
COPY apps/agent_net_server/Dockerfile.inject-replaces.sh /tmp/inject-replaces.sh
RUN sh /tmp/inject-replaces.sh

# Build the server
WORKDIR /app/apps/agent_net_server
RUN go build -o server .

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/apps/agent_net_server/server .

# Render sets PORT env var
EXPOSE 8080

CMD ["./server"]
