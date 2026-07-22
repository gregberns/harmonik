#!/usr/bin/env bash
set -euo pipefail

# queuewiring-freeze-gate.sh — P2 unit E3 freeze tripwire
# (plans/2026-07-21-p2-extraction/E3-queue-wiring.md §5b; _plan.md §3.2).
#
# The queue OWNERSHIP layer LEFT internal/daemon in slice E3a: the in-memory
# name-keyed QueueStore, the brcli -> queue.BeadLedger bridge, and the operator
# pause/resume consumer now live in internal/queuewiring. depguard fences the
# IMPORT edge (internal/queuewiring must not import internal/daemon) but it
# cannot forbid CREATING a file, so this grep ratchet closes the other door: it
# fails the build if a queue-ownership-shaped file reappears in internal/daemon,
# or if one of the moved symbols is re-declared there.
#
# Two test files legitimately STAY in internal/daemon and are carved out below
# by the `! -name '*_test.go'` filter:
#   - queue_operatoreventconsumer_composition_7urls_test.go — the EV-009 guard
#     proving the extracted consumer is still subscribed before bus.Seal. It
#     drives daemon.Start and cannot move.
#   - queue_perqueue_pause_globalflag_tigaf6_test.go — asserts on the
#     daemon-owned OperatorPauseController.
#
# NOT yet fenced: *spendmeter*. internal/daemon/spendmeter_hkk3f8g.go and
# perqueuespendmeter_tigaf11.go are still in daemon (they are slice E3b, gated on
# the RunRegistry closure port). Add the pattern when E3b lands, not before, or
# the guard fails its own repo.
#
# Exit 0: clean. Exit 1: the door was reopened.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

HITS=0

# (1) No new queue-ownership source file in the daemon package.
while IFS= read -r f; do
    echo "queuewiring-freeze-gate: FORBIDDEN queue-wiring file in internal/daemon: $f" >&2
    HITS=$((HITS + 1))
done < <(find internal/daemon -maxdepth 1 -type f \
             \( -name '*queuestore*.go' -o -name '*queueledger*.go' \
                -o -name '*queue_operatorevent*.go' \) \
             ! -name '*_test.go')

# (2) No re-declaration of the symbols that moved to internal/queuewiring.
for sym in newQueueStore NewQueueStore newBRQueueLedger NewBRQueueLedger \
           cloneQueue cloneGroup cloneItem submitWakeCBufSize \
           NewQueueOperatorEventConsumer; do
    if grep -rn --include='*.go' -E "^(func|var|const|type)[[:space:]]+${sym}\b" internal/daemon >/dev/null 2>&1; then
        echo "queuewiring-freeze-gate: FORBIDDEN re-declaration of ${sym} in internal/daemon:" >&2
        grep -rn --include='*.go' -E "^(func|var|const|type)[[:space:]]+${sym}\b" internal/daemon >&2
        HITS=$((HITS + 1))
    fi
done

# (3) No re-declaration of the moved types.
for sym in QueueStore LockedQueueStore BRQueueLedger \
           QueueOperatorEventConsumer QueueOperatorEventConsumerConfig; do
    if grep -rn --include='*.go' -E "^type[[:space:]]+${sym}[[:space:]]+(struct|interface)" internal/daemon >/dev/null 2>&1; then
        echo "queuewiring-freeze-gate: FORBIDDEN re-declaration of type ${sym} in internal/daemon:" >&2
        grep -rn --include='*.go' -E "^type[[:space:]]+${sym}[[:space:]]+(struct|interface)" internal/daemon >&2
        HITS=$((HITS + 1))
    fi
done

if [ "$HITS" -ne 0 ]; then
    echo "queuewiring-freeze-gate: FAIL — the queue-ownership concern was extracted in P2 E3a; build on internal/queuewiring, do not reopen internal/daemon" >&2
    exit 1
fi
echo "queuewiring-freeze-gate: OK — queue-ownership concern stays out of internal/daemon"
