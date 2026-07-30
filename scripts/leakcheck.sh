#!/usr/bin/env bash
# leakcheck.sh — report processes this user abandoned: orphaned to init, and of a
# shape that is NEVER meant to outlive its parent.
#
# Why this exists. A test-support script span up 30 `while :; do :; done` subshells
# to load the machine, ran a test, and failed to kill them. They ran for 13 hours
# at about 551% CPU before anyone noticed. Nothing in the repo could have told you.
#
# Why it has almost no false positives. Plenty of this user's processes are
# legitimately orphaned to init — ssh-agent, cfprefsd, node_exporter, a tmux
# server. All of them idle at 0.0% CPU, and NONE of them is a bare shell or a Go
# test binary. So this checks two narrow things:
#
#   1. An orphaned SHELL or Go test binary. Always a bug. A test or a script
#      spawned it and lost it. There is no benign version of this.
#   2. Any other orphan burning real CPU. Catches shapes rule 1 does not know
#      about, without flagging the idle daemons that are supposed to be there.
#
# It deliberately does NOT flag idle non-shell orphans. An abandoned daemon or
# tmux session is a real problem, but it is a DIFFERENT problem — it costs disk
# and memory, not CPU, and it needs pidfile/registry reconciliation to judge.
# Folding it in here would trade this check's zero false-positive rate for noise,
# and a check that cries wolf stops being read.
#
# Written in bash on purpose. Under zsh an unquoted list does not word-split and
# `jobs -p` is empty inside a command substitution — both have already produced
# cleanup steps here that reported success and did nothing. See the zsh table in
# docs/disk-reclaim.md.
#
# Exit 0 clean, 1 when something was found. Reports only, never kills: the caller
# decides, and a wrong automated kill is worse than the leak.

set -uo pipefail

CPU_FLOOR="${LEAKCHECK_CPU_FLOOR:-20}" # percent, for rule 2
uid="$(id -u)"
found=0

# ps output is trimmed, so fields are positional and stable: pid ppid %cpu etime comm
# `comm` is the full path on darwin, which is what the shape match needs.
while read -r pid ppid cpu etime comm; do
    [ "$ppid" = "1" ] || continue

    base="${comm##*/}"

    # Rule 1 — an orphaned shell or Go test binary is always a leak.
    case "$base" in
    sh | bash | zsh | dash | *.test)
        printf 'LEAK  pid=%-7s cpu=%-6s up=%-14s %s\n' "$pid" "$cpu" "$etime" "$comm"
        found=1
        continue
        ;;
    esac

    # Rule 2 — anything else orphaned AND burning CPU.
    #
    # Exclude GUI apps and Apple/system services FIRST. They are legitimately
    # orphaned to init and some are genuinely busy: iTerm2 alone sits above 20%
    # while you are watching output scroll, and a Storage extension hits 50%
    # during indexing. Both would drown the signal this check exists for.
    case "$comm" in
    /Applications/* | *.app/* | /System/* | /usr/libexec/* | /usr/sbin/* | /usr/bin/*)
        continue
        ;;
    esac

    # Integer compare only: avoids a float dependency, and a runaway is never 19.9%.
    cpu_int="${cpu%%.*}"
    case "$cpu_int" in
    '' | *[!0-9]*) continue ;; # not a number, skip rather than guess
    esac
    if [ "$cpu_int" -ge "$CPU_FLOOR" ]; then
        printf 'BUSY  pid=%-7s cpu=%-6s up=%-14s %s\n' "$pid" "$cpu" "$etime" "$comm"
        found=1
    fi
done < <(ps -eo uid=,pid=,ppid=,pcpu=,etime=,comm= | awk -v u="$uid" '$1==u {$1=""; print}')

if [ "$found" -ne 0 ]; then
    echo
    echo "Abandoned processes above. Nothing is attached to them."
    echo "Inspect:  ps -p <pid> -o pid,ppid,etime,time,%cpu,command"
    echo "Kill one: kill <pid>          Kill its group: kill -- -<pgid>"
    exit 1
fi

echo "leakcheck: clean (no orphaned shells, test binaries, or busy orphans)"
