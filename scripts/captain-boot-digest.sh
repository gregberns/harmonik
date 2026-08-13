#!/usr/bin/env bash
# captain-boot-digest.sh — collapse STARTUP.md Steps 2 & 4 discovery into one call.
#
# Runs ALL deterministic discovery from Steps 2a–2g and Step 4 (queue status,
# comms who, crew list, tmux fleet, paused queues, recent comms, ready beads,
# open epics, kerf map) and emits a single Markdown STATE DIGEST.
# The LLM reads ONE digest instead of 10+ individual discovery turns, reducing
# context accrued before real work begins.
#
# Judgment steps (zombie classification, lane planning, fleet establishment,
# bead selection) remain LLM-driven and are NOT attempted here.
#
# Usage:
#   scripts/captain-boot-digest.sh [--project DIR]
#
# Env:
#   HK_PROJECT  — default: /Users/gb/github/harmonik

set -uo pipefail

HK_PROJECT="${HK_PROJECT:-/Users/gb/github/harmonik}"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --project|-p) HK_PROJECT="$2"; shift 2 ;;
    *) echo "Unknown arg: $1" >&2; exit 1 ;;
  esac
done

cd "$HK_PROJECT"
TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

echo "# Captain Boot Digest — $TS"
echo ""
echo "> One-call digest: replaces STARTUP.md Steps 2a–2g + Step 4 individual discovery commands."
echo "> Judgment steps (zombie classification, lane planning, fleet establishment) stay LLM-driven."
echo ""

# ── 2a: Daemon up? ────────────────────────────────────────────────────────────
echo "## 1. Daemon Status (STARTUP.md §2a)"
QOUT=$(harmonik queue status 2>&1); QRC=$?
if [[ $QRC -eq 17 ]]; then
  echo "**DOWN (exit 17)** — daemon not running. Jump to STARTUP.md §2.1 (supervisor revive)."
  echo "Local reads (crew list, comms who, comms log) still work — continuing digest."
elif [[ $QRC -ne 0 ]]; then
  echo "**ERROR (exit $QRC)**"
  echo '```'; echo "$QOUT"; echo '```'
else
  echo "**UP (exit 0)**"
  echo '```'; echo "$QOUT"; echo '```'
fi
echo ""

# ── 2b: Who is online ─────────────────────────────────────────────────────────
echo "## 2. Agents Online — comms who (STARTUP.md §2b)"
WHO_JSON=$(harmonik comms who --json 2>&1)
WHO_RC=$?
if [[ $WHO_RC -eq 0 ]] && echo "$WHO_JSON" | jq -e '.' >/dev/null 2>&1; then
  echo "$WHO_JSON" | jq -r '.[] | "- \(.agent)  (age_seconds=\(.age_seconds // "?"))"' 2>/dev/null \
    || echo "$WHO_JSON"
else
  harmonik comms who 2>&1 || echo "(comms unavailable — daemon may be down)"
fi
echo ""

# ── 2c: Registered crews ──────────────────────────────────────────────────────
echo "## 3. Registered Crews — crew list (STARTUP.md §2c)"
CREW_JSON=$(harmonik crew list --json 2>&1)
CREW_RC=$?
if [[ $CREW_RC -eq 0 ]] && echo "$CREW_JSON" | jq -e '.' >/dev/null 2>&1; then
  echo "$CREW_JSON" | jq -r \
    '.[] | "- \(.name)  queue=\(.queue // "?")  session=\(.session_id // "?")  status=\(.status // "?")"' \
    2>/dev/null || echo "$CREW_JSON"
else
  harmonik crew list 2>&1 || echo "(crew list unavailable)"
fi
echo ""

# ── 2d: tmux fleet ────────────────────────────────────────────────────────────
echo "## 4. tmux Fleet (STARTUP.md §2d)"
echo "### Sessions"
tmux list-sessions 2>&1 || echo "(no tmux sessions or tmux not running)"
echo ""
echo "### Windows (all sessions)"
tmux list-windows -a 2>&1 || echo "(no windows)"
echo ""

