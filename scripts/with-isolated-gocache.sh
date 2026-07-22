#!/usr/bin/env bash
# Run one command with a private Go build cache. This prevents concurrent
# quality jobs (or cache cleanup jobs) from invalidating compiler facts while
# the command is still using them.

set -euo pipefail

if [[ $# -eq 0 ]]; then
    echo "usage: scripts/with-isolated-gocache.sh command [args ...]" >&2
    exit 2
fi

cache_dir=$(mktemp -d "${TMPDIR:-/tmp}/harmonik-gocache.XXXXXX")
# shellcheck disable=SC2329 # Invoked indirectly by the EXIT trap.
cleanup() {
    rm -rf -- "$cache_dir"
}
trap cleanup EXIT

(
    # An asynchronous shell may inherit ignored INT/HUP dispositions. Restore
    # defaults before exec so forwarded signals reach the requested command.
    trap - HUP INT TERM
    export GOCACHE="$cache_dir"
    exec "$@"
) &
child_pid=$!

# Do not remove the cache out from under a command that outlives this wrapper.
# Forward termination, reap the child, and only then let the EXIT trap clean up.
# shellcheck disable=SC2329 # Invoked indirectly by signal traps.
forward_and_exit() {
    local signal=$1
    local status=$2

    trap - HUP INT TERM
    kill -s "$signal" "$child_pid" 2>/dev/null || true
    wait "$child_pid" 2>/dev/null || true
    exit "$status"
}
trap 'forward_and_exit HUP 129' HUP
trap 'forward_and_exit INT 130' INT
trap 'forward_and_exit TERM 143' TERM

if wait "$child_pid"; then
    status=0
else
    status=$?
fi

exit "$status"
