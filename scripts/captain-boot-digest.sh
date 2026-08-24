#!/usr/bin/env bash
# captain-boot-digest.sh — report fleet condition in one call.
#
# Runs the deterministic fleet-condition checks in one call — queue status, comms
# who, crew list, tmux fleet, paused queues, recent comms, open epics — and emits a
# single Markdown STATE DIGEST. The agent reads ONE digest instead of ten discovery
# turns.
#
# What this script reports is what is RUNNING. It does not report what to work on:
# there is no ready-bead listing and no kerf map, on purpose. See the note above
# section 7.
#
# Judgment steps (zombie classification, lane planning, fleet establishment) remain
# agent-driven and are NOT attempted here.
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

cd "$HK_PROJECT" || { echo "cannot cd to $HK_PROJECT" >&2; exit 1; }
TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

echo "# Captain Boot Digest — $TS"
echo ""
echo "> One-call digest of fleet condition. It says what is running, not what to work on."
echo "> Where work comes from is your direction's call, not this digest's."
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
# `--json` emits JSON LINES, one object per line — not an array. `jq '.[]'` fails on
# an object, and the old code fell back to dumping raw JSON, so this section cost ~5x
# what it needed to and read like a bug. Format per line instead.
#
# Branch on EXIT STATUS, not on empty output. An empty result and a broken filter both
# produce "", so a fallback keyed on emptiness cannot tell a quiet fleet from a bug —
# which is exactly how the JSON-shape defect above survived. Each section below reports
# the three cases separately.
WHO_JSON=$(harmonik comms who --json 2>&1); WHO_RC=$?
WHO_LINES=$(jq -r 'select(.agent) | "- \(.agent)  \(.status // "?")  last_seen=\(.last_seen // "?")"' <<<"$WHO_JSON" 2>/dev/null); WHO_JQ=$?
if [[ $WHO_RC -ne 0 ]]; then
  harmonik comms who 2>&1 || echo "(comms unavailable — daemon may be down)"
elif [[ $WHO_JQ -ne 0 ]]; then
  echo "(could not parse \`comms who --json\` — the output shape changed. First lines:)"
  head -3 <<<"$WHO_JSON"
elif [[ -z "$WHO_LINES" ]]; then
  echo "Nobody on the bus."
else
  echo "$WHO_LINES"
fi
echo ""

# ── 2c: Registered crews ──────────────────────────────────────────────────────
echo "## 3. Registered Crews — crew list (STARTUP.md §2c)"
# JSON LINES again, and the same three-way branch — see the note in section 2.
CREW_JSON=$(harmonik crew list --json 2>&1); CREW_RC=$?
CREW_LINES=$(jq -r 'select(.name) | "- \(.name)  type=\(.type // "crew")  queue=\(.queue // "?")  started=\(.started_at // "?" | .[0:10])"' <<<"$CREW_JSON" 2>/dev/null); CREW_JQ=$?
if [[ $CREW_RC -ne 0 ]]; then
  harmonik crew list 2>&1 || echo "(crew list unavailable)"
elif [[ $CREW_JQ -ne 0 ]]; then
  echo "(could not parse \`crew list --json\` — the output shape changed. First lines:)"
  head -3 <<<"$CREW_JSON"
elif [[ -z "$CREW_LINES" ]]; then
  echo "No crews registered."
