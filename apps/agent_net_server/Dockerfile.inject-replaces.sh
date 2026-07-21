#!/bin/sh
# Inject `replace` directives into every copied go.mod pointing sibling
# modules at their local source trees in /app. Lets Go resolve cross-module
# deps without contacting the private GitHub remote.
#
# Called from apps/agent_net_server/Dockerfile after source is copied.
set -eu

REPO=github.com/original-david-knight/go_wild

modules="
agent_data:${REPO}/agent_data
agent_net:${REPO}/agent_net
agentic_loop:${REPO}/agentic_loop
data:${REPO}/data
knowledge_graph:${REPO}/knowledge_graph
my:${REPO}/my
"

for target_mod_dir in apps/agent_net_server agent_data agent_net agentic_loop data knowledge_graph my; do
    target_go_mod="/app/${target_mod_dir}/go.mod"
    [ -f "${target_go_mod}" ] || continue
    cd "/app/${target_mod_dir}"

    for entry in ${modules}; do
        dep_dir="${entry%%:*}"
        dep_path="${entry##*:}"

        case "${target_mod_dir}" in
            "${dep_dir}") continue ;;
        esac

        case "${target_mod_dir}" in
            apps/*) rel="../../${dep_dir}" ;;
            *)      rel="../${dep_dir}" ;;
        esac

        go mod edit -replace "${dep_path}=${rel}"
    done
done
