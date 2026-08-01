#!/usr/bin/env bash
# loadgen.sh — run a command with the machine held under CPU load, and never
# leave the load behind.
#
# WHY THIS EXISTS. Twice now an agent hand-rolled a load generator to measure a
# test under contention, and twice the cleanup reported success and did nothing:
#
#   for i in 1 2 3 ...; do (while :; do :; done) & done
#   LOADPIDS=$(jobs -p); go test ...; kill $LOADPIDS 2>/dev/null; echo "load killed"
#
# The agent shell here is zsh, and in zsh `jobs -p` is EMPTY inside command
# substitution. `kill` gets no arguments, `2>/dev/null` eats the error, the
# success line prints, and the spinners outlive the shell. First time: 30 loops,
# 13 hours, ~551% CPU. Second time: 12 loops, 22h39m, load average 27. The trap
# is recorded in docs/disk-reclaim.md, in a DISK runbook — which is not where
# anyone stands when they are about to write a load generator.
#
# So the fix is not another warning. It is this script, so nobody writes that
# line again.
#
# THE GUARANTEE, and how it is actually achieved. Three layers, stated as what
# the code does rather than as what would sound strongest:
#
#   1. A trap reaps on EXIT, INT and TERM.
#   2. The trap reaps BY PID, from `$!` collected at each spawn. It never reads
#      `jobs -p`. That is the whole point — `jobs -p` inside a command
#      substitution is what silently reaped nothing, twice. Workers are NOT put
#      in their own process group and this script does not group-kill; job
#      control is off in a non-interactive shell, so they inherit the caller's
#      pgid. Per-PID reap is the portable form and it is sufficient here.
#   3. EVERY WORKER CARRIES ITS OWN DEADLINE and exits on its own. This is the
#      layer that matters, and it is the only one that survives SIGKILL: if this
#      script is killed outright, or the terminal dies, or the agent session ends
#      mid-run, no trap runs at all — and the workers still stop. A leaked worker
#      costs you at most --seconds of CPU.
#
# Layer 3 is the reason the deadline is mandatory rather than optional. Layers 1
# and 2 are convenience; layer 3 is the guarantee.
#
# USAGE
#   scripts/loadgen.sh [--workers N] [--seconds S] -- <command> [args...]
#   scripts/loadgen.sh [--workers N] --seconds S           # load only, no command
#
#   --workers N   spinners to run. Default: one per CPU core.
#   --seconds S   hard ceiling on worker lifetime. Default 900 (15 min).
#                 Load also stops as soon as <command> returns, whichever first.
#
# EXAMPLES
#   # Run a race test under 12 spinners, load gone when the test returns.
#   scripts/loadgen.sh --workers 12 -- go test ./internal/daemon -run TestX -race
#
#   # Hold the box loaded for 60s while you watch something else.
#   scripts/loadgen.sh --workers 8 --seconds 60
#
# Exits with <command>'s exit status, so it is transparent in a pipeline.
#
# Written in bash on purpose. Under zsh an unquoted list does not word-split and
# `jobs -p` is empty in command substitution — the two dialect traps that caused
# both incidents. See the zsh table in docs/disk-reclaim.md.

set -uo pipefail

WORKERS=""
SECONDS_LIMIT=900

# need_value guards the one shape that turned this script into the incident it
# exists to prevent: `--workers` with no value left $# at 1, so `shift 2` failed,
# `case "$1"` re-matched the same token, and the parser itself became the spin
# loop — 99% CPU, no banner, no output, no worker for the trap to reap. A missing
# value is a hard exit, never a shift that may not happen.
#
# It also rejects an explicitly EMPTY value. `--workers ''` otherwise reaches the
# "unset means one spinner per core" default below, so a caller who passed a
# variable that turned out to be empty gets a full-strength load and no warning.
# An omitted flag means "default". A flag given with nothing after it is a
# mistake, and the two must not look the same.
need_value() {
    # $1 the flag, $2 arguments remaining, $3 the value (may be unset)
    if [ "$2" -lt 2 ] || [ -z "$3" ]; then
        echo "loadgen: $1 needs a value" >&2
        echo "usage: loadgen.sh [--workers N] [--seconds S] -- <command> [args...]" >&2
        exit 2
    fi
}

while [ $# -gt 0 ]; do
    case "$1" in
    --workers)
        need_value "$1" "$#" "${2-}"
        WORKERS="$2"
        shift 2
        ;;
    --seconds)
        need_value "$1" "$#" "${2-}"
        SECONDS_LIMIT="$2"
        shift 2
        ;;
    --)
        shift
        break
        ;;
    -h | --help)
        sed -n '2,59p' "$0" # through the zsh-dialect note, which is the part a reader needs most
        exit 0
        ;;
    *)
        echo "loadgen: unknown argument $1" >&2
        echo "usage: loadgen.sh [--workers N] [--seconds S] -- <command> [args...]" >&2
        exit 2
        ;;
    esac
done

# Default to one spinner per core: enough to make the box contend without the
# 12-at-66% pathology of the incident, and it scales with the machine.
if [ -z "$WORKERS" ]; then
    WORKERS="$(sysctl -n hw.ncpu 2>/dev/null || nproc 2>/dev/null || echo 4)"
fi

case "$WORKERS" in '' | *[!0-9]* | 0) echo "loadgen: --workers must be a positive integer" >&2 && exit 2 ;; esac
case "$SECONDS_LIMIT" in '' | *[!0-9]* | 0) echo "loadgen: --seconds must be a positive integer" >&2 && exit 2 ;; esac

# spin burns CPU until its own deadline, then returns. `SECONDS` is a bash
# builtin that counts up on its own, so the deadline check costs no fork — a
# `date` call per iteration would make this an I/O loop rather than a CPU loop
# and defeat the point.
spin() {
    local limit="$1"
    SECONDS=0
    while [ "$SECONDS" -lt "$limit" ]; do :; done
}

pids=()

# Reap by PID rather than by `jobs -p`. Collecting `$!` at each spawn is the
# portable form; `jobs -p` is the one that silently produced nothing twice.
reap() {
    local pid
    for pid in ${pids[@]+"${pids[@]}"}; do
        kill -9 "$pid" 2>/dev/null
        wait "$pid" 2>/dev/null
    done
    pids=()
}
trap reap EXIT INT TERM

for _ in $(seq 1 "$WORKERS"); do
    spin "$SECONDS_LIMIT" &
    pids+=("$!")
done

echo "loadgen: $WORKERS spinners up, self-terminating after ${SECONDS_LIMIT}s" >&2

status=0
if [ $# -gt 0 ]; then
    started_at=$SECONDS
    "$@"
    status=$?
    # A command that outlives the deadline finishes on a QUIET box, and the
    # result reads as "measured under load" when the last part of it was not.
    # That is a wrong measurement rather than a failure, so it has to be said
    # out loud — nothing else in the output would reveal it.
    ran_for=$((SECONDS - started_at))
    if [ "$ran_for" -ge "$SECONDS_LIMIT" ]; then
        echo "loadgen: WARNING — the command ran ${ran_for}s but the load window was only ${SECONDS_LIMIT}s." >&2
        echo "loadgen: the last $((ran_for - SECONDS_LIMIT))s ran on an UNLOADED box. Re-run with --seconds greater than the command's runtime." >&2
    fi
else
    # No command: hold the load for the full window, then let the trap reap.
    for pid in ${pids[@]+"${pids[@]}"}; do
        wait "$pid" 2>/dev/null
    done
fi

reap
echo "loadgen: load stopped" >&2
exit "$status"
