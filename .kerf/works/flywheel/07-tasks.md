# Implementation Tasks

> Pass-7 output. 12 beads decomposing the flywheel implementation. All `codename:flywheel`. Dependency graph + parallelization plan at the bottom.

## Task list

### B0 — Spec edits bundle (PL/EV/EM/HC + new cognition-loop.md)

- **What:** Atomic merge of all spec edits in `05-spec-drafts/` + the two amendment slices from the round-4 vets. One PR; one schema-version bump on event-model. Closes hk-hc3qq.
- **Spec sections:** PL-019 promotion + PL-006d + PL-028d; EV-037..044 + decision_required / decision_acknowledged + §10.3 CLI fix; EM-062..065 + EM-NOTE-WAKE + EM-NOTE-STREAM-CONCURRENCY; HC-064..067 (per-session lifecycle FSM); CL-014/CL-015 (band rename + non-conformance clause); new `specs/cognition-loop.md` (CL-001..CL-100 + 5 invariants).
- **Deliverables:** `specs/cognition-loop.md` (NEW); diffs to `specs/process-lifecycle.md`, `specs/event-model.md`, `specs/execution-model.md`, `specs/handler-contract.md`. CLI help text fix at `cmd/harmonik/subscribe.go:14,200`. Glossary entries in §3 of PL/HC.
- **Acceptance:** All 5 spec files parse/render; cross-refs resolve; agent-reviewer APPROVE; schema-version bump on event-model is N-1-readable per ON-018; hk-hc3qq references PL-006d as its fix.
- **Depends on:** none.
- **Type:** task. **Priority:** 1.

### B1 — `harmonik digest` Go subcommand

- **What:** Pure-Go status sheet builder. Reads `queue.json`, `origin/main` git log, `events.jsonl` via `ScanAfter(watermark)`, `br ready`/`br list --status in_progress`, `.harmonik/cognition/notes.jsonl`, `kerf next --format=json`. Emits structured JSON + a human view. NO LLM.
- **Spec sections:** CL-030..CL-033, OQ-CL-002 (pins as subcommand for v0.1). Also drafts a short `specs/digest-command.md`.
- **Deliverables:** `cmd/harmonik/digest/` package + `cmd/harmonik/main.go` dispatch entry (before `flag.Parse`); `internal/digest/` builder package; schema-versioned JSON struct; tests under `internal/digest/testdata/`. New tiny spec `specs/digest-command.md`.
- **Acceptance:** `harmonik digest` snapshot mode runs without daemon (file-surface read); `--json` emits one schema-versioned NDJSON object; `--since <event_id>` filters via ScanAfter; missing `.harmonik/` → exit 7; tests cover ≥10 active runs / ≥20 open notes truncation per CL-032.
- **Depends on:** B0 (CL spec must land for the schema reference).
- **Type:** feature. **Priority:** 2.

### B2 — `internal/handlercontract/lifecycle/` FSM Go package

- **What:** ~250 LOC Go port of the gateway agent-state machine. `LifecycleState` enum (8 states, PAUSED→Suspended rename), `Transition` struct, `Machine` w/ ring history (size 50), `InvalidStateTransitionError`. Leaf package (no internal/ deps).
- **Spec sections:** HC-064..HC-067 (in B0).
- **Deliverables:** `internal/handlercontract/lifecycle/{types,machine,errors,table}.go` + `machine_test.go`. Depguard/component-matrix entry.
- **Acceptance:** `go build/test ./internal/handlercontract/lifecycle/...` green; table-driven test covers every legal+illegal edge; ring-eviction test (51 transitions, oldest dropped); concurrent-Transition smoke; lint+vet clean. No event emission yet (B4 wires it).
- **Depends on:** B0.
- **Type:** feature. **Priority:** 2.

### B3 — `internal/supervise/` supervisor package

