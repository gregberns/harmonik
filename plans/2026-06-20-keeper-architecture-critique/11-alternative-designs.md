# Keeper — Alternative Designs (simpler-by-construction)

**Date:** 2026-06-20 · **Lens:** assume the current keeper is too fragile/complex; design fundamentally simpler ways to hit the same objective.

## The objective, stated minimally

A long-lived **interactive** Claude session (captain / crew / flywheel / orchestrator, driven over `claude --remote-control` on a tmux substrate, **subscription-billed**) must not overflow its context window and stop accepting keystrokes. When it fills, **intent must survive a context reset**. Two thresholds: warn, act. The operator has a **HARD-NO on widening the warn/act band** (out of scope here).

## What makes the current keeper complex (the failure sources to eliminate)

From `cycle.go` (1,464 lines), `injector.go`, `tmuxresolve.go`, `sessionid.go`, the precompact hook, and the prior investigation. The current design is an **external imperative actuator** that drives the session through a 7-step paste cycle. Its complexity is not incidental — it is the sum of these coupled mechanisms, each its own failure surface:

1. **tmux bracketed-paste as the actuator** — load-buffer → paste → settle(750ms) → Enter → bounded-retry Enters. Timing-fragile (`hk-89g`), no unit coverage, "works but UNRELIABLE — TIMING not executability."
2. **session-id liveness** — `/clear` re-mints the session-id; the keeper must re-resolve it from the gauge post-clear, and `--session-id` goes DEAD after the first `/clear`. UUIDv4-vs-v7-vs-uppercase discrimination in `sessionid.go`.
3. **gauge / `.managed` / `.sid` plumbing** — three on-disk channels with multi-writer races, foreign-session filtering, stale-`.managed` auto-recovery over ~3 ticks, `keeper rebind`.
4. **the anti-loop / boot-grace / force-retry state machine** — `lastFiredSID`, `seenLowPctAfterLastFire`, `BootGracePeriod`, `MaxBootGraceTotal`, `bootGraceFirstArmAt`, `ForceRetryInterval`, `consecutiveHandoffTimeouts`. This entire body of state exists **only because** the actuator is fire-and-hope: it can't observe whether its paste landed, so it must guess via SID transitions and pct re-arm. ~10 distinct `hk-*` fixes are layered into one `MaybeRun`.
5. **fighting native compaction** — the PreCompact hook exits 2 to **block Claude Code's own auto-compaction**, so the custom cycle can run instead (`keeper-precompact-hook.sh`). The keeper's entire reason-to-exist rests on the premise "native compaction is too lossy."
6. **the gates** — operator-attached, holding-dispatch, sleeping, crisp-idle, .managed opt-in. Each is a correct guard, but each is another tmux probe / file read that can false-positive (`hk-6qf`, `hk-0t5s` ~2265 false suppressions).

The throughline: **(4) is downstream of (1)+(2).** An imperative blind actuator forces a large compensating state machine. Every alternative below attacks that root, not the symptoms.

---

## Alternative A — Lean on native context management; keeper degrades to warn-only ⭐ RECOMMENDED

**Premise to re-test:** "native compaction is too lossy" was decided when the keeper was designed. As of the Jan-2026 model line, Claude Code ships **auto-compaction** for interactive sessions, and the API exposes **server-side compaction** (`compact-2026-01-12`) and **context-editing** (`context-management-2025-06-27`) as GA-beta on Opus 4.6+/Sonnet 4.6/Fable 5. The bet that a custom `/clear`+handoff beats native summarization is now **a testable hypothesis, not a given** — and the keeper is currently spending its whole complexity budget *fighting* the native path (the PreCompact exit-2 block).

