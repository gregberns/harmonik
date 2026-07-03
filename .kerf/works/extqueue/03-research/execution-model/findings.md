# execution-model.md — research findings for extqueue

Evidence-only. File:line anchors.

## Q1 — Current dispatch-loop shape

The normative dispatch loop is **§7.4 EM main-loop pseudocode** at `specs/execution-model.md:1021-1089`, declared "the single source of truth for how a run is created, dispatched, advanced, and terminated" (`:1023`). It maps to workloop.go as:

| Step | Spec anchor | workloop.go lines |
|---|---|---|
| Pause-between-runs | `:1035` `should_pause_between_runs()` (cites operator-nfr §4.3) | not yet wired |
| Ready-poll | `:1037` `beads.ready_work_query()` → BI-013 (`specs/beads-integration.md:253-258`) | `:391-407` `brAdapter.Ready` |
| Pick-one | `:1040` `pick_one(...)` — "caller policy; MVH: oldest-first tiebreak" | `:418-422` `readyRecords[0]` |
| Validator | `:1042` `validator.validate(workflow)` → EM-038 | not yet wired |
| Create run + atomic claim | `:1045-1046` (BI-009) | `:425-495` UUID mint + `ShowBead` pre-claim guard (hk-p4xbw, no spec anchor) + `ClaimBead` |
| run_started emit | `:1047` per EM-015a (`:231-233`) | downstream of `:540` goroutine spawn |
| Inner dispatch (cascade + checkpoint) | `:1052-1080` `execute_workflow` (§7.2 + §7.3) | `:540`+ mode dispatch, handler launch |
| Finalize / Step-9 terminal | `:1082-1089` `finalize_run` (EM-015b + BI-010) | `:842-908` |

§7.4 is single-run shape; concurrency is grafted on by the implementation (capacity gate `:382-389`, claim semaphore `:360-370`) with **no §7.4 anchor**. Implementation has run ahead of spec on parallelism.

## Q2 — Group-barrier / "wait for set of runs" precedents

None exist. Search `wait for|barrier|cohort|all runs|set of runs` across `execution-model.md`, `scenario-harness.md`, `reconciliation/spec.md` returns only:
- EM-042a gate-pending wait — **within one run** (`specs/execution-model.md:644`).
- RC-018 budget-exhausted wait for **one investigator subprocess** (`specs/reconciliation/spec.md:504`).
- Daemon-shutdown `wg.Wait()` (workloop.go:`:377, :385, :397`) — internal Go primitive, not spec'd.

§7.1 run state machine (`:953-961`) is **per-run**; EM-INV-001 (`:438`) is per-run. There is no multi-run/cohort state machine anywhere in the corpus. Synchronization primitives in workloop.go (`sync.WaitGroup` `:352`, buffered-channel semaphore `:370`, `runRegistry.Len()` poll `:383`) are entirely code-side.

## Q3 — `--max-concurrent` and the claim-semaphore (hk-e61c3.3)

`grep "e61c3"` against `specs/` returns **zero hits** — all hk-e61c3.* requirements are code-anchored only. References:
- `cmd/harmonik/main.go:158-162` — `--max-concurrent`, default 1; cites "POST_MVH_PARALLELISM_ROADMAP row 6, hk-e61c3.1".
- `internal/daemon/daemon.go:119-126` — `Config.MaxConcurrent`; "Default: 1. Valid range: ≥1."
- `internal/daemon/workloop.go:360-370` — `claimSem` buffered-channel semaphore, token acquired at `:474-479`, released at `:482`. Bounds **only concurrent SQLite-write surface** (ClaimBead invocations).
- `internal/daemon/workloop.go:382-389` — **separate** capacity gate on `runRegistry.Len() >= effectiveMax`. This is the in-flight-bead ceiling.

Two distinct knobs share one `effectiveMax`: (a) in-flight-bead ceiling (registry-Len gate), (b) claim-write throttle (semaphore). Both unspec'd. A queue group of 8 with max-concurrent=2 composes mechanically: the registry-Len gate at `:383` already serializes goroutine spawn to `effectiveMax`, so the group's parallelism is naturally capped.

## Q4 — State-machine spec patterns

Three precedents in the corpus:

**A. Numbered-step sequence (PL-005 startup)** — `specs/process-lifecycle.md:227-248`. Steps 0–9 in prose, each MUST/SHOULD, with side-by-side cross-refs. Closed with `Tags: mechanism` + `Axes:` lines (`:250-251`). Used for deterministic init order.

**B. State-transition table (EM §7.1)** — `specs/execution-model.md:949-963`. Columns `From | Event | Guard | To | Emits`. Seven rows. Followed by `> INFORMATIVE` carve-out (`:963`).

**C. Protocol pseudocode block (EM §7.2/§7.3/§7.4)** — `:965-989`, `:993-1017`, `:1021-1089`. Fenced code with FUNCTION/IF/RETURN; inline `-- §X.Y EM-NNN` per branch; closer paragraph: "every branch point above corresponds to a normative requirement" (`:991`, `:1019`, `:1091`).