else
  echo "$CREW_LINES"
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
#
# The next action is printed per queue, because it differs by status and one of the
# three has no verb at all. `internal/queue/types.go` defines them:
#
#   paused-by-failure  -> `queue recover`  re-arms the failed items.
#                         `queuewiring.RecoverFailed` refuses any other status.
#   paused-by-drain    -> `queue resume`   releases the drain pause.
#                         `queue.ResumeQueueFromDrain` refuses any other status.
#   paused-by-budget   -> NO VERB CLEARS IT IN PLACE, and one of the two reports
#                         success while doing nothing. `queue recover` is refused:
#                         `RecoverFailed` takes only paused-by-failure. `queue resume`
#                         is NOT refused — `refuseFailureParkedLocked` screens for
#                         paused-by-failure alone, so resume exits 0, emits
#                         `operator_resuming`, and leaves the status untouched.
#                         The only code that returns the queue to ACTIVE is
#                         `PerQueueSpendMeter.unpauseBudgetPausedQueues`, reached only
#                         from `rolloverIfNewDayLocked`, whose one caller is the
#                         spend-accrual subscriber. So it clears when the meter next
#                         sees a SPEND EVENT on a new UTC day, not at the rollover.
#                         That makes it SELF-LOCKING: if the paused queue is the only
#                         capped one, nothing dispatches on it to produce the accrual,
#                         and waiting never clears it.
#                         No mutator, no RPC and no CLI flag raises the ceiling in
#                         place. `queue cancel` then re-submit under the same name with
#                         a new `spend_cap_usd` does raise it, at the cost of archiving
#                         the pending items.
#
# The selector also matches `complete-with-failures`, which is NOT a queue status
# today — it is a GroupStatus, and a group reaching it is what sets the queue to
# `paused-by-failure`. `specs/digest-command.md` DC-010 reserves the name for a
# future queue-level status and says so, so the sweep matches it on purpose. Give
# it the failure verb rather than calling it unrecognised.
#
# This script used to print `resume` for all of them, which sent a captain to a
# refused command on two statuses out of three. A tool that names the right next
# action does not need a rule elsewhere telling agents what it should have said.
echo "## 5. Paused / Failed Queues (STARTUP.md §2g)"
# THE SECTION MOST WORTH GETTING RIGHT, because it fails toward reassurance. The other
# sections degrade to something a reader can see is broken; this one degraded to
# "None — all queues active or idle-healthy" while a queue sat paused-by-failure.
# Two ways it happened: `.queues[]?` swallows a shape change, and a queue with a null
# status makes `test()` exit 5, which `|| true` then hid. Both produced an empty result
# that was indistinguishable from a healthy fleet. Same three-way branch as sections 2,
# 3, 6 and 7 — and note that here the empty case is a real answer, not a fallback.
QL_JSON=$(harmonik queue list --json 2>&1); QL_RC=$?
PAUSED=$(jq -r '.queues[] | select((.status // "") | test("paused|complete-with-failures"))
             | . as $q
             | (if   $q.status == "paused-by-failure" then "-> harmonik queue recover --queue \($q.name)"
                elif $q.status == "paused-by-drain"   then "-> harmonik queue resume --queue \($q.name)"
                elif $q.status == "paused-by-budget"  then "-> no verb clears this in place. `queue resume` exits 0 and changes nothing; `queue recover` is refused. It clears only when the spend meter sees a spend event on a NEW UTC day — self-locking if this is the only capped queue. To move now: `queue cancel` then re-submit with a new spend_cap_usd (archives pending items), or resubmit the work elsewhere."
                elif $q.status == "complete-with-failures" then "-> harmonik queue recover --queue \($q.name)  (DC-010 reserves this name at queue level; a group in this state pauses its queue by failure)"
                else "-> unrecognised status; read internal/queue/types.go before acting"
                end) as $next
             | "- \($q.name): \($q.status)  \($next)"' <<<"$QL_JSON" 2>/dev/null); PAUSED_JQ=$?
if [[ $QL_RC -ne 0 ]]; then
  echo "(queue list unavailable — \`queue list\` exited $QL_RC. Daemon may be down.)"
elif [[ $PAUSED_JQ -ne 0 ]]; then
  echo "**CANNOT TELL** — \`queue list --json\` did not parse, so this section knows nothing."
  echo "Do NOT read this as all-clear. First lines of the raw output:"
  head -3 <<<"$QL_JSON"
elif [[ -z "$PAUSED" ]]; then
  echo "None — every queue is active or idle-healthy."
else
  echo "**BLOCKED QUEUES** — each line carries its next action. They are not all the same."
  echo "$PAUSED"
fi
echo ""

# ── 2f: Recent comms log ──────────────────────────────────────────────────────
echo "## 6. Recent Comms — last 30m (STARTUP.md §2f)"
# Two things were wrong here. The fields live under `.payload`, not at the top
# level, so every line rendered as `[?→?][topic=?]:` and told the reader nothing.
# And a single agent message body runs to several kilobytes, so reading them at
# full length would cost more than the bead listing this digest just dropped.
# A boot digest needs to know WHO talked to WHOM about WHAT. Truncate the body;
# `harmonik comms log` reads the full text when there is a reason to.
# The command's OWN exit status is captured, not just jq's. Piping to `tail` puts it in
# PIPESTATUS[0], and without it a failed `comms log` sends its error text into CLOG, jq's
# `select(.payload)` matches nothing, jq exits 0, and this section prints "Nothing on the
# bus" — the same false all-clear section 5 exists to warn about, reached by another road.
CLOG=$(harmonik comms log --since 30m --json 2>&1 | tail -40); CLOG_RC=${PIPESTATUS[0]}
CLOG_LINES=$(jq -r 'select(.payload) | .payload
        | ((.body // "") | gsub("\n"; " ")) as $b
        | "- \(.from // "?") → \(.to // "?")  [\(.topic // "?")]  \(if ($b|length) > 160 then ($b[0:160] + " …[truncated]") else $b end)"' \
    <<<"$CLOG" 2>/dev/null); CLOG_JQ=$?
if [[ $CLOG_RC -ne 0 ]]; then
  echo "(comms log unavailable — \`comms log\` exited $CLOG_RC. Do NOT read this as a quiet bus.)"
elif [[ $CLOG_JQ -eq 0 && -n "$CLOG_LINES" ]]; then
  echo "$CLOG_LINES"
elif [[ $CLOG_JQ -eq 0 ]]; then
  echo "Nothing on the bus in the last 30 minutes."
else
  harmonik comms log --since 30m 2>&1 | tail -20 || echo "(comms log unavailable)"
fi
echo ""

# ── Lane structure ─────────────────────────────────────────────────────────────
# There is deliberately NO ready-bead listing and NO kerf map here.
#
# Both were removed on 2026-08-24, and size was only half the reason: the ready
# listing was 78 KB of a 90 KB digest, and the kerf map another 7 KB. The other
# half is that **the boot digest states fleet condition; it does not decide what to
# work on.** How a captain finds work changes, and pinning it to whatever sits at
# the top of one ledger query makes that choice for it. The mission says where work
# comes from. This script says what is running.
#
# Open epics stay, because an epic is a lane — that is fleet structure, not a
# ranking.
echo "## 7. Open Epics — one epic is one lane"
# `br list --json` returns an OBJECT, `{"issues":[...]}`, not an array. `jq '.[]'`
# exits 5 against it. That defect lived here behind a text fallback that produced
# plausible output with the assignee field silently missing — and the lane model
# attributes a finished epic through exactly that field. Same lesson as section 2:
# branch on exit status, and never let a fallback stand in for a working filter.
EPICS_JSON=$(br list --status=open --type=epic --json 2>&1); EPICS_RC=$?
EPICS_LINES=$(jq -r '.issues[]? | "- \(.id)  assignee=\(.assignee // "unassigned"): \(.title)"' <<<"$EPICS_JSON" 2>/dev/null); EPICS_JQ=$?
if [[ $EPICS_RC -ne 0 ]]; then
  echo "(br unavailable — \`br list\` exited $EPICS_RC)"
elif [[ $EPICS_JQ -ne 0 ]]; then
  echo "(could not parse \`br list --json\` — the output shape changed. First lines:)"
  head -3 <<<"$EPICS_JSON"
elif [[ -z "$EPICS_LINES" ]]; then
  echo "No open epics."
else
  echo "$EPICS_LINES"
fi
echo ""

echo "---"
echo "_Digest complete — $(date -u +"%Y-%m-%dT%H:%M:%SZ")_"
# The footer used to print a "_Next:_" line. Removed 2026-08-24: this script says what is
# RUNNING and does not say what to do about it. A header that promises that and a footer that
# breaks it taught every reader that the header was decorative.
