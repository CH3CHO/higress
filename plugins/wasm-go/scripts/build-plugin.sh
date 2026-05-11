#!/bin/bash
set -euo pipefail

# Builds a single wasm plugin using Docker BuildKit.
# Must be run from plugins/wasm-go/.
#
# Required env vars:
#   PLUGIN_NAME       - plugin name (e.g. ai-proxy)
#   BUILDER_REGISTRY, GO_VERSION, ORAS_VERSION, GOPROXY
#   CI_COMMIT_SHA     - written to metadata.txt for traceability

BUILDER="${BUILDER_REGISTRY}wasm-go-builder:go${GO_VERSION}-oras${ORAS_VERSION}-ctrip"

echo "Building ${PLUGIN_NAME} (builder: ${BUILDER})..."

DOCKER_BUILDKIT=1 docker build \
    --build-arg PLUGIN_NAME="${PLUGIN_NAME}" \
    --build-arg BUILDER="${BUILDER}" \
    --build-arg GOPROXY="${GOPROXY}" \
    --output "extensions/${PLUGIN_NAME}" \
    .

echo "${CI_COMMIT_SHA}" > "extensions/${PLUGIN_NAME}/metadata.txt"

echo "Built extensions/${PLUGIN_NAME}/plugin.wasm"