For queue group lifecycle, B + C are the precedent: transition table, then pseudocode, then a branch-to-normative-requirement closer.

## Q5 — Step-9 terminal-event logic in spec

`workloop.go:846-907` (CloseBead vs ReopenBead branches) is anchored at:

- **EM-015b** (`specs/execution-model.md:238-240`): "MUST emit exactly one of `run_completed` or `run_failed` ... The terminal-transition bead write per [beads-integration.md §4.4 BI-010] MUST follow the terminal event emission; it MUST NOT precede it."
- **§7.4 finalize_run** (`:1082-1089`): binary branch on `terminal_success | terminal_failure`; each branch emits the event then writes the bead transition.

workloop.go has three branches (CHB-020 stop-hook outcome at `:870-879`; exit=0 fallback at `:880-891`; default-failure at `:893-908`). The three-branch fan-in lives in `claude-hook-bridge.md` CHB-020, not EM. The cleanest amendment shape for `queue_item_completed`/`queue_item_failed` is **additive at the EM-015b emission site** inside `finalize_run` — same call-site as `run_completed`/`run_failed`, payload extended with `queue_id` + `group_index`. (This matches `02-components.md` §5's "existing `run_started`/`run_completed`/`run_failed` SHOULD additionally carry `queue_id` and `queue_group_index`" framing.)

## Q6 — MVH-era prose to retire

- `specs/execution-model.md:1023` — "**the** orchestrator's main loop" framed as singular.
- `:1034-1050` — outer `WHILE NOT shutdown_requested()` reads as serial; no fork/spawn step between `pick_one` and `execute_workflow`.
- `:1037` — `ready_beads = beads.ready_work_query()` — the principal site extqueue displaces.
- `:1040` — `pick_one(ready_beads)  -- caller policy; MVH: oldest-first tiebreak`. This is the explicit MVH-era pick-one assumption.
- `:1042-1043` — in-line validator call assumes serial dispatch.
- `:1049-1050` — `execute_workflow(run); finalize_run(run)` flows serially with no parallel-dispatch alternative.

`:175` ("EXACTLY ONE workflow ... EXACTLY ONE input") is per-run-scoped invariant, NOT single-threaded prose; leave alone. The doc-fix agent has not touched §7.4.

## Patterns to adopt

- **State-transition table** (§7.1 precedent) for group lifecycle `pending → active → complete-success | complete-with-failures → paused`.
- **Numbered-step prose** with `Tags: mechanism` + `Axes:` closer (PL-005 precedent) for submit/append/dry-run validation.
- **Pseudocode + branch-to-requirement closer** (§7.2/§7.3/§7.4 precedent) for the queue-aware dispatch loop replacing §7.4 lines `:1037-1049`.
- **Additive event emission alongside existing emission** (EM-015b precedent): `queue_item_completed/failed` at same call-site as `run_completed/failed`, with `queue_id`+`group_index` payload fields.

## Risks / conflicts

- **§7.4 is the declared S01 single-source-of-truth (`:1023`).** Replacing `:1037-1049` is high-leverage; minimize blast radius by preserving the `orchestrator_main_loop` / `execute_workflow` function signatures and only swapping the queue-pull for the ready-poll.
- **No spec anchor for claim-semaphore or registry-Len gate.** Spec-first discipline suggests extqueue spec-bumps the implementation by adding EM-NNN requirements for both concurrency primitives. Otherwise hk-e61c3.* remains code-only forever.
- **§7.1 is per-run.** Adding a queue-group state machine creates a parallel state machine. Use the §7.1 `INFORMATIVE` carve-out pattern (`:963`) so readers don't conflate the two.
- **`pick_one` MVH policy at `:1040` is "oldest-first".** Post-extqueue policy is "head of active group". The line cannot be patched in place; the whole `ready_beads = ... pick_one` block is replaced by a queue-pull.
- **CHB-020's three-branch terminal mapping (workloop.go:870-908) is bridge-spec'd, not EM-spec'd.** Don't touch CHB. Queue-event emission attaches at the EM-015b emission site, the common downstream of all three branches.
- **PL-005 step 6's `br ready` (`specs/process-lifecycle.md:238`) is reconciliation input, not dispatch input.** This survives extqueue unchanged — don't conflate the two uses. The BI-013 ready-work query at `specs/beads-integration.md:253-258` is the **dispatch** consumer that retires.
- **hk-p4xbw `ShowBead` pre-claim guard (workloop.go:439-469) has no EM anchor.** It's defensive against concurrent claim from a competing daemon. Under extqueue with single-orchestrator-per-daemon (`02-components.md` §4), the multi-daemon TOCTOU window narrows — but the guard remains relevant if a human runs `br update --status=closed` concurrently. Note for design pass, not a blocker.
