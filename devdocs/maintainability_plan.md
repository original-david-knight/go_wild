# Maintainability Plan

## Goals

1. Reduce cognitive load in `gowild_agent_manager` and other high-change packages.
2. Preserve behavior while improving structure, testability, and onboarding speed.
3. Make architecture drift visible early via CI checks and generated docs.

## Baseline (March 2026)

1. 19 Go modules in workspace (`go.work`), 668 Go files, ~128k lines.
2. Main hotspot is `gowild_agent_manager` (176 files, ~35k lines).
3. Highest-complexity functions are concentrated in dispatch/routing and runtime orchestration paths.
4. Some important integration packages (`gowild_tools/shopify`, `ads`, `amazon`, `supplier`) have no package-local tests.

## Progress Update (March 2026)

Completed so far:

1. Phase 1 dispatch/routing stabilization:
   - `broker_tools_shopify.go`: replaced large switch dispatch with Shopify tool registry map.
   - `broker_tools_data_access.go`: replaced large switch dispatch with data-access tool handler map.
   - `broker_tools_dispatch.go`: replaced chained dispatch conditionals with ordered dispatcher table.
   - `broker_tools_ecommerce.go`: replaced tool switch with handler registry; extracted shared date-range parsing helper.
   - `broker_tools_ecommerce.go`: replaced duplicated P&L category switches with shared category-handler maps for product/daily accumulation.
   - `broker_tools_supplier.go`: replaced tool switch with supplier handler registry + explicit recognition helper.
   - `broker_tools_amazon.go`: replaced tool switch with amazon handler registry + explicit recognition helper.
   - `broker_tools_ads.go`: replaced tool switch with ads handler registry + explicit recognition helper.
   - `broker_tools_report.go`: replaced tool switch with report handler registry + explicit recognition helper.
   - `broker_tools_soul.go`: replaced tool switch with soul handler registry + explicit recognition helper.
   - `broker_tools_skills.go`: replaced tool switch with skills handler registry + explicit recognition helper.
   - `broker_tools_recurring.go`: replaced tool switch with recurring handler registry + explicit recognition helper.
   - `broker_tools_cache.go`: replaced tool switch with cache handler registry + explicit recognition helper.
   - `broker_tools_company_knowledge.go`: replaced tool switch with company-knowledge handler registry + explicit recognition helper.
   - `broker_tools_company_admin.go`: replaced dual switch dispatch with company-admin handler registry + explicit recognition helper.
   - `broker_tools_messaging.go`: replaced tool switch with messaging handler registry + explicit recognition helper.
   - `broker_tools_mcp.go`: replaced tool switch with mcp handler registry + explicit recognition helper.
   - `broker_tools_mcp.go`: replaced `setMCPServerEnabled` scope switch with scope-handler table + extracted local/host scope helpers.
   - `broker_tools_tasks.go`: replaced large task switch with task handler registry + shared helper extraction for task tool wrappers and task-list/context shaping.
   - `broker_tools_inventory_sync.go`: extracted shared supplier-alias recognition helper to remove duplicated CJ switch logic across sync/create paths.
   - `broker_tools_kg.go`: replaced knowledge-graph tool switch with handler registry + explicit recognition helper.
   - `broker_tools_compress.go`: replaced compress tool switch with handler registry + explicit recognition helper.
   - `broker_tools_claude_code.go`: replaced claude code tool switch with handler registry + explicit recognition helper.
   - `broker_tools_a2a.go`: replaced A2A tool switch with handler registry + explicit recognition helper (including removed legacy tool aliases).
   - `broker_tools_supplier.go`: replaced env-provider switch with factory registry (TopDawg/CJ aliases preserved).
   - `handlers_routes.go`: split agent route parsing/prefix handling/dispatch into focused functions.
   - `handlers_routes.go`: replaced `/api/agents` and agent action switches with collection/root/action handler tables + explicit action/method recognition helpers.
   - `ws_input.go`: replaced UI message/control action switches with dispatch tables + explicit type/action recognition helpers.
   - `ws_output.go`: replaced agent message/runtime status switches with side-effect/update registries.
   - `spend_governor.go`: replaced spend category switch in `EstimateCost` with estimator table + explicit category-estimator recognition helper.
   - `handlers_companies.go`: extracted shared provider-route method handling helper.
   - `handlers_kg.go`: replaced KG action switch with action-handler registry + explicit route parsing helper.
   - `handlers_missions.go`: replaced nested mission route/method switches with collection/resource/action route tables + escalation route helper.
   - `handlers_companies.go`: replaced connection-route method switch with declarative method handler table + explicit method-recognition helper.
   - `handlers_companies.go`: replaced top-level company action switch with action handler registry + focused per-action route helpers.
   - `handlers_companies.go`: replaced remaining company/company-members/webhooks/knowledge method switches with declarative handler tables + explicit method-recognition helpers.
   - `handlers_recurring.go`: replaced recurring route method switches with collection/task method handler tables + explicit method-recognition helpers.
   - `handlers_mcp.go`: replaced MCP route method switches with collection/server/agent method handler tables + explicit method-recognition helpers.
   - `handlers_deep_research_methods.go`: replaced deep-research route/method switches with collection/method/action handler tables + explicit route parsing helper.
   - `handlers_peer_groups.go`: replaced peer-group nested route/method switches with collection/group/members/member handler tables + explicit route parsing helper.
   - `handlers_a2a_methods.go`: replaced A2A method route/method switches with collection/method handler tables + explicit route parsing helper.
   - `handlers_capabilities.go`: replaced capability collection/resource method switches with declarative handler tables + explicit method-recognition helpers.
   - `handlers_pending_emails.go`: replaced pending-email action switch with declarative action/method handler table + explicit action parser helper.
   - `handlers_email_whitelist.go`: replaced whitelist method switch with declarative method handler table + explicit method-recognition helper.
   - `handlers_pipelines.go`: replaced top-level pipeline route/method switches with explicit route parser + collection/static/id/action handler tables.
   - `webhooks.go`: replaced ingress webhook provider switch with provider-handler registry + explicit ingress route parsing helper.
   - `a2a_local_queue.go`: replaced `ClaimJob` status switch with declarative claim-status handler table + extracted queued/claimed claim helpers.
