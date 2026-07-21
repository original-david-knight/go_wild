# Company-Scoped Multi-Agent Architecture (First-Class `Company`)

## Summary
Introduce a first-class `Company` model that owns shared resources (wallet identity, Polymarket identity, Shopify identity, shared knowledge, and company-scoped tool data) and has an operational parent agent (`CEO`) plus member agents.

Agents keep private state and private knowledge. They continue to run independently (heartbeats and pipelines). Company capabilities are exposed through separate company tool groups so they can be enabled and disabled independently.

## Core Decisions
- `Company` is a new first-class entity, not a reuse of `peer_groups`.
- An agent can belong to at most one company.
- Wallet and Polymarket identity are shared at company scope (single identity per company).
- Shopify is company-scoped only. All `shopify_*` calls resolve through company credentials and require company membership.
- Pipelines support optional company scoping. When enabled, only agents in that same company are eligible to receive pipeline tasks.
- Parent model is operational: one `ceo_agent_id` coordinates, while child agents still run independently.
- Knowledge model is dual-store:
  - Agent-private knowledge remains unchanged.
  - Company-shared knowledge is separate and accessed only through company tools.
- Company tools are separate groups and can be toggled independently from private tools.

## Data Model and Persistence

### New tables and types
1. `Company`
   - `id`, `name`, `description`
   - `ceo_agent_id` (operational parent)
   - `wallet_seed_phrase` (shared wallet root secret)
   - `created_at`, `updated_at`

2. `CompanyMember`
   - `id`, `company_id`, `agent_id`, `role`, `created_at`
   - constraints:
     - unique(`agent_id`) to enforce one-company-max
     - unique(`company_id`, `agent_id`) to prevent duplicate membership

3. `CompanyKnowledgeEntry` (or company-scoped mapping to KG in a later pass)
   - `id`, `company_id`, `kind`, `title`, `content`
   - `tags_json`, `metadata_json`
   - `created_by_agent_id`, `created_at`, `updated_at`

4. Company tool state tables (domain-owned, no catch-all JSON state)
   - Each company-level tool domain stores state in explicit DB tables keyed by `company_id`.
   - Examples: domain-specific configs, execution state, and artifacts for company tools.
   - Avoid generic `company.state_json`; require typed schema per tool/domain.

5. `CompanyShopifyConnection`
   - `id`, `company_id`
   - `shop_url`, `api_version`
   - `access_token_enc` (encrypted at rest)
   - `webhook_secret_enc` (encrypted at rest, optional if webhooks are enabled elsewhere)
   - `enabled`, `created_at`, `updated_at`, `last_tested_at`
   - constraints:
     - unique(`company_id`) for one active Shopify connection per company (initial version)

6. `CompanyPolymarketConnection`
   - `id`, `company_id`
   - `proxy_url`, `onchain_rpc_url`
   - `funder_address`, `signature_type`
   - `chain_id`, `enabled`
   - `created_at`, `updated_at`, `last_tested_at`
   - constraints:
     - unique(`company_id`) for one active Polymarket connection per company (initial version)

7. Pipeline scope fields (persisted on existing pipeline tables)
   - `PipelineDefinition.scope_mode` enum: `"global"` or `"company"` (default `"global"`)
   - `PipelineDefinition.scope_company_id` nullable string
   - `PipelineRun.scope_mode` and `PipelineRun.scope_company_id` snapshot at run start
   - validation:
     - if `scope_mode == "company"`, `scope_company_id` is required
     - `scope_company_id` must reference an existing company

### Existing models unchanged in this phase
- Keep `PeerGroup` and `PeerGroupMember` unchanged for now.
- Defer rename/refactor of peer groups to a later phase.

## Service Layer Changes (`gowild_agent/data` and manager service)

### New service methods
- Company lifecycle:
  - `CreateCompany`
  - `GetCompany`
  - `ListCompanies`
  - `UpdateCompany`
  - `DeleteCompany`
- Membership:
  - `AddCompanyMember`
  - `RemoveCompanyMember`
  - `ListCompanyMembers`
  - `GetCompanyForAgent(agentID)` (returns zero or one company due to one-company-max)
  - `SetCompanyCEO(companyID, agentID)` with membership validation
- Company knowledge:
  - `CompanyKnowledgeAdd`
  - `CompanyKnowledgeSearch`
  - `CompanyKnowledgeGet`
  - `CompanyKnowledgeUpdate`
  - `CompanyKnowledgeDelete`
