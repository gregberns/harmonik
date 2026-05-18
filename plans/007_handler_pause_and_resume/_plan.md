# Plan 007: handler-pause-and-resume

## Objective
Add per-handler-type pause-and-resume to the daemon so a handler-fatal failure (Claude rate-limit, session-token cap) pauses dispatch for that handler only, persists across restart, and exposes an operator + submitter-agent surface — instead of tombing a 50-bead wave against a known-broken handler.

## Status
research-phase — design landed; implementation not started.

## What's done
- Design doc landed at commit `b554b6f`: [`docs/components/internal/handler-pause-and-resume.md`](../../docs/components/internal/handler-pause-and-resume.md) — problem, Phase-1 scope, trigger taxonomy, controller shape, persistence layout, CLI surface, spec amendments (Appendix A: HC-020a, execution-model §8 INFORMATIVE note, QM-060 single-writer mirror, PL-005 startup step 8a). **Now superseded: normative spec at [`specs/handler-pause.md`](../../specs/handler-pause.md) (hk-m7joe).**
- 13 beads filed (all labeled `handler-pause`, 2026-05-18).
- ROADMAP entry inserted at position 9 (between Phase-2 multi-bead E2E and remaining spec corpus); Phase-3 DOT shifted 11 → 12. See [`ROADMAP.md`](../../ROADMAP.md).

## What's remaining
- **P1 Phase-1 (9 beads):**
  - `hk-107gz` — handler-fatal failure-class taxonomy + policy table (Go constants + spec amends HC-020a, §8 note)
  - `hk-m0k0a` — persistence: `.harmonik/handler-state.json` + atomic-write + load on startup
  - `hk-9hwbw` — `HandlerPauseController` + in-flight bead freeze-list (central hub)
  - `hk-37zy8` — daemon outcome-ingestion → pause-trigger policy goroutine
  - `hk-kac8g` — dispatcher skip-on-paused + `queue_item_held_for_handler_pause` event
  - `hk-ejyku` — `harmonik handler resume <type>` CLI
  - `hk-39ryh` — `harmonik handler status` CLI + JSON output (submitter-agent surface)
  - `hk-siuo2` — `QueueValidationReason = handler_paused` on queue-submit
  - `hk-ifqnj` — event-model amendments (`handler_paused`, `handler_resumed`, `queue_item_held_for_handler_pause`)
- **P2 (2 beads):** `hk-xlq2e` (submitter-agent docs), `hk-tvsl7` (per-handler `Diagnose()` seam, post-MVH).
- **P3 / post-MVH (3 beads):** `hk-0otqs` (auto-resume on timed backoff), `hk-bdvae` (external-trigger resume — webhook / SIGUSR1 / file-marker), `hk-lhxzc` (per-account pause within a handler type).
- **P3 research-only (1 bead):** `hk-bm9qm` (cross-handler task transfer memo).

## References
- normative spec: `specs/handler-pause.md` (elevated from design doc by hk-m7joe)
- design doc (SUPERSEDED, history only): `docs/components/internal/handler-pause-and-resume.md` (commit `b554b6f`)
- specs touched (amendments pending in `hk-107gz`): `specs/handler-contract.md` §4.5a (HC-020a), `specs/execution-model.md` §8, `specs/queue-model.md` (QM-060 single-writer mirror), `specs/process-lifecycle.md` (PL-005 step 8a)
- beads: label `handler-pause` (13 total) — P1 Phase-1 set `hk-107gz hk-m0k0a hk-9hwbw hk-37zy8 hk-kac8g hk-ejyku hk-39ryh hk-siuo2 hk-ifqnj`; P2 `hk-xlq2e hk-tvsl7`; post-Phase-1 `hk-0otqs hk-bdvae hk-lhxzc hk-bm9qm`
- roadmap: `ROADMAP.md` row 9
- chat-context: Phase-2 dogfooding (HANDOFF v47 §3) flagged that a single Claude rate-limit near the head of a 50-bead wave would tomb the entire wave with non-work FAIL records. A 2026-05-18 design pass produced the doc + bead set above; implementation deferred to its own slot.

## Next steps
1. Start the P1 Phase-1 chain at the two roots in parallel — they have no upstream blockers and together unblock the central controller:
   - `hk-107gz` (taxonomy: Go constants + HC-020a + §8 note)
   - `hk-m0k0a` (persistence file + atomic-write + startup load)
