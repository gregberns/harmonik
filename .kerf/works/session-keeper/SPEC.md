# session-keeper — Spec (draft)

> Status: DRAFT on the kerf bench. Normative once copied to `specs/` at finalize (with operator check-in on spec text). Bead: hk-ekap1. Codename: session-keeper.

## 1. Purpose & scope

`session-keeper` is an external, per-orchestrator watcher that prevents a long-running orchestrator agent (flywheel, named-queues, controlpoints) from hitting Claude Code's **lossy auto-compaction**. It watches the agent's context-window fill and, at thresholds and only on an idle boundary, (Phase 1) injects a wrap-up warning, or (Phase 2) drives an intent-preserving `/session-handoff → /clear → /session-resume` cycle.

**In scope:** orchestrator (interactive Claude Code) sessions. **Out of scope (normative non-goals):** per-bead *implementer* sessions (already reaped by the daemon); redesigning the handoff/resume skills' *content semantics*; cross-agent intent transfer (each agent resumes itself).
> **NG2 reconciliation (from review).** "Don't redesign the handoff skill" is narrowed to: the keeper does not change *what the agent chooses to write*. It MAY require the handoff to carry a single keeper-supplied passthrough token (§5.2) — a minimal, content-neutral addition the skill echoes, not a redesign of the handoff format.

**Supported floor:** Claude Code **v2.1.140+**. On older versions the keeper runs Phase-1 (warn-only) and logs that Phase-2 is unavailable.

## 2. Hosting (decision R5 = A) — CONFIRMED by operator 2026-06-02

A standalone subcommand `harmonik keeper --agent <name> [--tmux <target>] [--warn-pct N] [--act-pct N]`, one process per orchestrator. It reuses the probe/backoff/crash-window patterns of `internal/supervise/supervisor.go` but supervises an orchestrator *pane*, not a child process. It does **not** modify the `harmonik supervise` contract.
> If the operator selects (B) instead, §2 is replaced by "a new target kind under `harmonik supervise`"; §§3–8 are unchanged.

## 3. The signal (C1 — gauge-signal)

3.1. The keeper ships a statusLine script (`scripts/keeper-statusline.sh`) referenced from the orchestrator's `~/.claude/settings.json` `statusLine`.
3.2. On each invocation it reads `context_window.used_percentage` and the live `session_id` from its stdin JSON and **atomically** writes `.harmonik/keeper/<agent>.ctx` as `{"pct": <float>, "session_id": "<id>", "ts": "<rfc3339>"}`.
3.3. The file MUST update within ~1s of each assistant message. The keeper treats a gauge with `ts` older than a staleness bound (default 120s) as "agent not live" and takes no action.
3.4. **No-gauge self-check (from review).** A missing keeper statusLine in the orchestrator's settings means the gauge file never appears and the keeper would silently do nothing forever. At boot and every `staleness` interval, if the gauge is absent or stale, the keeper emits `session_keeper_no_gauge{agent}` (warn) so the misconfiguration is visible on the bus rather than failing silent.

## 4. The watcher (C2)

4.1. The keeper polls `.harmonik/keeper/<agent>.ctx` at a fixed interval (default 5s).
4.2. **Warn threshold** (`warn_pct`, default 80): on the first crossing upward, in **warn mode**, perform one injection (§6) of the wrap-up warning and emit `session_keeper_warn`. Do not repeat until the gauge has dropped below `warn_pct` and risen again.
4.3. **Act threshold** (`act_pct`, default 90): in **reset mode** (Phase 2), on crossing upward *and* an idle boundary (§4.5) *and* no in-flight dispatch (§4.6), run the handoff cycle (§5).
4.4. In **Phase 1**, `act_pct` behavior is disabled; the keeper only warns.
4.5. **Idle gating.** Phase 1 infers idle from gauge quiescence (no update for ≥ `idle_quiesce` seconds, default 8) — acceptable because Phase 1 only *warns* (worst case = a premature warning during a long single turn). Phase 2 **MUST** gate on a crisp idle-marker touched by a Stop hook (`scripts/keeper-stop-hook.sh` → `.harmonik/keeper/<agent>.idle`), never on the quiesce heuristic; the keeper acts only when the marker is newer than the last assistant activity. (§12 verifies the Stop hook marks a true await-input boundary, not a mid-todo continuation.)
4.6. **Dispatch gating.** Before a reset the keeper defers while the agent holds an in-flight dispatch. **Attribution:** the queue keys runs by `run_id`, not submitting-agent, so the orchestrator MUST write a `.harmonik/keeper/<agent>.dispatching` marker while it owns submitted-but-unfinished work and clear it when its queue is drained; the keeper defers while that marker is present. (Absent a clean attribution, the keeper fails *closed* — defers — rather than reset mid-dispatch.)