# ── 2g: Paused / failed queues ────────────────────────────────────────────────
# (Placed before comms log since it's a go/no-go gate)
echo "## 5. Paused / Failed Queues (STARTUP.md §2g)"
QL_JSON=$(harmonik queue list --json 2>&1)
if echo "$QL_JSON" | jq -e '.' >/dev/null 2>&1; then
  PAUSED=$(echo "$QL_JSON" \
    | jq -r '.queues[]? | select(.status | test("paused|complete-with-failures")) | "- \(.name): \(.status)"' \
    2>/dev/null || true)
  if [[ -z "$PAUSED" ]]; then
    echo "None — all queues active or idle-healthy."
  else
    echo "**BLOCKED QUEUES** — resume each with: \`harmonik queue resume --queue <name>\`"
    echo "$PAUSED"
  fi
else
  echo "(queue list unavailable — daemon may be down)"
fi
echo ""

# ── 2f: Recent comms log ──────────────────────────────────────────────────────
echo "## 6. Recent Comms — last 30m (STARTUP.md §2f)"
CLOG=$(harmonik comms log --since 30m --json 2>&1 | tail -40)
if echo "$CLOG" | jq -e '.' >/dev/null 2>&1; then
  echo "$CLOG" | jq -r '"[\(.from // "?")→\(.to // "?")][topic=\(.topic // "?")]: \(.body // "")"' \
    2>/dev/null || echo "$CLOG"
else
  harmonik comms log --since 30m 2>&1 | tail -20 || echo "(comms log unavailable)"
fi
echo ""

# ── Step 4: Work plan discovery ───────────────────────────────────────────────
# --sort priority: `br ready` defaults to `hybrid` and to 20 rows. Both defaults
# mislead a captain reading a boot digest — a short listing is not a short backlog,
# and hybrid order is not priority order.
echo "## 7. Ready Beads — all rows, priority order (STARTUP.md §4)"
# Capture, then slice the capture. `br` is a Go binary and a slow streaming writer:
# in `br ready ... | head -40` the head leaves as soon as it has 40 lines, br dies on
# the closed pipe (exit 134, an abort trap, measured on this machine at 451 lines /
# 58 KB), and `pipefail` reports that death as the status of the whole pipeline. A
# here-string has no writer process to kill, so it cannot fail that way.
READY_JSON="$(br ready --sort priority --limit 0 --json 2>&1)"
READY_LINES="$(jq -r '.[] | "- \(.id)  P\(.priority // "?"): \(.title)"' <<<"$READY_JSON" 2>/dev/null)" || READY_LINES=""
if [[ -n "$READY_LINES" ]]; then
  echo "$READY_LINES"
else
  READY_TXT="$(br ready --sort priority --limit 0 2>&1)"
  head -40 <<<"$READY_TXT"
fi
echo ""

echo "## 8. Open Epics (STARTUP.md §4)"
# Same capture-then-slice shape as section 7, and for the same reason.
EPICS_JSON="$(br list --status=open --type=epic --json 2>&1)"
EPICS_LINES="$(jq -r '.[] | "- \(.id)  assignee=\(.assignee // "unassigned"): \(.title)"' <<<"$EPICS_JSON" 2>/dev/null)" || EPICS_LINES=""
if [[ -n "$EPICS_LINES" ]]; then
  echo "$EPICS_LINES"
else
  EPICS_TXT="$(br list --status=open --type=epic 2>&1)"
  head -20 <<<"$EPICS_TXT"
fi
echo ""

# NOTE: there is deliberately no `kerf next` section. kerf plans work, it does not
# rank work — its score comes from graph structure and never reads the `br` priority
# field, so a P0 and a P3 bead come back the same. Priority is stated intent first
# (operator / admiral initiatives), then `br ready --sort priority --limit 0`, which
# is section 7 above. `kerf map` stays: it answers "which kerf work owns this bead
# and what context does it carry", which nothing else answers.
echo "## 9. Kerf Map — which work owns which bead (STARTUP.md §4)"
# `kerf map` is small enough today (89 lines / 9.5 KB) that it fits the pipe buffer
# and finishes before `head` closes the pipe. That is luck, not safety: the map grows
# with the plan. Capture first so growing past the buffer changes nothing.
KERF_MAP="$(kerf map 2>&1)"
head -60 <<<"$KERF_MAP"
echo ""

echo "---"
echo "_Digest complete — $(date -u +"%Y-%m-%dT%H:%M:%SZ")_"
echo "_Next: STARTUP.md Step 3 (zombie reconciliation) → Step 4 (lane table) → Step 5 (fleet establishment)._"
