# extqueue v0.1 — Tasks

Implementation decomposition for the extqueue v0.1 spec package. Each task names: scope, spec traceability, files touched, acceptance criteria, deps.

Conventions:
- Task IDs `TXX` are work-package handles. Each becomes a bead (or epic + child beads) at finalize time.
- "Spec ref" cites the section/ID inside the drafted `05-spec-drafts/*.md` that the task implements.
- "Files" lists primary code files; tests are bundled per task unless called out.
- Deps form a DAG; "depends on" is the strict prerequisite set.

## Tier 0 — Spec landing

Drop the 6 drafted spec files into `specs/` and propagate front-matter version bumps. No code changes. Single commit per spec file recommended for git-blame readability.

### T01 — Land queue-model.md

- **Scope**: new file `specs/queue-model.md`.
- **Source**: `05-spec-drafts/queue-model.md`.
- **Files**: `specs/queue-model.md` (new).
- **Acceptance**: file lands; spec-audit tests (if any) pass; no markdown lint failures.
- **Deps**: none. Foundational; every other task references this spec.

### T02 — Land 5 spec edits

- **Scope**: replace `specs/{execution-model,beads-integration,process-lifecycle,event-model,operator-nfr}.md` with drafted versions.
- **Source**: `05-spec-drafts/{...}.md`.
- **Files**: 5 spec files.
- **Acceptance**: files land; existing spec-audit tests (`TestAR*` family, `specaudit/*_test.go`) pass; front-matter `depends-on` correct.
- **Deps**: T01 (cross-refs to queue-model resolve).

### T03 — Land 05-changelog.md and integration check artifacts

- **Scope**: archive `05-changelog.md` and `06-integration.md` into `docs/historical/extqueue-v0.1-{changelog,integration}.md` for traceability. Optional but consistent with project's "archive significant kerf outputs" pattern.
- **Files**: 2 new docs.
- **Acceptance**: files copied; not gitignored.
- **Deps**: T01, T02. Trivial.

## Tier 1 — Go package scaffold (`internal/queue/`)

### T10 — Subsystem package scaffold

- **Scope**: create `internal/queue/` package directory with `doc.go`, `package queue` declarations, and depguard component-matrix entry per `.golangci.yml`.
- **Spec ref**: queue-model.md §1 (Scope).
- **Files**: `internal/queue/doc.go`, `internal/queue/queue.go` (placeholder), `.golangci.yml` (component-matrix entry).
- **Acceptance**: `go build ./internal/queue/...` succeeds; depguard does not flag misallocated imports.
- **Deps**: T01.
- **Skill**: `go-subsystem-add` (from skill registry).

### T11 — RECORD types in Go

- **Scope**: translate queue-model.md §2 RECORDs into Go structs. `Queue`, `Group`, `Item`, `QueueStatus`, `GroupKind`, `GroupStatus`, `ItemStatus`. JSON marshaling with `schema_version: 1` envelope.
- **Spec ref**: queue-model.md §2.
- **Files**: `internal/queue/types.go`, `internal/queue/types_test.go` (round-trip JSON encode/decode).
- **Acceptance**: round-trip test passes for sample queue documents; `schema_version` enforced on unmarshal (reject != 1).
- **Deps**: T10.

### T12 — source_subsystem registration

- **Scope**: register `github.com/harmonik/internal/queue` as a source_subsystem at init time per EV-034a.
- **Spec ref**: queue-model.md §4 (Identity, by reference); event-model.md §4.9 EV-034a.
- **Files**: `internal/queue/init.go` (calls `core.RegisterSourceSubsystem(...)`).
- **Acceptance**: subsystem appears in registry; duplicate-registration test in `subsystemregistry_hqwn44_test.go` extended to cover it.
- **Deps**: T10.

## Tier 2 — Event-bus integration

### T20 — Register 6 new event types

- **Scope**: add `registerQueueEvents()` helper to `internal/core/eventreg_hqwn59.go` covering §8.10's 6 types. Payload Go types live in `internal/core/queueevents_<codename>.go`.
- **Spec ref**: event-model.md §8.10; queue-model.md §4 (queue_id semantics).
- **Files**: `internal/core/eventreg_hqwn59.go` (call site), `internal/core/queueevents_<codename>.go` (new), `internal/core/eventreg_hqwn59_test.go` (cohort assertion).
- **Acceptance**: all 6 events registered; durability classes match §8.10 (queue_submitted=F, queue_group_started=O, etc.); `mustRegister` panics on duplicate; payload roundtrip test passes.
- **Deps**: T02 (event-model.md landed).

