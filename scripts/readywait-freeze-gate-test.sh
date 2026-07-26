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

# Mutation 1: add an open-coded wait under a NEW symbol in a currently
# wall-clock-clean launch consumer. This deliberately avoids the retired-symbol
# ratchet so only WALLCLOCK_RE plus the pinned-file scan can catch it.
cat >>"$FIXTURE/internal/daemon/reviewloop.go" <<'EOF'

func openCodedReadyWait() {
	<-time.After(time.Second)
}
EOF
expect_gate_failure "open-coded agent_ready wait mutation" \
    "FORBIDDEN raw wall-clock in internal/daemon/reviewloop.go"
cp "$REPO_ROOT/internal/daemon/reviewloop.go" "$FIXTURE/internal/daemon/reviewloop.go"

# Mutation 2: remove only the first of reviewloop's two seam constructions. This
# proves exact counts catch one launch escaping even while its sibling remains.
awk '
    !changed && /&runloop\.DispatchSegment{/ {
        sub(/&runloop\.DispatchSegment{/, "\\&runloop.DispatchSegmentEscaped{")
        changed = 1
    }
    { print }
' "$FIXTURE/internal/daemon/reviewloop.go" >"$FIXTURE/internal/daemon/reviewloop.go.mutated"
mv "$FIXTURE/internal/daemon/reviewloop.go.mutated" "$FIXTURE/internal/daemon/reviewloop.go"
printf '\n// &runloop.DispatchSegment{ comment-only decoy must not count\n' \
    >>"$FIXTURE/internal/daemon/reviewloop.go"
expect_gate_failure "dispatch seam escape mutation" \
    "internal/daemon/reviewloop.go builds runloop.DispatchSegment 1 time(s), expected 2"

echo "readywait-freeze-gate-test: PASS"
