#!/usr/bin/env bash
# crew-boot-digest.sh — collapse crew boot discovery (SKILL.md Steps 1-2) into one call.
#
# Reads the mission file, checks daemon + comms state, epic health, and ready beads,
# emitting a single Markdown STATE DIGEST scoped to this crew's epic and queue.
# The LLM reads ONE digest instead of ~6 individual discovery turns.
#
# Action steps (Steps 3-6: comms join, br update --assignee, recv --follow, boot
# status post) remain LLM-driven — this script covers DISCOVERY only.
#
# Usage:
#   scripts/crew-boot-digest.sh [--crew NAME] [--project DIR]
#
# Env:
#   HARMONIK_AGENT  — crew name (set automatically in a crew session)
#   HK_PROJECT      — default: /Users/gb/github/harmonik

set -uo pipefail

HK_PROJECT="${HK_PROJECT:-/Users/gb/github/harmonik}"
CREW_NAME="${HARMONIK_AGENT:-}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --project|-p) HK_PROJECT="$2"; shift 2 ;;
    --crew|-c)    CREW_NAME="$2"; shift 2 ;;
    *) echo "Unknown arg: $1" >&2; exit 1 ;;
  esac
done

cd "$HK_PROJECT"
TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

echo "# Crew Boot Digest — $TS"
echo ""
echo "> Crew: **${CREW_NAME:-UNKNOWN}** | Project: $HK_PROJECT"
echo "> One-call digest: replaces crew-launch SKILL.md Steps 1-2 individual discovery."
echo "> Action steps (Steps 3-6: join, mirror, recv, boot-status) remain LLM-driven."
echo ""

# ── Step 1: Mission file ───────────────────────────────────────────────────────
echo "## 1. Mission File (SKILL.md §Step 1)"
MISSION_FILE=".harmonik/crew/missions/${CREW_NAME:-UNKNOWN}.md"
EPIC_ID=""
QUEUE_NAME=""
CAPTAIN_NAME="captain"
CREW_NAME_FROM_MISSION=""

if [[ -n "${CREW_NAME:-}" ]] && [[ -f "$MISSION_FILE" ]]; then
  echo '```'
  cat "$MISSION_FILE"
  echo '```'
  # Parse YAML frontmatter fields (simple awk; no yq dependency)
  EPIC_ID=$(awk '/^epic_id:/{gsub(/[" ]/, "", $2); print $2}' "$MISSION_FILE" 2>/dev/null || true)
  QUEUE_NAME=$(awk '/^queue:/{gsub(/[" ]/, "", $2); print $2}' "$MISSION_FILE" 2>/dev/null || true)
  CAPTAIN_NAME=$(awk '/^captain_name:/{gsub(/[" ]/, "", $2); print $2}' "$MISSION_FILE" 2>/dev/null || true)
  CREW_NAME_FROM_MISSION=$(awk '/^crew_name:/{gsub(/[" ]/, "", $2); print $2}' "$MISSION_FILE" 2>/dev/null || true)
  CAPTAIN_NAME="${CAPTAIN_NAME:-captain}"
elif [[ -z "${CREW_NAME:-}" ]]; then
  echo "**WARN:** \$HARMONIK_AGENT unset and --crew not passed — cannot locate mission file."
  echo "Pass \`--crew <name>\` or ensure \$HARMONIK_AGENT is set."
else
  echo "**MISSING** — file not found at \`$MISSION_FILE\`"
  echo "See SKILL.md §Invalid handoff. Must NOT dispatch until mission is resolved."
fi
echo ""

# ── Step 2a: Identity check ────────────────────────────────────────────────────
echo "## 2. Identity Check (SKILL.md §Step 2)"
echo "- \$HARMONIK_AGENT  = \`${HARMONIK_AGENT:-UNSET}\`"
echo "- crew_name (mission) = \`${CREW_NAME_FROM_MISSION:-not parsed}\`"
echo "- epic_id   (mission) = \`${EPIC_ID:-not parsed}\`"
echo "- queue     (mission) = \`${QUEUE_NAME:-not parsed}\`"
echo "- captain   (mission) = \`${CAPTAIN_NAME:-not parsed}\`"
if [[ -n "${HARMONIK_AGENT:-}" ]] && [[ -n "$CREW_NAME_FROM_MISSION" ]]; then
  if [[ "${HARMONIK_AGENT:-}" == "$CREW_NAME_FROM_MISSION" ]]; then
    echo "- Identity: **MATCH** — proceed."
  else
    echo "- Identity: **MISMATCH** — use \`--from $CREW_NAME_FROM_MISSION\` on all comms/br ops."
  fi