**Mechanism:**
- **Stop blocking native compaction.** Delete the PreCompact exit-2 path; let Claude Code auto-compact. Intent-preservation rides on (a) native compaction's own summary, plus (b) the durable artifacts the fleet *already* keeps — beads (`br`), comms bus, HANDOFF files, `events.jsonl`. The session's working memory is reconstructible from those regardless of what compaction drops.
- **Keeper keeps ONLY the gauge + warn.** It watches context fill and, at the warn threshold, injects **one advisory line** ("commit, flush a HANDOFF, native compaction will fire soon") — the existing `wrapUpWarningText`. No `/clear`, no `/session-handoff`, no `/session-resume`, no nonce, no journal.
- **Hard-overflow safety net = process restart, not in-place /clear.** If context still climbs past the force threshold (native compaction failed or was declined), the keeper does the *one* destructive thing it still needs: kill + respawn the session **from the HANDOFF file** (the existing `respawn.go` / `ForceRestartFn` path), not an in-place paste cycle. A from-scratch spawn has a deterministic, observable outcome (new pid, new session boots reading HANDOFF) — unlike a blind paste into a live REPL.

**What complexity it ELIMINATES:**
- (1) the paste actuator for the *cycle* — gone. Only the single warn-line paste remains (1 message, failure is non-fatal, no submit-race that matters).
- (2) session-id liveness — **entirely gone.** No `/clear` means the SID never re-mints mid-cycle; nothing to re-resolve. `sessionid.go`'s primary reason to exist evaporates.
- (4) the anti-loop / boot-grace / force-retry state machine — **gone.** There is no cycle to loop, so nothing to suppress. `cycle.go` collapses from ~1,464 lines to a gauge-read + threshold-compare + one inject.
- (5) the native-compaction fight — inverted from "block it" to "rely on it."
- (3) `.managed` can stay as a simple opt-in flag; the SID-binding dance disappears.

**What it costs / gives up:**
- **Trusts native compaction's summary quality.** This is the operator's original objection. Mitigation: intent does not actually live in the conversation — it lives in beads/comms/HANDOFF, which survive any compaction. The keeper was over-indexing on conversation fidelity. *This is the load-bearing bet and must be smoke-tested (see Validation).*
- The hard-overflow restart loses in-flight uncommitted REPL state (same risk the current force-clear already has).
- Captain/crew identity must be re-anchored on respawn — already solved by the HANDOFF identity block + `HARMONIK_AGENT` env (keep that part).

**Feasibility:** High. Works within subscription-billed `--remote-control` + tmux + daemon. Uses only mechanisms already proven (warn inject, respawn). It is strictly *less* tmux automation than today.

---

## Alternative B — Self-managing session (agent watches its own context, triggers its own handoff)

**Premise:** the external watcher exists because the daemon-side process can't see inside the Claude session. But the **session can see its own context** (the statusline / gauge data originates there), and an interactive Claude session can run its own tools and slash-commands without anyone pasting into its pane.

**Mechanism:**
- Give the agent a **self-check directive in its launch context** (skill/system text): "When your context crosses ~X%, before doing anything else: commit, write `HANDOFF-<name>.md`, then run `/clear` and `/session-resume HANDOFF-<name>.md` yourself." The agent owns the whole cycle from the inside.
- The agent already has the statusline number; if not reliable, expose a trivial `harmonik ctx` tool it can poll.
- The external keeper shrinks to a **dead-man's-switch only**: if the gauge shows the session went past the force threshold *without* self-handling (agent ignored the directive / was wedged mid-turn), respawn from HANDOFF — same net as A's safety net.

