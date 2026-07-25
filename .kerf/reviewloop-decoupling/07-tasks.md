# Implementation tasks

## R0 — Characterization and dependency ratchet

- **What:** Freeze shipped review-loop decisions, event order/identity defects, checkpoint crash cuts,
  lifecycle acquisition/teardown order, local/remote artifact placement, and reviewer allowance timing.
  Record the current daemon-private relocation dependency census as a ratchet.
- **Spec sections:** Execution EM-015d/e §10; Event §8.1a/§10; Handler HC-011/§10; Process PL-014b;
  Workspace WM-027a.
- **Deliverables:** table/trace tests in `internal/daemon`, pure fixtures in `internal/runloop`, and a
  dependency/probe test that fails if new daemon-private review-loop dependencies appear.
- **Acceptance:** current oracle is explicit; target-conformance defects are separate failing/disabled
  cases; no production behavior changes; L8 relocation probe remains reproducible.
- **Depends on:** none.

## R1 — Pure review-cycle kernel

- **What:** Implement value-only `CycleState + Observation -> CycleDecision`, including raw versus
  normalized verdict, HEAD-based fix-up stall, cap, needs-attention result, ordered cycle intents, stable
  implementer/fresh reviewer selection, and terminal absorption.
- **Spec sections:** Execution EM-015d-KERNEL/EM-015e; Event §8.1a.
- **Deliverables:** daemon-independent package under `internal/runloop` with exhaustive table/property
  tests. It imports no daemon, git, filesystem, clock, event bus, process, or network package.
- **Acceptance:** all decision rows and properties pass; historical `no_progress` is readable but never
  produced by the built-in loop.
- **Depends on:** R0.

## R2 — Continuity checkpoint transaction

- **What:** Replace Claude-only best-effort persistence and sampled channels with one awaitable,
  location-explicit continuity transaction for Minted/Captured identity. Inspect committed state, reject
  conflicts, return the exact baseline, and order Minted checkpoint before `version_selected`.
- **Spec sections:** Execution EM-015d-CONT/EM-023a; Handler HC-006/045c; CHB-023; CLS continuity clauses.
- **Deliverables:** reusable continuity service/port; local and remote git adapters; removal of detached
  persistence goroutine and fallback identity synthesis.
- **Acceptance:** write/stage/commit/ACK crash matrix, identical reuse, conflict, selected-version
  propagation, Captured fail-closed, and remote parity tests.
- **Depends on:** R0.

## R3 — Authoritative observer, ready, input, and phase lifetime

- **What:** Add substrate-neutral watcher cancel/completion, genuine exactly-once SessionStart ready,
  three-way input resolution, quiescent callback unregister/tap unsubscribe, joinable heartbeat/observers,
  and bounded idempotent `PhaseScope.Close`.
- **Spec sections:** Handler HC-011/011a/039/041/056/057/070; Process PL-014b; Event §8.3/§8.21.
- **Deliverables:** owner APIs in handler/hook/runloop; direct/tmux adapters; race/leak tests; eliminate
  sleeps used to accommodate unjoinable goroutines.
- **Acceptance:** no input before ready; ProcessExit gains no heartbeat-staleness branch; callbacks/events
  cannot cross phase close; watcher/process/pane and all auxiliary tasks complete before dependent release.
- **Depends on:** R0.

## R4 — Explicit artifact transfer and reviewer projection

- **What:** Implement location-aware atomic/digest-verified file/tree transfer, typed unleased
  `ReviewerProjection` with manifest and exact-SHA validation, and reviewer evidence archiver for verdict,
  feedback, budget diagnostic, session directory, sidecar, and logs.
- **Spec sections:** Workspace WM-026/027a/033/WM-RIA-001; Process PL-006/014b; Handler HC-006a.
- **Deliverables:** workspace transfer/projection/archive APIs with local/worker adapters; replace nullable
  runner inference and raw cleanup closure.
- **Acceptance:** local/remote matrix, SHA/path/manifest adversarial cases, partial transfer faults,
  archive-before-cleanup proof, idempotent cleanup, and no second lease.
- **Depends on:** R0.

## R5 — Event truth, terminal spine, and attention-close

- **What:** Project kernel intents with real launch identities; propagate F-event errors; guarantee
  witness → durable cycle-complete → one run terminal → Beads effect; implement recoverable idempotent
  attention-close and operator outcome mapping.
- **Spec sections:** Event §4.4/§8.1a; RSM-020/RSM-INV-001/RSM-025; ON-009a/035;
  BI-010a/010f/029–031.
- **Deliverables:** typed event port/projector, reactor/terminal integration, Beads attention-close intent
  and recovery, operator projection.
- **Acceptance:** all five normalized outcomes; F fault halts terminal/ledger work; crash cuts converge to
  closed plus `needs-attention`; approved never labels; exactly one terminal/cycle completion.
- **Depends on:** R1.

## R6 — Claude and Pi launch conformance

- **What:** Make Minted identity caller-owned; validate capabilities; remove bootstrap ready; derive ready
  from SessionStart; distinguish `version_selected`, relay ACK, and input Ack; launch Claude/Pi reviewers
  from the projection and persist Captured native identity without touching implementer continuity.
- **Spec sections:** Handler HC-006a/045c; CHB-008/013/018/023/028; CLS launch lifecycle; Pi reviewer
  WorkDir; Harness HN-008/015.
- **Deliverables:** launch artifacts/builders, hook relay/watcher, Claude/Pi harness adapters and twins.
- **Acceptance:** Minted and Captured matrices, capability mismatch, identity separation, genuine-ready
  order, projection WorkDir, local/remote launch tests.
