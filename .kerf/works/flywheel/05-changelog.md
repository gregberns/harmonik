# 05 — Spec-draft changelog (flywheel)

> Rollup of the four spec drafts in `05-spec-drafts/` produced 2026-05-30. Use this as orientation when merging the drafts into `specs/` in the implementation phase.

## Files in `05-spec-drafts/`

| File | Status | Purpose |
|---|---|---|
| `cognition-loop.md` | NEW (v0.1.0) | The cognition loop itself. CL-001 … CL-100 + 5 invariants + 5 conformance scenarios + 5 open questions. The primary new spec. |
| `process-lifecycle.md` | DIFF | PL-019 promotion to normative cognition-process spec; PL-006d orphan-sweep exclusion (closes hk-hc3qq); PL-028 / PL-028d `harmonik supervise` + `harmonik digest` command-surface. |
| `event-model.md` | DIFF | §4.11 consumer contract for `harmonik subscribe` (EV-037 … EV-041); §8.12 new `decision_required` / `decision_acknowledged` types + §4.12 dispatch-blocking rule (EV-042 … EV-044); §10.3 stale-CLI-help fix. |
| `execution-model.md` | DIFF | §4.13 eager-refill (EM-062 / EM-063); §4.14 check-observed-before-submit guard (EM-064 / EM-065); EM-NOTE-WAKE + EM-NOTE-STREAM-CONCURRENCY (hk-24xn1 closed; streams DO run concurrently — V2 correction). |

## Cross-spec coordination required

The four drafts are mutually consistent. Three cross-spec edits land together as one atomic adoption:

1. **PL-019 → CL.** `process-lifecycle.md` PL-019's informative paragraph SHOULD become "see `cognition-loop.md`" rather than describing the role inline. PL-018 LLM-free invariant is unchanged.
2. **PL-006d ↔ EV §8.7.14.** New PL-006d adds `coordinator_sessions_skipped` to the `daemon_orphan_sweep_completed` payload — `event-model.md` §8.7.14 gains this field on the same merge.
3. **CL-060 / CL-061 ↔ EV §4.11 + §8.12.** Cognition-loop consumer obligations (CL-060) reference EV-037 … EV-041; CL-061's wake-filter includes `decision_required` (EV §8.12.1).

## Locked-in design positions reflected in the drafts

- **Architecture B-plus (agent-led judgment + deterministic floor).** CL-010 / CL-011 / CL-013.
- **MemGPT 70 / 90 / 100 % thresholds.** CL-011. Tunable operational constants at v0.1.
- **Regime-B fresh context per recycle.** CL-020. Incremental mid-stream trimming FORBIDDEN at v0.1.
- **Two-phase done** (`run_completed` event AND `Refs:` trailer on `origin/main`). CL-051.
- **Watermark + reacted-ledger + ordering invariant** (effect → ledger → watermark). CL-052 / CL-053. Effectively-once across crashes (CL-056).
- **Eager pure-code refill** (no LLM on slot-freed). EM-062 / EM-063 + CL-071 / CL-072.
- **Empty-queue boundary is the ONLY LLM-wake for queue composition.** CL-073.
- **Single supervisor process, separated lifecycle from daemon, sentinel-file orphan-sweep carve-out.** PL-019 + PL-006d.
- **`harmonik supervise` and `harmonik digest` are the operator surfaces.** PL-028 / PL-028d + CL-080 / CL-082.
- **`harmonik subscribe` is the event channel** (landed; not "planned"). CL-060 + EV §4.11.

## Corrections folded in from the round-2 verification (review-synthesis.md)

- V1: hk-24xn1 (wake-on-append) is **closed**. EM-NOTE-WAKE.
- V2: stream groups DO run concurrently at `max_concurrent > 1` — `streamEligible()` skips dispatched items. EM-NOTE-STREAM-CONCURRENCY.
- V3: "git wins" must read `origin/main`, not local. EM-063 + EM-064 + CL-051.
- V4: `event_id_hwm` is spec-only, not implemented; watermark uses UUIDv7 + ScanAfter and tolerates absence. CL-052.
- V5: `ScanAfter(missingID)` does NOT cold-start. CL-054 mandates the loop's own corrupt-state detector triggers cold-start.
- V6: Pi-OAuth-Max do-not-ship; substrate is raw API on an API key (Pi-as-code OK). OQ-CL-003.
- V7: cross-request prefix cache reuse confirmed (content-addressed). CL-020 / CL-022 / CL-023.
- V8: Claude Code SDK can't disable compaction; `--resume` replays full transcript → SDK unsuitable at the cognition layer. OQ-CL-003.
- V9: heartbeat `active_runs` has NO `run_id` field. EV-039.

## Bead already filed against this draft

- **hk-hc3qq** (P1 bug) — "PL-006 orphan-sweep kills coordinator/orchestrator tmux sessions (flywheel blocker)". Closing this bead implements PL-006d.

## Pending leverage items (under vet in round-4)

Three vetting agents are deep-diving `flywheel_gateway` for ports into harmonik. The integration pass (06-integration.md) folds in whichever survive vetting:

1. Agent lifecycle state machine (`agent-state.ts` + `agent-state-machine.ts`). Candidate Go port at `internal/agent/lifecycle/`.
2. Supervisor restart-policy module (`supervisor.service.ts`). Candidate Go port at `internal/supervise/` — replaces the placeholder `--watch-restart` shim.
3. Context-health 75 / 85 / 95 banding + 4 named strategies. Candidate amendment to `cognition-loop.md` (spec-text only).

## Next passes

- **06-integration.md** — wiring: how new specs compose, dependency order for the bead set, the `harmonik` ↔ supervise process ↔ Pi extension surface, spec-edit ordering. Includes vetted leverage items.
- **07-tasks.md** — bead decomposition. Estimated: `harmonik digest` Go subcommand (+spec); `harmonik supervise` Go subcommand group + config schema; spec edits (PL/EV/EM bundle, atomic merge); Pi extension wire-up (event bridge, kerf priority source, fullness floor enforcement, custom TUI panel); fat-skills authoring (initial catalog); the three leverage-item beads (vetting-pending).
- **Finalize** — `kerf finalize flywheel --branch flywheel-impl` produces branch + beads; or `br create` per task. Then `harmonik run --beads ...`.

## Revision history

| 2026-05-30 | 0.1.0 | flywheel/spec-draft | Four-file initial draft (cognition-loop NEW; process-lifecycle / event-model / execution-model diffs). |
