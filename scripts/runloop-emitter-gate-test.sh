#!/usr/bin/env bash
set -euo pipefail

# Mutation-strength proof for runloop-emitter-gate.sh.
#
# The fixture is a disposable non-Git copy: no checkout/reset/revert operation
# can touch the linked worktree. First prove the current inventory is green,
# then prove stale ownership, a port bypass, and a comment decoy each turn the
# gate red with the intended diagnostic.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/runloop-emitter-gate-test.XXXXXX")"
trap 'rm -rf "$TEST_ROOT"' EXIT

FIXTURE="$TEST_ROOT/repo"
mkdir -p "$FIXTURE/scripts" "$FIXTURE/internal"
cp "$SCRIPT_DIR/runloop-emitter-gate.sh" "$FIXTURE/scripts/"
cp -R "$REPO_ROOT/internal/daemon" "$FIXTURE/internal/"
cp -R "$REPO_ROOT/internal/runloop" "$FIXTURE/internal/"

run_gate() {
    "$FIXTURE/scripts/runloop-emitter-gate.sh"
}

expect_gate_failure() {
    local label=$1
    local diagnostic=$2
    local output

    if output="$(run_gate 2>&1)"; then
        echo "runloop-emitter-gate-test: $label unexpectedly passed" >&2
        exit 1
    fi
    if ! grep -Fq "$diagnostic" <<<"$output"; then
        echo "runloop-emitter-gate-test: $label failed without expected diagnostic: $diagnostic" >&2
        printf '%s\n' "$output" >&2
        exit 1
    fi
}

restore_dot_core() {
    cp "$REPO_ROOT/internal/daemon/dot_cascade_core.go" \
        "$FIXTURE/internal/daemon/dot_cascade_core.go"
    rm -f "$FIXTURE/internal/daemon/dot_cascade.go"
}

mutate_first_port_site() {
    awk '
        !changed && /ports\.Emitter/ {
            sub(/ports\.Emitter/, "ports.EscapedEmitter")
            changed = 1
        }
        { print }
        END {
            if (!changed) {
                exit 2
            }
        }
    ' "$FIXTURE/internal/daemon/dot_cascade_core.go" \
        >"$FIXTURE/internal/daemon/dot_cascade_core.go.mutated"
    mv "$FIXTURE/internal/daemon/dot_cascade_core.go.mutated" \
        "$FIXTURE/internal/daemon/dot_cascade_core.go"
}

# The unmodified post-split/post-move ownership inventory must be accepted.
run_gate >/dev/null

# Mutation 1: recreate the stale pre-split filename while removing the current
# owner. A recursive raw-field scan alone would stay green; the owner pin must
# fail closed.
mv "$FIXTURE/internal/daemon/dot_cascade_core.go" \
    "$FIXTURE/internal/daemon/dot_cascade.go"
expect_gate_failure "stale DOT owner path mutation" \
    "budgeted file internal/daemon/dot_cascade_core.go is gone"
restore_dot_core

# Mutation 2: make one of the two DOT core consumers bypass RunPorts.Emitter.
# The sibling remains, proving this is an exact per-owner ratchet rather than a
# mere "the seam appears somewhere" assertion.
mutate_first_port_site
expect_gate_failure "emitter port bypass mutation" \
    "internal/daemon/dot_cascade_core.go driveDotWorkflow reaches the emitter through the port at 0 code site(s), expected exactly 1"
restore_dot_core

# Mutation 3: repeat the bypass and add both line- and block-comment decoys
# containing the missing spelling. A plain grep count would return to the
# expected value; code-only counting must remain red.
mutate_first_port_site
cat >>"$FIXTURE/internal/daemon/dot_cascade_core.go" <<'EOF'

// ports.Emitter comment-only decoy must not count
/*
ports.Emitter block-comment decoy must not count
*/
EOF
expect_gate_failure "comment-decoy mutation" \
    "internal/daemon/dot_cascade_core.go reaches the emitter through the port at 1 code site(s), expected exactly 2"

echo "runloop-emitter-gate-test: PASS"
