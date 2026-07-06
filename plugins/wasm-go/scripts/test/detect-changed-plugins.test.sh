#!/bin/bash
# Tests for detect-changed-plugins.sh dev/trip branching behavior.
# Uses temporary local git repos as fixtures; no network.
set -uo pipefail

PASS=0
FAIL=0

ok()   { echo "  PASS: $1"; PASS=$((PASS+1)); }
fail() { echo "  FAIL: $1"; FAIL=$((FAIL+1)); }

assert_contains() {
    local label="$1" file="$2" pattern="$3"
    if grep -qE "$pattern" "$file"; then ok "$label"; else fail "$label (pattern '$pattern' not in $file)"; fi
}
assert_not_contains() {
    local label="$1" file="$2" pattern="$3"
    if grep -qE "$pattern" "$file"; then fail "$label (pattern '$pattern' unexpectedly in $file)"; else ok "$label"; fi
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WASM_GO_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

DETECT_ENV=$(printf '%s\n' \
    "CI_COMMIT_BRANCH=dev" \
    "CI_PIPELINE_SOURCE=push" \
    "REGISTRY=dummy" \
    "BUILDER_REGISTRY=dummy" \
    "GO_VERSION=1.24.4" \
    "ORAS_VERSION=1.0.0" \
    "GONOSUMDB=dummy" \
    "DOCKER_HOST=tcp://docker:2375" \
    "DOCKER_TLS_CERTDIR=" \
    "PLUGIN_SERVER_REPO=dummy" \
    "PLUGIN_SERVER_DEFAULT_BRANCH=trip" \
)

setup_repo() {
    # Sets up a fixture repo with origin bare remote, trip baseline, and a dev branch.
    # $1 = work dir
    local WORK="$1"
    git init --bare "$WORK/origin.git" >/dev/null
    git init "$WORK/repo" >/dev/null
    git -C "$WORK/repo" symbolic-ref HEAD refs/heads/trip
    git -C "$WORK/repo" config user.email t@t.t
    git -C "$WORK/repo" config user.name t
    git -C "$WORK/repo" remote add origin "$WORK/origin.git"
    mkdir -p "$WORK/repo/plugins/wasm-go/extensions" "$WORK/repo/plugins/wasm-go/scripts"
    cp "$WASM_GO_DIR/scripts/detect-changed-plugins.sh" "$WORK/repo/plugins/wasm-go/scripts/"
    echo "readme" > "$WORK/repo/README.md"
    git -C "$WORK/repo" add .
    git -C "$WORK/repo" commit -m "init" >/dev/null
    git -C "$WORK/repo" push -q origin trip >/dev/null
}

run_detect() {
    # $1 = work dir, $2 = CI_COMMIT_BRANCH
    (
        cd "$1/repo/plugins/wasm-go" && \
        env $(echo "$DETECT_ENV" | sed "s/CI_COMMIT_BRANCH=dev/CI_COMMIT_BRANCH=$2/") \
        bash scripts/detect-changed-plugins.sh
    ) >/dev/null 2>&1
}

test_dev_nochange_emits_submit_only() {
    echo "=== test: detect dev no-change emits submit-wasm with needs: [] ==="
    local WORK; WORK=$(mktemp -d)
    trap "rm -rf '$WORK'" RETURN
    setup_repo "$WORK"
    git -C "$WORK/repo" checkout -q -b dev
    git -C "$WORK/repo" push -q origin dev >/dev/null
    run_detect "$WORK" dev
    local OUT="$WORK/repo/plugins/wasm-go/child-pipeline.yml"
    assert_contains    "submit-wasm job present" "$OUT" "^submit-wasm:"
    assert_contains    "needs empty array"      "$OUT" "needs: \\[\\]"
    assert_not_contains "no build jobs"         "$OUT" "build:"
}

test_dev_with_change_keeps_build_jobs() {
    echo "=== test: detect dev with change emits build + submit (needs non-empty) ==="
    local WORK; WORK=$(mktemp -d)
    trap "rm -rf '$WORK'" RETURN
    setup_repo "$WORK"
    git -C "$WORK/repo" checkout -q -b dev
    mkdir -p "$WORK/repo/plugins/wasm-go/extensions/foo"
    echo "x" > "$WORK/repo/plugins/wasm-go/extensions/foo/main.go"
    git -C "$WORK/repo" add .
    git -C "$WORK/repo" commit -m "change foo" >/dev/null
    git -C "$WORK/repo" push -q origin dev >/dev/null
    run_detect "$WORK" dev
    local OUT="$WORK/repo/plugins/wasm-go/child-pipeline.yml"
    assert_contains "build:foo job present"  "$OUT" "^build:foo:"
    assert_contains "submit-wasm job present" "$OUT" "^submit-wasm:"
    assert_contains "needs references build:foo" "$OUT" "job: build:foo"
}

test_trip_nochange_still_noop() {
    echo "=== test: detect trip no-change still emits no-op ==="
    local WORK; WORK=$(mktemp -d)
    trap "rm -rf '$WORK'" RETURN
    setup_repo "$WORK"
    run_detect "$WORK" trip
    local OUT="$WORK/repo/plugins/wasm-go/child-pipeline.yml"
    assert_contains    "noop stage present"  "$OUT" "stages: \\[noop\\]"
    assert_not_contains "no submit-wasm job" "$OUT" "^submit-wasm:"
}

test_dev_nochange_emits_submit_only
test_dev_with_change_keeps_build_jobs
test_trip_nochange_still_noop

echo
echo "detect: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