2. Additional policy maintainability:
   - `broker_tools_policy.go`: replaced mixed switch logic in `toolGroupForToolName` with exact/prefix mapping tables.
   - `broker_tools_policy.go`: replaced `isToolGroupEnabled` switch with declarative rule table + shared alias-check helper.
3. Phase 2 started:
   - `server.go`: split API and broker route wiring into explicit registration helpers.
   - `main.go`: extracted objectives scheduler startup and server startup into focused helpers.
4. Phase 4 baseline test depth:
   - Added first package-local tests for `gowild_tools/amazon`.
   - Added first package-local tests for `gowild_tools/supplier`.
   - Added first package-local tests for `gowild_tools/ads`.
   - Added first package-local tests for `gowild_tools/shopify`.
   - Added first package-local tests for `gowild_tools/ecommerce`.
   - Added first provider-helper tests for `gowild_tools/supplier/providers`.
   - Added `gowild_tools/amazon/cmd/test_live` for opt-in real PAAPI validation with production credentials.

Characterization/regression tests added:

1. `gowild_agent_manager/maintainability_dispatch_test.go`
2. `gowild_agent_manager/broker_tools_dispatch_test.go` (unknown-tool path)
3. `gowild_agent_manager/broker_tools_policy_test.go` (expanded mapping cases)
4. `gowild_agent_manager/server_test.go` (broker/API route registration coverage)
5. `gowild_agent_manager/main_config_test.go` (startup config resolution)
6. `gowild_tools/amazon/client_test.go`
7. `gowild_tools/supplier/tools_test.go`
8. `gowild_tools/ads/client_test.go`
9. `gowild_tools/shopify/client_test.go`
10. `gowild_tools/ecommerce/tools_test.go`
11. `gowild_tools/supplier/providers/helpers_test.go`
12. `gowild_agent_manager/broker_tools_ecommerce_test.go`
13. `gowild_agent_manager/broker_tools_supplier_test.go` (dispatch + env-provider coverage expansion)
14. `gowild_agent_manager/broker_tools_amazon_test.go`
15. `gowild_agent_manager/broker_tools_ads_test.go`
16. `gowild_agent_manager/broker_tools_basic_sets_test.go`
17. `gowild_agent_manager/broker_tools_recurring_cache_test.go`
18. `gowild_agent_manager/broker_tools_messaging_test.go`
19. `gowild_agent_manager/broker_tools_mcp_test.go`
20. Expanded `gowild_agent_manager/broker_tools_company_admin_test.go` for explicit unknown/recognition dispatch behavior.
21. Expanded `gowild_agent_manager/broker_tools_company_knowledge_test.go` for explicit unknown/recognition dispatch behavior.
22. `gowild_agent_manager/handlers_lifecycle_docker_test.go` hardened test agent id uniqueness (flake reduction).
23. `gowild_agent_manager/broker_tools_tasks_test.go`
24. `gowild_agent_manager/broker_tools_kg_compress_test.go`
25. `gowild_agent_manager/broker_tools_a2a_test.go`
26. Expanded `gowild_agent_manager/broker_tools_claude_code_test.go` for explicit unknown/recognition dispatch behavior.
27. `gowild_agent_manager/handlers_kg_test.go`
28. `gowild_agent_manager/handlers_missions_route_test.go`
29. Expanded `gowild_agent_manager/maintainability_dispatch_test.go` for company-connection method recognition coverage.
30. `gowild_agent_manager/handlers_recurring_test.go`
31. Expanded `gowild_agent_manager/handlers_route_test.go` for MCP method-recognition helper coverage.
32. Expanded `gowild_agent_manager/handlers_deep_research_methods_test.go` for unknown-action and method/action recognition helper coverage.
33. `gowild_agent_manager/handlers_peer_groups_test.go`
34. Expanded `gowild_agent_manager/handlers_a2a_methods_test.go` for route/method guard and parser helper coverage.
35. Expanded `gowild_agent_manager/handlers_capabilities_test.go` for collection method-guard and route method-recognition helper coverage.
36. Expanded `gowild_agent_manager/email_approval_test.go` for pending-email wrong-method behavior and route parser/action-recognition helper coverage.
37. Expanded `gowild_agent_manager/email_approval_test.go` for email-whitelist method-recognition helper coverage.
38. Expanded `gowild_agent_manager/handlers_pipelines_test.go` for top-level route/method guards and parser/helper coverage.
39. Expanded `gowild_agent_manager/handlers_route_test.go` for agent collection/root method guards and action/method recognition helper coverage.
40. Expanded `gowild_agent_manager/broker_tools_inventory_sync_test.go` for shared supplier-alias helper behavior.
41. Expanded `gowild_agent_manager/broker_tools_policy_test.go` for shared policy-rule helper coverage.
42. Expanded `gowild_agent_manager/maintainability_dispatch_test.go` for company collection/resource/members/webhooks/knowledge method-recognition helper coverage.
43. Expanded `gowild_agent_manager/ws_input_test.go` for unknown-type fallback and UI/control recognition helper coverage.
44. Expanded `gowild_agent_manager/ws_test.go` for ws-output helper no-op behavior on unknown message types.
45. Expanded `gowild_agent_manager/broker_tools_mcp_test.go` for mcp-set-server-enabled scope recognition and validation-path coverage.
46. Expanded `gowild_agent_manager/broker_tools_ecommerce_test.go` for P&L category helper recognition/no-op behavior coverage.
47. `gowild_agent_manager/spend_governor_test.go` (new coverage for spend estimation and daily-limit default/override behavior).
48. `gowild_agent_manager/webhooks_test.go` (new coverage for ingress path validation, provider routing, and unsupported-provider behavior).
49. Expanded `gowild_agent_manager/a2a_local_queue_test.go` for ClaimJob state-edge characterization (claimed-by-other, expired-claim reclaim, invalid-state rejection) and claim-status recognition helper coverage.

