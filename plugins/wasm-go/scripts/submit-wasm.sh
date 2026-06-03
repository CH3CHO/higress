#!/bin/bash
set -euo pipefail

# Submits all built wasm plugins to ai-gateway-plugin-server in a single commit.
# Must be run from plugins/wasm-go/ after build-wasm artifacts are downloaded.
#
# Required env vars:
#   TRIGGER_BRANCH               - "dev" or "trip" (set by detect-changed-plugins.sh)
#   PLUGIN_SERVER_REPO           - host/path (e.g. git.dev.sh.ctripcorp.com/framework/...)
#   PLUGIN_SERVER_DEFAULT_BRANCH - default branch of plugin-server (e.g. trip)
#   GIT_USERNAME_PLUGIN_SERVER   - git username and committer name
#   GIT_EMAIL_PLUGIN_SERVER      - git committer email
#   GIT_TOKEN_PLUGIN_SERVER      - git token + GitLab API token
#   CI_SERVER_URL                - GitLab instance URL
#   CI_COMMIT_SHA, CI_COMMIT_SHORT_SHA, CI_PROJECT_URL, CI_PIPELINE_URL
#
# Optional env vars:
#   LAST_RELEASE_SHA             - diff base for trip branch MR description

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AI_GATEWAY_DIR="$(cd "${SCRIPT_DIR}/../../.." && pwd)"

# Determine target branch and MR behavior
if [ "${TRIGGER_BRANCH}" = "dev" ]; then
    TARGET_BRANCH="wasm-dev"
    CREATE_MR=false
elif [ "${TRIGGER_BRANCH}" = "trip" ]; then
    TARGET_BRANCH="wasm-trip"
    CREATE_MR=true
else
    echo "Unknown TRIGGER_BRANCH: ${TRIGGER_BRANCH}"
    exit 1
fi

