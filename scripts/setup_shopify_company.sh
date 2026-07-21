#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8888}"
COMPANY_NAME="${COMPANY_NAME:-Shopify Storefront Co}"
COMPANY_DESCRIPTION="${COMPANY_DESCRIPTION:-Autonomous drop-shipping storefront operated by specialized agents.}"
COMPANY_CEO_AGENT_ID="${COMPANY_CEO_AGENT_ID:-scout}"
TARGET_COMPANY_ID="${TARGET_COMPANY_ID:-}"

DEFAULT_MODEL="${DEFAULT_MODEL:-gemini-3-flash-preview}"
DEFAULT_SMART_MODEL="${DEFAULT_SMART_MODEL:-gemini-3-flash-preview}"
DEFAULT_MAX_TURNS="${DEFAULT_MAX_TURNS:-12}"
DEFAULT_WORK_TASKS_TIMEOUT="${DEFAULT_WORK_TASKS_TIMEOUT:-15m}"
WORKER_CONTEXT_MODE="${WORKER_CONTEXT_MODE:-persistent}"
AUTO_START_AGENTS="${AUTO_START_AGENTS:-false}"

SHOPIFY_SHOP_URL="${SHOPIFY_SHOP_URL:-${SHOPIFY_STORE_URL:-}}"
SHOPIFY_CLIENT_ID="${SHOPIFY_CLIENT_ID:-}"
SHOPIFY_CLIENT_SECRET="${SHOPIFY_CLIENT_SECRET:-}"
SHOPIFY_API_VERSION="${SHOPIFY_API_VERSION:-2025-01}"
SKIP_SHOPIFY_TEST="${SKIP_SHOPIFY_TEST:-0}"
TOPDAWG_API_KEY="${TOPDAWG_API_KEY:-}"
TOPDAWG_SUPPLIER_ID="${TOPDAWG_SUPPLIER_ID:-}"
SKIP_TOPDAWG_TEST="${SKIP_TOPDAWG_TEST:-0}"

HTTP_STATUS=""
HTTP_BODY=""
COMPANY_ID=""

usage() {
	cat <<'EOF'
Usage: setup_shopify_company.sh [options]

Bootstraps a Shopify company with the v1 agent team:
  scout, curator, lister, fulfiller

Options:
  --base-url URL              Manager API base URL (default: http://localhost:8888)
  --company-name NAME         Company name
  --company-id ID            Existing company ID to target directly
  --company-description TEXT  Company description
  --ceo-agent-id ID           CEO agent ID (default: scout)
  --shop-url URL              Shopify store URL (example: my-store.myshopify.com)
  --shopify-client-id ID      Shopify app client ID (Dev Dashboard)
  --shopify-client-secret SEC Shopify app client secret (Dev Dashboard)
  --shopify-api-version VER   Shopify API version (default: 2025-01)
  --topdawg-api-key KEY       TopDawg API key
  --topdawg-supplier-id ID    TopDawg supplier ID
  --worker-context MODE       Worker context mode: stateless|persistent
  --auto-start BOOL           Auto-start agents: true|false
  --no-shopify-test           Skip POST /api/companies/{id}/shopify/test
  --no-topdawg-test           Skip POST /api/companies/{id}/topdawg/test
  -h, --help                  Show this help
EOF
}

log() {
	printf '[setup] %s\n' "$*"
}

fail() {
	printf '[setup] ERROR: %s\n' "$*" >&2
	exit 1
}

need_cmd() {
	local cmd="$1"
	command -v "$cmd" >/dev/null 2>&1 || fail "required command not found: $cmd"
}

http_json() {
	local method="$1"
	local path="$2"
	local payload="${3-}"
	local url="${BASE_URL%/}${path}"
	local response

	if [[ -n "$payload" ]]; then
		response="$(curl -sS -X "$method" "$url" -H 'Content-Type: application/json' -d "$payload" -w $'\n%{http_code}')"
	else
		response="$(curl -sS -X "$method" "$url" -w $'\n%{http_code}')"
	fi
	HTTP_STATUS="${response##*$'\n'}"
	HTTP_BODY="${response%$'\n'*}"
}

status_is_one_of() {
	local status="$1"
	shift
	local expected
	for expected in "$@"; do
		if [[ "$status" == "$expected" ]]; then
			return 0
		fi
	done
	return 1
}

require_status() {
	local context="$1"
	shift
	if ! status_is_one_of "$HTTP_STATUS" "$@"; then
		fail "$context failed (HTTP $HTTP_STATUS): $HTTP_BODY"
	fi
}

agent_tools_json() {
	local agent_id="$1"
	case "$agent_id" in
	scout) echo '["web_search","supplier","a2a","tasks","company_knowledge"]' ;;
	curator) echo '["shopify","a2a","tasks","company_knowledge","company_commerce"]' ;;
	lister) echo '["shopify","supplier","a2a","tasks","company_knowledge","company_commerce"]' ;;
	fulfiller) echo '["shopify","supplier","a2a","tasks","company_knowledge","company_commerce"]' ;;
	*) fail "unknown agent_id for tools: $agent_id" ;;
	esac
}

