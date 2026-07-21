# Repository Reorganization

Design rationale and decisions: [docs/archive/REORG-2026-04.md](docs/archive/REORG-2026-04.md).

Solo dev, commit after every step. Only one genuine ordering constraint: **push to `main` on GitHub** after the rename steps are committed, before starting the replace-directive-removal steps. That push is called out inline below.

---

## Cleanup

- [x] Delete stray binary `/agentnode` at repo root
- [x] Delete stray binary `/mcp-broker-server` at repo root
- [x] Add `/agentnode` (repo-root only) to `.gitignore` (`/mcp-broker-server` already present)
- [x] Delete empty directory `api/` (contains only `.gitkeep`)
- [x] Delete empty directory `configs/` (contains only `.gitkeep`)
- [x] Delete empty directory `internal/` (contains only `.gitkeep`)
- [x] Delete empty directory `test/` (contains only `.gitkeep`)
- [x] Delete entire `mottopedia/` directory and all contents
- [x] Remove `./mottopedia` entry from `go.work`
- [x] Remove all `mottopedia` references from `README.md`
- [x] Remove all `mottopedia` references from `AGENTS.md`
- [x] Regenerate `docs/module-deps.md` without `mottopedia`
- [x] Verify `go build ./...` passes from workspace root
- [x] Verify `go test ./...` passes from workspace root

---

## Move apps under `apps/` (keep `replace` directives intact)

### Create apps/ and move pure apps

- [x] Create `apps/` directory
- [x] `git mv gowild_agent apps/agent` (preserves history)
- [x] `git mv gowild_agent_manager apps/agent_manager`

### Split `gowild_agent_net` into library + two apps

- [x] Keep `gowild_agent_net/` at top level as a library
- [x] Create `apps/agent_net_server/` with its own `go.mod`; add `replace knight.fm/gowild/gowild_agent_net => ../../gowild_agent_net`
- [x] `git mv gowild_agent_net/cmd/server/*` into `apps/agent_net_server/`
- [x] Create `apps/agent_net_admin/` with its own `go.mod` and matching `replace` directive
- [x] `git mv gowild_agent_net/cmd/admin/*` into `apps/agent_net_admin/`
- [x] Delete the now-empty `gowild_agent_net/cmd/` directory
- [x] `git mv Dockerfile.agent-net apps/agent_net_server/Dockerfile`
- [x] `git mv render.yaml apps/agent_net_server/render.yaml`
- [x] Update paths inside `apps/agent_net_server/Dockerfile` and `apps/agent_net_server/render.yaml` to reflect the new location

### Split `gowild_objectives` into library + app

- [x] Keep `gowild_objectives/` at top level as a library
- [x] Create `apps/objectives/` with its own `go.mod` and `replace` directive for `gowild_objectives`
- [x] `git mv gowild_objectives/cmd/objectives/*` into `apps/objectives/`
- [x] Delete the now-empty `gowild_objectives/cmd/` directory

### Promote `cmd/mcp-broker-server/` to a real app

- [x] Create `apps/mcp-broker-server/` with its own `go.mod`
- [x] `git mv cmd/mcp-broker-server/*` into `apps/mcp-broker-server/`
- [x] Copy the `require` blocks from root `go.mod` into `apps/mcp-broker-server/go.mod`
- [x] Delete root `go.mod` and `go.sum`
- [x] Delete now-empty `cmd/` directory

### Update workspace + tooling for the new paths

- [x] Update `go.work`: remove `.` entry (root module gone); remove old `./gowild_agent` + `./gowild_agent_manager`; add `./apps/agent`, `./apps/agent_manager`, `./apps/agent_net_server`, `./apps/agent_net_admin`, `./apps/objectives`, `./apps/mcp-broker-server`
- [x] Update `Makefile` for new paths
- [x] Update `README.md` build instructions for new `apps/` paths
- [x] Update `AGENTS.md` if it references moved directories
- [x] Update `.github/workflows/go-checks.yml` if it references moved directories

### Verify

- [x] `go build ./...` passes from workspace root (via `go.work`)
- [x] `go test ./...` passes

---

## Rename: drop `gowild_` prefix, adopt GitHub module paths, bump Go floor