**What it ELIMINATES:**
- (1) tmux paste for the cycle — the agent issues `/clear` and `/session-resume` to *itself* as ordinary REPL input; no external bracketed-paste timing race, no settle/retry tuning. This is the single biggest fragility, removed.
- (2) session-id re-resolution — the agent knows it just /cleared; it doesn't need an external process to discover the new SID by polling a gauge.
- (4) the anti-loop machine — the agent won't re-fire because *it* is the controller and knows it just handed off; no SID-transition guessing.
- (6) crisp-idle / operator-attached gates — the agent only self-clears at a genuine await-input boundary by construction (it's between its own turns), so the "is the pane busy?" probes are unnecessary.

**What it costs / gives up:**
- **Probabilistic actuator.** The agent might forget, misjudge timing, or be mid-tool-call when it should stop — exactly the "deterministic skeleton, probabilistic organs" tension the project is built around. The keeper was deliberately deterministic *because* you can't trust the agent to self-police under load.
- Needs a reliable in-session context signal; if the statusline lies near the ceiling, self-trigger fires late.
- The dead-man's-switch still needs a gauge + respawn, so it doesn't reach zero external machinery.

**Feasibility:** Medium. The mechanism is sound (agents can run their own slash-commands), but it trades a tested deterministic failure mode for an untested probabilistic one. Best as a *complement* to A (A's safety net + B's happy path), not a standalone.

---

## Alternative C — Declarative desired-state reconciler (replace imperative tick-driven paste)

**Premise:** keep an external controller, but stop making it an *imperative step-runner*. Today `MaybeRun` is a 150-line procedure that fires a sequence and then tries to infer what happened. A **reconciler** instead computes one fact each tick — "is this session healthy (under threshold) or not?" — and converges toward the desired state idempotently, the way the daemon already reconciles git/beads.

**Mechanism:**
- Desired state: `session.context < act_threshold AND session.alive`.
- Each tick reads observed state (gauge pct, pid alive) and emits **at most one corrective action**, chosen purely from current observation, never from remembered cycle-phase: under warn → nothing; warn..act → advisory line (idempotent, re-sending is harmless); over act → request reset; over force → respawn.
- "Request reset" is delegated to **B's self-handoff** or **A's restart-from-HANDOFF**, not an in-place paste cycle. The reconciler never tracks "I'm in phase handoff_injected" — it just re-observes next tick.

**What it ELIMINATES:**
- (4) the *stateful* anti-loop machine — replaced by stateless idempotent convergence. No `lastFiredSID`/`seenLowPctAfterLastFire`/boot-grace burst windows; "don't double-fire" falls out of "the corrective action is a pure function of current pct." The journal/crash-recovery (`RecoverFromCrash`, phase replay) disappears because there's no multi-step transaction to recover.
- This is the cleanest answer to the brief's Q3 (**untestability → consistent failure**): a pure `observe → decide(one action)` function is trivially unit-testable with table-driven cases, which the current `MaybeRun` (10 interacting `hk-*` patches) is not.

**What it costs / gives up:**
- Still needs *some* actuator for "request reset." If that actuator is in-place tmux `/clear`, you've only refactored the controller and kept the paste/SID fragility (1)+(2). C only pays off when paired with A or B's restart-style actuator.
- A reconciler that re-derives from pct alone can be fooled by a stale/laggy gauge into firing twice; needs a debounce (one timer), but that's far less state than the current machine.

**Feasibility:** Medium-high as a *refactor pattern* layered on A. On its own it doesn't remove the hardest failure sources — it removes the *guessing*, not the *paste*.

---

## Recommendation

**Adopt Alternative A** (lean on native context management; keeper degrades to warn-only + restart-from-HANDOFF safety net), **refactored in C's reconciler shape** (pure `observe→one-action` tick), with **B's self-handoff as an optional happy-path** once A is proven.

Rationale: A attacks the *root* — it deletes the in-place `/clear` cycle, which is what forces session-id re-resolution (2) and the entire anti-loop/boot-grace state machine (4), and it stops the keeper from fighting native compaction (5). What remains is small enough to be a pure function (C) and is therefore actually testable, directly answering the brief's three questions: the design stops being an imperative blind actuator (Q1), collapses from ~1,464 lines to a gauge+threshold+inject (Q2), and becomes table-test-able (Q3). It respects the operator's HARD-NO on the band — thresholds are unchanged; only the *action at threshold* changes.

The one real risk is the load-bearing bet that intent survives native compaction. It is de-risked by the fact that the fleet's intent already lives in **beads + comms + HANDOFF + events.jsonl**, not in the conversation transcript — so even aggressive native compaction is survivable.

**Validation gate before committing:** run one captain or crew for a full long session with the PreCompact block removed and the cycle disabled (warn-only + restart-on-overflow). Confirm via comms/beads that the agent re-grounds correctly after a native compaction and after a forced respawn. If intent visibly degrades, fall back to B (self-handoff keeps an explicit HANDOFF write in the loop) rather than reviving the external paste cycle.