- Company Shopify configuration:
  - `GetCompanyShopifyConnection`
  - `UpsertCompanyShopifyConnection`
  - `DeleteCompanyShopifyConnection`
  - `TestCompanyShopifyConnection`

### Validation and policy rules
- Agent must exist before membership insertion.
- `ceo_agent_id` must reference an agent who is already a company member.
- Attempting to add an agent already in another company returns conflict.
- Removing the current CEO is blocked unless CEO is reassigned first.
- Pipeline definition validation enforces company scope constraints (`scope_mode` and `scope_company_id` pair).

## Broker/Auth and Tool Routing (`gowild_agent_manager`)

### Identity resolution
- Broker session auth resolves the authenticated agent ID from the verified Ethereum auth address.
- Add runtime resolver:
  - `resolveCompanyContext(agent_id) -> { company_id, role, is_ceo } | nil`

### Wallet and Polymarket behavior
- wallet and Polymarket handlers are company-scoped only.
- For every wallet or Polymarket call:
  - resolve `agent_id -> company_id`
  - deny if agent has no company membership
  - derive identity from `company.wallet_seed_phrase`
  - for Polymarket, apply optional company connection settings from `company_polymarket_connections`
- Include explicit metadata in responses:
  - `identity_scope: "company"`
  - `company_id`

### Shopify behavior (company-only)
- Remove global singleton Shopify client behavior for broker tool execution.
- For every `shopify_*` call:
  - resolve `agent_id -> company_id`
  - deny if agent has no company membership
  - load company Shopify connection from DB
  - deny if company Shopify connection is missing or disabled
  - execute request using company Shopify credentials
- No per-agent or global fallback for `shopify_*` tool calls.
- Include `company_id` in Shopify tool result metadata for auditability.

### Rate limit and governance
- Wallet and Polymarket write limits should key by `company_id` for company-scoped execution.
- This prevents bypass by spreading writes across multiple member agents.
- Shopify write limits and spend records should also key by `company_id`.

## Tool Surface and Enable/Disable Controls

### New tool groups
- `company_admin`
  - create/update company metadata, manage membership, assign CEO, manage governance settings
- `company_knowledge`
  - company knowledge CRUD and query
- `company_finance`
  - company finance context helpers and inspect-style operations
- `company_commerce`
  - company-scoped Shopify operations and commerce orchestration

### Behavior rules
- Existing private tools stay unchanged.
- Company access is explicit through company tools (no implicit private+company merge).
- If company tool groups are disabled, company operations are unavailable even for members.
- Shopify tools are treated as company commerce tools and require company membership regardless of agent-local settings.
- Default policy:
  - `company_admin` enabled for CEO by default
  - `company_admin` disabled for non-CEO members by default unless explicitly enabled

## HTTP/API and UI Changes (`gowild_agent_manager`)

### New API routes
- `GET|POST /api/companies`
- `GET|PATCH|DELETE /api/companies/{id}`
- `GET|POST /api/companies/{id}/members`
- `DELETE /api/companies/{id}/members/{agent_id}`
- `PATCH /api/companies/{id}/members/{agent_id}` (optional, for role update)
- `PUT /api/companies/{id}/ceo`
- `GET|PUT|DELETE /api/companies/{id}/shopify`
- `POST /api/companies/{id}/shopify/test`
- `GET|PUT|DELETE /api/companies/{id}/polymarket`
- `GET|POST /api/companies/{id}/knowledge`
- `GET|PATCH|DELETE /api/companies/{id}/knowledge/{entry_id}`
- `GET /api/agents/{agent_id}/company`

### Pipeline schema additions (`/api/pipelines`)
- Upsert request fields:
  - `scope_mode`: `"global"` (default) or `"company"`
  - `scope_company_id`: required when `scope_mode == "company"`
- Response fields:
  - `scope_mode`
  - `scope_company_id`

### UI additions
- Company management panel:
  - create and list companies
  - assign or reassign CEO
  - add and remove members
  - inspect and edit company metadata and governance settings
- Company Shopify settings panel:
  - configure shop URL, API version, access token, webhook secret
  - test connection
  - enable and disable company Shopify integration
- Company knowledge tab:
  - search, add, update, delete shared entries