Validation status:

1. `go test ./gowild_agent_manager` passing after each slice.
2. `go list -m | xargs -I{} go test {}/...` passing for full workspace.

## Refactor Sequence

### Phase 1: Manager Dispatch and Routing Stabilization

Target code:

1. `gowild_agent_manager/handlers_routes.go`
2. `gowild_agent_manager/handlers_companies.go`
3. `gowild_agent_manager/broker_tools_data_access.go`
4. `gowild_agent_manager/broker_tools_shopify.go`

Changes:

1. Replace large switch-based dispatch with declarative route/tool registries.
2. Keep HTTP contracts and error semantics unchanged.
3. Split high-branch handlers into focused files grouped by domain.

Exit criteria:

1. No API behavior regressions on existing routes/tools.
2. New/updated tests cover critical dispatch paths and method guards.
3. Per-file complexity and function length reduced in touched areas.

### Phase 2: Composition Boundary Cleanup

Target code:

1. `gowild_agent_manager/main.go`
2. `gowild_agent_manager/server.go`
3. `gowild_agent_manager/handlers_core.go`

Changes:

1. Move startup wiring into `app`/`runtime` constructors.
2. Keep `main` focused on flag parsing and process lifecycle.
3. Introduce typed config loaders (replace scattered env parsing).

Exit criteria:

1. Startup logic is testable without full process boot.
2. Environment validation is centralized and explicit.

### Phase 3: Monorepo Dependency and Docs Reliability

Target code:

1. `scripts/module_deps.py`
2. `scripts/gen_module_deps.sh`
3. `.github/workflows/`

Changes:

1. Fix module dependency generation to match current module namespace.
2. Add verification tests for dependency graph generation.
3. Expand CI to include lint/vet/test checks on changed modules.

Exit criteria:

1. Generated module diagram matches `go.work` modules.
2. CI catches dependency-doc drift and core quality regressions.

### Phase 4: Integration Package Test Depth

Target code:

1. `gowild_tools/shopify`
2. `gowild_tools/supplier`
3. `gowild_tools/ads`
4. `gowild_tools/amazon`

Changes:

1. Add client/request-shape and error-handling tests with mocked transports.
2. Codify expected request/response contracts before refactors.

Exit criteria:

1. Critical tool packages have baseline behavior coverage.
2. Refactors can proceed without relying on manual integration checks.

## Test-First Policy For Planned Changes

Before changing internals in each phase:

1. Add characterization tests for current observable behavior.
2. Refactor in small steps with tests green after each step.
3. Add focused regression tests for every bug found during refactor.

## Next Test Backlog

1. `main.go` config/startup extraction: characterize DB URL/env precedence and objectives scheduler enablement gates.
2. `handlers_core.go` lazy external DB wiring: characterize fallback/no-env behavior and injected DB precedence.
3. Expand `gowild_tools/supplier/providers` coverage from helper-level tests to API request-shape and error-path tests (TopDawg/CJ).
