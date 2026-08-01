#!/usr/bin/env bash
# Tests for scripts/loadgen.sh. Each case names the claim it defends.
#
# The first case is the reason this file exists. `loadgen.sh --workers`, with the
# value left off, used to spin the argument parser at 99% CPU forever: `shift 2`
# fails when only one argument remains, $# never changes, and `case "$1"` matches
# the same token again. The script written to stop runaway CPU loops reproduced
# the incident recorded in its own header, and it did it silently — no banner, no
# output, and no worker for the trap to reap, because the parent itself was the
# spin loop. Every argument-parsing case below is a guard on that shape.
#
# There is no `timeout` on macOS, so a case that must not hang is run in the
# background and watched. A hang fails the test rather than wedging the gate.

set -uo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
subject="$script_dir/loadgen.sh"

failures=0

fail() {
    echo "FAIL: $1" >&2
    failures=$((failures + 1))
}

# run_watched runs the subject with a wall-clock ceiling and prints its exit
# status. It prints 124 if the ceiling was reached, which is the shape a hang
# takes here. The process is killed on the way out either way.
run_watched() {
    local limit=$1
    shift

    "$subject" "$@" > /dev/null 2>&1 &
    local pid=$!

    local waited=0
    while [ "$waited" -lt "$limit" ]; do
        kill -0 "$pid" 2> /dev/null || break
        sleep 1
        waited=$((waited + 1))
    done

    if kill -0 "$pid" 2> /dev/null; then
        kill -9 "$pid" 2> /dev/null
        wait "$pid" 2> /dev/null
        echo 124
        return
    fi

    wait "$pid"
    echo $?
}

# An option with its value omitted must exit, not spin. This is the regression.
for opt in --workers --seconds; do
    got=$(run_watched 5 "$opt")
    case "$got" in
    2) ;;
    124) fail "$opt with no value hung — the argument parser is spinning again" ;;
    *) fail "$opt with no value: exit $got, want 2" ;;
    esac
done

# A value that is not a positive integer is rejected rather than guessed at.
for bad in 0 abc -3 ''; do
    got=$(run_watched 5 --workers "$bad")
    if [ "$got" != "2" ]; then
        fail "--workers '$bad': exit $got, want 2"
    fi
done

got=$(run_watched 5 --seconds 0)
if [ "$got" != "2" ]; then
    fail "--seconds 0: exit $got, want 2"
fi

# An unknown option is a usage error, not a silently ignored token.
got=$(run_watched 5 --nonsense)
if [ "$got" != "2" ]; then
    fail "--nonsense: exit $got, want 2"
fi

# The command's exit status passes through, so the script is transparent in a
# pipeline and a failing test under load still reads as failing.
got=$(run_watched 20 --workers 2 --seconds 2 -- sh -c 'exit 7')
if [ "$got" != "7" ]; then
    fail "exit-status passthrough: got $got, want 7"
fi

got=$(run_watched 20 --workers 2 --seconds 2 -- true)
if [ "$got" != "0" ]; then
    fail "exit-status passthrough on success: got $got, want 0"
fi

# The load must be gone when the command returns. This is layer 1 of the
# script's stated guarantee, and it is the layer the two incidents lost.
before=$(pgrep -f "$subject" | wc -l | tr -d ' ')
run_watched 20 --workers 2 --seconds 30 -- sh -c 'exit 0' > /dev/null
sleep 1
after=$(pgrep -f "$subject" | wc -l | tr -d ' ')
if [ "$after" -gt "$before" ]; then
    fail "workers survived the command returning: $before before, $after after"
fi

# --help prints through the zsh-dialect note. That paragraph is the one a reader
# is most likely to need and it has already been sliced off once.
if ! "$subject" --help 2> /dev/null | grep -q 'jobs -p'; then
    fail "--help does not reach the zsh-dialect note"
fi

if [ "$failures" -ne 0 ]; then
    echo "loadgen-test: $failures failure(s)" >&2
    exit 1
fi

echo "loadgen-test: PASS"