### T21 — Optional fields on run_* payloads

- **Scope**: add `QueueID *string` and `QueueGroupIndex *int` to `RunStartedPayload`, `RunCompletedPayload`, `RunFailedPayload`.
- **Spec ref**: event-model.md §6.3 amendments; queue-model.md §4 QM-011/QM-012.
- **Files**: `internal/core/runterminalpayload.go`, related event payload structs, test.
- **Acceptance**: encoded JSON omits the fields when nil (test); accepts presence when set (test); existing run_* tests pass unchanged.
- **Deps**: T02.

## Tier 3 — Persistence

### T30 — queue.json atomic write

- **Scope**: implement WM-026 atomic-write for `.harmonik/queue.json`.
- **Spec ref**: queue-model.md §3 QM-001 (cites workspace-model.md §4.7 WM-026); workspace-model.md §4.7.
- **Files**: `internal/queue/persistence.go`, `internal/queue/persistence_test.go`.
- **Acceptance**: write produces target file; concurrent-write test sees atomic-or-old (never partial); 1 MiB size limit enforced per QM-004.
- **Deps**: T11.

### T31 — queue.json load on PL-005 step 8a

- **Scope**: extend daemon startup to read `.harmonik/queue.json` after `daemon.state` / `daemon.upgrading`. Recognized schema_version → load. Forward-incompatible → exit 2 (`queue-format-unsupported`). Corrupt → warn + proceed.
- **Spec ref**: process-lifecycle.md §4.2 PL-005 step 8a; queue-model.md §3 QM-002; operator-nfr.md §8 row 2.
- **Files**: `internal/lifecycle/startup_pl005.go` (or wherever step 8a is implemented), `internal/queue/persistence.go` (LoadFromDisk function).
- **Acceptance**: 3 scenario sub-tests: (a) recognized schema_version → loaded; (b) forward-incompatible → exit code 2 + daemon_startup_failed event; (c) corrupt → warning + proceed with nil queue. All asserted via twin-driven scenario tests.
- **Deps**: T11, T30.

### T32 — queue.json unlink on completion

- **Scope**: implement QM-003 unlink + fsync(parent_dir) when queue.status transitions to `completed`.
- **Spec ref**: queue-model.md §3 QM-003.
- **Files**: `internal/queue/persistence.go`, test.
- **Acceptance**: after queue completion, file is gone; parent directory entry is fsync'd; new `queue-submit` recreates the file with a fresh queue_id.
- **Deps**: T30.

## Tier 4 — Core mechanics

### T40 — Validation pipeline (QM-020..QM-027)

- **Scope**: implement the 8 validation rules in order per QM-029a. Returns typed JSON-RPC error (one of 8 reason values); no state mutation on failure.
- **Spec ref**: queue-model.md §6 (entire section).
- **Files**: `internal/queue/validation.go`, `internal/queue/validation_test.go`.
- **Acceptance**: per-rule unit tests (8); order-of-evaluation test (first-failure-short-circuits); QM-025 parallelism-narrowed emits `queue_item_deferred_for_ledger_dep` events but does NOT fail validation.
- **Deps**: T11, T20, T21.

### T41 — Group state machine + dispatch eligibility

- **Scope**: implement queue-model.md §5 transitions. Per-group `pending → active → complete-success | complete-with-failures`; queue-level `active → paused-by-failure | paused-by-drain | completed`.
- **Spec ref**: queue-model.md §5; queue-model.md §8 (Queue Lifecycle).
- **Files**: `internal/queue/state.go`, `internal/queue/state_test.go`.
- **Acceptance**: transition-table conformance test (every row exercisable); emits correct events at each transition (queue_group_started, queue_group_completed, queue_paused, etc.).
- **Deps**: T11, T20.

### T42 — Append semantics

- **Scope**: implement `queue-append` to a stream group. Validates per QM-040..QM-044; mutates in-memory queue; persists via T30; emits `queue_appended` event.
- **Spec ref**: queue-model.md §7.
- **Files**: `internal/queue/append.go`, test.
- **Acceptance**: append to active stream → tail-extended + event; append to wave → rejected with `append_target_invalid`; append to completed group → rejected.
- **Deps**: T40, T41, T30.

## Tier 5 — Dispatch integration

### T50 — Workloop rewrite (TS-1, TS-2, queue_id propagation)

