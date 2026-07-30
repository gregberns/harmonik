#!/usr/bin/env bash
# leakcheck.sh — report processes this user has lost control of: work abandoned
# to init, and live parents spawning a runaway number of children.
#
# Why this exists. A test-support script span up 30 `while :; do :; done` subshells
# to load the machine, ran a test, and failed to kill them. They ran for 13 hours
# at about 551% CPU before anyone noticed. Nothing in the repo could have told you.
#
# Why it has almost no false positives. Plenty of this user's processes are
# legitimately orphaned to init — ssh-agent, cfprefsd, node_exporter, a tmux
# server. All of them idle at 0.0% CPU, and NONE of them is a bare shell or a Go
# test binary. So this checks three narrow things:
#
#   1. An orphaned SHELL or Go test binary. Always a bug. A test or a script
#      spawned it and lost it. There is no benign version of this.
#   2. Any other orphan burning real CPU. Catches shapes rule 1 does not know
#      about, without flagging the idle daemons that are supposed to be there.
#   3. A LIVE parent with a runaway child pool. Rules 1 and 2 are blind to this
#      by construction: a process spawning hundreds of children is not orphaned,
#      and neither are its children — they are parented to it. A keeper once did
#      exactly this. Nothing that only looks at init would have seen it.
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

# Rule 3 — a live parent with a runaway number of children.
#
# Rules 1 and 2 only see processes orphaned to init. They are blind to the other
# real failure here: a process that is alive and healthy-looking while spawning
# hundreds of children. A keeper once did exactly that. Its children were
# parented to IT, never to init, so nothing above would have reported them.
#
# Threshold has enormous margin, measured on a healthy box 2026-07-30:
#   launchd (ppid 1)   302 children  <- normal, everything reparents there
#   Google Chrome       39 children  <- normal for a browser
#   every other CLI parent <= 4 children
# So after excluding init and GUI apps, the real ceiling is about 4 and the
# default floor of 25 cannot fire by accident.
CHILD_CEILING="${LEAKCHECK_CHILD_CEILING:-25}"

while read -r count parent_pid; do
    # init legitimately parents hundreds. Never flag it.
    [ "$parent_pid" = "1" ] && continue
    [ "$count" -ge "$CHILD_CEILING" ] || continue

    parent_cmd="$(ps -p "$parent_pid" -o comm= 2>/dev/null)"
    [ -n "$parent_cmd" ] || continue # parent already exited, nothing to report

    # Browsers and GUI apps legitimately run large child pools.
    case "$parent_cmd" in
    /Applications/* | *.app/* | /System/* | /usr/libexec/*)
        continue
        ;;
    esac

    printf 'SPAWN pid=%-7s children=%-5s %s\n' "$parent_pid" "$count" "$parent_cmd"
    found=1
done < <(ps -eo uid=,ppid= | awk -v u="$uid" '$1==u {c[$2]++} END {for (p in c) print c[p], p}')

if [ "$found" -ne 0 ]; then
    echo
    echo "LEAK / BUSY  are ORPHANED — parented to init, nothing attached, safe to kill."
    echo "SPAWN        is a LIVE parent with a runaway child pool. It is still doing"
    echo "             something. Look before you kill it, and kill the GROUP, not the"
    echo "             parent alone, or the children are orphaned and you get a LEAK."
    echo
    echo "Inspect:     ps -p <pid> -o pid,ppid,pgid,etime,time,%cpu,command"
    echo "Its children:ps -eo pid,ppid,etime,command | awk '\$2==<pid>'"
    echo "Kill one:    kill <pid>"
    echo "Kill group:  kill -- -<pgid>      # pgid from the inspect command above"
    exit 1
fi

echo "leakcheck: clean (no orphaned shells or test binaries, no busy orphans, no runaway child pools)"