- Agent config panel:
  - independent toggles for `company_admin`, `company_knowledge`, `company_finance`, `company_commerce`
- Pipeline editor:
  - scope selector: `Global` or `Company`
  - company picker shown when `Company` is selected
  - validation error if company-scoped pipeline has no selected company

## Pipelines and Heartbeats Integration

### Pipelines
- No restriction on child members participating in pipelines.
- Existing role and method based matching remains valid.
- Scope modes:
  - `global`: existing behavior, any eligible agent may receive task.
  - `company`: only eligible agents that are members of `scope_company_id` may receive task.
- Task pickup enforcement for company-scoped runs:
  - candidate agents are filtered by company membership before assignment.
  - cross-company agents are excluded even if role and method match.
  - if no in-company candidate exists, step/run fails with explicit reason (`no eligible agents in company scope`).
- Add optional `company_id` filter in capabilities listing to improve scoped pipeline authoring UX.

### Heartbeats
- Keep existing per-agent heartbeat delivery unchanged.
- Add optional manager utility for company fan-out:
  - `send_company_heartbeat(company_id, message, include_ceo, member_filter)`
- Implement with fan-out calls to `WorkerManager.SendHeartbeat(agentID, ...)`.

## Migration and Compatibility Plan

### Phase 1 (non-breaking)
- Add new company tables, services, routes, and company tool groups.
- Keep peer groups untouched.
- Keep all non-company agents working exactly as they do now.
- For Shopify specifically, move from global env client to company-scoped credentials for tool execution.

### Phase 2 (optional convergence)
- Add deprecation guidance in UI/docs for using peer groups as orgs.

### Phase 3 (later rename)
- Rename or refactor `peer_groups` after company adoption stabilizes.

## Test Plan

### Data/service tests
1. Create company and add members.
2. Enforce one-company-max for membership.
3. Validate CEO must be a member.
4. Block removing CEO until reassignment.
5. Company knowledge CRUD is properly scoped by company.

### Broker/tool tests
1. Member wallet call resolves to company identity and returns `identity_scope=company`.
2. Non-member wallet call is denied (company membership required).
3. Company-scoped write limits are shared across multiple members.
4. Disabled company tool group denies company operation.
5. Non-CEO member calling mutating `company_admin` operation is forbidden.
6. `shopify_*` call from non-member agent is denied.
7. `shopify_*` call from member agent without company Shopify connection is denied.
8. `shopify_*` call from member with configured company Shopify connection succeeds using company credentials.
9. Company-scoped pipeline step never dispatches to out-of-company agents, even when they match role/method.
10. Global pipeline behavior remains unchanged and can dispatch across companies.

### API handler tests
1. `/api/companies` CRUD happy paths and validation failures.
2. Conflict when member already belongs to another company.
3. Company knowledge CRUD endpoints work for valid company IDs.
4. `/api/agents/{agent_id}/company` returns correct company mapping.
5. `/api/companies/{id}/shopify` create/update/get/delete and connection-test endpoints work as expected.
6. `/api/pipelines` validation rejects `scope_mode=company` without `scope_company_id`.
7. `/api/pipelines` persists and returns `scope_mode` and `scope_company_id`.

### Integration tests
1. Pipeline step targeting a company child agent still executes.
2. Per-agent heartbeat to child still works.
3. Company heartbeat fan-out reaches intended members only.
4. Company-scoped pipeline runs only dispatch within selected company.
5. Company-scoped pipeline with no in-company eligible agent fails with explicit scope error.

## Public Interface Additions
- New data types:
  - `Company`
  - `CompanyMember`
  - `CompanyKnowledgeEntry`
  - `CompanyShopifyConnection`
- New manager routes under `/api/companies`.
- New company tool groups and tool names for admin, knowledge, finance, and commerce.
- Existing interfaces remain backward compatible (additive change set).

## Assumptions and Defaults
- One-company-max is enforced at both DB constraint and service validation layers.
- Wallet and Polymarket tools require company membership (no agent fallback).
- Shopify has no non-member fallback: all `shopify_*` execution is company-scoped.
- Pipeline default remains `scope_mode="global"` for backward compatibility.
- Company tools remain explicit and separately toggleable.
- No implicit merge of private and company knowledge in existing private tools.
- No generic company `state_json`; company-level tool state is persisted in typed domain tables keyed by `company_id`.
- `peer_groups` rename/refactor is intentionally deferred.
