#!/usr/bin/env bash
# Tests the pilot without writing to a real tmux server.

set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
watch="$root/scripts/codex-keeper-watch.sh"
notify="$root/scripts/codex-keeper-notify.sh"
temp=$(mktemp -d)
trap 'rm -rf "$temp"' EXIT
mkdir -p "$temp/bin" "$temp/state"

# shellcheck disable=SC2016 # These are literal lines for the fake tmux program.
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'printf '\''%s\\n'\'' "$*" >> "$TMUX_LOG"' \
  'case "$1" in' \
  'display-message) printf '\''%%1\\n'\'' ;;' \
  'esac' > "$temp/bin/tmux"
chmod 700 "$temp/bin/tmux"
export PATH="$temp/bin:$PATH" TMUX_LOG="$temp/tmux.log"

make_rollout() {
    local text=$1 turn=$2
    printf '%s\n' \
      '{"type":"event_msg","timestamp":"2026-08-02T01:00:00Z","payload":{"type":"agent_message","message":"'"$text"'"}}' \
      '{"type":"event_msg","timestamp":"2026-08-02T01:00:01Z","payload":{"type":"task_complete","turn_id":"'"$turn"'"}}' > "$temp/rollout.jsonl"
    touch -t 202608010000 "$temp/rollout.jsonl"
}

run_once() {
    "$watch" --agent bravo --tmux bravo:1.1 --work-state "$temp/state/work" \
        --journal "$temp/state/journal" --rollout "$temp/rollout.jsonl" \
        --idle-secs 0 --poll-secs 1 --max-nudges 3 --once
}

printf 'remaining\n' > "$temp/state/work"
make_rollout 'Finished the focused test.' turn-1
run_once
grep -q 'paste-buffer' "$TMUX_LOG"
grep -q 'send-keys -t bravo:1.1 Enter' "$TMUX_LOG"
grep -q '^decision=prompted$' "$temp/state/journal"

# The durable fingerprint blocks a duplicate prompt.
before=$(wc -l < "$TMUX_LOG")
run_once
after=$(wc -l < "$TMUX_LOG")
[ "$before" = "$after" ]

printf 'terminal\n' > "$temp/state/work"
make_rollout 'Finished the lane.' turn-2
run_once
grep -q '^decision=no-prompt:terminal$' "$temp/state/journal"

printf 'remaining\n' > "$temp/state/work"
make_rollout 'I need your decision on which approach to take.' turn-3
run_once
grep -q '^decision=no-prompt:operator-question$' "$temp/state/journal"

# A prior completion cannot interrupt a later turn that has no fresh output.
make_rollout 'Finished an earlier item.' turn-4
printf '%s\n' '{"type":"event_msg","payload":{"type":"task_started","turn_id":"turn-5"}}' >> "$temp/rollout.jsonl"
touch -t 202608010000 "$temp/rollout.jsonl"
rm -f "$temp/state/journal"
run_once
[ ! -e "$temp/state/journal" ] || [ ! -s "$temp/state/journal" ]

payload='{"type":"agent-turn-complete","thread_id":"thread-a","turn_id":"turn-a","last_assistant_message":"done"}'
"$notify" --event "$temp/state/event.json" "$payload"
jq -e '.fingerprint == "thread-a:turn-a"' "$temp/state/event.json" >/dev/null

echo "codex keeper pilot tests: PASS"
