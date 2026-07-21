#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
MANAGER_DIR="$REPO_ROOT/apps/agent_manager"
MCP_BROKER_DIR="$REPO_ROOT/apps/mcp-broker-server"
PROXY_SCRIPT="$REPO_ROOT/scripts/polymarket_proxy_tmux.sh"

USE_PROXY=1
SOCKS_PORT="${SOCKS_PORT:-1080}"
PROXY_SESSION="${POLYMARKET_PROXY_SESSION:-polymarket-proxy}"
PROXY_URL_DEFAULT="socks5://127.0.0.1:${SOCKS_PORT}"

sanitize_workspace_goflags() {
	local current="${GOFLAGS:-}"
	if [[ -z "$current" ]]; then
		current="$(go env GOFLAGS 2>/dev/null || true)"
	fi

	local -a cleaned=()
	local -a flags=()
	local removed_mod=0
	local i
	read -r -a flags <<<"$current"
	for ((i = 0; i < ${#flags[@]}; i++)); do
		case "${flags[$i]}" in
			-mod=mod)
				removed_mod=1
				continue
				;;
			-mod)
				if [[ "${flags[$((i + 1))]:-}" == "mod" ]]; then
					removed_mod=1
					((i++))
					continue
				fi
				;;
		esac
		cleaned+=("${flags[$i]}")
	done

	if [[ "$removed_mod" == "1" && ${#cleaned[@]} -eq 0 ]]; then
		cleaned=(-mod=readonly)
	fi
	export GOFLAGS="${cleaned[*]}"
}

if [[ "${1:-}" == "--no-proxy" ]]; then
	USE_PROXY=0
	shift
fi

proxy_check() {
	"$PROXY_SCRIPT" check --session "$PROXY_SESSION" --port "$SOCKS_PORT" >/dev/null
}

start_proxy_if_requested() {
	if [[ "$USE_PROXY" != "1" ]]; then
		return
	fi
	if [[ ! -x "$PROXY_SCRIPT" ]]; then
		echo "Proxy script not found or not executable: $PROXY_SCRIPT" >&2
		exit 1
	fi

	export POLYMARKET_PROXY_URL="${POLYMARKET_PROXY_URL:-$PROXY_URL_DEFAULT}"

	if command -v tmux >/dev/null 2>&1 && tmux has-session -t "$PROXY_SESSION" 2>/dev/null; then
		if proxy_check; then
			echo "Using existing proxy session: $PROXY_SESSION"
			return
		fi
		echo "Existing proxy session is unhealthy; restarting: $PROXY_SESSION"
		"$PROXY_SCRIPT" restart --session "$PROXY_SESSION" --port "$SOCKS_PORT"
		if ! proxy_check; then
			echo "Proxy session failed health check after restart: $PROXY_SESSION" >&2
			exit 1
		fi
		return
	fi

	echo "Starting Polymarket VPN proxy session: $PROXY_SESSION"
	"$PROXY_SCRIPT" start --session "$PROXY_SESSION" --port "$SOCKS_PORT"
	if ! proxy_check; then
		echo "Proxy session failed health check after start: $PROXY_SESSION" >&2
		exit 1
	fi
}

sanitize_workspace_goflags

echo "Building mcp-broker-server..."
(
	cd "$MCP_BROKER_DIR"
	go build -o "$MANAGER_DIR/mcp-broker-server" .
)

echo "Building agent_manager..."
(
	cd "$MANAGER_DIR"
	go build -o agent_manager .
)

start_proxy_if_requested

echo "Starting manager..."
cd "$MANAGER_DIR"
exec ./agent_manager "$@"