## 5. The handoff cycle (C4 — Phase 2)

Ordered, each step gated on the prior completing:
5.0. **Open the cycle journal** (§7.3) with a fresh `cycle_id` (monotonic counter) *before* any injection — so a keeper crash mid-cycle is recoverable.
5.1. Inject `/session-handoff HANDOFF-<agent>.md` **with the directive to embed `<!-- KEEPER:<cycle_id> -->` as the handoff's last line** (passthrough token per §1 NG2-reconciliation).
5.2. **Confirm** by polling `HANDOFF-<agent>.md` until it contains `<!-- KEEPER:<cycle_id> -->` (timeout `handoff_timeout`, default 180s). The token is per-cycle unique, so confirmation is unambiguous even for repeated same-day/same-branch resets — this replaces the non-unique date+branch stamp (which the review proved identical across same-day resets). On timeout: abort, emit `session_keeper_cycle_aborted{reason}`, close the journal, leave the session untouched (fail-safe — never `/clear` an unconfirmed handoff).
5.3. Inject `/clear`.
5.3a. **Confirm `/clear` completed** (best-effort; §12-E2 proved the resume is FIFO-preserved across `/clear`, so this is belt-and-suspenders, not a hard gate): after injecting `/clear`, optionally wait up to `clear_settle` (default 3s) for the statusLine to emit a line with a *new* `session_id` (§12-E1/E3 confirm it does). Then inject §5.4. If the new-`session_id` line does not appear within `clear_settle`, inject §5.4 anyway (FIFO ordering means the resume is still queued) but log `session_keeper_clear_unconfirmed` for observability. Re-bind all per-session keeper state to the new `session_id`.
5.4. Inject `/session-resume HANDOFF-<agent>.md`.
5.5. Write the anti-loop marker (§7), **close the cycle journal**, and emit `session_keeper_cycle_complete`.

## 6. Injection (C3 + target registry)

6.1. The keeper resolves the tmux target from `--tmux` or, by convention, `harmonik-<project_hash>-<agent>` (`internal/lifecycle/provenance.go`).
6.2. Injection uses external tmux `send-keys` (bracketed-paste body + Enter), modeled on the pasteinject delivery mechanics. It is unaffected by the Stop-hook no-TTY limitation because the keeper, not a hook, sends the keys.
6.3. Each injection targets a quiescent pane; the keeper MUST NOT inject mid-stream (enforced by §4.5).

## 7. Anti-loop (C4)

7.1. After §5.5 the keeper writes `.harmonik/keeper/<agent>.json {last_cleared_at, resumed_stamp, session_id}`.
7.2. After a reset, the keeper suppresses any new reset until **both**: (a) the gauge reports a `session_id` different from the marker's, AND (b) the gauge has dropped below `warn_pct`. Emit `session_keeper_suppressed_loop` if a threshold crossing is suppressed by this rule. **Both conditions depend on the §12 experiment outcomes** (does `/clear` mint a new `session_id` the statusLine sees; does the gauge drop). If the experiment shows `/clear` does *not* change `session_id`, condition (a) is replaced by the cycle-journal `cycle_id` advancing — the spec must not ship Phase 2 keyed on an unverified `session_id` change.
7.3. **Cycle journal + crash recovery (from review).** §5.0 opens `.harmonik/keeper/<agent>.cycle {cycle_id, phase, opened_at}` before the first injection; §5.5/abort closes it. On keeper boot, a *present* journal means a prior keeper crashed mid-cycle: if `phase ≥ cleared` and no resume confirmed, the agent may be cleared-and-idle → the keeper injects `/session-resume HANDOFF-<agent>.md` to recover, emits `session_keeper_half_cycle_recovered`, then closes the journal. Never start a new cycle while a journal is open.

## 8. Compaction backstop (C5 — Phase 2)

8.1. The keeper ships a `PreCompact` hook (`scripts/keeper-precompact-hook.sh`) the orchestrator installs.
8.2. On `compaction_trigger: "auto"` the hook returns `decision: "block"` and touches `.harmonik/keeper/<agent>.precompact` so the watcher runs §5 instead. (Manual `/compact` is not blocked.)
8.3. This is a safety net for the case where §4.3 missed the threshold (e.g., a single huge turn jumped past `act_pct`).

## 9. Events (C6)

Emitted via `EmitWithRunID` (new constants in `internal/core/eventtype.go`): `session_keeper_warn`, `session_keeper_handoff_started`, `session_keeper_cycle_complete`, `session_keeper_cycle_aborted`, `session_keeper_suppressed_loop`, `session_keeper_no_gauge`, `session_keeper_half_cycle_recovered`. Visible in `harmonik subscribe` and the replay log.

