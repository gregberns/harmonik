#!/usr/bin/env bash
# Keep one Codex crew moving after a completed turn.
#
# This is an opt-in pilot. It does not decide that a crew is finished. The
# work-state file is the authority: `remaining` permits a prompt; `terminal`,
# `blocked`, and `awaiting_assignment` do not. The explicit source path is an
# identity boundary. Do not replace it with a newest-file search.

set -euo pipefail

usage() {
    cat <<'EOF'
Usage:
  codex-keeper-watch.sh --agent NAME --tmux PANE --work-state FILE --journal FILE \
    (--rollout FILE | --event FILE) [--idle-secs N] [--poll-secs N] \
    [--max-nudges N] [--once] [--dry-run]

The work-state file must contain one of: remaining, blocked, awaiting_assignment,
terminal. The watcher injects at most one prompt for each completed turn and
records its decision in JOURNAL. Use --dry-run before a live pane.
EOF
}

die() { echo "codex-keeper-watch: $*" >&2; exit 64; }
log() { printf '%s [%s] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$agent" "$*"; }

agent=""
tmux_target=""
work_state=""
journal=""
rollout=""
event_file=""
idle_secs=120
poll_secs=20
max_nudges=3
once=0
dry_run=0

while [ "$#" -gt 0 ]; do
    case "$1" in
    --agent) agent=${2-}; shift 2 ;;
    --tmux) tmux_target=${2-}; shift 2 ;;
    --work-state) work_state=${2-}; shift 2 ;;
    --journal) journal=${2-}; shift 2 ;;
    --rollout) rollout=${2-}; shift 2 ;;
    --event) event_file=${2-}; shift 2 ;;
    --idle-secs) idle_secs=${2-}; shift 2 ;;
    --poll-secs) poll_secs=${2-}; shift 2 ;;
    --max-nudges) max_nudges=${2-}; shift 2 ;;
    --once) once=1; shift ;;
    --dry-run) dry_run=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown option: $1" ;;
    esac
done

[ -n "$agent" ] || die "--agent is required"
[ -n "$tmux_target" ] || die "--tmux is required"
[ -n "$work_state" ] || die "--work-state is required"
[ -n "$journal" ] || die "--journal is required"
[ -n "$rollout" ] || [ -n "$event_file" ] || die "one signal source is required"
[ -z "$rollout" ] || [ -z "$event_file" ] || die "choose one signal source"
case "$idle_secs:$poll_secs:$max_nudges" in *[!0-9:]*|:*|*::*) die "timing values must be whole seconds" ;; esac
[ "$poll_secs" -gt 0 ] || die "--poll-secs must be greater than zero"
[ "$max_nudges" -gt 0 ] || die "--max-nudges must be greater than zero"
[ -f "$work_state" ] || die "work-state file does not exist: $work_state"
[ -r "$work_state" ] || die "work-state file is not readable: $work_state"

source_file=$rollout
[ -n "$source_file" ] || source_file=$event_file
[ -f "$source_file" ] || die "signal file does not exist: $source_file"
mkdir -p "$(dirname "$journal")"
touch "$journal"

read_work_state() {
    local value
    value=$(tr -d '[:space:]' < "$work_state")
    case "$value" in
    remaining|blocked|awaiting_assignment|terminal) printf '%s\n' "$value" ;;
    *) die "invalid work state in $work_state: $value" ;;
    esac
}

# Print fingerprint, completion time, and final assistant text as TSV. A partial
# JSONL tail is ignored. Rollout format is an observed, temporary bridge only.
read_rollout_signal() {
    python3 - "$rollout" <<'PY'
import json, sys
last_message = ""
complete = None
turn_in_progress = False
try:
    lines = open(sys.argv[1], encoding="utf-8").read().splitlines()
except OSError:
    raise SystemExit(0)
for raw in lines:
    try:
        row = json.loads(raw)
    except json.JSONDecodeError:
        continue
    payload = row.get("payload") or {}
    if payload.get("type") == "task_started":
        turn_in_progress = True
    if payload.get("type") == "agent_message":
        last_message = payload.get("message") or ""
    if payload.get("type") == "task_complete":
        turn_in_progress = False
        complete = (row.get("timestamp", ""), payload.get("turn_id", ""),
                    payload.get("last_agent_message") or last_message)
# A former completed turn is not permission to interrupt a later, quiet turn.
if complete and not turn_in_progress:
    timestamp, turn_id, message = complete
    fingerprint = "%s:%s" % (timestamp, turn_id)
    print("\t".join(part.replace("\t", " ").replace("\n", " ")
                     for part in (fingerprint, timestamp, message)))
PY
}