2. With both root beads landed, implement `hk-9hwbw` (`HandlerPauseController` + freeze-list) — the central hub blocking the remaining six P1 beads.
3. Fan out the six dependents of `hk-9hwbw` in dependency order: `hk-37zy8` (policy goroutine, also depends on `hk-107gz`) → `hk-kac8g` (dispatcher skip) → `hk-ejyku` (resume CLI) → `hk-39ryh` (status CLI) → `hk-siuo2` (queue-submit validation) → `hk-ifqnj` (event-model amendments — can land anytime after `hk-9hwbw` defines the event names).

## Done means...

Phase-1 completion requires ALL of the following observable states — not "the beads shipped":

1. **Pause triggers automatically.** `harmonik run` processing a bead that returns a `handler_fatal` outcome emits a `handler_paused` event (Class F) AND writes `.harmonik/handler-state.json` within 1 second of outcome ingestion. Verified by `TestHandlerPauseController_TriggerOnFatal` passing.

2. **Pause persists across restart.** A daemon killed while paused and restarted from the same working directory resumes in paused state — no beads of the paused handler type are dispatched before `harmonik handler resume` is called. Verified by `TestHandlerPausePersistence_AcrossRestart` passing.

3. **Queue holds, not drops.** Beads submitted to a paused handler are held in the queue with `validation_reason = handler_paused` and emitted as `queue_item_held_for_handler_pause` (Class O). They are NOT failed, NOT dropped, NOT silently queued. Verified by `TestQueueHoldsOnHandlerPaused` passing.

4. **Operator surface is functional.** `harmonik handler status` exits 0 and returns valid JSON containing `paused: true` + `cause` when paused. `harmonik handler resume <type>` unpause and emits `handler_resumed` (Class F). Both verified by CLI integration tests.

5. **Submitter-agent surface is functional.** `harmonik handler status --json` output (from item 4) is the sole signal a submitter agent needs to decide whether to queue new beads. No bead is auto-failed until the operator resumes. Verified by `hk-39ryh` acceptance criteria.

6. **Smoke test GREEN.** The Phase-2 dogfood smoke (`harmonik run` dispatching ≥2 beads sequentially, with the second bead's handler forced to rate-limit) produces: first bead DONE, second bead HELD (not FAILED), daemon paused, `handler_paused` event in JSONL, `.harmonik/handler-state.json` present.

7. **Scenario-test bead GREEN.** A twin-based end-to-end scenario test verifies that `harmonik run` dispatching a bead that returns `handler_fatal` triggers pause, emits `handler_paused`, and writes `.harmonik/handler-state.json` — confirming the policy goroutine is wired into the composition root. Covered by `hk-6f1uj` (scenario test: HandlerPause policy goroutine wired end-to-end).

8. **Exploratory-test bead GREEN.** An exploratory test exercises the operator-facing CLI surface (`harmonik handler status`, `harmonik handler resume`) against a running daemon, verifying the JSON output schema and that resume actually unpauses dispatch. Covered by `hk-qxtbq` (scenario test: handler-fatal outcome trips dispatcher gate).

Items 1–5 are covered by unit/integration tests in `internal/lifecycle/`. Item 6 is an E2E test. Items 7–8 are scenario/exploratory tests per the `plans/README.md` testing-criteria requirement. This plan is not done until items 6, 7, and 8 are GREEN.

## Open questions
- **Per-account vs per-handler-type granularity** (`hk-lhxzc`, post-Phase-1): Phase-1 pauses the whole handler type when any account in the pool rate-limits. If multi-account Claude pools become common, we will need a per-account axis. Deferring until evidence demands it.
- **Auth-expired and api-unreachable sub-reasons** (design doc §3 items 2–3): listed in the Phase-1 handler-fatal set but ride on rate-limit's path until handler-contract formally surfaces them. Tracked as a follow-up inside `hk-107gz`; may spin out a separate bead if the migration is non-trivial.
- **Cross-handler task transfer** (`hk-bm9qm`, research-only): whether a Claude-Code-bound bead can be rerouted to Codex while Claude is paused. Memo, not implementation; revisit after Phase-1 ships.
