#!/usr/bin/env bash
# Normalize Codex's documented agent-turn-complete notification for the watcher.
# Configure this script only through a wrapper that preserves any existing notify
# command. It accepts the JSON payload as $1 or on standard input.

set -euo pipefail

usage() {
    echo "usage: codex-keeper-notify.sh --event FILE [JSON]" >&2
}

event_file=""
if [ "${1-}" = "--event" ]; then
    event_file=${2-}
    shift 2
fi
[ -n "$event_file" ] || { usage; exit 64; }

if [ "$#" -gt 0 ]; then payload=$1; else payload=$(cat); fi
event_type=$(printf '%s' "$payload" | jq -r '.type // empty')
[ "$event_type" = "agent-turn-complete" ] || exit 0

fingerprint=$(printf '%s' "$payload" | jq -r '[(.thread_id // ."thread-id" // ""), (.turn_id // ."turn-id" // "")] | join(":")')
[ "$fingerprint" != ":" ] || { echo "codex-keeper-notify: no thread and turn id" >&2; exit 65; }
completed_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
message=$(printf '%s' "$payload" | jq -r '.last_assistant_message // ."last-assistant-message" // ""')

mkdir -p "$(dirname "$event_file")"
temp=$(mktemp "${event_file}.tmp.XXXXXX")
jq -n --arg fingerprint "$fingerprint" --arg completed_at "$completed_at" --arg message "$message" \
    '{fingerprint:$fingerprint, completed_at:$completed_at, last_assistant_message:$message}' > "$temp"
chmod 600 "$temp"
mv "$temp" "$event_file"
