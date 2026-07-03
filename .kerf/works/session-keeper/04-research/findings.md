# session-keeper — Research findings (decisions)

Consolidated from 3 investigations (see 02-analysis.md for the full citations). This pass records the *decisions* those facts drive.

## R1 — Signal: statusLine→file, polled. (No alternative exists.)
The live context gauge is available **only** via the statusLine script's stdin `context_window.used_percentage`. Transcript `.jsonl` carries no per-message tokens. → C1 writes the gauge file; the watcher polls it. No richer API to wait for.

## R2 — Version envelope: Claude Code v2.1.140+.
`used_percentage` (v2.0+) suffices for Phase 1. Phase 2's PreCompact backstop needs v2.1.140+; the no-TTY-hook change (v2.1.139) is irrelevant because we inject **externally**, not from a hook. → Document v2.1.140+ as the supported floor; degrade gracefully (Phase-1 warn-only) on older.

## R3 — Inject in place; no harness resume-picker.
We keep one live tmux pane and inject `/session-handoff → /clear → /session-resume <path>` as slash commands. The harness `claude --resume` picker (which *can* hang on an unnamed session) is **never invoked**. The `/session-resume` *skill* takes a path arg and reads it directly. → The bead's "named-session-or-hang" prereq is downgraded to: we only need the tmux *target* name to inject, not a harness session name.

## R4 — Reuse the existing per-agent handoff convention.
`HANDOFF-flywheel.md` / `-named-queues.md` / `-controlpoints.md` already exist. C4 triggers `/session-handoff HANDOFF-<agent>.md` and confirms completion by polling that file's **first-line `<!-- PP-TRIAL:v2 DATE branch -->` stamp** (mtime is unreliable; the shared HANDOFF.md is raced by 3 agents). → No new handoff-path convention invented.

## R5 — Hosting decision: (A) standalone `harmonik keeper`. *(Proceeding; revisit if operator picks B.)*
Orchestrator panes are **not** under `harmonik supervise` today — that supervises the daemon. A standalone `harmonik keeper --agent <name>` (one per orchestrator), reusing `supervisor.go`'s probe/backoff patterns, models reality and keeps named-queues' `supervise` contract untouched. (B = generalize `supervise`; larger blast radius in a peer's lane.)

## R6 — Idle-gating signal.
The safe-to-inject boundary is "the agent just finished a response." Two viable sources: (a) a **Stop hook** that touches an idle-marker file the watcher reads, or (b) infer idle from gauge-file quiescence (no update for N seconds after a rise). → Phase 1 can use (b) (simplest, non-destructive); Phase 2 uses (a) for a crisp idle signal before a reset. Decide concretely in the spec.

## R7 — Thresholds (initial, tunable).
Warn at **80%**, act at **90%**, hard backstop via PreCompact. Values are config knobs (`keeper.warn_pct`, `keeper.act_pct`), not constants — the auto-tune sibling work (hk-ymav1) may later feed these. → Spec them as configurable with these defaults.

## R8 — Anti-loop.
After a reset, write `.harmonik/keeper/<agent>.json {last_cleared_at, resumed_stamp, session_id}`. Suppress re-trigger until **both** a new session id is observed AND the gauge has dropped below the warn mark. Prevents a resume that itself re-trips the threshold from looping.
