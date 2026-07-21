#!/bin/sh
# Inject `replace` directives into every copied go.mod pointing sibling
# modules at their local source trees in /build. This lets Go resolve
# cross-module deps without contacting the private GitHub remote.
#
# Called from apps/agent/Dockerfile after source is copied to /build.
set -eu

REPO=github.com/original-david-knight/go_wild

# module_dir → module_path (must match `module` lines in the go.mod files)
modules="
agent_auth:${REPO}/agent_auth
agent_data:${REPO}/agent_data
agentic_loop:${REPO}/agentic_loop
claudellm:${REPO}/claudellm
crypto:${REPO}/crypto
data:${REPO}/data
knowledge_graph:${REPO}/knowledge_graph
my:${REPO}/my
tools:${REPO}/tools
"

# For each copied module (including apps/agent itself), rewrite every
# sibling require to a local-path replace.
for target_mod_dir in apps/agent agent_auth agent_data agentic_loop claudellm crypto data knowledge_graph my tools; do
    target_go_mod="/build/${target_mod_dir}/go.mod"
    [ -f "${target_go_mod}" ] || continue
    cd "/build/${target_mod_dir}"

    for entry in ${modules}; do
        dep_dir="${entry%%:*}"
        dep_path="${entry##*:}"

        # Skip self-replace
        case "${target_mod_dir}" in
            "${dep_dir}") continue ;;
        esac

        # Compute the relative path from the target module to the dep module.
        # apps/agent is two levels deep; everything else is one level deep.
        case "${target_mod_dir}" in
            apps/*) rel="../../${dep_dir}" ;;
            *)      rel="../${dep_dir}" ;;
        esac

        go mod edit -replace "${dep_path}=${rel}"
    done
done
