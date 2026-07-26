#!/usr/bin/env bash
set -euo pipefail

# Mutation-strength proof for readywait-freeze-gate.sh.
#
# Work only in a temporary copy: first prove the current tree is green, then
# prove that (1) an open-coded wall-clock ready wait and (2) one launch consumer
# escaping runloop.DispatchSegment each turn the gate red with the intended
# diagnostic.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/readywait-freeze-gate-test.XXXXXX")"
trap 'rm -rf "$TEST_ROOT"' EXIT

FIXTURE="$TEST_ROOT/repo"
mkdir -p "$FIXTURE/scripts" "$FIXTURE/internal"
cp "$SCRIPT_DIR/readywait-freeze-gate.sh" "$FIXTURE/scripts/"
cp -R "$REPO_ROOT/internal/daemon" "$FIXTURE/internal/"
cp -R "$REPO_ROOT/internal/runloop" "$FIXTURE/internal/"

run_gate() {
    "$FIXTURE/scripts/readywait-freeze-gate.sh"
}

expect_gate_failure() {
    local label=$1
    local diagnostic=$2
    local output

    if output="$(run_gate 2>&1)"; then
        echo "readywait-freeze-gate-test: $label unexpectedly passed" >&2
        exit 1
    fi
    if ! grep -q "$diagnostic" <<<"$output"; then
        echo "readywait-freeze-gate-test: $label failed without expected diagnostic: $diagnostic" >&2
        printf '%s\n' "$output" >&2
        exit 1
    fi
}

# The unmodified current ownership inventory must be accepted.
run_gate >/dev/null

# Mutation 1: recreate the historical wait shape under its retired symbol in a
# currently wall-clock-clean launch consumer. Either protection is sufficient,
# but assert the symbol ratchet specifically fired.
cat >>"$FIXTURE/internal/daemon/reviewloop.go" <<'EOF'

func waitAgentReady() {
	<-time.After(time.Second)
}
EOF
expect_gate_failure "open-coded agent_ready wait mutation" \
    "FORBIDDEN re-declaration of waitAgentReady"
cp "$REPO_ROOT/internal/daemon/reviewloop.go" "$FIXTURE/internal/daemon/reviewloop.go"

# Mutation 2: make the single-mode launch consumer bypass the exported seam.
# Exact per-consumer counts ensure a second construction elsewhere cannot mask
# the escape.
sed -i.bak 's/&runloop\.DispatchSegment{/\&runloop.DispatchSegmentEscaped{/' \
    "$FIXTURE/internal/daemon/workloop.go"
rm -f "$FIXTURE/internal/daemon/workloop.go.bak"
expect_gate_failure "dispatch seam escape mutation" \
    "internal/daemon/workloop.go builds runloop.DispatchSegment 0 time(s), expected 1"

echo "readywait-freeze-gate-test: PASS"
