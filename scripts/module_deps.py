#!/usr/bin/env python3
from __future__ import annotations

import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]


def parse_workspace_module_dirs(root: pathlib.Path) -> list[pathlib.Path]:
    go_work = root / "go.work"
    if not go_work.exists():
        raise FileNotFoundError("go.work not found")

    dirs: list[pathlib.Path] = []
    in_use_block = False
    for raw in go_work.read_text().splitlines():
        line = raw.strip()
        if not line or line.startswith("//"):
            continue
        if line.startswith("use ("):
            in_use_block = True
            continue
        if in_use_block and line == ")":
            in_use_block = False
            continue
        if line.startswith("use "):
            line = line[len("use ") :].strip()
        elif not in_use_block:
            continue

        if not line.startswith("./"):
            continue
        dirs.append((root / line).resolve())
    return sorted(set(dirs))


def parse_module_path(go_mod_text: str) -> str:
    match = re.search(r"^module\s+(\S+)", go_mod_text, re.M)
    if not match:
        raise ValueError("missing module declaration")
    return match.group(1)


def parse_direct_requirements(go_mod_text: str) -> list[str]:
    requires: list[str] = []
    in_require_block = False

    for raw in go_mod_text.splitlines():
        line = raw.strip()
        if not line or line.startswith("//"):
            continue
        if line.startswith("require ("):
            in_require_block = True
            continue
        if in_require_block and line == ")":
            in_require_block = False
            continue
        if line.startswith("require "):
            line = line[len("require ") :].strip()
        elif not in_require_block:
            continue

        comment = ""
        if "//" in line:
            line, comment = line.split("//", 1)
        if "indirect" in comment:
            continue

        parts = line.split()
        if not parts:
            continue
        requires.append(parts[0])

    return requires


def find_workspace_deps(root: pathlib.Path) -> dict[str, list[str]]:
    module_dirs = parse_workspace_module_dirs(root)
    module_path_to_name: dict[str, str] = {}
    module_go_mods: dict[str, pathlib.Path] = {}

    for module_dir in module_dirs:
        go_mod = module_dir / "go.mod"
        if not go_mod.exists():
            continue
        module_path = parse_module_path(go_mod.read_text())
        module_name = module_path.split("/")[-1]
        module_path_to_name[module_path] = module_name
        module_go_mods[module_name] = go_mod

    graph: dict[str, list[str]] = {}
    for module_name, go_mod in module_go_mods.items():
        requires = parse_direct_requirements(go_mod.read_text())
        deps = {
            module_path_to_name[req]
            for req in requires
            if req in module_path_to_name and module_path_to_name[req] != module_name
        }
        graph[module_name] = sorted(deps)

    return graph


def render_mermaid(modules: dict[str, list[str]]) -> str:
    lines = ["flowchart LR"]
    for mod in sorted(modules):
        deps = modules[mod]
        if not deps:
            lines.append(f'  {mod}["{mod}"]')
            continue
        for dep in deps:
            lines.append(f"  {mod} --> {dep}")
    return "\n".join(lines) + "\n"


def main() -> int:
    modules = find_workspace_deps(ROOT)
    sys.stdout.write(render_mermaid(modules))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