- **Scope**: replace `internal/daemon/workloop.go:391-495` (br-ready-poll + nothing-ready-sleep + pick_one + ShowBead-pre-claim-guard + ClaimBead block) with queue-pull per execution-model.md §7.4 amended pseudocode. Implements EM-015f group-advance gate. **Also** populates `QueueID` and `QueueGroupIndex` fields (added by T21) on the `run_started` / `run_completed` / `run_failed` event payloads when the dispatched run originated from a queued submission. Implements QM-011 / QM-012.
- **Spec ref**: execution-model.md §7.4 (TS-1); EM-015f (TS-2); queue-model.md §4 QM-011 / QM-012.
- **Files**: `internal/daemon/workloop.go`.
- **Acceptance**: daemon polls socket for queue-submit (no more `br ready` poll); pulls head of active group; honors EM-015f all-terminal gate; on `complete-with-failures` halts at group boundary and emits `queue_paused`. Dispatched runs emit `run_started` / `run_completed` / `run_failed` with non-nil `QueueID` + `QueueGroupIndex`. Sub-agent dispatch tests run as before.
- **Deps**: T41, T42, T70, T21 (queue_id field exists on payload types).

### T51 — EM-049/050/051 concurrency primitives spec-anchored

- **Scope**: ensure the existing claim semaphore (`workloop.go:360-370`) and registry-Len gate (`:382-389`) match the new EM-049/050/051 normative text. Add comments citing the EM-NNN IDs. No code rewrite expected — just verify and annotate.
- **Spec ref**: execution-model.md §4.11 (TS-4).
- **Files**: `internal/daemon/workloop.go` comments.
- **Acceptance**: code comments cite EM-049/050/051; conformance tests for these (§10.2) pass.
- **Deps**: T02 (spec landed).

## Tier 6 — Transport (CLI + JSON-RPC)

### T60 — JSON-RPC method handlers

- **Scope**: register `queue-submit`, `queue-append`, `queue-status`, `queue-dry-run` in the daemon socket handler. Remove the (never-built) `enqueue` placeholder.
- **Spec ref**: process-lifecycle.md §4.4 PL-003a (method-set extension); queue-model.md §6 (payload schemas).
- **Files**: `internal/daemon/socket.go` (dispatch switch), `internal/queue/rpc.go` (handler implementations).
- **Acceptance**: each method round-trips a request; payloads match spec; errors return JSON-RPC `{code, message}` with codes from `-32010..-32019`; `enqueue` is NOT in the registered set.
- **Deps**: T11, T40, T41, T42.

### T61 — JSON-RPC error-code allocation

- **Scope**: allocate concrete numbers in `-32010..-32019` for each of the 8 `QueueValidationReason` enum values + the `queue_already_active` / `queue_not_advancing` / `append_target_invalid` shapes.
- **Spec ref**: queue-model.md §6.10 QM-029; process-lifecycle.md §4.4 PL-003a.
- **Files**: `internal/queue/errors.go`.
- **Acceptance**: codes are stable (constants); each maps 1:1 to a reason value; test enumerates the mapping.
- **Deps**: T40.

### T62 — CLI: `hk queue` subcommand family

- **Scope**: add `hk queue {submit,append,status,dry-run}` to `cmd/harmonik/main.go`. Pre-`flag.Parse` dispatch per PL-028c. Each subcommand: opens daemon.sock, sends JSON-RPC, prints response, exits with PL-008a / ON §8 code mapping.
- **Spec ref**: process-lifecycle.md §4.4 PL-028 + PL-028c.
- **Files**: `cmd/harmonik/main.go`, `internal/queue/cli/{submit,append,status,dryrun}.go`.
- **Acceptance**: every subcommand callable from a shell; daemon-down (no socket) exits with code 17; unknown verb exits with 2; both `--flag value` and `--flag=value` forms accepted.
- **Deps**: T60, T61.

## Tier 7 — Daemon glue

### T70 — Queue handle wiring in daemon

- **Scope**: thread the active queue handle through the daemon's dependency injection so workloop, socket handlers, and CLI all see the same in-memory queue. Single-writer discipline per QM-060.
- **Spec ref**: queue-model.md §9 QM-060..065.
- **Files**: `internal/daemon/daemon.go`, `internal/daemon/workloop.go`.
- **Acceptance**: single `*queue.Queue` instance lives on the daemon struct; mutations serialized via mutex; concurrent reads work fine.
- **Deps**: T11, T40, T41. (Append handler T42 hooks into the same handle via T60; not a prerequisite for the wiring task itself.)