## 10. Config knobs

`keeper.warn_pct` (80), `keeper.act_pct` (90), `keeper.poll_interval` (5s), `keeper.idle_quiesce` (8s), `keeper.handoff_timeout` (180s), `keeper.staleness` (120s). All overridable per-agent via flags. The auto-tune sibling (hk-ymav1) MAY later drive `warn_pct`/`act_pct`.

## 11. Done means

- **Phase 1:** `harmonik keeper --agent flywheel` running against a live orchestrator: the gauge file updates per-message (S1); crossing 80% injects exactly one wrap-up warning at an idle boundary with no other effect (S2); `session_keeper_warn` appears on the bus. Non-destructive end-to-end.
- **Phase 2:** crossing 90% while idle and not mid-dispatch completes handoff→clear→resume; the resumed session reads the fresh `HANDOFF-<agent>.md` and continues its lane (S3); never fires mid-dispatch and never double-fires within one post-resume window (S4); with the PreCompact hook installed, native auto-compaction never fires on a supervised session (S5).
- An aborted handoff (keeper token not confirmed) **never** proceeds to `/clear` (fail-safe).

## 12. Phase-2 verification prerequisites (gate — from review)

**STATUS: RESOLVED 2026-06-02 — spike `hk-vp9i8`, live Claude Code v2.1.161, ALL FOUR PASS. Phase 2 is UNBLOCKED.** Evidence in `12-experiments-findings.md`. Phase 1 was always buildable; Phase 2's in-place mechanism is now empirically validated:
- **E1 PASS** — `/clear` mints a new `session_id` visible in statusLine JSON (`d8bc3122→15ad5eac→…`); `/session-resume` runs inside the cleared session and does *not* mint a new id. → §7.2 condition (a) `session_id`-change is VALID.
- **E2 PASS (critical)** — `/clear`⏎ then `/session-resume`⏎ back-to-back preserves the resume; the pane input queue is FIFO across `/clear`. A settle delay is harmless but **not required**. → §5.3a readiness probe can be a thin guard, not a hard gate; no pane-teardown fallback needed.
- **E3 PASS** — statusLine re-runs on a bare `/clear` (no assistant message); `used_percentage` reads NA on a freshly-cleared session. → the keeper observes the clear via the new `session_id` line.
- **E4 PASS** — the Stop hook fires only at await-input boundaries, once per completed turn.
- **Build notes:** gauge field is `.context_window.used_percentage` (reads NA right after `/clear`); `session_id`/`transcript_path` rotate each cycle, so keeper per-session state MUST re-bind to the new id after every reset.
- **Residual caveat (→ dogfood SK-6/SK-11):** the spike ran at ~2% fill; behavior at the real ~90% threshold (and whether native auto-compaction ever races us) is validated during dogfood, not here.

Original gate (now satisfied) — each could have deadlocked or silently no-op'd the cycle if assumed wrong:
- **E1 (anti-loop, §7.2):** Does `/clear` mint a new `session_id` visible in the statusLine stdin JSON, and how fast does `used_percentage` drop after `/session-resume`? → fixes §7.2 condition (a).
- **E2 (whole cycle, §5.3–5.4):** Does injecting `/clear` then `/session-resume <path>` into one pane preserve the second command, or does `/clear` flush queued input? → determines whether §5.3a needs a readiness probe vs. is impossible (forcing a restart-the-pane design instead of in-place).
- **E3 (drop visibility, §3.2/§7.2b):** Does the statusLine re-run (gauge update) after `/clear` with no assistant message, so the keeper can observe the drop? → if not, §7.2(b) and §5.3a need a different signal.
- **E4 (idle truth, §4.5):** Does the Stop hook fire on a true await-input boundary, or also mid-todo-list continuation? → fixes the Phase-2 idle gate.

Exit criterion: **SATISFIED** — all four answered (above) and wired into §§5–7. Phase-2 implementation beads may open.

## 13. Operational guards (from review)

- **13.1 Single-keeper lock.** A keeper acquires a per-agent lockfile `.harmonik/keeper/<agent>.lock` (pidfile semantics) at start; a second `harmonik keeper --agent <same>` exits non-zero. Prevents duelling injectors into one pane.
- **13.2 Opt-in marker.** The keeper acts **only** on a pane whose agent has set `.harmonik/keeper/<agent>.managed` (the orchestrator opts itself in at launch). The keeper refuses to inject `/clear` into an operator-driven, non-context-managed pane lacking the marker. Fail-safe default: no marker ⇒ no destructive action.
