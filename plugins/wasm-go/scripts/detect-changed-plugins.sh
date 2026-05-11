#!/bin/bash
set -euo pipefail

# Detects which wasm plugins need to be built and generates child-pipeline.yml.
# Must be run from plugins/wasm-go/.
#
# Required env vars (from parent pipeline variables section):
#   CI_COMMIT_BRANCH, CI_PIPELINE_SOURCE
#   REGISTRY, BUILDER_REGISTRY, GO_VERSION, ORAS_VERSION, GONOSUMDB
#   DOCKER_HOST, DOCKER_TLS_CERTDIR
#   PLUGIN_SERVER_REPO, PLUGIN_SERVER_DEFAULT_BRANCH
#
# Optional env vars:
#   FORCE_BUILD_PLUGINS  - comma-separated plugin names, skips change detection

# Determine which plugins to build
if [ -n "${FORCE_BUILD_PLUGINS:-}" ]; then
    echo "Force building: ${FORCE_BUILD_PLUGINS}"
    PLUGINS=$(echo "${FORCE_BUILD_PLUGINS}" | tr ',' '\n' | tr -d ' ' | grep -v '^$' | sort -u)

elif [ "${CI_COMMIT_BRANCH:-}" = "dev" ]; then
    echo "Dev branch: detecting changes vs trip..."
    git fetch origin trip --depth=10 2>/dev/null || git fetch origin trip
    CHANGED_FILES=$(git diff origin/trip..HEAD --name-only 2>/dev/null || true)
    PLUGINS=$(echo "${CHANGED_FILES}" | grep "^plugins/wasm-go/extensions/" | cut -d/ -f4 | sort -u || true)

elif [ "${CI_COMMIT_BRANCH:-}" = "trip" ]; then
    echo "Trip branch: detecting changes since last release..."
    # Try to read LATEST_RELEASE from plugin-server; fall back to HEAD^
    LAST_RELEASE_SHA=""
    if [ -n "${GIT_TOKEN_PLUGIN_SERVER:-}" ] && [ -n "${CI_SERVER_URL:-}" ]; then
        LATEST_RELEASE=$(curl -sf \
            --header "PRIVATE-TOKEN: ${GIT_TOKEN_PLUGIN_SERVER}" \
            "${CI_SERVER_URL}/api/v4/projects/framework%2Fai-gateway-plugin-server/repository/files/LATEST_RELEASE/raw?ref=${PLUGIN_SERVER_DEFAULT_BRANCH}" \
            | tr -d '[:space:]' || true)
        if [ -n "${LATEST_RELEASE}" ] && git cat-file -e "${LATEST_RELEASE}^{commit}" 2>/dev/null; then
            LAST_RELEASE_SHA="${LATEST_RELEASE}"
            echo "  Using LATEST_RELEASE as diff base: ${LAST_RELEASE_SHA}"
        else
            echo "  LATEST_RELEASE not found or not reachable (${LATEST_RELEASE:-empty}), falling back to HEAD^"
        fi
    fi
    if [ -z "${LAST_RELEASE_SHA}" ]; then
        LAST_RELEASE_SHA=$(git rev-parse HEAD^ 2>/dev/null || true)
    fi
    CHANGED_FILES=$(git diff "${LAST_RELEASE_SHA}"..HEAD --name-only 2>/dev/null || true)
    PLUGINS=$(echo "${CHANGED_FILES}" | grep "^plugins/wasm-go/extensions/" | cut -d/ -f4 | sort -u || true)

else
    # Only reachable via web trigger on a non-dev/trip branch
    echo "ERROR: web trigger is only supported on 'dev' or 'trip' branches (current: '${CI_COMMIT_BRANCH:-unknown}')."
    exit 1
fi

if [ -z "${PLUGINS}" ]; then
    echo "No plugins to build, generating no-op child pipeline"
    cat > child-pipeline.yml << 'EOF'
stages: [noop]
noop:
  stage: noop
  image: hub.cloud.ctripcorp.com/devops/almalinux:prod
  script:
    - echo "No plugins changed, nothing to build"
EOF
    exit 0
fi

echo "Plugins to build: $(echo "${PLUGINS}" | tr '\n' ' ')"

# Write YAML header with shared job template
cat > child-pipeline.yml << EOF
stages:
  - build
  - submit

variables:
  REGISTRY: "${REGISTRY}"
  BUILDER_REGISTRY: "${BUILDER_REGISTRY}"
  GO_VERSION: "${GO_VERSION}"
  ORAS_VERSION: "${ORAS_VERSION}"
  GOPROXY: "http://goproxy.release.ctripcorp.com,https://trip-reader:\$ARTIFACTORY_GOLANG_TOKEN@artifactory.release.ctripcorp.com/artifactory/api/go/trip-golang-prod"
  GONOSUMDB: "${GONOSUMDB}"
  DOCKER_HOST: "${DOCKER_HOST}"
  DOCKER_TLS_CERTDIR: "${DOCKER_TLS_CERTDIR}"
  PLUGIN_SERVER_REPO: "${PLUGIN_SERVER_REPO}"
  PLUGIN_SERVER_DEFAULT_BRANCH: "${PLUGIN_SERVER_DEFAULT_BRANCH}"
  TRIGGER_BRANCH: "${CI_COMMIT_BRANCH}"
  LAST_RELEASE_SHA: "${LAST_RELEASE_SHA:-}"

.alma-dind-service: &alma-dind-service
  - alias: docker
    name: hub.cloud.ctripcorp.com/devops/docker-dind-20.10.24:latest
    command:
     - dockerd
     - --host=tcp://127.0.0.1:2375
     - --storage-driver=overlay2
     - --insecure-registry=hub.cloud.ctripcorp.com
     - --tls=false

.build-wasm-plugin:
  stage: build
  image: hub.cloud.ctripcorp.com/devops/almalinux-docker-20.10.24:0.0.1
  services: *alma-dind-service
  before_script:
    - docker version
    - echo "\$CI_HUB_PASSWORD" | docker login hub.cloud.ctripcorp.com -u "\$CI_HUB_USERNAME" --password-stdin
  script:
    - cd plugins/wasm-go && bash scripts/build-plugin.sh
  artifacts:
    paths:
      - plugins/wasm-go/extensions/*/plugin.wasm
      - plugins/wasm-go/extensions/*/metadata.txt
    expire_in: 1 day

EOF

# Append one job per plugin
BUILT_JOB_NAMES=""
while IFS= read -r PLUGIN; do
    [ -z "${PLUGIN}" ] && continue
    cat >> child-pipeline.yml << EOF
build:${PLUGIN}:
  extends: .build-wasm-plugin
  variables:
    PLUGIN_NAME: ${PLUGIN}
    WASM_VERSION: "1.0.0"

EOF
    BUILT_JOB_NAMES="${BUILT_JOB_NAMES}    - job: build:${PLUGIN}\n      artifacts: true\n"
done <<< "${PLUGINS}"

# Write submit job with needs referencing all build jobs
cat >> child-pipeline.yml << 'EOF'
submit-wasm:
  stage: submit
  image: hub.cloud.ctripcorp.com/devops/almalinux-docker-20.10.24:0.0.1
  variables:
    GIT_DEPTH: "0"
  needs:
EOF
printf '%b' "${BUILT_JOB_NAMES}" >> child-pipeline.yml
cat >> child-pipeline.yml << 'EOF'
  script:
    - cd plugins/wasm-go && bash scripts/submit-wasm.sh
EOF

echo "Generated child-pipeline.yml:"
cat child-pipeline.yml