### T71 — PL-013 retirement: idle-wait code path

- **Scope**: remove the prior Beads-poll wakeup machinery (if present in code). Replace with `idle_wait_for_queue_submission()` that blocks on socket activity. The "MUST NOT exit on idle" obligation survives.
- **Spec ref**: process-lifecycle.md §4.5 PL-013 (retired-with-stub).
- **Files**: `internal/daemon/workloop.go`.
- **Acceptance**: no `T_queue_empty_recheck` (or similar) timer in daemon; idle daemon waits indefinitely until queue-submit or shutdown signal.
- **Deps**: T50.

## Tier 8 — Tests + verification

### T80 — Scenario test: end-to-end queue lifecycle

- **Scope**: scenario test that (a) starts daemon; (b) submits a queue with 2 wave-groups; (c) verifies group 1 dispatches in parallel; (d) verifies group-advance to group 2 after group 1 all-terminal; (e) verifies queue.json unlinked on completion. Use the existing scenario-harness conventions.
- **Spec ref**: queue-model.md §8 (Queue Lifecycle); execution-model.md §7.4.
- **Files**: `internal/scenario/queue_lifecycle_test.go` (new), fixture beads in `.beads/queue-test-fixtures/`.
- **Acceptance**: scenario test runs green; events.jsonl trail matches §8.10 emission ordering rule.
- **Deps**: T50, T60, T62, T31.

### T81 — Scenario test: paused-by-failure recovery

- **Scope**: scenario test that submits a queue, makes one bead fail, asserts the group reaches `complete-with-failures`, the queue transitions to `paused-by-failure`, and `queue_paused{reason: group_failure}` event lands. v0.1 has no resume — confirm daemon stays running, queue.json persists.
- **Spec ref**: queue-model.md §5, §8 (Pause-by-failure).
- **Files**: `internal/scenario/queue_paused_test.go` (new).
- **Acceptance**: events match expected; queue.json persists across daemon restart with `paused-by-failure` status preserved.
- **Deps**: T31, T80.

### T82 — Scenario test: queue.json crash-recovery

- **Scope**: scenario test that submits a queue, kills the daemon (SIGKILL), restarts, asserts the queue is loaded with persisted statuses at PL-005 step 8a, asserts dispatch resumes from the right item.
- **Spec ref**: queue-model.md §3 QM-002; process-lifecycle.md §4.2 PL-005 step 8a.
- **Files**: `internal/scenario/queue_crash_recovery_test.go` (new).
- **Acceptance**: restart loads queue.json with `active` status + correct group_index; dispatch continues without losing items.
- **Deps**: T31, T80.

### T83 — Unit-test sweep for validation contract

