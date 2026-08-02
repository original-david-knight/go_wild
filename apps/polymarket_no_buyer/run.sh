#!/usr/bin/env bash
#
# Ensure the Polymarket VPN/SOCKS proxy is up, then start the NO-signal / YES-buyer app.
#
# All Polymarket API access (CLOB, Gamma market data, Data positions) must egress
# through the proxy — Polymarket is geo-restricted. The Polygon JSON-RPC and
# everything else go direct.
#
# Usage:
#   apps/polymarket_no_buyer/run.sh [app flags...]
# Examples:
#   apps/polymarket_no_buyer/run.sh --once --dry-run
#   apps/polymarket_no_buyer/run.sh --once
#   apps/polymarket_no_buyer/run.sh --schedule --interval 6h
#
# Env knobs:
#   PROXY_WAIT_SECS   seconds to wait for the proxy to connect (default 90)
#   CHECK_URL         proxy health-check URL (default https://clob.polymarket.com/time)

set -euo pipefail

APP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$APP_DIR/../.." && pwd)"
PROXY_TMUX="$REPO_ROOT/scripts/polymarket_proxy_tmux.sh"
CHECK_URL="${CHECK_URL:-https://clob.polymarket.com/time}"

need() { command -v "$1" >/dev/null 2>&1 || { echo "[run] missing required command: $1" >&2; exit 1; }; }
need curl
need go

# Resolve POLYMARKET_PROXY_URL: shell env, then the app .env, then the repo-root
# .env (the app loads both; the proxy is normally shared in the repo-root .env).
proxy_url="${POLYMARKET_PROXY_URL:-}"
if [[ -z "$proxy_url" ]]; then
	for f in "$APP_DIR/.env" "$REPO_ROOT/.env"; do
		if [[ -z "$proxy_url" && -f "$f" ]]; then
			proxy_url="$(sed -n 's/^[[:space:]]*POLYMARKET_PROXY_URL=//p' "$f" | tail -1 | tr -d "\"' " )"
		fi
	done
fi
if [[ -z "$proxy_url" ]]; then
	echo "[run] POLYMARKET_PROXY_URL is not set (shell, app .env, or repo-root .env)." >&2
	echo "[run] All Polymarket access must use the proxy — set it before running." >&2
	exit 1
fi

# host:port for the SOCKS proxy (strip the scheme and any embedded credentials).
hostport="${proxy_url#*://}"
hostport="${hostport##*@}"
socks_port="${hostport##*:}"

proxy_ok() { curl -fsS --max-time 12 --socks5-hostname "$hostport" "$CHECK_URL" >/dev/null 2>&1; }

echo "[run] Polymarket proxy = $hostport  (health check: $CHECK_URL)"
if proxy_ok; then
	echo "[run] proxy already up ✓"
else
	echo "[run] proxy is down — bringing it up via $PROXY_TMUX"
	[[ -x "$PROXY_TMUX" ]] || { echo "[run] proxy tool not found/executable: $PROXY_TMUX" >&2; exit 1; }
	# start (no-op if the tmux session already exists); else restart to recover a
	# session whose VPN dropped.
	"$PROXY_TMUX" start --port "$socks_port" 2>/dev/null || "$PROXY_TMUX" restart --port "$socks_port"

	wait_secs="${PROXY_WAIT_SECS:-90}"
	echo "[run] waiting up to ${wait_secs}s for the VPN/proxy to connect…"
	end=$((SECONDS + wait_secs))
	until proxy_ok; do
		if (( SECONDS >= end )); then
			echo "[run] ERROR: proxy did not come up. Inspect: $PROXY_TMUX status" >&2
			exit 1
		fi
		sleep 3
	done
	echo "[run] proxy up ✓"
fi

echo "[run] building and starting the app: $*"
cd "$APP_DIR"
go build -o ./polymarket_no_buyer .
exec ./polymarket_no_buyer "$@"
