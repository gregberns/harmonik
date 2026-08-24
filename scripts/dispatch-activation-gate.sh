#!/usr/bin/env bash
set -euo pipefail

# Keep the crash-safe dispatch producer inert until activation has a complete
# design. Boot rejects every unresolved stored intent, while replay implements
# only part of the action set. A production Store.Create call can therefore
# write an intent that permanently prevents the daemon from restarting.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

hits=""
while IFS= read -r file; do
    grep -qF 'internal/dispatchstore"' "$file" || continue

    # dispatch_replay_execute.go imports dispatchstore for Advance and also has
    # a different port named provisioner.Create. Keep that exact replay action
    # legal. Any other Create call in a file that knows the concrete store is a
    # closed door, independent of receiver spelling or import alias.
    file_hits="$(grep -nE '\.Create[[:space:]]*\(' "$file" 2>/dev/null | \
        grep -vF 'provisioner.Create(ctx, record)' || true)"
    if [[ -n "$file_hits" ]]; then
        printf -v hits '%s%s:\n%s\n' "$hits" "$file" "$file_hits"
    fi
done < <(find internal -type f -name '*.go' ! -name '*_test.go' | sort)

if [[ -n "$hits" ]]; then
    echo "dispatch-activation-gate: FORBIDDEN production dispatchstore.Store.Create call:" >&2
    printf '%s' "$hits" >&2
    echo "dispatch-activation-gate: FAIL — boot rejects unresolved dispatch intents and replay implements only part of the action set. Activating the producer can permanently prevent restart. Design and complete activation before writing the first production intent." >&2
    exit 1
fi

echo "dispatch-activation-gate: OK — the incomplete crash-safe dispatch producer stays inert"