`replace` directives stay in place — only their target paths change.

### Rename library directories

- [x] `git mv gowild_my my`
- [x] `git mv gowild_data data`
- [x] `git mv gowild_tools tools`
- [x] `git mv gowild_knowledge_graph knowledge_graph`
- [x] `git mv gowild_deep_research deep_research`
- [x] `git mv gowild_polymarket polymarket`
- [x] `git mv gowild_crypto crypto`
- [x] `git mv gowild_agentic_loop agentic_loop`
- [x] `git mv gowild_codexllm codexllm`
- [x] `git mv gowild_claudellm claudellm`
- [x] `git mv gowild_agent_data agent_data`
- [x] `git mv gowild_agent_node agent_node`
- [x] `git mv gowild_agent_net agent_net` (mixed module)
- [x] `git mv gowild_objectives objectives` (mixed module)

### Update module paths in every library's `go.mod`

- [x] `my/go.mod`: module `github.com/original-david-knight/go_wild/my`
- [x] `data/go.mod`: module `github.com/original-david-knight/go_wild/data`
- [x] `tools/go.mod`: module `github.com/original-david-knight/go_wild/tools`
- [x] `knowledge_graph/go.mod`: module `github.com/original-david-knight/go_wild/knowledge_graph`
- [x] `deep_research/go.mod`: module `github.com/original-david-knight/go_wild/deep_research`
- [x] `polymarket/go.mod`: module `github.com/original-david-knight/go_wild/polymarket`
- [x] `crypto/go.mod`: module `github.com/original-david-knight/go_wild/crypto`
- [x] `agentic_loop/go.mod`: module `github.com/original-david-knight/go_wild/agentic_loop`
- [x] `codexllm/go.mod`: module `github.com/original-david-knight/go_wild/codexllm`
- [x] `claudellm/go.mod`: module `github.com/original-david-knight/go_wild/claudellm`
- [x] `agent_data/go.mod`: module `github.com/original-david-knight/go_wild/agent_data`
- [x] `agent_node/go.mod`: module `github.com/original-david-knight/go_wild/agent_node`
- [x] `agent_net/go.mod`: module `github.com/original-david-knight/go_wild/agent_net`
- [x] `objectives/go.mod`: module `github.com/original-david-knight/go_wild/objectives`

### Update module paths in every app's `go.mod`

- [x] `apps/agent/go.mod`: module `github.com/original-david-knight/go_wild/apps/agent`
- [x] `apps/agent_manager/go.mod`: module `github.com/original-david-knight/go_wild/apps/agent_manager`
- [x] `apps/agent_net_server/go.mod`: module `github.com/original-david-knight/go_wild/apps/agent_net_server`
- [x] `apps/agent_net_admin/go.mod`: module `github.com/original-david-knight/go_wild/apps/agent_net_admin`
- [x] `apps/objectives/go.mod`: module `github.com/original-david-knight/go_wild/apps/objectives`
- [x] `apps/mcp-broker-server/go.mod`: module `github.com/original-david-knight/go_wild/apps/mcp-broker-server`

### Rewrite every cross-module `require` across all 20 `go.mod` files

- [x] Replace `knight.fm/gowild/gowild_my` → `github.com/original-david-knight/go_wild/my`
- [x] Replace `knight.fm/gowild/gowild_data` → `github.com/original-david-knight/go_wild/data`
- [x] Replace `knight.fm/gowild/gowild_tools` → `github.com/original-david-knight/go_wild/tools`
- [x] Replace `knight.fm/gowild/gowild_knowledge_graph` → `github.com/original-david-knight/go_wild/knowledge_graph`
- [x] Replace `knight.fm/gowild/gowild_deep_research` → `github.com/original-david-knight/go_wild/deep_research`
- [x] Replace `knight.fm/gowild/gowild_polymarket` → `github.com/original-david-knight/go_wild/polymarket`
- [x] Replace `knight.fm/gowild/gowild_crypto` → `github.com/original-david-knight/go_wild/crypto`
- [x] Replace `knight.fm/gowild/gowild_agentic_loop` → `github.com/original-david-knight/go_wild/agentic_loop`
- [x] Replace `knight.fm/gowild/gowild_codexllm` → `github.com/original-david-knight/go_wild/codexllm`
- [x] Replace `knight.fm/gowild/gowild_claudellm` → `github.com/original-david-knight/go_wild/claudellm`
- [x] Replace `knight.fm/gowild/gowild_agent_data` → `github.com/original-david-knight/go_wild/agent_data`
- [x] Replace `knight.fm/gowild/gowild_agent_node` → `github.com/original-david-knight/go_wild/agent_node`
- [x] Replace `knight.fm/gowild/gowild_agent_net` → `github.com/original-david-knight/go_wild/agent_net`
- [x] Replace `knight.fm/gowild/gowild_objectives` → `github.com/original-david-knight/go_wild/objectives`

