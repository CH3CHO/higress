#!/bin/bash
# Tests for submit-wasm.sh dev-path sync behavior.
# Uses a local bare repo as a fake plugin-server; dev path exits before any
# GitLab API call, so no network is needed.
set -uo pipefail

PASS=0
FAIL=0

ok()   { echo "  PASS: $1"; PASS=$((PASS+1)); }
fail() { echo "  FAIL: $1"; FAIL=$((FAIL+1)); }

assert_eq_sha() {
    local label="$1" repo="$2" refa="$3" refb="$4"
    local sa sb
    sa=$(git -C "$repo" rev-parse "$refa" 2>/dev/null || echo MISSING)
    sb=$(git -C "$repo" rev-parse "$refb" 2>/dev/null || echo MISSING)
    if [ "$sa" = "$sb" ] && [ "$sa" != "MISSING" ]; then ok "$label"; else fail "$label ($refa=$sa vs $refb=$sb)"; fi
}
assert_ne_sha() {
    local label="$1" repo="$2" refa="$3" refb="$4"
    local sa sb
    sa=$(git -C "$repo" rev-parse "$refa" 2>/dev/null || echo MISSING)
    sb=$(git -C "$repo" rev-parse "$refb" 2>/dev/null || echo MISSING)
    if [ "$sa" != "$sb" ] && [ "$sa" != "MISSING" ] && [ "$sb" != "MISSING" ]; then ok "$label"; else fail "$label ($refa=$sa vs $refb=$sb)"; fi
}
assert_blob_exists() {
    local label="$1" repo="$2" path="$3" ref="$4"
    if git -C "$repo" cat-file -e "${ref}:${path}" 2>/dev/null; then ok "$label"; else fail "$label ($path missing at $ref)"; fi
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WASM_GO_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

setup_plugin_server() {
    # $1 = work dir. Creates bare plugin-server with trip branch carrying baseline.
    local WORK="$1"
    git init --bare "$WORK/plugin-server.git" >/dev/null
    git init "$WORK/seed" >/dev/null
    git -C "$WORK/seed" symbolic-ref HEAD refs/heads/trip
    git -C "$WORK/seed" config user.email t@t.t
    git -C "$WORK/seed" config user.name t
    echo "baseline" > "$WORK/seed/README.md"
    git -C "$WORK/seed" add .
    git -C "$WORK/seed" commit -m "init" >/dev/null
    git -C "$WORK/seed" remote add origin "$WORK/plugin-server.git"
    git -C "$WORK/seed" push -q origin trip >/dev/null
}

setup_higress() {
    # $1 = work dir. Creates higress repo with plugins/wasm-go and the current
    # submit-wasm.sh. No plugin.wasm artifacts by default.
    local WORK="$1"
    git init "$WORK/higress" >/dev/null
    git -C "$WORK/higress" symbolic-ref HEAD refs/heads/dev
    git -C "$WORK/higress" config user.email t@t.t
    git -C "$WORK/higress" config user.name t
    mkdir -p "$WORK/higress/plugins/wasm-go/extensions" "$WORK/higress/plugins/wasm-go/scripts"
    cp "$WASM_GO_DIR/scripts/submit-wasm.sh" "$WORK/higress/plugins/wasm-go/scripts/"
    echo "src" > "$WORK/higress/plugins/wasm-go/extensions/README"
    git -C "$WORK/higress" add .
    git -C "$WORK/higress" commit -m "init" >/dev/null
}

run_submit_dev() {
    # $1 = work dir. Runs submit-wasm.sh as dev from plugins/wasm-go/.
    local WORK="$1"
    local HG_SHA; HG_SHA=$(git -C "$WORK/higress" rev-parse HEAD)
    local HG_SHORT; HG_SHORT=$(git -C "$WORK/higress" rev-parse --short HEAD)
    (
        cd "$WORK/higress/plugins/wasm-go" && \
        env \
            HOME="$WORK/home" \
            TRIGGER_BRANCH=dev \
            PLUGIN_SERVER_REPO="$WORK/plugin-server.git" \
            PLUGIN_SERVER_DEFAULT_BRANCH=trip \
            GIT_USERNAME_PLUGIN_SERVER=t \
            GIT_EMAIL_PLUGIN_SERVER=t@t.t \
            GIT_TOKEN_PLUGIN_SERVER=dummy \
            CI_SERVER_URL=file: \
            CI_COMMIT_SHA="$HG_SHA" \
            CI_COMMIT_SHORT_SHA="$HG_SHORT" \
            CI_PROJECT_URL=https://example.com/higress \
            CI_PIPELINE_URL=https://example.com/p/1 \
        bash scripts/submit-wasm.sh
    ) 2>&1
}

test_dev_no_artifact_syncs_wasmdev_to_trip() {
    echo "=== test: submit-wasm dev no-artifact syncs wasm-dev to trip ==="
    local WORK; WORK=$(mktemp -d)
    mkdir -p "$WORK/home"
    trap "rm -rf '$WORK' 2>/dev/null" RETURN
    setup_plugin_server "$WORK"
    setup_higress "$WORK"
    run_submit_dev "$WORK" >/dev/null
    assert_eq_sha "wasm-dev == trip after sync" "$WORK/plugin-server.git" wasm-dev trip
}

test_dev_with_artifact_commits_and_pushes() {
    echo "=== test: submit-wasm dev with artifact still commits and pushes ==="
    local WORK; WORK=$(mktemp -d)
    mkdir -p "$WORK/home"
    trap "rm -rf '$WORK' 2>/dev/null" RETURN
    setup_plugin_server "$WORK"
    setup_higress "$WORK"
    # Add a built plugin artifact
    mkdir -p "$WORK/higress/plugins/wasm-go/extensions/foo"
    printf 'WASM-BYTES' > "$WORK/higress/plugins/wasm-go/extensions/foo/plugin.wasm"
    printf 'meta'      > "$WORK/higress/plugins/wasm-go/extensions/foo/metadata.txt"
    run_submit_dev "$WORK" >/dev/null
    assert_ne_sha "wasm-dev != trip (new commit)" "$WORK/plugin-server.git" wasm-dev trip
    assert_blob_exists "plugin.wasm submitted" "$WORK/plugin-server.git" "plugins/foo/1.0.0/plugin.wasm" wasm-dev
    assert_blob_exists "metadata.txt submitted" "$WORK/plugin-server.git" "plugins/foo/1.0.0/metadata.txt" wasm-dev
}

test_dev_no_artifact_syncs_wasmdev_to_trip
test_dev_with_artifact_commits_and_pushes

echo
echo "submit: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
