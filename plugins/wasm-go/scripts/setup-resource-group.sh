#!/bin/bash
# Sets resource_group wasm-pipeline process_mode to oldest_first.
# Requires Maintainer access on the ai-gateway project.
#
# Usage:
#   GITLAB_TOKEN=<your-token> bash scripts/setup-resource-group.sh
#
# Or pass token as argument:
#   bash scripts/setup-resource-group.sh <your-token>

GITLAB_URL="https://git.dev.sh.ctripcorp.com"
PROJECT_PATH="framework%2Fai-gateway"
RESOURCE_GROUP="wasm-pipeline"

TOKEN="${1:-${GITLAB_TOKEN:-}}"
if [ -z "${TOKEN}" ]; then
    echo "Error: GitLab token required. Set GITLAB_TOKEN env var or pass as argument."
    exit 1
fi

API="${GITLAB_URL}/api/v4/projects/${PROJECT_PATH}/resource_groups/${RESOURCE_GROUP}"

CURRENT_MODE=$(curl -sf --header "PRIVATE-TOKEN: ${TOKEN}" "${API}" \
    | grep -o '"process_mode":"[^"]*"' | cut -d'"' -f4 || true)

if [ -z "${CURRENT_MODE}" ]; then
    echo "resource_group '${RESOURCE_GROUP}' not found (run a pipeline with trigger-build first to create it)"
    exit 1
fi

echo "Current process_mode: ${CURRENT_MODE}"

if [ "${CURRENT_MODE}" = "oldest_first" ]; then
    echo "Already set to oldest_first, nothing to do."
    exit 0
fi

echo "Setting process_mode to oldest_first..."
curl -sf --request PUT \
    --header "PRIVATE-TOKEN: ${TOKEN}" \
    --data "process_mode=oldest_first" \
    "${API}"
echo "Done."