### Update every `replace` directive target path

- [x] Replace `../gowild_X` path targets with `../X` (libraries) or `../../X` (apps)
- [x] Grep-verify no stale paths remain: `grep -r "knight\.fm/gowild" --include="*.mod"` returns zero results
- [x] Grep-verify no stale `gowild_` directory references in any `go.mod`

### Update every Go source file's import paths

- [x] Global `.go` replace: `"knight.fm/gowild/gowild_<name>"` → `"github.com/original-david-knight/go_wild/<name>"` for all 14 modules
- [x] Grep-verify: `grep -rn "knight\.fm/gowild" --include="*.go"` returns zero results
- [x] Grep-verify: `grep -rn "\"knight\.fm" --include="*.go"` returns zero results

### Bump Go version floor to 1.25

- [x] Set `go 1.25` in every `go.mod` (20 files)
- [x] Set `go 1.25` in `go.work`

### Update tooling and docs for new paths

- [x] Update `go.work` `use` entries to the renamed directories
- [x] Update `Makefile` targets
- [x] Update `README.md` with new module paths in examples
- [x] Update root `CLAUDE.md` (and any per-module `CLAUDE.md` files)
- [x] Update `AGENTS.md`
- [x] Regenerate `docs/module-deps.md`
- [x] Update `.github/workflows/go-checks.yml`
- [x] Update `scripts/backfill_market_images.go` and any other scripts with hardcoded paths
- [x] Update `apps/agent_net_server/Dockerfile` for new paths
- [x] Update `apps/agent_net_server/render.yaml` for new paths
- [x] Update `apps/agent/Dockerfile` `COPY` paths for all 9 renamed `gowild_*` dirs (already stale for `gowild_agent`; also touches `gowild_agent_data`, `gowild_agentic_loop`, `gowild_claudellm`, `gowild_crypto`, `gowild_data`, `gowild_knowledge_graph`, `gowild_my`, `gowild_tools`)
- [x] Update `apps/agent_manager/dockermgr/build_info.go` `agentBuildPaths` string table (lines 25-34) — literal directory names used for build fingerprint, not caught by Go import rewrite

### Verify

- [x] `go build ./...` passes from workspace root (via `go.work`)
- [x] `go test ./...` passes
- [x] Final grep sweep: no `gowild_` in `.go`/`.mod`/`.yaml`/`Makefile`/`Dockerfile` except incidental variable names
- [x] Final grep sweep: no `knight.fm/gowild` anywhere

---

## 🚀 Push to GitHub — required before the next phase

The replace-removal steps below depend on Go being able to resolve `github.com/original-david-knight/go_wild/<module>@main` against the remote. Push first.

- [x] `git push origin main`
- [x] Set `GOPRIVATE` locally (once): `go env -w GOPRIVATE=github.com/original-david-knight/*`

---

## Remove `replace` directives

Now that paths resolve via GitHub, every `go.mod` can drop its `replace` block. `go mod tidy` rewrites `require` lines with real pseudo-versions.

**Must be done bottom-up by dep depth.** `go mod tidy` loads the full module graph transitively, so each level's deps need to have their tidied `go.mod` already on `origin/main` before the next level runs. Commit + push after each level.

For each module: drop any `require` stubs pinned to `v0.0.0`, remove the `replace` block, run `GOWORK=off go mod tidy`.

### Level 0 — no cross-module deps

