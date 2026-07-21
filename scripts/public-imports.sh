#!/usr/bin/env bash
#
# public-imports.sh — list a module's subpackages and show which ones are
# imported from other workspace modules (i.e. from outside the module).
#
# Usage:
#   scripts/public-imports.sh <module-dir>
#
# Example:
#   scripts/public-imports.sh my
#   scripts/public-imports.sh apps/agent
#
# For each subpackage of the target module, prints the set of packages in
# other workspace modules (per go.work) that import it. Subpackages with no
# external importers are candidates for `internal/`.
#
set -euo pipefail

if [[ $# -ne 1 ]]; then
  cat >&2 <<EOF
usage: $0 <module-dir>

Lists the module's subpackages and which workspace packages outside the
module import each one.

examples:
  $0 my
  $0 apps/agent
EOF
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INPUT="${1%/}"
if [[ ! -d "${ROOT_DIR}/${INPUT}" ]]; then
  echo "no such directory: ${ROOT_DIR}/${INPUT}" >&2
  exit 1
fi
TARGET_DIR="$(cd "${ROOT_DIR}/${INPUT}" && pwd)"
MODULE_DIR="${TARGET_DIR#${ROOT_DIR}/}"

if [[ ! -f "${TARGET_DIR}/go.mod" ]]; then
  echo "no go.mod found at ${TARGET_DIR}" >&2
  exit 1
fi

TARGET_MODULE="$(awk '$1 == "module" { print $2; exit }' "${TARGET_DIR}/go.mod")"
if [[ -z "${TARGET_MODULE}" ]]; then
  echo "could not parse module path from ${TARGET_DIR}/go.mod" >&2
  exit 1
fi

declare -A PKG_NAME
TARGET_PKGS=()
while IFS=$'\t' read -r pkgpath pkgname; do
  [[ -z "${pkgpath}" ]] && continue
  TARGET_PKGS+=("${pkgpath}")
  PKG_NAME["${pkgpath}"]="${pkgname}"
done < <(cd "${TARGET_DIR}" && go list -f '{{.ImportPath}}	{{.Name}}' ./...)

if [[ ${#TARGET_PKGS[@]} -eq 0 ]]; then
  echo "no packages found in ${MODULE_DIR}" >&2
  exit 1
fi

declare -A IS_TARGET
for pkg in "${TARGET_PKGS[@]}"; do
  IS_TARGET["${pkg}"]=1
done

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
      if (line == "") next
      if (line == "use (") { in_use = 1; next }
      if (in_use && line == ")") { in_use = 0; next }
      if (in_use) {
        if (line ~ /^\.\//) print line
        next
      }
      if (line ~ /^use[[:space:]]+\.\//) {
        sub(/^use[[:space:]]+/, "", line)
        print line
      }
    }
  ' "${GO_WORK}"
)

declare -A IMPORTERS

TEMPLATE='{{ $ip := .ImportPath }}{{ range .Imports }}{{ $ip }}	{{ . }}
{{ end }}{{ range .TestImports }}{{ $ip }}	{{ . }}
{{ end }}{{ range .XTestImports }}{{ $ip }}	{{ . }}
{{ end }}'

for dir in "${MODULE_DIRS[@]}"; do
  ABS="${ROOT_DIR}/${dir#./}"
  [[ "${ABS}" == "${TARGET_DIR}" ]] && continue
  while IFS=$'\t' read -r importer imported; do
    [[ -z "${imported}" ]] && continue
    if [[ -n "${IS_TARGET[${imported}]+x}" ]]; then
      IMPORTERS["${imported}"]+="${importer}"$'\n'
    fi
  done < <(
    cd "${ABS}" && go list -e -f "${TEMPLATE}" ./... 2>/dev/null || true
  )
done

printf 'Module:    %s\n' "${TARGET_MODULE}"
printf 'Directory: %s\n' "${MODULE_DIR}"
printf 'Subpackages: %d\n\n' "${#TARGET_PKGS[@]}"

external_count=0
main_count=0
internal_candidates=()
for pkg in "${TARGET_PKGS[@]}"; do
  printf '%s\n' "${pkg}"
  importers="${IMPORTERS[${pkg}]:-}"
  is_main=0
  [[ "${PKG_NAME[${pkg}]:-}" == "main" ]] && is_main=1

  if [[ -n "${importers}" ]]; then
    printf '%s' "${importers}" | sort -u | sed 's/^/  /'
    external_count=$((external_count + 1))
  elif [[ ${is_main} -eq 1 ]]; then
    echo "  (main package — entrypoint, not importable)"
    main_count=$((main_count + 1))
  else
    echo "  (no external importers — candidate for internal/)"
    internal_candidates+=("${pkg}")
  fi
done

printf '\nSummary: %d/%d subpackages have external importers; %d main entrypoint(s); %d candidate(s) for internal/.\n' \
  "${external_count}" "${#TARGET_PKGS[@]}" "${main_count}" "${#internal_candidates[@]}"