fi
echo ""

# ── Daemon status ──────────────────────────────────────────────────────────────
echo "## 3. Daemon Status"
QOUT=$(harmonik queue status 2>&1); QRC=$?
if [[ $QRC -eq 17 ]]; then
  echo "**DOWN (exit 17)** — do NOT dispatch until daemon is up."
elif [[ $QRC -ne 0 ]]; then
  echo "**ERROR (exit $QRC)**"; echo '```'; echo "$QOUT"; echo '```'
else
  echo "**UP**"; echo '```'; echo "$QOUT"; echo '```'
fi
echo ""

# ── Agents online ─────────────────────────────────────────────────────────────
echo "## 4. Agents Online — comms who"
WHO_JSON=$(harmonik comms who --json 2>&1)
if echo "$WHO_JSON" | jq -e '.' >/dev/null 2>&1; then
  echo "$WHO_JSON" | jq -r '.[] | "- \(.agent)  (age_seconds=\(.age_seconds // "?"))"' 2>/dev/null \
    || echo "$WHO_JSON"
else
  harmonik comms who 2>&1 || echo "(comms unavailable)"
fi
echo ""

# ── My queue status ────────────────────────────────────────────────────────────
echo "## 5. My Queue Status (queue=${QUEUE_NAME:-UNKNOWN})"
if [[ -n "${QUEUE_NAME:-}" ]]; then
  QL=$(harmonik queue list --json 2>&1)
  if echo "$QL" | jq -e '.' >/dev/null 2>&1; then
    QSTATUS=$(echo "$QL" | jq -r --arg q "$QUEUE_NAME" \
      '.queues[]? | select(.name == $q) | "status=\(.status)  depth=\(.depth // "?")  paused=\(.paused // false)"' \
      2>/dev/null || true)
    if [[ -n "$QSTATUS" ]]; then echo "$QSTATUS"
    else echo "(queue \`$QUEUE_NAME\` not yet in list — will be created on first submit)"; fi
  else
    echo "(queue list unavailable)"
  fi
else
  echo "(queue name unknown — parse mission file first)"
fi
echo ""

# ── Epic state ────────────────────────────────────────────────────────────────
echo "## 6. Epic State (epic=${EPIC_ID:-UNKNOWN})"
if [[ -n "${EPIC_ID:-}" ]]; then
  br show "$EPIC_ID" 2>&1 | head -30
else
  echo "(epic_id unknown — parse mission file first)"
fi
echo ""

# ── Ready beads ───────────────────────────────────────────────────────────────
echo "## 7. Ready Beads (all — filter for your epic's children)"
br ready --limit 0 --json 2>&1 \
  | jq -r '.[] | "- \(.id)  P\(.priority // "?"): \(.title)"' 2>/dev/null \
  || br ready --limit 0 2>&1 | head -30
echo ""

# ── Recent comms ──────────────────────────────────────────────────────────────
echo "## 8. Recent Comms — last 30m (check for topic=assign from captain)"
CLOG=$(harmonik comms log --since 30m --json 2>&1 | tail -30)
if echo "$CLOG" | jq -e '.' >/dev/null 2>&1; then
  echo "$CLOG" \
    | jq -r '"[\(.from // "?")→\(.to // "?")][topic=\(.topic // "?")]: \(.body // "")"' 2>/dev/null \
    || echo "$CLOG"
else
  harmonik comms log --since 30m 2>&1 | tail -20 || echo "(comms log unavailable)"
fi
echo ""

echo "---"
echo "_Digest complete — $(date -u +"%Y-%m-%dT%H:%M:%SZ")_"
echo "_Next (still LLM-driven): Step 3 comms join → Step 4 br update --assignee → Step 5 recv --follow → Step 6 boot status post._"