# Find built plugins (only directories that have a plugin.wasm artifact)
BUILT_PLUGINS=$(ls extensions/*/plugin.wasm 2>/dev/null | cut -d/ -f2 | sort -u || true)

if [ -z "${BUILT_PLUGINS}" ]; then
    echo "No plugin.wasm files found, nothing to submit"
    exit 0
fi

echo "Submitting plugins to ${TARGET_BRANCH}: $(echo "${BUILT_PLUGINS}" | tr '\n' ' ')"

# --- Configuration ---
# git identity (global, applies to all repos in this job)
git config --global user.email "${GIT_EMAIL_PLUGIN_SERVER}"
git config --global user.name "${GIT_USERNAME_PLUGIN_SERVER}"

# git authentication via GIT_ASKPASS
ASKPASS_SCRIPT=$(mktemp)
chmod +x "${ASKPASS_SCRIPT}"
cat > "${ASKPASS_SCRIPT}" << EOF
#!/bin/sh
case "\$1" in
  *Username*) echo "${GIT_USERNAME_PLUGIN_SERVER}" ;;
  *Password*) echo "${GIT_TOKEN_PLUGIN_SERVER}" ;;
esac
EOF
export GIT_ASKPASS="${ASKPASS_SCRIPT}"
export GIT_TERMINAL_PROMPT=0

# Clone plugin-server
SCHEME=$(echo "${CI_SERVER_URL}" | cut -d: -f1)
git clone "${SCHEME}://${PLUGIN_SERVER_REPO}" plugin-server
cd plugin-server

# Checkout target branch.
# dev: always rebase onto plugin-server's default branch — wasm-dev is a snapshot of
#      "trip + in-flight dev changes", not an append-only branch. Avoids divergence
#      accumulating as MRs land on trip but their stale wasm commits linger on wasm-dev.
# trip: continue from existing wasm-trip if present (the open MR may be accumulating commits).
git fetch origin
if [ "${TRIGGER_BRANCH}" = "dev" ]; then
    git checkout -b "${TARGET_BRANCH}" "origin/${PLUGIN_SERVER_DEFAULT_BRANCH}"
elif git ls-remote --exit-code --heads origin "${TARGET_BRANCH}" > /dev/null 2>&1; then
    git checkout -b "${TARGET_BRANCH}" "origin/${TARGET_BRANCH}"
else
    git checkout -b "${TARGET_BRANCH}"
fi

# Update all built plugins
while IFS= read -r PLUGIN; do
    [ -z "${PLUGIN}" ] && continue

    VERSION="1.0.0"

    # Fail if plugin-server already has a newer commit than ours.
    # With oldest_first ordering, this should never happen in normal operation.
    # If it does, something is wrong (e.g. a manual push) — surface it as an error.
    # Skipped on dev: we reset wasm-dev to trip each run, so EXISTING_SHA is from trip baseline.
    EXISTING_META="plugins/${PLUGIN}/${VERSION}/metadata.txt"
    if [ "${TRIGGER_BRANCH}" != "dev" ] && [ -f "${EXISTING_META}" ]; then
        EXISTING_SHA=$(tr -d '[:space:]' < "${EXISTING_META}")
        if ( cd "${AI_GATEWAY_DIR}" && git merge-base --is-ancestor "${CI_COMMIT_SHA}" "${EXISTING_SHA}" ) 2>/dev/null; then
            echo "ERROR: ${PLUGIN} in plugin-server already has a newer commit (${EXISTING_SHA:0:8}) than ours (${CI_COMMIT_SHA:0:8}). Aborting."
            exit 1
        fi
    fi

    mkdir -p "plugins/${PLUGIN}/${VERSION}"
    cp "../extensions/${PLUGIN}/plugin.wasm" "plugins/${PLUGIN}/${VERSION}/plugin.wasm"
    cp "../extensions/${PLUGIN}/metadata.txt" "plugins/${PLUGIN}/${VERSION}/metadata.txt"
    echo "  Updated plugins/${PLUGIN}/${VERSION}/"
done <<< "${BUILT_PLUGINS}"

# Update LATEST_RELEASE in the same commit (trip branch only)
if [ "${TRIGGER_BRANCH}" = "trip" ]; then
    echo "${CI_COMMIT_SHA}" > LATEST_RELEASE
fi

# Commit and push only if there are changes
git add .
if git diff --cached --quiet; then
    echo "No changes to commit, skipping"
    exit 0
fi

PLUGIN_LIST=$(echo "${BUILT_PLUGINS}" | tr '\n' ' ' | sed 's/ $//')
git commit -m "chore: update wasm plugins (${CI_COMMIT_SHORT_SHA})

Plugins: ${PLUGIN_LIST}
Commit: ${CI_PROJECT_URL}/-/commit/${CI_COMMIT_SHA}"

# dev rebuilds wasm-dev from trip each run, so the push is non-fast-forward by design.
if [ "${TRIGGER_BRANCH}" = "dev" ]; then
    git push --force-with-lease origin "${TARGET_BRANCH}"
else
    git push origin "${TARGET_BRANCH}"
fi

# For trip branch: create or update MR
if [ "${CREATE_MR}" != "true" ]; then
    exit 0
fi

GITLAB_API="${CI_SERVER_URL}/api/v4"
PROJECT_PATH="framework%2Fai-gateway-plugin-server"
AI_GATEWAY_PROJECT_PATH="framework%2Fai-gateway"

# Build MR description: collect commits and MR references since LAST_RELEASE_SHA
build_mr_description() {
    local description=""

    if [ -n "${LAST_RELEASE_SHA:-}" ]; then
        local commits=""
        commits=$(cd "${AI_GATEWAY_DIR}" && git log --oneline --no-merges "${LAST_RELEASE_SHA}..HEAD" 2>/dev/null || true)

        if [ -n "${commits}" ]; then
            # Extract MR IIDs from "See merge request !NNN" lines in commit messages
            local mr_iids=""
            mr_iids=$(cd "${AI_GATEWAY_DIR}" && \
                git log "${LAST_RELEASE_SHA}..HEAD" --format="%B" 2>/dev/null \
                | grep -oE 'See merge request [^!]*!([0-9]+)' \
                | grep -oE '[0-9]+$' \
                | sort -un || true)

            # Build MR links section
            local mr_links=""
            if [ -n "${mr_iids}" ]; then
                while IFS= read -r iid; do
                    [ -z "${iid}" ] && continue
                    local mr_info=""
                    mr_info=$(curl -sf \
                        --header "PRIVATE-TOKEN: ${GIT_TOKEN_PLUGIN_SERVER}" \
                        "${GITLAB_API}/projects/${AI_GATEWAY_PROJECT_PATH}/merge_requests/${iid}" \
                        2>/dev/null || true)
                    if [ -n "${mr_info}" ]; then
                        local mr_title=""
                        mr_title=$(echo "${mr_info}" | grep -o '"title":"[^"]*"' | head -1 | sed 's/"title":"//;s/"$//' || true)
                        local mr_url="${CI_PROJECT_URL}/-/merge_requests/${iid}"
                        if [ -n "${mr_title}" ]; then
                            mr_links="${mr_links}
- [!${iid} ${mr_title}](${mr_url})"
                        else
                            mr_links="${mr_links}
- !${iid}"
                        fi
                    fi
                done <<< "${mr_iids}"
            fi

            description="## Plugins

${PLUGIN_LIST}
"
            if [ -n "${mr_links}" ]; then
                description="${description}
## Merged MRs
${mr_links}
"
            fi
            description="${description}
## Commits

\`\`\`
${commits}
\`\`\`

Pipeline: ${CI_PIPELINE_URL}"
            echo "${description}"
            return
        fi
    fi

    echo "Plugins: ${PLUGIN_LIST}

Commit: ${CI_PROJECT_URL}/-/commit/${CI_COMMIT_SHA}
Pipeline: ${CI_PIPELINE_URL}"
}

MR_DESCRIPTION=$(build_mr_description)
MR_TITLE="update wasm plugins: ${PLUGIN_LIST} (${CI_COMMIT_SHORT_SHA})"

# Write JSON payload to temp file to avoid shell quoting issues with description
MR_PAYLOAD=$(mktemp)
# Encode description as JSON string using awk
MR_DESC_JSON=$(echo "${MR_DESCRIPTION}" | awk '
BEGIN { printf "\"" }
{
    gsub(/\\/, "\\\\")
    gsub(/"/, "\\\"")
    gsub(/\t/, "\\t")
    gsub(/\r/, "\\r")
    if (NR > 1) printf "\\n"
    printf "%s", $0
}
END { printf "\"" }
')

MR_RESPONSE=$(curl -sf \
    --header "PRIVATE-TOKEN: ${GIT_TOKEN_PLUGIN_SERVER}" \
    "${GITLAB_API}/projects/${PROJECT_PATH}/merge_requests?source_branch=${TARGET_BRANCH}&state=opened")

if ! echo "${MR_RESPONSE}" | grep -q '"id"'; then
    # Create new MR
    cat > "${MR_PAYLOAD}" << EOF
{
  "source_branch": "${TARGET_BRANCH}",
  "target_branch": "${PLUGIN_SERVER_DEFAULT_BRANCH}",
  "title": "${MR_TITLE}",
  "description": ${MR_DESC_JSON},
  "squash": true,
  "remove_source_branch": true
}
EOF
    curl -sf --request POST \
        --header "PRIVATE-TOKEN: ${GIT_TOKEN_PLUGIN_SERVER}" \
        --header "Content-Type: application/json" \
        --data "@${MR_PAYLOAD}" \
        "${GITLAB_API}/projects/${PROJECT_PATH}/merge_requests"
    echo "MR created: ${TARGET_BRANCH} -> ${PLUGIN_SERVER_DEFAULT_BRANCH}"
else
    # Update existing MR title and description
    EXISTING_MR_IID=$(echo "${MR_RESPONSE}" | grep -o '"iid":[0-9]*' | head -1 | cut -d: -f2)
    if [ -n "${EXISTING_MR_IID}" ]; then
        cat > "${MR_PAYLOAD}" << EOF
{
  "title": "${MR_TITLE}",
  "description": ${MR_DESC_JSON}
}
EOF
        curl -sf --request PUT \
            --header "PRIVATE-TOKEN: ${GIT_TOKEN_PLUGIN_SERVER}" \
            --header "Content-Type: application/json" \
            --data "@${MR_PAYLOAD}" \
            "${GITLAB_API}/projects/${PROJECT_PATH}/merge_requests/${EXISTING_MR_IID}"
        echo "MR !${EXISTING_MR_IID} updated: ${TARGET_BRANCH} -> ${PLUGIN_SERVER_DEFAULT_BRANCH}"
    fi
fi

rm -f "${MR_PAYLOAD}"