- **Depends on:** R2, R3, R4.

## R7 — Implementer phase shell

- **What:** Extract the implementer imperative shell in place: launch, continuity baseline, ready/input,
  wait/logical result, work-product facts, phase close. Feed only facts to R1.
- **Spec sections:** Execution EM-015d/CONT/KERNEL; Event `implementer_phase_complete`; Process PL-014b.
- **Deliverables:** focused implementer phase module and coordinator adapter; remove accumulated
  implementer defers and sampled channels from `reviewloop.go`.
- **Acceptance:** initial/no-work/resume/cancel/timeout traces preserve public order; phase N is quiescent
  before reviewer launch; no daemon-private policy leaks into the kernel.
- **Depends on:** R1, R2, R3, R6.

## R8 — Reviewer phase shell

- **What:** Extract reviewer projection creation, launch, ready/input, allowance observer, verdict
  validation, archive/feedback/session transfer, event projection, and close. Keep allowance policy pure.
- **Spec sections:** Execution EM-015d-RIA/RFD/RVA; Workspace WM-027a; Process PL-014b; Event
  reviewer dispatch/verdict/budget clauses.
- **Deliverables:** reviewer phase module plus pure allowance transition; migrate review-loop coordinator.
- **Acceptance:** approve/request/block/cap/malformed/budget/transfer-failure traces; projection retained on
  unresolved failure; iteration N projection removed before N+1.
- **Depends on:** R1, R3, R4, R5, R6.

## R9 — Projection orphan sweep and reconciliation handoff

- **What:** Partition run workspaces, projections, and scratch before generic no-lock cleanup; reclaim only
  manifest/SHA/archive-proven projections after process quiescence; preserve ambiguous evidence and route
  `reviewer-projection-integrity`.
- **Spec sections:** Workspace WM-003a/013c/033; Process PL-006/007/INV-003.
- **Deliverables:** workspace/daemon sweep and reconciliation adapters, report counters, crash matrix.
- **Acceptance:** no path-pattern deletion; complete evidence is salvaged; malformed/mismatched/live-owned
  projections survive and route evidence.
- **Depends on:** R3, R4.

## R10 — Thin coordinator, relocation probe, and dependent reuse

- **What:** Reduce `runReviewLoop` to composition and iteration; rerun the L8 relocation probe and move the
  coordinator only when daemon-private undefined symbols reach zero. Reuse the owner APIs in DOT reviewer
  paths without converting DOT into the review-loop kernel.
- **Spec sections:** Architecture dependency direction; all amended owner specs.
- **Deliverables:** thin coordinator under `internal/runloop` when the gate passes, import/depguard ratchet,
  DOT adapter follow-up, deletion of superseded legacy seams.
- **Acceptance:** `internal/runloop` does not import daemon; behavior traces remain green; zero relocation
  blockers; no generic god port and no L12/L13 conversion.
- **Depends on:** R7, R8, R9.

## R11 — Scenario acceptance campaign

- **What:** Close every scenario-test bead below with twin/real-runtime, local/remote, crash, race, replay,
  and fault-injection coverage. The work cannot close while any remains open.
- **Spec sections:** each drafted spec's conformance section.
- **Deliverables / beads:** `hk-0zkpw`, `hk-ehygf`, `hk-1fr7a`, `hk-koqv8`, `hk-z7vqw`, `hk-k4300`,
  `hk-e3niy`, `hk-tc36h`, `hk-gv3i7`, `hk-6npn8`, `hk-8ox47`.
- **Acceptance:** durable order, identity, attention recovery, archive/cleanup, zero leak, and local/remote
  terminal oracles all pass under race where applicable.
- **Depends on:** R5, R6, R7, R8, R9, R10.

## R12 — Operator exploratory campaign and final gates

- **What:** Close every exploratory-test bead through the supported CLI/API/artifact surfaces; run scoped
  UBS, package/race tests, `make check-fast`, then milestone `make check-short`.
- **Deliverables / beads:** `hk-zfr49`, `hk-2eua9`, `hk-a2yi3`, `hk-y6r40`, `hk-5x9kz`, `hk-ljh9q`,
  `hk-qmyo1`, `hk-tw05c`, `hk-zzyne`, `hk-yko90`, `hk-9l4hb`.
- **Acceptance:** an operator can explain every outcome, identity, retained artifact, and failure from
  supported surfaces; all beads closed and gates green.
- **Depends on:** R11.

## Dependency graph

```text
R0 -> R1 -> R5
 |     |
 |     +-----------------> R7
 +-> R2 -----> R6 ------> R7
 +-> R3 -----> R6 ------> R8
 +-> R4 -----> R6 ------> R8
       \------> R9
R1 + R3 + R4 + R5 + R6 -> R8
R7 + R8 + R9 -> R10 -> R11 -> R12
```

This graph is acyclic. R5 can begin after R1 while R2/R3/R4 proceed.

## Parallelization plan

- **Wave 1:** R0 alone establishes the shared oracle.
- **Wave 2 (three lanes):** R1, R2, and R3/R4 owner primitives; R3 and R4 may use separate builders.
- **Wave 3:** R5 and R6 in parallel after their prerequisites.
- **Wave 4:** R7 and R8 in parallel; R9 runs alongside once R3/R4 are stable.
- **Wave 5:** R10 serial integration/relocation, then R11 and R12 gates.

At most two builders modify production code concurrently. Every nontrivial task receives independent
review before integration. The scenario and exploratory beads are terminal dependencies, not optional
follow-ups.