- **What:** ~250-350 LOC Go port of the gateway `supervisor.service.ts`. Spawn loop + onExit + restart policy (`on-failure | never` — `always` dropped) + exponential backoff (mirrors HC-048a `Base/Cap/Jitter/MaxRestarts`) + crash-loop guard (sliding window). Health probe = `kill(pid,0)` + heartbeat-file freshness (NO HTTP).
- **Spec sections:** PL-019(f) — replaces the placeholder "30-50 LOC shim" with a richer module.
- **Deliverables:** `internal/supervise/{supervisor,spec,state,backoff,health}.go` + tests; depguard entry. No CLI yet (B5 wires).
- **Acceptance:** Simulated child exit code=1 three times → 3 restarts with monotonic backoff; child exit code=0 + policy=on-failure → no restart; MaxRestarts=2 → 2 restarts then `crashloop` + `Run()` returns; heartbeat-file staleness → `Status="unhealthy"`, no restart; SIGTERM forwarded to child; `Stop(timeout)` follows PL-011 SIGTERM→bounded-wait→SIGKILL.
- **Depends on:** B0.
- **Type:** feature. **Priority:** 2.

### B4 — Wire LifecycleState into Session/watcher; emit `lifecycle_transition`

- **What:** Integrate the B2 FSM into the live handler/Session/watcher; register the new `lifecycle_transition` event with the existing event bus; ensure twin parity (HC §4.8).
- **Spec sections:** HC-064..067 + event-model.md `lifecycle_transition` registration (in B0).
- **Deliverables:** edits to `internal/handler/session.go` (NewSession constructs Machine, Transitions to StateSpawning on cmd.Start); `internal/handlercontract/adapter.go` (watcher transitions on `agent_ready/started/completed/failed/rate_limited`); `internal/daemon/workloop.go` (SIGTERM→Terminating; Wait→Terminated|Failed); `internal/eventmodel/types.go` registers `lifecycle_transition`. Twin parity smoke.
- **Acceptance:** Scenario test of happy-path emits Spawning→Initializing→Ready→Executing→Terminating→Terminated in order; silent-hang scenario transitions to StateFailed{silent_hang} BEFORE `run_stale` would have; hk-za5mz repro surfaces as a deterministic transition-timeout event (not 10-min `run_stale`). Schema-version on event-model.md preserved (additive).
- **Depends on:** B2, B0.
- **Type:** feature. **Priority:** 2.

### B5 — `cmd/harmonik/supervise/` CLI surface

- **What:** Implement the `harmonik supervise [start|stop|status|attach|restart|logs]` family per PL-028d. `start` acquires flock (`.harmonik/cognition/supervisor.lock`), writes pid + sentinel + config, launches Pi extension in a tmux pane named `harmonik-<project_hash>-flywheel` with `remain-on-exit on`; `--watch-restart` invokes `internal/supervise.Supervisor`. `stop` follows PL-011. `status` is file-surface only. `attach` execve's `tmux attach-session`. `restart` re-reads config. `logs` is `tmux capture-pane -p -S -<n>`.
- **Spec sections:** PL-019(b,c,e,f), PL-028d, PL-006d sentinel discipline (in B0).
- **Deliverables:** `cmd/harmonik/supervise/{start,stop,status,attach,restart,logs}.go`; config.json schema (`schema_version`, `model`, `token_cap`, `max_concurrent`, `budget_cap_usd_per_day`, `instructions_path`, `priority_source`, `areas`, `epic`, `restart_policy`, `restart_max`, `restart_base_ms`, `restart_cap_ms`, etc.). Pre-`flag.Parse` dispatch in `cmd/harmonik/main.go`.
- **Acceptance:** `harmonik supervise start --watch-restart` launches a tmux pane with respawn-on-crash; `harmonik supervise stop` cleanly terminates both shim and child; integration test: fake supervisee exits code=1 → shim restarts 2x then `crashloop` visible via `status --json`; flock + sentinel properly held/released; `start` refuses with exit 17 when daemon socket missing; refuses with exit 25 when supervisor.lock held.
- **Depends on:** B3, B1.
- **Type:** feature. **Priority:** 2.

