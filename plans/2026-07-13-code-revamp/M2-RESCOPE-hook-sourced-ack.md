# M2 re-scope — hook-sourced input-ack (operator-ratified 2026-07-14)

**Status:** ratified with the operator in the 2026-07-14 planning session. Supersedes the earlier M2
framing of "replace tmux input with a structured protocol driver, then delete the paste stack." Logged
as COORD c021. This is the authoritative design addendum; PLAN/TASKS/AIS-spec edits derive from it.

## The decision in one paragraph

There are **two peer input methods, not a hierarchy**: (1) **tmux paste** for Claude, and (2) a
**structured app-server driver** for Codex. Claude runs on tmux **by design** — a structured Claude input
driver would need an API key or `-p`, both of which break subscription-first, so it is off the table (the
already-investigated dead end). The tmux paste path is **kept and made first-class**, not demoted or
deleted. The thing the old plan wanted from a protocol driver — a real acceptance/done signal instead of
blind `Enter×3` + screen-scrape — we get from a source we **already have**: the **Claude-hook-bridge**.

## The signal model (what replaces blind paste + pane-scraping)

The input transport stays tmux paste. The **ack / liveness signals come from the hook bridge**
(`specs/claude-hook-bridge.md`, `internal/hookrelay/`), which are structured events on the daemon bus —
**not** the tmux pane (pane-scraping is dropped), and **not** a Claude wire protocol.

| Need | Signal (already built) | Source |
|---|---|---|
| New session is alive | **`agent_ready`** (provenance `claude_session_start`) | `SessionStart` hook → `hookrelay.buildSessionStartMessage` |
| **Resumed** session is alive (post-`/clear`) | same **`agent_ready`** — SessionStart fires on resume too | `hookrelay.go` (does not distinguish startup vs resume; both synthesize `agent_ready`) |
| Agent finished a turn | **`outcome_emitted`** | `Stop` hook → `hookrelay.buildStopMessage` |
| Turn ended abnormally | `StopFailure` mapping | `hookrelay.buildStopMessage`/`buildStopFailureMessage` |

Transcript-tail (the keeper's `transcript_turn` source) is retained only as **secondary corroboration**,
never the primary ack. Pane `capture-pane` survives only as a human observation window, never as a signal.

## The done-gate (the genuinely-hard case: handoff completion)

The Stop hook fires at **every turn-end**, not only at "task complete." So "the agent is done with the
handoff" is **not** just "Stop fired once." The gate is:

> **handoff-done = `outcome_emitted` (Stop) fired  AND  the expected artifact is present.**

The artifact check is a file-existence/fingerprint test at Stop time (e.g., the handoff file written, or a
`PostToolUse` hook matching the `Write` to the expected path). **This exact pattern already exists**: for
the reviewer phase, `buildStopMessage` reads `.harmonik/review.json` on Stop and maps its contents into
`outcome_emitted`. The handoff-done gate is the same shape — Stop + read/confirm the expected file.

**This is the open design problem the M2 pass must actually nail** — defining that gate precisely
(which artifact, how "present" is confirmed, how a Stop-with-pending-question is distinguished from a
Stop-that-completed). The hook bridge makes it *tractable and deterministic*; it does not hand it to us
free. Do not claim it is solved; claim we finally have the right signal to solve it with.

## Bounded liveness (the resume-hang fix)

Every submit reaches exactly one terminal within a bounded window (AIS-INV-001):
- **acked** — the expected hook signal arrived (`agent_ready` for a start/resume, `outcome_emitted` for a
  turn completion), gated by the artifact check where a completion is required; OR
- **`agent_input_stale`** — no hook signal within the window → the daemon recovers instead of hanging.

The bug today is "paste, assume success, wait forever." The fix is "paste, then wait for the hook signal
under a bound, and on silence emit `agent_input_stale` and recover." No capability class, no `Degraded`.

## Why this is a rebuild, not a bolt-on — and why it's tractable

The hook bridge already emits `agent_ready` (start/resume) and `outcome_emitted` (Stop). The **run/task
path already consumes them.** The gap is that the **keeper's restart/handoff cycle does not** — it leans
on a flaky `.idle` marker + transcript-parse (PLAN.md problem #6: the "wait for model done before
`/clear`" step has *no interior implementation*, only an `.idle` pre-gate). So the M2 work is largely
**wiring the input-ack and the restart/handoff done-gate to the hook events that already fire**, plus
defining the artifact check — not inventing a transport or a protocol.

## Task-graph consequences (see PLAN.md #3, TASKS.md M2-*)

- **M2-1** (seam input method + ack contract) — stays. The `InputPort`/`Ack` is the binary
  delivered/rejected + async-acked/stale model (the terminology purge already applied).
- **M2-2** (structured driver) — **re-scoped to the Codex app-server driver only** (proven,
  subscription-compatible, largely done). NOT a Claude driver.
- **M2-3** (was "observation-only tmux; retire write verbs") — **rewritten**: keep the tmux paste path as
  Claude's first-class input; wire its ack to the hook bridge (`agent_ready` / `outcome_emitted`); drop
  the blind `Enter×3` + screen-scrape heuristics. tmux write verbs are **retained** for the Claude input
  path (the keeper/CLI carve-outs already preserve them).
- **NEW M2 task — restart/handoff done-gate** — wire the keeper/restart cycle's "model done" gate to the
  Stop hook + artifact check (closes PLAN.md problem #6's unimplemented SR4). This is the "even a whole
  phase is worth it" piece; scope it explicitly rather than folding it into M2-3.
- **M2-6** (was "delete the paste-inject / tmux-write stack, ~5,400 LOC") — **rewritten**: no wholesale
  deletion. Retire only the flaky heuristics the hook-sourced ack replaces; the paste transport stays.
- **M2-4/M2-5** (capture tee + replay/fault harness + output-or-stale oracle) — stay; the oracle now
  asserts hook-acked-or-stale over the tmux path, not pane-scrape.

## Guardrails (do not re-litigate)

- Subscription-first is locked. No `-p`, no Agent SDK HTTP API, no structured Claude input driver.
- tmux is a real, first-class driver. Not a fallback, not "degraded," not something to fix.
- Codex structured driver is proven and done; treat it as such.
