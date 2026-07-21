#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_FILE="${ROOT_DIR}/docs/module-deps.md"
README_FILE="${ROOT_DIR}/README.md"
MERMAID="$("${ROOT_DIR}/scripts/module_deps.py")"

cat > "${OUT_FILE}" <<'EOF'
# Module Dependency Diagram

Generated from `go.mod` files (direct module dependencies only). Excludes `vendor/` and `examples/`.

```mermaid
EOF
printf "%s" "${MERMAID}" >> "${OUT_FILE}"
printf "\n" >> "${OUT_FILE}"
cat >> "${OUT_FILE}" <<'EOF'
```

Regenerate with:
`./scripts/gen_module_deps.sh`
EOF

ROOT_DIR="${ROOT_DIR}" python3 - <<'PY'
import pathlib
import sys
import os

root_dir = pathlib.Path(os.environ["ROOT_DIR"])
readme_path = root_dir / "README.md"
mermaid_path = root_dir / "docs" / "module-deps.md"

start = "<!-- MODULE_DEPS_START -->"
end = "<!-- MODULE_DEPS_END -->"

readme = readme_path.read_text()
if start not in readme or end not in readme:
    sys.stderr.write("README.md is missing MODULE_DEPS markers.\n")
    sys.exit(1)

mermaid = mermaid_path.read_text()
block_start = mermaid.find("```mermaid")
block_end = mermaid.find("```", block_start + 1)
if block_start == -1 or block_end == -1:
    sys.stderr.write("docs/module-deps.md does not contain a mermaid block.\n")
    sys.exit(1)

mermaid_block = mermaid[block_start:block_end].splitlines()
mermaid_body = "\n".join(mermaid_block[1:]) + "\n"

before, rest = readme.split(start, 1)
_, after = rest.split(end, 1)
replacement = (
    f"{start}\n"
    "```mermaid\n"
    f"{mermaid_body}"
    "```\n"
    f"{end}"
)
readme_path.write_text(before + replacement + after)
PY