agent_heartbeat() {
	local agent_id="$1"
	case "$agent_id" in
	scout) echo "1h" ;;
	curator) echo "30s" ;;
	lister) echo "30s" ;;
	fulfiller) echo "10s" ;;
	*) fail "unknown agent_id for heartbeat: $agent_id" ;;
	esac
}

agent_role() {
	local agent_id="$1"
	case "$agent_id" in
	scout) echo "scout" ;;
	curator) echo "curator" ;;
	lister) echo "lister" ;;
	fulfiller) echo "fulfiller" ;;
	*) fail "unknown agent_id for role: $agent_id" ;;
	esac
}

agent_display_name() {
	local agent_id="$1"
	case "$agent_id" in
	scout) echo "Scout" ;;
	curator) echo "Curator" ;;
	lister) echo "Lister" ;;
	fulfiller) echo "Fulfiller" ;;
	*) fail "unknown agent_id for display name: $agent_id" ;;
	esac
}

agent_system_prompt() {
	local agent_id="$1"
	case "$agent_id" in
	scout)
		cat <<'EOF'
You are the Product Scout for a Shopify drop-shipping business.
Find promising products using supplier and market signals, prioritize clear demand and margin potential, and package recommendations for curator review.
When reporting outputs, keep candidate data structured and action-ready.
EOF
		;;
	curator)
		cat <<'EOF'
You are the Curator for a Shopify drop-shipping business.
Evaluate product candidates for brand fit, expected conversion potential, and operational risk.
Approve only high-conviction listings and provide concise rationale for each decision.
EOF
		;;
	lister)
		cat <<'EOF'
You are the Listing Agent for a Shopify drop-shipping business.
Create and update Shopify listings from approved candidates, including title, description, images, and merchandising fields.
Focus on clean product data, policy-safe copy, and high conversion quality.
EOF
		;;
	fulfiller)
		cat <<'EOF'
You are the Fulfillment Agent for a Shopify drop-shipping business.
Process incoming orders quickly, place supplier orders, and keep Shopify order state accurate with tracking and references.
Escalate ambiguous or risky fulfillment cases immediately.
EOF
		;;
	*) fail "unknown agent_id for prompt: $agent_id" ;;
	esac
}