# The documented notify path writes this small normalized event atomically.
read_event_signal() {
    jq -r '[.fingerprint // "", .completed_at // "", .last_assistant_message // ""] | @tsv' "$event_file" 2>/dev/null || true
}

read_signal() {
    if [ -n "$rollout" ]; then read_rollout_signal; else read_event_signal; fi
}

field() {
    # The prompt text itself is never written to the journal.
    sed -n "s/^$1=//p" "$journal" | tail -1
}

write_journal() {
    local fingerprint=$1 decision=$2 count=$3
    local temp
    temp=$(mktemp "${journal}.tmp.XXXXXX")
    {
        printf 'fingerprint=%s\n' "$fingerprint"
        printf 'decision=%s\n' "$decision"
        printf 'nudge_count=%s\n' "$count"
        printf 'recorded_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    } > "$temp"
    mv "$temp" "$journal"
}

asks_operator() {
    grep -qiE 'requires direction|need (your |operator )?(direction|decision|approval|permission|guidance)|blocked (until|on|by)|which (approach|path|option)|permission to|should I|shall I|do you want|let me know'
}

inject() {
    local buffer prompt
    buffer="codex_keeper_${agent//[^A-Za-z0-9_]/_}_$$"
    prompt="Continue your assigned lane. Re-read HANDOFF-${agent}.md and work the next unfinished item. Do not claim overall completion unless the durable work state says terminal. If you need a decision only the operator can make, state it clearly and stop."
    if [ "$dry_run" -eq 1 ]; then
        log "DRY RUN would inject into $tmux_target"
        return
    fi
    tmux display-message -p -t "$tmux_target" '#{pane_id}' >/dev/null
    printf '%s' "$prompt" | tmux load-buffer -b "$buffer" -
    tmux paste-buffer -d -b "$buffer" -t "$tmux_target"
    # paste-buffer uses bracketed paste. Codex needs one redraw cycle to leave
    # paste mode before it can receive the literal Enter key.
    sleep 1
    tmux send-keys -t "$tmux_target" Enter
}

log "watching $tmux_target; source=$source_file; work-state=$work_state"
while :; do
    if ! tmux display-message -p -t "$tmux_target" '#{pane_id}' >/dev/null 2>&1; then
        log "pane is gone; stopping"
        exit 0
    fi

    state=$(read_work_state)
    signal=$(read_signal)
    if [ -z "$signal" ]; then
        [ "$once" -eq 1 ] && exit 0
        sleep "$poll_secs"
        continue
    fi
    IFS=$'\t' read -r fingerprint _completed_at message <<EOF
$signal
EOF
    if [ -z "$fingerprint" ]; then
        [ "$once" -eq 1 ] && exit 0
        sleep "$poll_secs"
        continue
    fi

    previous=$(field fingerprint)
    if [ "$fingerprint" = "$previous" ]; then
        [ "$once" -eq 1 ] && exit 0
        sleep "$poll_secs"
        continue
    fi

    age=$(( $(date +%s) - $(stat -f %m "$source_file") ))
    if [ "$age" -lt "$idle_secs" ]; then
        log "completed turn is only ${age}s idle; waiting"
        [ "$once" -eq 1 ] && exit 0
        sleep "$poll_secs"
        continue
    fi

    count=$(field nudge_count)
    count=${count:-0}
    case "$state" in
    terminal|blocked|awaiting_assignment)
        write_journal "$fingerprint" "no-prompt:$state" "$count"
        log "completed turn retained without prompt: work state is $state"
        ;;
    remaining)
        if printf '%s' "$message" | asks_operator; then
            write_journal "$fingerprint" "no-prompt:operator-question" "$count"
            log "completed turn retained without prompt: it asks the operator"
        elif [ "$count" -ge "$max_nudges" ]; then
            write_journal "$fingerprint" "no-prompt:nudge-cap" "$count"
            log "completed turn retained without prompt: nudge cap $max_nudges reached"
        else
            inject
            count=$((count + 1))
            write_journal "$fingerprint" "prompted" "$count"
            log "prompted completed turn (${count}/${max_nudges})"
        fi
        ;;
    esac
    [ "$once" -eq 1 ] && exit 0
    sleep "$poll_secs"
done
