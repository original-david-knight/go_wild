---
name: Deploy
description: >
  Build, test, and deploy the GoWild agent system. Rebuilds Go binaries,
  Docker images, restarts containers, and verifies health.
---

# Deploy Skill

Build, test, and deploy the GoWild agent system.

## Steps

1. **Build all Go packages** from the workspace root:
   ```bash
   cd /home/david/workspace/golang && go build ./...
   ```
   If this fails, stop and report the compilation errors.

2. **Run tests**:
   ```bash
   cd /home/david/workspace/golang && go test ./...
   ```
   If tests fail, stop and report the failures.

3. **Rebuild Docker images** (from workspace root):
   ```bash
   cd /home/david/workspace/golang && docker build -f gowild_agent/Dockerfile -t gowild-agent:latest .
   ```

4. **Restart the agent manager**:
   - If the manager is running, stop it and restart:
   ```bash
   cd /home/david/workspace/golang/gowild_agent_manager && go build . && ./gowild_agent_manager &
   ```

5. **Verify health**:
   - Wait 3 seconds, then check the manager is responding:
   ```bash
   curl -s http://localhost:8888/health
   ```
   - Check running containers:
   ```bash
   docker ps --filter "label=gowild" --format "table {{.Names}}\t{{.Status}}"
   ```

6. **Report results**: Show a summary of what was built, test results, and service status.

## Notes

- If only Go code changed (no Dockerfile changes), skip the Docker image rebuild and just restart the manager.
- If the user says "deploy agent-net", rebuild and restart the agent-net server instead.
- Always run `go build ./...` and `go test ./...` before any deployment step.