ensure_company() {
	if [[ -n "$TARGET_COMPANY_ID" ]]; then
		http_json GET "/api/companies/${TARGET_COMPANY_ID}"
		require_status "get company ${TARGET_COMPANY_ID}" 200
		COMPANY_ID="$TARGET_COMPANY_ID"
		local fetched_name
		fetched_name="$(echo "$HTTP_BODY" | jq -r '.name // empty')"
		if [[ -n "$fetched_name" ]]; then
			COMPANY_NAME="$fetched_name"
		fi
		log "Using specified company ID ($COMPANY_ID)"
		return
	fi

	http_json GET "/api/companies"
	require_status "list companies" 200

	local existing_id
	existing_id="$(echo "$HTTP_BODY" | jq -r --arg name "$COMPANY_NAME" '.companies[]? | select((.name | ascii_downcase) == ($name | ascii_downcase)) | .id' | head -n1)"
	if [[ -n "$existing_id" ]]; then
		COMPANY_ID="$existing_id"
		log "Using existing company \"$COMPANY_NAME\" ($COMPANY_ID)"
		return
	fi

	local payload
	payload="$(jq -cn --arg name "$COMPANY_NAME" --arg description "$COMPANY_DESCRIPTION" '{name: $name, description: $description}')"
	http_json POST "/api/companies" "$payload"
	require_status "create company" 201
	COMPANY_ID="$(echo "$HTTP_BODY" | jq -r '.id')"
	[[ -n "$COMPANY_ID" && "$COMPANY_ID" != "null" ]] || fail "create company succeeded but id missing in response: $HTTP_BODY"
	log "Created company \"$COMPANY_NAME\" ($COMPANY_ID)"
}

ensure_agent_exists() {
	local agent_id="$1"
	local display_name="$2"

	http_json GET "/api/agents/$agent_id"
	if [[ "$HTTP_STATUS" == "200" ]]; then
		log "Agent $agent_id already exists"
		return
	fi
	if [[ "$HTTP_STATUS" != "404" ]]; then
		fail "load agent $agent_id failed (HTTP $HTTP_STATUS): $HTTP_BODY"
	fi

	local payload
	payload="$(jq -cn --arg name "$display_name" '{name: $name}')"
	http_json POST "/api/agents" "$payload"
	require_status "create agent $agent_id" 201
	log "Created agent $agent_id"
}