- **Scope**: ensure QM-020..QM-027 each have a focused unit test; QM-029a order-of-evaluation gets a dedicated test; QM-025 parallelism-narrowed emits the correct count of `queue_item_deferred_for_ledger_dep` events.
- **Spec ref**: queue-model.md §6.
- **Files**: `internal/queue/validation_test.go` (extends T40's tests).
- **Acceptance**: full coverage of the 8 validation rules + order test + parallelism-narrowed test.
- **Deps**: T40.

### T84 — Conformance assertion: enqueue retirement

- **Scope**: spec-audit test in `specaudit/` confirming `enqueue` is NOT in PL-003a method-set, ON-013a list, ON-041 command list, or ON-050 attach inline subset. Assert `queue-submit / queue-append / queue-status / queue-dry-run` are in all four.
- **Spec ref**: process-lifecycle.md §4.4 PL-003a; operator-nfr.md ON-013a, ON-041, ON-050.
- **Files**: `internal/specaudit/extqueue_enqueue_retired_test.go` (new).
- **Acceptance**: assertion passes against the v0.1 specs; would fail if `enqueue` re-appears.
- **Deps**: T02.

## Dependency graph (DAG)

```
T01 (queue-model.md lands) ──┬─→ T02 (5 spec edits land) ──┬─→ T10 (Go pkg scaffold)
                             │                              ├─→ T20 (event registry)
                             │                              ├─→ T21 (run_* fields)
                             │                              └─→ T51 (annotate concurrency)
                             └─→ T03 (changelog archive)

T10 ──┬─→ T11 (RECORD types)
      └─→ T12 (source_subsystem)

T11 ──┬─→ T30 (queue.json write)
      ├─→ T40 (validation)
      ├─→ T41 (state machine)
      └─→ T70 (daemon wiring)

T30 ──┬─→ T31 (PL-005 read)
      └─→ T32 (unlink)

T40 ──┬─→ T42 (append)
      ├─→ T60 (JSON-RPC handlers)
      ├─→ T61 (error codes)
      └─→ T83 (validation tests)

T41 ──→ T42, T50

T42 ──→ T50, T60

T21 ──→ T50 (queue_id payload propagation)

T50 ──┬─→ T71 (PL-013 retire code)
      ├─→ T80 (lifecycle scenario)
      ├─→ T81 (paused scenario)
      └─→ T82 (crash recovery)

T60 ──→ T62 (CLI)
T61 ──→ T62
T62 ──→ T80

T31 ──→ T80, T81, T82

T84 (conformance) parallel to T20-T62; depends only on T02
```

No cycles. Critical path: T01 → T02 → T10 → T11 → T40 → T60 → T62 → T80.

## Parallelization plan

**Wave 1** (after T01, T02 spec-land):
- T03 (archival)
- T10 (Go pkg scaffold)
- T84 (conformance assertion)

**Wave 2** (after T10):
- T11 (RECORDs)
- T12 (source_subsystem)

**Wave 3** (after T11, T20, T21):
- T30 (queue.json write)
- T40 (validation) — needs T20/T21
- T41 (state machine) — needs T20
- T70 (daemon wiring)

**Wave 4** (after T30, T40, T41):
- T31, T32, T42, T51, T60, T61

**Wave 5** (after T50, T60, T61, T62, T31):
- T50 (workloop rewrite — depends on T41/T42/T70)
- T62 (CLI)
- T71 (retire PL-013 code)

**Wave 6** (after T50):
- T80, T81, T82 (scenario tests) — can run in parallel
- T83 (unit-test sweep)

Critical-path length: ~7 implementer cycles. Wide fan-out at Waves 3-4 (4-5 implementers in parallel).

## Spec-traceability coverage

| Spec section | Tasks |
|---|---|
| queue-model.md §1 (Scope) | T10 |
| queue-model.md §2 (Data Model) | T11 |
| queue-model.md §3 (Persistence) | T30, T31, T32 |
| queue-model.md §4 (Identity) | T11, T21 |
| queue-model.md §5 (State Machine) | T41 |
| queue-model.md §6 (Validation) | T40, T83 |
| queue-model.md §7 (Append) | T42 |
| queue-model.md §8 (Queue Lifecycle) | T41, T80, T81 |
| queue-model.md §9 (Concurrency) | T70, T51 |
| execution-model.md §7.4 (TS-1) | T50 |
| execution-model.md EM-015f (TS-2) | T50 |
| execution-model.md EM-015a/b TS-3 | T21 |
| execution-model.md §4.11 EM-049/050/051 (TS-4) | T51 |
| execution-model.md §7.1 INFORMATIVE (TS-5) | (doc-only; no code task) |
| beads-integration.md BI-013/013a/013b/013c | T40 (BI-013b/c read surface) |
| beads-integration.md §4.10 informative | (doc-only; no code task) |
| process-lifecycle.md PL-003a | T60, T84 |
| process-lifecycle.md PL-028 + PL-028c | T62 |
| process-lifecycle.md PL-005 step 8a | T31 |
| process-lifecycle.md PL-013 (retired) | T71 |
| process-lifecycle.md PL-004 file-surface | (doc-only) |
| process-lifecycle.md PL-027(iii) non-regression | T50 (fd-passing verified) |
| event-model.md §8.10 (6 new events) | T20 |
| event-model.md §6.3 run_* extensions | T21 |
| event-model.md §8.7.16 operator_command_failed enum | T84 |
| operator-nfr.md ON-001 / ON-004 / ON-009a / ON-013a / ON-015 / ON-018 / ON-027 / ON-041 / ON-050 / ON-INV-001 | (doc-only; T84 asserts retire consistency) |
| operator-nfr.md §8 row 2 (queue-format-unsupported extension) | T31 (test asserts the failure mode) |

Every changelog entry has at least one task. Every task traces to ≥1 spec section.

## Out-of-scope (v0.2)

The following work is intentionally NOT in this task list — defers to v0.2 spec pass:
- `queue-remove` / `queue-pause` / `queue-resume` / `queue-clear` CLI + handlers.
- `queue_resumed` event registration.
- Multi-orchestrator submission, queue-ownership ACLs.
- Stream priorities, weighted scheduling.
- Pause / stop / kill of running beads.
- Write coalescing for queue.json (per-mutation fsync stays in v0.1).
