#!/usr/bin/env bash
# eval-grade.sh — deterministic grade step for eval-bead.dot (hk-olzgq).
#
# Derives the task_id from the bead_id in .harmonik/agent-task.md and runs
# the task's own go test. Exit 0 = pass, exit ≠ 0 = fail.
#
# Bead ID convention: hk-{task_id}-{5-char-suffix}
# e.g. hk-eval-fizzbuzz-avjjr → task_id = eval-fizzbuzz
#
# Must be invoked from the worktree root (the DOT shell node runs it there).
set -euo pipefail

AGENT_TASK_MD=".harmonik/agent-task.md"

if [ ! -f "$AGENT_TASK_MD" ]; then
    echo "eval-grade: $AGENT_TASK_MD not found" >&2
    exit 1
fi

BEAD_ID=$(grep '^bead_id:' "$AGENT_TASK_MD" | awk '{print $2}')
if [ -z "$BEAD_ID" ]; then
    echo "eval-grade: no bead_id in $AGENT_TASK_MD" >&2
    exit 1
fi

# Strip hk- prefix and the last -<suffix> segment (the beads_rust random ID).
# e.g. hk-eval-fizzbuzz-avjjr → eval-fizzbuzz
TASK_ID=$(echo "$BEAD_ID" | sed 's/^hk-//' | sed 's/-[a-z0-9]*$//')

if [ -z "$TASK_ID" ]; then
    echo "eval-grade: could not derive task_id from bead_id '$BEAD_ID'" >&2
    exit 1
fi

EVALTASK_DIR="evaltasks/$TASK_ID"
if [ ! -d "$EVALTASK_DIR" ]; then
    echo "eval-grade: evaltask directory '$EVALTASK_DIR' does not exist (bead_id=$BEAD_ID task_id=$TASK_ID)" >&2
    exit 1
fi

echo "eval-grade: running go test ./$EVALTASK_DIR/... (bead_id=$BEAD_ID task_id=$TASK_ID)"
go test "./$EVALTASK_DIR/..." -timeout 120s
grade_status=$?

if [ $grade_status -eq 0 ]; then
    # Compute objective metrics for the judge (WS3b). Non-blocking: failure here
    # must not change the grade outcome.
    harmonik eval metrics >/dev/null 2>&1 || true
fi

exit $grade_status
