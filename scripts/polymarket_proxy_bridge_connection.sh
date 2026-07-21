#!/usr/bin/env bash
set -euo pipefail

SOCKS_PORT="${SOCKS_PORT:-1080}"
NAMESPACE_PATTERN="${NAMESPACE_PATTERN:-vo_pr}"

find_namespace() {
	ip netns list | awk -v pat="$NAMESPACE_PATTERN" '$1 ~ pat {print $1; exit}'
}

ns="$(find_namespace || true)"
if [[ -z "$ns" ]]; then
	echo "No namespace matching '${NAMESPACE_PATTERN}' is available" >&2
	exit 1
fi

if ip netns exec "$ns" true >/dev/null 2>&1; then
	exec ip netns exec "$ns" socat STDIO TCP:127.0.0.1:"$SOCKS_PORT"
fi

if command -v sudo >/dev/null 2>&1 && sudo -n ip netns exec "$ns" true >/dev/null 2>&1; then
	exec sudo -n ip netns exec "$ns" socat STDIO TCP:127.0.0.1:"$SOCKS_PORT"
fi

echo "Namespace '${ns}' is present but requires privileges for ip netns exec" >&2
exit 1
