#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT_PATH="$SCRIPT_DIR/$(basename "${BASH_SOURCE[0]}")"
BRIDGE_HELPER="$SCRIPT_DIR/polymarket_proxy_bridge_connection.sh"

SESSION_NAME="${SESSION_NAME:-polymarket-proxy}"
VPN_PROVIDER="${VPN_PROVIDER:-protonvpn}"
VPN_SERVER="${VPN_SERVER:-netherlands-nl}"
SOCKS_PORT="${SOCKS_PORT:-1080}"
LOCAL_BIND_IP="${LOCAL_BIND_IP:-127.0.0.1}"
NAMESPACE_PATTERN="${NAMESPACE_PATTERN:-vo_pr}"
CHECK_URL="${CHECK_URL:-https://clob.polymarket.com/time}"

usage() {
	cat <<'EOF'
Usage:
  scripts/polymarket_proxy_tmux.sh [command] [options]

Commands:
  start       Start tmux session with VPN SOCKS + host bridge (default)
  stop        Stop tmux session
  restart     Restart tmux session
  status      Show session/listener status
  attach      Attach to tmux session
  check       Test CLOB connectivity through local SOCKS bridge

Internal commands (used by tmux):
  run-vpn
  run-bridge

Options:
  --session NAME            tmux session name (default: polymarket-proxy)
  --provider NAME           vopono provider (default: protonvpn)
  --server NAME             VPN server name (default: netherlands-nl)
  --port PORT               SOCKS port (default: 1080)
  --local-bind-ip IP        Host bind IP for bridge (default: 127.0.0.1)
  --namespace-pattern REGEX Namespace pattern to match (default: vo_pr)
  --check-url URL           Health-check URL (default: https://clob.polymarket.com/time)
  -h, --help                Show this help

Examples:
  scripts/polymarket_proxy_tmux.sh start
  scripts/polymarket_proxy_tmux.sh restart --server us-new-york
  scripts/polymarket_proxy_tmux.sh check
EOF
}

log() {
	printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"
}

require_cmd() {
	local cmd="$1"
	if ! command -v "$cmd" >/dev/null 2>&1; then
		echo "Missing required command: $cmd" >&2
		exit 1
	fi
}

build_shell_cmd() {
	local out
	printf -v out '%q ' "$@"
	printf '%s' "${out% }"
}

find_namespace() {
	ip netns list | awk -v pat="$NAMESPACE_PATTERN" '$1 ~ pat {print $1; exit}'
}

run_vpn() {
	log "Starting VPN SOCKS in namespace via vopono: provider=$VPN_PROVIDER server=$VPN_SERVER port=$SOCKS_PORT"
	exec vopono exec \
		--provider "$VPN_PROVIDER" \
		--server "$VPN_SERVER" \
		--disable-ipv6 \
		--open-ports "$SOCKS_PORT" \
		"microsocks -i 0.0.0.0 -p $SOCKS_PORT"
}

run_bridge() {
	log "Starting host bridge on ${LOCAL_BIND_IP}:${SOCKS_PORT} (namespace pattern: $NAMESPACE_PATTERN)"
	while true; do
		local ns
		ns="$(find_namespace || true)"
		if [[ -z "$ns" ]]; then
			log "No namespace matching '$NAMESPACE_PATTERN' yet; retrying in 2s"
			sleep 2
			continue
		fi

		log "Bridging ${LOCAL_BIND_IP}:${SOCKS_PORT} with dynamic namespace lookup (current: ${ns})"
		if ! socat \
			"TCP-LISTEN:${SOCKS_PORT},fork,reuseaddr,bind=${LOCAL_BIND_IP}" \
			"EXEC:${BRIDGE_HELPER}"; then
			log "Bridge exited; retrying in 1s"
			sleep 1
		fi
	done
}

check_proxy() {
	curl -fsS --max-time 12 --socks5-hostname "${LOCAL_BIND_IP}:${SOCKS_PORT}" "$CHECK_URL"
}

start_session() {
	require_cmd tmux
	require_cmd vopono
	require_cmd socat
	require_cmd ip
	require_cmd curl
	if [[ ! -x "$BRIDGE_HELPER" ]]; then
		echo "Bridge helper not found or not executable: $BRIDGE_HELPER" >&2
		exit 1
	fi

	if tmux has-session -t "$SESSION_NAME" 2>/dev/null; then
		echo "tmux session '$SESSION_NAME' already exists" >&2
		echo "Use: $SCRIPT_PATH attach --session $SESSION_NAME" >&2
		exit 1
	fi

	local -a envs=(
		"SESSION_NAME=$SESSION_NAME"
		"VPN_PROVIDER=$VPN_PROVIDER"
		"VPN_SERVER=$VPN_SERVER"
		"SOCKS_PORT=$SOCKS_PORT"
		"LOCAL_BIND_IP=$LOCAL_BIND_IP"
		"NAMESPACE_PATTERN=$NAMESPACE_PATTERN"
		"CHECK_URL=$CHECK_URL"
	)

	local vpn_cmd bridge_cmd
	vpn_cmd="$(build_shell_cmd env "${envs[@]}" "$SCRIPT_PATH" run-vpn)"
	bridge_cmd="$(build_shell_cmd env "${envs[@]}" "$SCRIPT_PATH" run-bridge)"

	tmux new-session -d -s "$SESSION_NAME" -n vpn "$vpn_cmd"
	tmux new-window -t "$SESSION_NAME" -n bridge "$bridge_cmd"

	log "Started tmux session: $SESSION_NAME"
	log "Attach: tmux attach -t $SESSION_NAME"
	log "Check:  $SCRIPT_PATH check --session $SESSION_NAME --port $SOCKS_PORT"
}

stop_session() {
	if tmux has-session -t "$SESSION_NAME" 2>/dev/null; then
		tmux kill-session -t "$SESSION_NAME"
		log "Stopped tmux session: $SESSION_NAME"
	else
		log "Session '$SESSION_NAME' not running"
	fi
}

status_session() {
	if tmux has-session -t "$SESSION_NAME" 2>/dev/null; then
		echo "tmux session: running ($SESSION_NAME)"
		tmux list-windows -t "$SESSION_NAME"
	else
		echo "tmux session: not running ($SESSION_NAME)"
	fi

	if ss -ltn 2>/dev/null | grep -qE "[.:]${SOCKS_PORT}[[:space:]]"; then
		echo "listener: present on ${LOCAL_BIND_IP}:${SOCKS_PORT} (or another interface)"
	else
		echo "listener: missing on port ${SOCKS_PORT}"
	fi

	if check_proxy >/dev/null 2>&1; then
		echo "proxy check: ok (${CHECK_URL})"
	else
		echo "proxy check: failed (${CHECK_URL})"
	fi
}

attach_session() {
	exec tmux attach -t "$SESSION_NAME"
}

COMMAND="start"
if [[ $# -gt 0 && "$1" != -* ]]; then
	COMMAND="$1"
	shift
fi

while [[ $# -gt 0 ]]; do
	case "$1" in
		--session)
			SESSION_NAME="$2"
			shift 2
			;;
		--provider)
			VPN_PROVIDER="$2"
			shift 2
			;;
		--server)
			VPN_SERVER="$2"
			shift 2
			;;
		--port)
			SOCKS_PORT="$2"
			shift 2
			;;
		--local-bind-ip)
			LOCAL_BIND_IP="$2"
			shift 2
			;;
		--namespace-pattern)
			NAMESPACE_PATTERN="$2"
			shift 2
			;;
		--check-url)
			CHECK_URL="$2"
			shift 2
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			echo "Unknown argument: $1" >&2
			usage
			exit 1
			;;
	esac
done

case "$COMMAND" in
	start)
		start_session
		;;
	stop)
		stop_session
		;;
	restart)
		stop_session
		start_session
		;;
	status)
		status_session
		;;
	attach)
		attach_session
		;;
	check)
		check_proxy
		echo
		;;
	run-vpn)
		run_vpn
		;;
	run-bridge)
		run_bridge
		;;
	*)
		echo "Unknown command: $COMMAND" >&2
		usage
		exit 1
		;;
esac