- [x] `my/go.mod`
- [x] `data/go.mod`
- [x] `crypto/go.mod`
- [x] `codexllm/go.mod`
- [x] `claudellm/go.mod`
- [x] `apps/mcp-broker-server/go.mod`
- [x] push `origin main`

### Level 1

- [x] `agentic_loop/go.mod` (→ my)
- [x] `polymarket/go.mod` (→ crypto)
- [x] push `origin main`

### Level 2

- [x] `knowledge_graph/go.mod` (→ agentic_loop, data, my)
- [x] push `origin main`

### Level 3

- [x] `agent_data/go.mod` (→ data, knowledge_graph, agentic_loop, my)
- [x] push `origin main`

### Level 4

- [x] `tools/go.mod` (→ agent_data, agentic_loop, claudellm, crypto, data, knowledge_graph, my)
- [x] `agent_net/go.mod` (→ agent_data, data, my, agentic_loop, knowledge_graph)
- [x] push `origin main`

### Level 5

- [x] `deep_research/go.mod` (→ agentic_loop, claudellm, codexllm, tools)
- [x] `apps/agent_net_server/go.mod` (→ agent_net, data, my)
- [x] `apps/agent_net_admin/go.mod` (→ agent_net)
- [x] `apps/agent/go.mod` (→ agent_data, agentic_loop, crypto, data, knowledge_graph, my, tools)
- [x] push `origin main`

### Level 6

- [x] `agent_node/go.mod` (→ agentic_loop, deep_research, my, tools)
- [x] push `origin main`

### Level 7

- [x] `objectives/go.mod` (→ agent_node, data, my)
- [x] push `origin main`

### Level 8

- [x] `apps/objectives/go.mod` (→ objectives)
- [x] `apps/agent_manager/go.mod` (→ agent_net, agent_node, objectives, + many libs)
- [x] push `origin main`

### Verify module-mode builds (the real test)

- [x] `GOWORK=off go build ./...` passes in every library module (14)
- [x] `GOWORK=off go build ./...` passes in every app module (6)
- [x] `GOWORK=off go test ./...` passes in every module
- [x] Spot check from outside the repo: scratch dir, `go get github.com/original-david-knight/go_wild/agent_net@main && go build ./...` succeeds

---

## Hide internals (incremental, per library)

Each library gets its own sweep. Before hiding any package, audit what imports it from sibling modules and apps.

### One-time audit script

- [x] Write `scripts/public-imports.sh`: given a module path, list its subpackages and which are imported from outside the module
- [x] Run it and capture a baseline in `docs/public-api-audit.md`

### Per-library `internal/` sweep (14 libraries, in whatever order feels right)

For each library: run the audit script, identify packages with zero external importers, move them under `internal/`, re-run the script to confirm nothing broke, `go build ./...`, `go test ./...`.

- [x] `my/` — audit + sweep
- [x] `data/` — audit + sweep
- [x] `tools/` — audit + sweep (preserve public: `broker/`, `amazon/`, `shopify/`, `supplier/`, `supplier/providers/cjdropshipping/`)
- [x] `knowledge_graph/` — audit + sweep
- [x] `deep_research/` — audit + sweep
- [x] `polymarket/` — audit + sweep
- [x] `crypto/` — audit + sweep
- [x] `agentic_loop/` — audit + sweep (preserve public: `mcp/`)
- [x] `codexllm/` — audit + sweep
- [x] `claudellm/` — audit + sweep
- [x] `agent_data/` — audit + sweep
- [x] `agent_node/` — audit + sweep (preserve symbols imported by `objectives`)
- [x] `agent_net/` — audit + sweep (preserve public: `server/`)
- [x] `objectives/` — audit + sweep

---

## Post-migration

- [x] Add a "Consuming these libraries in another project" section to `README.md` covering `GOPRIVATE=github.com/original-david-knight/*`, Git credentials, and an example `go get github.com/original-david-knight/go_wild/<lib>@main`
- [x] Update root `CLAUDE.md` to match the final layout
- [x] Regenerate `docs/module-deps.md`
- [x] Move `docs/REORG.md` to `docs/archive/REORG-2026-04.md`
- [x] Add a `CHANGELOG.md` entry noting the reorg date and the new module paths