configure_agent() {
	local agent_id="$1"
	local heartbeat="$2"
	local tools_json="$3"
	local prompt="$4"

	http_json GET "/api/agents/$agent_id"
	require_status "load agent for update $agent_id" 200
	local current="$HTTP_BODY"

	local payload
	payload="$(
		echo "$current" | jq -c \
			--arg model "$DEFAULT_MODEL" \
			--arg smart_model "$DEFAULT_SMART_MODEL" \
			--arg mode "worker" \
			--arg worker_context_mode "$WORKER_CONTEXT_MODE" \
			--arg heartbeat "$heartbeat" \
			--arg work_tasks_timeout "$DEFAULT_WORK_TASKS_TIMEOUT" \
			--arg prompt "$prompt" \
			--argjson max_turns "$DEFAULT_MAX_TURNS" \
			--argjson auto_start "$AUTO_START_AGENTS" \
			--argjson enabled_tools "$tools_json" \
			'{
				model: (if (.model // "") != "" then .model else $model end),
				smart_model: (if (.smart_model // "") != "" then .smart_model else $smart_model end),
				smart_default: (.smart_default // false),
				mode: $mode,
				worker_context_mode: $worker_context_mode,
				max_turns: (if (.max_turns // 0) > 0 then .max_turns else $max_turns end),
				heartbeat: $heartbeat,
				work_tasks_timeout: (if (.work_tasks_timeout // "") != "" then .work_tasks_timeout else $work_tasks_timeout end),
				env_vars: (.env_vars // {}),
				memory_limit: (.memory_limit // ""),
				cpu_limit: (.cpu_limit // ""),
				auto_start: $auto_start,
				enabled_tools: $enabled_tools,
				system_prompt: $prompt
			}'
	)"

	http_json PUT "/api/agents/$agent_id" "$payload"
	require_status "update agent $agent_id" 200
	log "Configured agent $agent_id (worker, heartbeat $heartbeat)"
}

ensure_company_membership() {
	local company_id="$1"
	local agent_id="$2"
	local role="$3"
	local payload
	payload="$(jq -cn --arg agent_id "$agent_id" --arg role "$role" '{agent_id: $agent_id, role: $role}')"

	http_json POST "/api/companies/$company_id/members" "$payload"
	if [[ "$HTTP_STATUS" == "200" ]]; then
		log "Ensured company membership: $agent_id ($role)"
		return
	fi
	if [[ "$HTTP_STATUS" == "409" ]]; then
		fail "agent $agent_id belongs to another company; cannot add to $company_id: $HTTP_BODY"
	fi
	fail "add member $agent_id failed (HTTP $HTTP_STATUS): $HTTP_BODY"
}

set_company_ceo() {
	local company_id="$1"
	local ceo_agent_id="$2"
	local payload
	payload="$(jq -cn --arg agent_id "$ceo_agent_id" '{agent_id: $agent_id}')"

	http_json PUT "/api/companies/$company_id/ceo" "$payload"
	require_status "set company CEO $ceo_agent_id" 200
	log "Set company CEO: $ceo_agent_id"
}

ensure_a2a_method() {
	local method="$1"
	local description="$2"
	local instructions="$3"
	local input_schema="${4:-}"
	local output_schema="${5:-}"

	# Build JSON payload with optional schemas.
	local base_payload
	base_payload="$(jq -cn --arg method "$method" --arg description "$description" --arg instructions "$instructions" '{method: $method, description: $description, instructions: $instructions}')"
	if [[ -n "$input_schema" ]]; then
		base_payload="$(echo "$base_payload" | jq --argjson s "$input_schema" '. + {input_schema: $s}')"
	fi
	if [[ -n "$output_schema" ]]; then
		base_payload="$(echo "$base_payload" | jq --argjson s "$output_schema" '. + {output_schema: $s}')"
	fi

	http_json GET "/api/a2a-methods/$method"
	if [[ "$HTTP_STATUS" == "404" ]]; then
		http_json POST "/api/a2a-methods" "$base_payload"
		if [[ "$HTTP_STATUS" == "201" ]]; then
			log "Created A2A method: $method"
			return
		fi
		if [[ "$HTTP_STATUS" == "409" ]]; then
			log "A2A method already exists (race): $method"
			return
		fi
		fail "create A2A method $method failed (HTTP $HTTP_STATUS): $HTTP_BODY"
	fi
	require_status "load A2A method $method" 200

	# For update, omit method field from payload.
	local update_payload
	update_payload="$(echo "$base_payload" | jq 'del(.method)')"
	http_json PUT "/api/a2a-methods/$method" "$update_payload"
	require_status "update A2A method $method" 200
	log "Updated A2A method: $method"
}

ensure_capability() {
	local agent_id="$1"
	local role="$2"
	local method="$3"

	http_json GET "/api/agents/$agent_id/capabilities"
	require_status "list capabilities for $agent_id" 200
	if echo "$HTTP_BODY" | jq -e --arg role "$role" --arg method "$method" '.capabilities[]? | select(.role == $role and .method == $method)' >/dev/null; then
		log "Capability already present for $agent_id: $role/$method"
		return
	fi

	local payload
	payload="$(jq -cn --arg role "$role" --arg method "$method" '{role: $role, method: $method}')"
	http_json POST "/api/agents/$agent_id/capabilities" "$payload"
	if [[ "$HTTP_STATUS" == "201" ]]; then
		log "Added capability for $agent_id: $role/$method"
		return
	fi
	if [[ "$HTTP_STATUS" == "409" ]]; then
		log "Capability already exists (race) for $agent_id: $role/$method"
		return
	fi
	fail "add capability $role/$method for $agent_id failed (HTTP $HTTP_STATUS): $HTTP_BODY"
}

configure_shopify_connection_if_provided() {
	if [[ -z "$SHOPIFY_SHOP_URL" ]]; then
		log "Skipping Shopify connection (set SHOPIFY_SHOP_URL plus client credentials)"
		return
	fi

	if [[ -z "$SHOPIFY_CLIENT_ID" || -z "$SHOPIFY_CLIENT_SECRET" ]]; then
		log "Skipping Shopify connection (set SHOPIFY_CLIENT_ID+SHOPIFY_CLIENT_SECRET)"
		return
	fi

	log "Configuring Shopify with client credentials (manager will exchange short-lived tokens automatically)"

	local payload
	payload="$(jq -cn \
		--arg shop_url "$SHOPIFY_SHOP_URL" \
		--arg api_version "$SHOPIFY_API_VERSION" \
		--arg client_id "$SHOPIFY_CLIENT_ID" \
		--arg client_secret "$SHOPIFY_CLIENT_SECRET" \
		'{shop_url: $shop_url, api_version: $api_version, client_id: $client_id, client_secret: $client_secret, enabled: true}')"

	http_json PUT "/api/companies/$COMPANY_ID/shopify" "$payload"
	require_status "upsert Shopify connection" 200
	log "Configured company Shopify connection for $SHOPIFY_SHOP_URL"

	if [[ "$SKIP_SHOPIFY_TEST" == "1" ]]; then
		log "Skipped Shopify connection test (--no-shopify-test)"
		return
	fi

	http_json POST "/api/companies/$COMPANY_ID/shopify/test"
	require_status "test Shopify connection" 200
	log "Shopify test call succeeded"
}

configure_topdawg_connection_if_provided() {
	if [[ -z "$TOPDAWG_API_KEY" || -z "$TOPDAWG_SUPPLIER_ID" ]]; then
		log "Skipping TopDawg connection (set TOPDAWG_API_KEY+TOPDAWG_SUPPLIER_ID)"
		return
	fi

	log "Configuring TopDawg supplier connection"

	local payload
	payload="$(jq -cn \
		--arg api_key "$TOPDAWG_API_KEY" \
		--arg supplier_id "$TOPDAWG_SUPPLIER_ID" \
		'{api_key: $api_key, supplier_id: $supplier_id, enabled: true}')"

	http_json PUT "/api/companies/$COMPANY_ID/topdawg" "$payload"
	require_status "upsert TopDawg connection" 200
	log "Configured company TopDawg connection for supplier $TOPDAWG_SUPPLIER_ID"

	if [[ "$SKIP_TOPDAWG_TEST" == "1" ]]; then
		log "Skipped TopDawg connection test (--no-topdawg-test)"
		return
	fi

	http_json POST "/api/companies/$COMPANY_ID/topdawg/test"
	require_status "test TopDawg connection" 200
	log "TopDawg test call succeeded"
}

validate_inputs() {
	need_cmd curl
	need_cmd jq

	case "$WORKER_CONTEXT_MODE" in
	stateless | persistent) ;;
	*) fail "WORKER_CONTEXT_MODE must be stateless or persistent (got: $WORKER_CONTEXT_MODE)" ;;
	esac

	case "$AUTO_START_AGENTS" in
	true | false) ;;
	*) fail "AUTO_START_AGENTS must be true or false (got: $AUTO_START_AGENTS)" ;;
	esac
}

parse_args() {
	while [[ $# -gt 0 ]]; do
		case "$1" in
		--base-url)
			BASE_URL="$2"
			shift 2
			;;
		--company-name)
			COMPANY_NAME="$2"
			shift 2
			;;
		--company-id)
			TARGET_COMPANY_ID="$2"
			shift 2
			;;
		--company-description)
			COMPANY_DESCRIPTION="$2"
			shift 2
			;;
		--ceo-agent-id)
			COMPANY_CEO_AGENT_ID="$2"
			shift 2
			;;
		--shop-url)
			SHOPIFY_SHOP_URL="$2"
			shift 2
			;;
		--shopify-client-id)
			SHOPIFY_CLIENT_ID="$2"
			shift 2
			;;
		--shopify-client-secret)
			SHOPIFY_CLIENT_SECRET="$2"
			shift 2
			;;
		--shopify-webhook-secret)
			log "--shopify-webhook-secret is deprecated and ignored"
			shift 2
			;;
		--shopify-api-version)
			SHOPIFY_API_VERSION="$2"
			shift 2
			;;
		--topdawg-api-key)
			TOPDAWG_API_KEY="$2"
			shift 2
			;;
		--topdawg-supplier-id)
			TOPDAWG_SUPPLIER_ID="$2"
			shift 2
			;;
		--worker-context)
			WORKER_CONTEXT_MODE="$2"
			shift 2
			;;
		--auto-start)
			AUTO_START_AGENTS="$2"
			shift 2
			;;
		--no-shopify-test)
			SKIP_SHOPIFY_TEST=1
			shift
			;;
		--no-topdawg-test)
			SKIP_TOPDAWG_TEST=1
			shift
			;;
		-h | --help)
			usage
			exit 0
			;;
		*)
			usage
			fail "unknown argument: $1"
			;;
		esac
	done
}