### B6 — `harmonik digest --watch` live TUI loop

- **What:** Watch-mode for the digest command: polls digest producer at ~1s cadence; renders structured human-readable view of in-flight runs, recent completions, open notes, watermark age. Read-only.
- **Spec sections:** CL-082.
- **Deliverables:** `cmd/harmonik/digest/watch.go` + minimal TUI (bubbles/lipgloss or rich_rust analog — match harmonik's existing TUI idiom); graceful degrade to file-poll when daemon socket absent.
- **Acceptance:** `harmonik digest --watch` displays live counts updating as the daemon emits events; quitting cleanly; renders durations + ages.
- **Depends on:** B1.
- **Type:** feature. **Priority:** 3.

### B7 — Pi extension: event bridge + watchdog timers

- **What:** Implement the bridge inside `./.pi/extensions/flywheel/`: spawn `harmonik subscribe --types ... --heartbeat 60s --since-event-id <watermark>` as a child process; tail NDJSON; classify per the 3-tier wake filter (CL-061); ~400ms debounce for bursts (CL-062); urgent class (`merge_conflict`) calls `harness.abort()` then `harness.prompt(urgent_digest)`; watchdog timers (quiet / run-stall / daemon-down per CL-064). Persist watermark + reacted-ledger to `.harmonik/cognition/state.json` per CL-052/053. Emit cognition events to `.harmonik/cognition/cognition-events.jsonl` (OQ-CL-004 pinned).
- **Spec sections:** CL-060..CL-064, CL-052..CL-056 (watermark + ordering invariant).
- **Deliverables:** `./.pi/extensions/flywheel/bridge.ts`, `watermark.ts`, `wake-filter.ts`, `debounce.ts`, `watchdog.ts`. Updates to `index.ts` to register them on startup. Tests under `__tests__/`.
- **Acceptance:** Replay-test: feed a recorded events.jsonl through the bridge, assert correct reactions WITHOUT real-push; effect→ledger→watermark ordering verified by crash-injection test; watermark never regresses (max with heartbeat.last_event_id); merge_conflict aborts in-flight turn within 2s; subscription_gap forces a `ScanAfter` re-sync + git+queue.json re-sense.
- **Depends on:** B0.
- **Type:** feature. **Priority:** 2.

### B8 — Pi extension: stratified model routing + budget kill-switch

- **What:** Implement `prepareNextTurn` model stratification (Haiku tier 1 / Sonnet tier 2 / Opus tier 3) per `03-research/multi-llm-stratification/findings.md`. Add per-day USD budget tracking with the 80%/90%/100% graceful-downgrade pattern + hard halt at 100% (CL-090). Add reaction-rate circuit breaker (CL-091).
- **Spec sections:** CL-070..CL-073 (queue pressure), CL-090..CL-091 (safety+budget).
- **Deliverables:** `./.pi/extensions/flywheel/router.ts`, `budget.ts`, `circuit-breaker.ts`. Updates to `index.ts`.
- **Acceptance:** Routine wake → Haiku; normal wake → Sonnet; pattern_detected / failed-twice / escalate_user → Opus once (one-shot exception_flag); budget tracking emits `flywheel_budget_exhausted` at cap; circuit breaker trips on >N reactions/window; emits structured events for operator observability.
- **Depends on:** B0.
- **Type:** feature. **Priority:** 2.

### B9 — Pi extension: custom TUI status panel

- **What:** Custom Pi TUI widget rendering the same status sheet (the digest) the agent reasons over: running/done/failed with durations + ages, what needs a decision, open notes, current fullness %. Uses `ctx.ui.setWidget` (no Pi fork).
- **Spec sections:** CL-081 (tmux inspectability) + CL-082 (digest --watch parity).
- **Deliverables:** `./.pi/extensions/flywheel/tui-panel.ts`. Wires into `index.ts` startup.
- **Acceptance:** Panel renders live; operator sees same digest the agent sees; updates within 1s of daemon events.
- **Depends on:** B7 (uses watermark + classifier).
- **Type:** feature. **Priority:** 3.

### B10 — Fat-skills initial catalog

- **What:** Author the initial fat-skills under `.flywheel/skills/`: `triage-failure.md`, `investigate-run.md`, `compose-batch.md`, `escalate.md`, `reconcile-state.md`. Each is a markdown procedure the agent fetches on demand via the `read_skill` tool.
- **Spec sections:** Layered-instructions L3 (operational, not normative); referenced by CL-002 skill-index.
- **Deliverables:** 5 markdown files in `.flywheel/skills/`. `read_skill` tool registration in `./.pi/extensions/flywheel/index.ts` if not yet present.
- **Acceptance:** Each skill is ≤200 lines; the agent invokes `read_skill("triage-failure")` on a synthetic `run_failed` event and the response is consumed in-context; skills are versioned (sha-pinned per CL-002).
- **Depends on:** B7 (skill consumption happens in the agent's loop driven by the bridge).
- **Type:** docs. **Priority:** 3.

### B11 — Integration smoke

- **What:** Run the flywheel unattended for 4h against the real harmonik daemon. Verify the CL conformance scenarios (1-5) hold in vivo.
- **Spec sections:** CL §7 (Conformance) acceptance scenarios.
- **Deliverables:** A runbook under `.flywheel/runbooks/v0.1-smoke.md` documenting startup, observed cache-hit rate, refill behavior, the 10-in-flight crash scenario reproduction, any defects found.
- **Acceptance:** (1) MemGPT 100% floor fires when agent skips reset_context; (2) `cache_read_input_tokens ≥ 0.8 × stable_prefix_size` post-recycle; (3) 10-in-flight crash scenario: zero double-dispatch, zero dropped completion, zero missed failure; (4) merge_conflict aborts current turn + wakes model within 2s; (5) empty-queue boundary wakes model exactly once per boundary. Budget kill-switch verified.
- **Depends on:** B4, B5, B6, B7, B8, B9, B10.
- **Type:** task. **Priority:** 2.

## Dependency graph

```
                            B0 (spec edits)
                            /  |   |  \
                          /    |   |    \
                       B1     B2  B3   B7  B8
                        |      |   |    |
                        |      B4  B5   |
                        ├─ B6  |   |    |
                        │      |   B5──┤
                        │      │      │
                        │      B9 ←───┤  (B9 depends on B7; placed visually here)
                        │              │
                        └─ B10 ←───────┤  (B10 depends on B7)
                                       │
                                      B11 ←─ ALL OF B4, B5, B6, B7, B8, B9, B10
```

Cleaner edge list:
- B0 → B1, B2, B3, B7, B8
- B1 → B5, B6
- B2 → B4
- B3 → B5
- B7 → B9, B10
- {B4, B5, B6, B7, B8, B9, B10} → B11

## Parallelization plan

- **Phase 0 (serial):** B0 alone. Must land first.
- **Phase 1 (parallel ×3):** B1, B2, B3, B7, B8 (and B7/B8 are TS, B1/B2/B3 are Go — clean separation).
- **Phase 2 (parallel ×3):** B4 (after B2); B5 (after B1+B3); B6 (after B1); B9 (after B7); B10 (after B7).
- **Phase 3 (serial):** B11 (depends on everything above).

Estimated wall-clock at 4-agent concurrency: B0 (~1 day) + max(phase 1 ~2 days) + max(phase 2 ~2 days) + B11 (~1 day) ≈ **6 working days** with appropriate `--max-concurrent` on harmonik runs.

## Total: 12 beads (+ 1 existing P1 bug hk-hc3qq folded into B0)

The orphan-sweep bead **hk-hc3qq** (already filed, P1) is the only pre-existing bead; B0 closes it. All 12 new beads are labeled `codename:flywheel` for kerf grouping.
