#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: $0 <go-subcommand> <packages-or-flags...>" >&2
  echo "example: $0 test ./..." >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GO_WORK="${ROOT_DIR}/go.work"

if [[ ! -f "${GO_WORK}" ]]; then
  echo "go.work not found at ${GO_WORK}" >&2
  exit 1
fi

mapfile -t MODULE_DIRS < <(
  awk '
    BEGIN { in_use = 0 }

    {
      line = $0
      sub(/\/\/.*/, "", line)
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", line)
      if (line == "") {
        next
      }
      if (line == "use (") {
        in_use = 1
        next
      }
      if (in_use && line == ")") {
        in_use = 0
        next
      }
      if (in_use) {
        if (line ~ /^\.\//) {
          print line
        }
        next
      }
      if (line ~ /^use[[:space:]]+\.\//) {
        sub(/^use[[:space:]]+/, "", line)
        print line
      }
    }
  ' "${GO_WORK}"
)

if [[ ${#MODULE_DIRS[@]} -eq 0 ]]; then
  echo "no workspace modules found in ${GO_WORK}" >&2
  exit 1
fi

for dir in "${MODULE_DIRS[@]}"; do
  label="${dir#./}"
  echo "==> ${label}: go $*"
  (
    cd "${ROOT_DIR}/${dir}"
    go "$@"
  )
done