main() {
	parse_args "$@"
	validate_inputs

	log "Using manager API: ${BASE_URL%/}"
	http_json GET "/health"
	require_status "manager health check" 200

	ensure_company

	local agent_id display_name role heartbeat tools prompt
	for agent_id in scout curator lister fulfiller; do
		display_name="$(agent_display_name "$agent_id")"
		role="$(agent_role "$agent_id")"
		heartbeat="$(agent_heartbeat "$agent_id")"
		tools="$(agent_tools_json "$agent_id")"
		prompt="$(agent_system_prompt "$agent_id")"

		ensure_agent_exists "$agent_id" "$display_name"
		configure_agent "$agent_id" "$heartbeat" "$tools" "$prompt"
		ensure_company_membership "$COMPANY_ID" "$agent_id" "$role"
	done

	set_company_ceo "$COMPANY_ID" "$COMPANY_CEO_AGENT_ID"

	ensure_a2a_method "find_product_candidates" \
		"Discover candidate products for drop-shipping." \
		"Return a shortlist of candidate products with supplier context and confidence." \
		'{"type":"object","properties":{"category":{"type":"string","description":"Product category or niche to search"},"max_results":{"type":"integer","description":"Maximum candidates to return"}}}'

	ensure_a2a_method "review_product_candidate" \
		"Curate a single candidate for listing decision." \
		"Approve or reject candidate with concise reasoning and actionable feedback." \
		'{"type":"object","required":["candidate"],"properties":{"candidate":{"type":"object","description":"Product candidate from scout including supplier details, pricing, and images"}}}' \
		'{"type":"object","required":["decision"],"properties":{"decision":{"type":"string","enum":["approved","rejected"],"description":"Approval decision"},"reasoning":{"type":"string","description":"Concise reasoning for the decision"},"product":{"type":"object","description":"Enriched product data (when approved)"},"supplier_id":{"type":"string","description":"Supplier product ID (when approved)"},"target_price":{"type":"number","description":"Recommended retail price (when approved)"}}}'

	ensure_a2a_method "create_listing" \
		"Create a Shopify listing from an approved candidate." \
		"Create or update product listing in Shopify with required merchandising fields." \
		'{"type":"object","required":["product"],"properties":{"product":{"type":"object","description":"Product data from curator approval"},"supplier_id":{"type":"string","description":"Supplier product ID"},"target_price":{"type":"number","description":"Target retail price"}}}' \
		'{"type":"object","required":["shopify_product_id"],"properties":{"shopify_product_id":{"type":"string","description":"Created Shopify product ID"},"title":{"type":"string","description":"Product title as listed"}}}'

	ensure_a2a_method "fulfill_order" \
		"Fulfill a Shopify order through supplier ordering." \
		"Place supplier order, attach tracking, and synchronize fulfillment state." \
		'{"type":"object","required":["order"],"properties":{"order":{"type":"object","description":"Shopify order data including items and shipping address"}}}' \
		'{"type":"object","required":["supplier_order_id"],"properties":{"supplier_order_id":{"type":"string","description":"Supplier order ID"},"estimated_delivery":{"type":"string","description":"Estimated delivery date"}}}'

	ensure_capability "scout" "scout" "find_product_candidates"
	ensure_capability "curator" "curator" "review_product_candidate"
	ensure_capability "lister" "lister" "create_listing"
	ensure_capability "fulfiller" "fulfiller" "fulfill_order"

	configure_shopify_connection_if_provided
	configure_topdawg_connection_if_provided

	log "Setup complete."
	log "Company: $COMPANY_NAME ($COMPANY_ID)"
	log "Agents: scout, curator, lister, fulfiller"
}

main "$@"
