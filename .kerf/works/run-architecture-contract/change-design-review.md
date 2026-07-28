# Change Design Review — `run-architecture-contract`

## Round 1

- Reviewer: independent Sol/xhigh architecture review
- Baseline: `b4e59aec85f370e95984a6d0498c1d11bf5ad553`
- Verdict: **REQUEST_CHANGES**

## What is sound

- C1 defines a coherent one-way construction graph, distinct queued/direct
  constructors, immutable plans, ordered per-run scope ownership, and the
  existing `internal/daemon -> internal/runloop` dependency direction.
- C2 correctly treats the reviewed handler-contract and process-lifecycle
  specifications as target authority. It accurately identifies the current
  watcher/`WaitOwner` and Handler/Session signature shapes as production
  migration drift rather than silently blessing them.
- C3 keeps mode policy outside lifecycle and durable terminal writers outside
  mode results.
- C5 makes explicit adopt/adapt/retire decisions for the required run packages,
  RL-01 artifacts, LIFT levels, and QueueStore ownership.
- C6 supplies literal span and cognition ceilings for the five named hotspots
  and correctly forbids `internal/runloop -> internal/daemon`.

Those strengths do not yet satisfy the Change Design gate. The following
findings are blocking.

## Required changes

### R1 — The HC/PL drift is identified but has no executable migration owner

Artifacts:

- `04-design/c2-process-session-lifecycle-design.md` “Drift treatment”
- `04-design/c6-migration-gates-design.md` “DAG”
- `specs/process-lifecycle.md` PL-014 and PL-016
- `specs/handler-contract.md` HC-001, HC-002, HC-011, and §6.1
- production `internal/handler/handler.go` `(*handler).Launch`
- production `internal/handler/session.go` `session.runWait` and
  `(*session).Wait`
- production `internal/lifecycle/spawnwait_pl014.go` `WaitOwner`

The current/target distinction in C2 is accurate:

- PL-014 and PL-016 make the watcher the exclusive `*exec.Cmd` owner and
  exclusive raw `cmd.Wait()` caller.
- HC §6.1 specifies `Handler.Launch(ctx, spec) -> (Session, error)` and
  `Session.Wait(ctx) -> (Outcome, error)`.
- Production instead returns `(Session, *handlercontract.Watcher, error)`,
  exposes `Session.Wait(ctx) error`, and has `session.runWait` call
  `WaitOwner.WaitAndReap` while the watcher reads progress independently.

C2 says a missing companion task will reconcile these sites, but C6 supplies
no such task. Its proposed `ARCH-HCPL-01` writes only new runloop lifecycle
files and a daemon adapter; those paths cannot change the production
Handler/Session signatures or transfer raw-wait ownership. Existing `PS-01`
and `PS-02` already own the shared phase protocol and local adapter, but the
design neither maps the drift into their exact authorized surfaces nor
proposes the smallest additional evidence-only conformance slice.

Required correction: retain HC/PL as the unchanged target, then map every
drifted production symbol to `PS-01`/`PS-02` where their cards authorize it.
For any remaining signature or wait-owner site, propose one evidence-only
companion task with exact paths, symbols, prerequisites, lease, compiling
intermediate state, and rollback boundary. Do not alter HC or PL inside
ARCH-01 and do not make the RAC target conform to current production.

### R2 — An existing HC/RSM `InputPort` contradiction is not surfaced

Artifacts:

- `specs/handler-contract.md` HC-069, HC-070, and §6.1 `Ack`
- `specs/run-state-machine.md` RSM-027
- `04-design/c2-process-session-lifecycle-design.md`
- `04-design/c3-mode-execution-design.md`

HC's current normative `Ack.outcome` is the two-value
`{Delivered, Rejected}` shape. HC-070 says positive acceptance is the later
asynchronous `agent_input_acked` event. RSM-027 instead requires a
three-valued synchronous acceptance class
`{Accepted, Rejected, Degraded}` and gives `Degraded` special liveness
semantics.

Both are members of ARCH-01's declared read-only dependency set. The
decomposition explicitly says an existing semantic contradiction is a stop
condition, but the designs currently say only that RAC stops at HC-069 and
preserves RSM behavior. A spec writer cannot satisfy both without silently
choosing one.

Required correction: record this as a blocking pre-draft coordination item and
route an evidence-only companion amendment/task to the owning spec lane.
ARCH-01 must not redefine the `Ack` enum or silently ignore RSM-027. The RAC
target may proceed only after the owner decision is explicit and its
dependency/order is represented in C6.

### R3 — C6 duplicates existing tasks instead of composing the approved DAG

Artifacts:

- `04-design/c6-migration-gates-design.md` “DAG”
- `plans/2026-07-24-code-health-audit/TASK-INDEX.yaml`
- existing cards `PS-01`, `PS-02`, `WL-02A`, `WL-02B`, `WL-03`,
  `RL-02A`, `RL-02B`, `RL-02C`, `RL-03`, `DOT-01`, `DOT-02`, `DOT-03`,
  and `DOT-GATE-01`

The task card and C6 requirement say to use existing task IDs where possible.
The index already contains the lifecycle, outer-loop, review, and DOT
migrations. C6 instead proposes parallel `ARCH-CONSTRUCT-01`,
`ARCH-HCPL-01`, `ARCH-SINGLE-01`, `ARCH-REVIEW-01`, and `ARCH-DOT-01`
owners. In particular, `ARCH-REVIEW-01` overlaps the complete RL-02A/B/C and
RL-03 chain, while `ARCH-DOT-01` overlaps DOT-01/02/03 and DOT-GATE-01. That
would recreate the shadow-owner problem C5 is intended to eliminate.

The proposed rows are also not dispatch-ready:

- `ARCH-TERM-01` uses an undeclared `dispatch_terminal` lease even though its
  workloop terminal edits collide with the registered `dispatch_spine` and
  JR-03.
- prerequisites such as “RL R3 integration” and “adopted R0-R3” are not task
  IDs;
- writes such as “named gate call sites”, “named terminal regions”,
  `reviewcycle/**`, and “affected focused tests” are not exact path/symbol
  leases; and
- “one node family at a time” is not a rollback boundary for one task node.

Required correction: rebuild C6 around the existing indexed tasks and their
registered lease families. Add evidence-only proposed tasks only for genuine
unowned gaps, including R1/R2. Every row must carry exact prerequisites,
writable paths and shared-file symbols, collision lease, compiling/testable
intermediate state, rollback unit, and unlocked RAC condition.

### R4 — ARCH-GATE targets are not complete in the required row shape

Artifacts:

- `04-design/c6-migration-gates-design.md` “Literal ARCH-GATE targets”
- `plans/2026-07-24-code-health-audit/tasks/ARCH-01.md` required decisions 6–7
  and `targets` evidence schema
- `plans/2026-07-24-code-health-audit/tasks/ARCH-GATE.md`

The span/cognition ceilings are useful, but the design does not provide
`baseline_reach` and `target_reach` for each required hotspot. It gives reach
only for `workLoopDeps` and the review relocation census. It also uses
non-integer baselines (`new`) and `n/a`, although the required evidence row
requires integer span, complexity, and reach fields. Targets are not assigned
to the migration task that must meet them.

ARCH-GATE additionally requires pinned treatment for `executeCognitionGate`
and `dot_gate.go`; the design does not disposition those targets.

Required correction: define the exact target-row population and reproducible
reach metric for every required symbol/task, with literal integers in all six
numeric fields and explicit forbidden-import lists. Include or explicitly
route every additional ARCH-GATE symbol, and map each target to the existing
task that consumes it.

### R5 — C4 defers exact authority work and contains incorrect citations

Artifact: `04-design/c4-terminal-recovery-design.md` “Target state”.

The C4 requirement demands exact decision, effect, serialization, recovery,
normative-owner, and event-edge citations for every durable transition. The
design matrix leaves event observation edges unpopulated and tells the later
spec writer to replace summary citations. That postpones the central design
decision rather than specifying it.

One summary is factually wrong: EM-044 through EM-046b govern backtracking,
context restore, no-matching-edge failure, and retry re-dispatch; they do not
own the merge/terminal ledger row. Relevant existing owners include EM-015b
and EM-015f for terminal observations and queue-group composition, EM-052 and
EM-053 for merge/close/reopen ordering, RSM-020 through RSM-023 for the
terminal spine, and BI-010/BI-029 through BI-031 for Beads writes and recovery.
Likewise the Bead-claim row's “terminal mutex/intent” wording does not identify
BI-010d's claim activity-marker transition or its exact BI intent/recovery
clauses.

Required correction: perform the exact clause mapping in the design now,
including an explicit event observation value for every row and exact
CQ-02/JR artifact clauses. Keep the terminal composer as an ordering composer
with no store ownership.

### R6 — The closed `ModeResult` vocabulary loses normative failure classes

Artifacts:

- `04-design/c3-mode-execution-design.md` `ModeResult`
- `specs/execution-model.md` EM-005/EM-005c and failure taxonomy
- `specs/beads-integration.md` BI-010a

The proposed closed result class contains `success`, `retry`,
`deterministic-failure`, `canceled`, `budget-exhausted`, and
`needs-attention`. It does not say how `structural`, `transient`,
`compilation_loop`, `PARTIAL_SUCCESS`, or a handler/daemon classification
disagreement maps into that vocabulary. BI-010a deliberately gives transient,
structural, deterministic, compilation-loop, canceled, and budget-exhausted
different terminal Beads effects. C4 cannot preserve those differences if C3
collapses them before durable composition.

Required correction: either carry the authoritative EM failure class through
`ModeResult` unchanged or provide an exhaustive, lossless mapping table for
every EM status/failure-class combination and every review/DOT terminal
result. Define what `retry` means relative to in-mode retry versus a terminal
transient failure.

### R7 — The task card's locked-decision preservation has no target

Artifacts:

- `plans/2026-07-24-code-health-audit/tasks/ARCH-01.md`
  “Non-negotiable boundaries”
- all six design files

The card requires all ten locked decisions to appear verbatim in ARCH-01
evidence with an explanation of how the contract preserves each, plus the
later Beads-terminal-owner rule in the durable-transition evidence. The
designs preserve several decisions implicitly, and C4 mentions Beads, but no
target state requires the complete ten-row preservation mapping.

Required correction: assign this evidence/spec traceability obligation to a
design section. It may cite existing normative owners and need not restate
their semantics in RAC, but the later evidence writer must have a complete,
explicit target rather than infer ten dispositions from scattered prose.

## Criteria assessment

| Change Design criterion | Assessment |
| --- | --- |
| Current state is accurate | **Partially met.** C1, C2, C3, C5, and C6 research-backed production descriptions are strong; C4's normative attribution is not. |
| Every C1–C6 requirement has a target | **Not met.** Exact C4 citations/event edges, executable HC/PL drift routing, complete C6 target rows, and locked-decision mapping are missing. |
| No unrequired target exists | **Not met.** C6 proposes duplicate ARCH mode/lifecycle tasks despite existing indexed owners. |
| Target is specific enough for spec drafting | **Not met.** C3 failure mapping, C4 authority rows, and C6 task/path/target rows require unresolved design choices. |
| Rationales use research evidence | **Met for the stated designs.** The missing contradiction and task routing must be added to research/design rationale. |
| No cross-area contradictions | **Not met.** HC/RSM Ack semantics conflict, and C6 overlaps existing PS/RL/DOT/WL owners and leases. |

## Review boundary

This review does not authorize changes to any existing normative
specification, production file, task card, task index, or Kerf status. The
smallest next step is to correct the six design artifacts and return the
Change Design pass for a second independent review.

## Round 2

- Reviewer: independent Sol/xhigh architecture review
- Baseline: `b4e59aec85f370e95984a6d0498c1d11bf5ad553`
- Verdict: **REQUEST_CHANGES**

## Round-1 disposition

The amendment resolves most Round-1 findings:

- The HC-070/RSM-027 Ack contradiction is now an explicit pre-draft blocker.
  Proposed `INPUT-ACK-CONTRACT-01` owns the coordinated HC/RSM/AIS amendment;
  RAC states no replacement enum and cannot advance to spec drafting until the
  owner-spec decision is reviewed.
- C6 no longer creates shadow single/review/DOT architecture tasks. It composes
  the indexed PS/WL/BR/RL/DOT/CQ/JR graph and proposes only the Ack amendment,
  the genuine HC/PL conformance gap, and final obsolete-bundle deletion.
- The ARCH-GATE table now has all 18 required rows, including
  `executeCognitionGate`, `dot_gate.go`, and `workLoopDeps`. Every row contains
  six literal integer fields and a consuming task. The stated AST reach metric
  reproduces the six function baselines `7, 24, 13, 6, 11, 10`; the file
  import metric reproduces `dot_gate.go = 14`, and the explicit-type-use
  interpretation supports `workLoopDeps = 7`.
- C3 now carries the EM status and all six daemon-authoritative failure classes
  losslessly, preserves `PARTIAL_SUCCESS`, records handler disagreement, and
  distinguishes in-mode retry from terminal transient failure.
- C1 contains the ten locked decisions verbatim with preservation targets, and
  C4 contains the literal later rule that Beads owns terminal Bead
  transitions with BI authority.

Two precision defects still prevent a spec writer or task author from
executing the design without making new decisions.

## Remaining required changes

### R8 — `PS-HCPL-CONFORMANCE-01` still has no exact compiling compatibility
route

Artifacts:

- `04-design/c2-process-session-lifecycle-design.md` “Drift treatment”
- `04-design/c6-migration-gates-design.md` `PS-HCPL-CONFORMANCE-01`
- production `internal/handler/handler.go` `Handler.Launch`
- production `internal/handler/session.go` `Session.Wait`
- production daemon launch/wait callers

The proposed row now names the drifted definitions, prerequisites, lease,
intermediate state, and rollback unit. It nevertheless says that “old mode
callers use compatibility adapters” without naming the adapter type,
constructor, methods, or exact writable symbols that make this compile.

That missing bridge is load-bearing. The current concrete production surface
returns `(Session, *Watcher, error)` from `Launch` and `error` from `Wait`,
while the target surfaces return `(handlercontract.Session, error)` and
`(Outcome, error)`. Existing production call sites in
`internal/daemon/workloop.go`, `reviewloop.go`, `dot_cascade_core.go`, and
`dot_gate.go`, plus the current socket-grace wait path, consume the old arity
and result. Those files are not in this proposed slice. Go cannot overload the
same methods to provide both signatures.

“Exact focused tests” is also not an exact writable path. Therefore the row
does not yet satisfy its own claim that the target signatures compile while
old mode callers remain unchanged.

Required correction: name the exact compatibility adapter type/API,
constructor-return boundary, production symbols it preserves, and exact test
paths inside the proposed lease. State which conforming Handler/Session object
the adapter wraps and how watcher ownership remains singular. Alternatively,
include every required call-site symbol in this node and serialize the
additional spines, but do not leave the compilation strategy to the
implementer.

### R9 — C4 still conflates distinct Run transitions and cites the dead-session
classifier for live adoption

Artifact: `04-design/c4-terminal-recovery-design.md` transition matrix.

The row titled “Run ID, durable record, registry registration” combines facts
that JR-00 explicitly separates:

1. Run ID patch plus Bead claim;
2. claimed Run to memory-only `RunRegistry` registration and goroutine launch;
3. the later, conditional independent-session `runpkg.Record` write.

The cited JR row
`transition_table["claimed -> in-memory Run registered and goroutine launched"]`
states `run_record_before_after: none -> none` and says no event occurs until
the later `run_started`. It cannot authorize the combined row's “durable
record” effect or recovery owner. JR-00 has a separate
`transition_table["registered Run -> independent durable session record
(conditional)"]`, whose ordering says `run_started` precedes that write and
whose missing-record recovery is deliberately different.

The “live-session adoption” row then cites
`recovery_paths["durable independent run record, dead session"]`. That
classifier resets the Bead and removes the record; it is the opposite of live
adoption. The exact live path is
`recovery_paths["durable independent record, session survives"]`, and JR-00
records a conflict in that current path which JR-04 must settle. The generic
Run-record cleanup row similarly cites whole `.recovery_paths` and
`.crash_windows` collections rather than the exact normal/dead/live authority
rows required by C4.

Required correction: split registry registration from conditional durable
session-record creation, give each its exact owners/order/event edge, cite the
matching JR transition, and correct live adoption to the surviving-session
classifier and its named crash window. Split or precisely cite normal,
dead-session, and live-session record cleanup rather than assigning them one
generic collection reference.

## Round-2 criteria assessment

| Change Design criterion | Assessment |
| --- | --- |
| Current state is accurate | **Partially met.** C1–C3, C5, and C6 are accurate; C4 still merges memory-only registration with a later conditional durable write. |
| Every C1–C6 requirement has a target | **Partially met.** All areas now have targets, but the HC/PL compatibility bridge and exact Run-record transitions remain underspecified. |
| No unrequired target exists | **Met.** The duplicate ARCH mode/review/DOT owners are removed. |
| Target is specific enough for spec drafting | **Not met.** C4 would transcribe incorrect JR authority, and the HC/PL task cannot be implemented from its stated lease without inventing the compatibility API. |
| Rationales use research evidence | **Met.** |
| No cross-area contradictions | **Partially met.** Ack is correctly blocked and routed; the C2/C6 compiling-intermediate claim still conflicts with the unlisted old-call-site compatibility surface. |

## Round-2 boundary

This review changes only this review artifact. It does not authorize existing
spec, task, index, production, evidence, or Kerf-status changes. Keep the work
at `change-design`; after R8 and R9 are corrected, return it for Round 3.

## Round 3

- Reviewer: independent Sol/xhigh architecture review
- Baseline: `b4e59aec85f370e95984a6d0498c1d11bf5ad553`
- Verdict: **REQUEST_CHANGES**
- Scope: exact re-review of remaining Round-2 findings R8 and R9 only

## Round-2 disposition

### R9 — resolved

C4 now separates the memory-only
`claimed -> in-memory Run registered and goroutine launched` transition from
the later conditional
`registered Run -> independent durable session record (conditional)`
transition. Their owners, serialization boundaries, recovery rules, and event
edges match the exact JR-00 transition rows: registry registration has
`none -> none` Run-record state and no event; `run_started` precedes the
conditional `runpkg.Write`, whose absence has no recovery.

The cleanup/adoption rows are also separated and exact:

- normal cleanup cites the `active Run -> terminal close or reopen` and
  `terminal Run -> queue item/group/queue release` Run-record fields;
- dead-session cleanup cites
  `recovery_paths["durable independent run record, dead session"]`; and
- live adoption cites
  `recovery_paths["durable independent record, session survives"]` plus
  `crash_windows["after run_started and independent runpkg.Write while session
  is alive"]`, preserves the recorded JR-00 conflict as a JR-04 prerequisite,
  and forbids premature reset/deletion.

This satisfies R9 without treating JSONL or the volatile registry as durable
authority.

### R8 — still blocking

C2 and C6 now name most of the compatibility bridge, but the claimed compiling
route is still incomplete against the concrete production APIs.

1. `handlercontract.Handler` has two required methods:
   `Launch(context.Context, *handlercontract.LaunchSpec)
   (handlercontract.Session, error)` and `AgentType() string`. The design names
   the adapter's `Launch` behavior and accepts `agentType core.AgentType`, but
   never specifies `ContractHandlerAdapter.AgentType`. A compile-time
   assertion in a proposed test is not a substitute for the missing target API
   and return conversion.

2. Moving the sole raw reap from `session.runWait` to `Watcher.runLoop`
   necessarily changes `newSessionWithIDs`, which currently starts
   `go s.runWait(ctx)`. The proposed C6 lease names `Session`,
   `session.runWait -> session.finishWait`, and `(*session).Wait`, but does not
   authorize the `newSessionWithIDs` construction/start symbol. Without that
   edit, either the old goroutine still calls `WaitAndReap` (two possible raw
   owners) or there is no concrete caller that supplies `waitErr` to
   `finishWait`.

3. The proposed `NewContractSessionAdapter(legacy Session, watcher *Watcher)`
   unconditionally derives ID, outcome, and log location from the watcher.
   Production `Handler.Launch` deliberately returns `(Session, nil, nil)` for
   a substrate whose `Stdout()` is nil; this is the normal hook/socket path,
   not an error. The mapper is not constrained to exclude that hosting regime,
   and the adapter constructor has no hook-session completion dependency.
   Therefore the advertised `handlercontract.Handler` can return a session
   that cannot implement `ID`, `Wait`, or `LogLocation` without nil access or
   invented behavior. None of the five exact tests covers the nil-watcher
   legacy result.

Required correction: specify
`ContractHandlerAdapter.AgentType() string` exactly; add
`internal/handler/session.go` `newSessionWithIDs` to the conformance task's
writable symbols and state the new goroutine/construction handoff; and either
make the contract adapter reject watcherless hosting before launching, or name
the concrete watcherless Session adapter/completion source and its exact test.
The correction must retain the existing three-return legacy Launch and
one-error legacy Wait behavior for the four unleased daemon callers.

## Round-3 criteria assessment

| Change Design criterion | Assessment |
| --- | --- |
| Current state is accurate | **Partially met.** C4/JR-00 is now exact; C2 omits the production watcherless Launch result and the `newSessionWithIDs` reap start. |
| Every C1-C6 requirement has a target | **Partially met.** R9 has complete targets; the HC/PL bridge lacks one required Handler method and one construction symbol/path. |
| No unrequired target exists | **Met within R8/R9 scope.** |
| Target is specific enough for spec drafting | **Not met.** A writer must invent AgentType behavior, the reap-construction cutover, and watcherless-session semantics. |
| Rationales use research evidence | **Met within R8/R9 scope.** |
| No cross-area contradictions | **Not met.** The claimed universal contract adapter conflicts with the existing nil-watcher substrate result. |

## Round-3 review boundary

This review changes only this review artifact. It does not authorize Kerf
advancement or edits to designs, specs, production, tests, task cards, or the
task index.

## Round 4

- Reviewer: independent Sol/xhigh architecture review
- Baseline: `b4e59aec85f370e95984a6d0498c1d11bf5ad553`
- Verdict: **REQUEST_CHANGES**
- Scope: remaining Round-3 R8 lifecycle bridge only, with an integrity check
  that the resolved R9 correction remains intact

## Round-3 disposition

R8 is substantially narrowed:

- `ContractHandlerAdapter.AgentType() string` now returns
  `string(agentType)`, and `NewContractHandlerAdapter` rejects an invalid
  `core.AgentType` as a composition defect.
- The conformance lease now includes `newSessionWithIDs`, removes its
  `runWait` goroutine, names `Watcher.runLoop` as the sole
  `WaitOwner.WaitAndReap` caller, and routes the cached result through
  `session.finishWait`.
- Watcherless substrate hosting is now precisely legacy-only:
  `(*handler).launchViaSubstrate` retains `(Session, nil, nil)`, while the
  contract mapper rejects a non-nil substrate before invoking legacy Launch.
  The exact contract rejection and legacy-preservation tests are named.
- R9 remains resolved. C4 still separates memory-only registry registration
  from the later conditional durable record, and its normal, dead-session, and
  surviving-session cleanup/adoption rows still cite the exact JR-00
  transition, recovery-path, and crash-window owners.

One R8 ownership gap remains.

### R8 — the post-start reap transfer is not yet total or concretely
constructible

Artifacts:

- `04-design/c2-process-session-lifecycle-design.md` “Drift treatment”
- `04-design/c6-migration-gates-design.md` `PS-HCPL-CONFORMANCE-01`
- production `internal/handler/session.go` `newSessionWithIDs`
- production `internal/handler/handler.go` `(*handler).Launch`

The amendment says that after `newSessionWithIDs` starts the child,
`(*handler).Launch` “immediately” binds
`SpawnWatcherConfig.Reap = s.waitOwner.WaitAndReap`. That expression is not
constructible from the stated current API: `newSessionWithIDs` returns the
package interface `Session`, which exposes neither `waitOwner` nor a reap
handoff method. The lease permits several materially different implementations
(change the private constructor to return `*session`, add a private interface
method, or type-assert), but the design does not select one. A compile-time
contract-adapter assertion does not exercise this private construction edge.

More importantly, removing `go s.runWait(ctx)` creates a real interval after
`cmd.Start` in which no goroutine owns reap. Moving `openWireTap` before
`newSessionWithIDs` removes its current post-start error, but does not make the
remaining assembly infallible:

- a successfully opened wire-tap file has no stated close owner when
  `newSessionWithIDs` returns an error; and
- `LaunchSpec.StdoutWrapper` is still invoked only after the Session exists.
  It is an unconstrained callback that can panic or return nil; the latter
  makes `SpawnWatcher` panic before its goroutine starts. On either path the
  started child has neither the old `runWait` owner nor the proposed Watcher
  owner.

Therefore the claim “there is no unowned post-start failure window” is false
for the named production surface, and the proposed normal/cancellation/
framing/panic Watcher tests begin only after the missing transfer has
succeeded.

Required correction: choose and state one exact compiling private
`newSessionWithIDs`-to-Launch reap API/signature; assign cleanup of a pre-opened
wire tap on every constructor failure; and name the temporary post-start owner
and atomic handoff rule that guarantees exactly one `WaitAndReap` even when
stdout wrapping or Watcher construction panics/fails. Add exact focused tests
for constructor failure after wire-tap preparation and for a nil/panicking
`StdoutWrapper` after child start. The legacy three-return Launch and one-error
Wait surfaces, watcherless substrate behavior, and Watcher ownership after a
successful handoff must remain unchanged.

## Round-4 criteria assessment

| Change Design criterion | Assessment |
| --- | --- |
| Current state is accurate | **Partially met.** AgentType, watcherless substrate behavior, and C4 are accurate; the claimed absence of a post-start unowned interval is not. |
| Every C1-C6 requirement has a target | **Partially met within R8 scope.** The steady-state Watcher owner is named, but the construction-to-Watcher transfer and pre-opened wire-tap cleanup are not. |
| No unrequired target exists | **Met within R8 scope.** |
| Target is specific enough for spec drafting | **Not met.** The writer must choose the private reap-access API and invent failure-safe transfer semantics. |
| Rationales use research evidence | **Met within R8 scope.** |
| No cross-area contradictions | **Partially met.** Legacy watcherless behavior is now separated correctly, but removing the constructor waiter conflicts with fallible post-start Watcher assembly. |

## Round-4 review boundary

This review changes only this review artifact. It does not authorize Kerf
advancement or edits to designs, specs, production, tests, task cards, or the
task index. Keep the work at `change-design`.

## Round 5

- Reviewer: independent Sol/xhigh architecture review
- Baseline: `b4e59aec85f370e95984a6d0498c1d11bf5ad553`
- Verdict: **REQUEST_CHANGES**
- Scope: final Round-4 R8 only, with an integrity check that resolved R9
  remains intact

## Round-4 disposition

The direct `exec.Cmd` route now resolves the principal Round-4 defects:

- `newSessionWithIDs(ctx, cmd, sessID, runID, wireTap)` has one exact return
  shape, and the returned `sessionReapHandoff` owns the concrete `*session` and
  `WaitOwner`; `(*handler).Launch` no longer needs to reach through the
  `Session` interface.
- `transferToWatcher`, `abort`, and `startLegacyOwner` name one
  `launch_owned` CAS winner. Losing transitions perform no raw wait, kill,
  finalization, or tap close.
- `SpawnWatcher` validates and allocates before the transfer callback, and the
  successful callback is immediately adjacent to starting `Watcher.runLoop`.
  The Watcher owns `reap -> finishWait -> tap close` after that edge.
- The handler recovery defer and exact tests cover the direct-path nil
  callback, nil-returning callback, wrapper panic, Watcher validation panic,
  bounded abort, exactly-one raw wait/finalization, and exactly-one tap close.
- Public `NewSession` preserves its API and explicitly selects the legacy
  waiter; its constructor call carries a nil tap.

R9 remains resolved. C4 still preserves separate memory-only registration and
conditional durable-record transitions, plus exact normal, dead-session, and
surviving-session cleanup/adoption authority.

Two edges still keep R8 from being a closed, compiling intermediate.

### R8 — constructor-internal post-start failure still has no named bounded
owner

Production `newSessionWithIDs` has fallible work after `cmd.Start` and before
it can return a Session/handoff: closing the parent's stdout/stderr write ends
and the `Spawning -> Initializing` machine transition can both fail. Today
those paths call `abandonStartedSession`.

The amendment assigns the constructor tap close for pipe/setup/`cmd.Start`
failure, then says a successful `cmd.Start` returns `launch_owned`. It does not
state whether the two post-start constructor failures are removed, moved, or
run through a constructed handoff. They therefore fall between the named
constructor owner and the returned Launch abort owner. The exact test
`TestNewSessionWithIDs_ConstructorFailureClosesPreparedWireTap` does not name
either post-start cut or require bounded kill/reap and one finalization there.

Required correction: name the exact owner and algorithm for each fallible
constructor operation after `cmd.Start`. Either construct
`sessionReapHandoff` before those operations and route each failure through
its bounded abort, or specify an equivalent bounded constructor-local
kill/reap/tap-close owner. Add exact injected tests for parent-write-end close
failure and lifecycle-transition failure after a successful start, asserting
one raw wait, bounded termination, and one tap close.

### R8 — the new required Watcher ownership callback does not cover the
existing substrate Watcher path

`SpawnWatcherConfig.TakeReapOwnership` is described as validated and invoked
exactly once before every `Watcher.runLoop`. Production has two
`SpawnWatcher` callers, not one. Besides direct `(*handler).Launch`,
`(*handler).launchViaSubstrate` invokes it whenever
`SubstrateSession.Stdout()` is non-nil. That legacy path has no
`sessionReapHandoff`; its `substrateSessionAdapter.Wait` delegates to
`SubstrateSession.Wait`.

The C6 lease mentions `launchViaSubstrate` only for the nil-stdout/nil-Watcher
result. It supplies no exact `TakeReapOwnership` callback or compatible legacy
carve-out for the non-nil-stdout path. It likewise does not cover that path's
post-spawn wrapper panic/nil return, Watcher-validation panic, abort owner, or
tap close. The named substrate test proves only `(Session, nil, nil)` when
stdout is nil; all four new `handler_reaphandoff_test.go` cuts are stated for
the direct constructor handoff.

This is also an exact test-lease problem. There are seven existing
`internal/handlercontract/*_test.go` files with 27 `SpawnWatcherConfig`
literals. If `TakeReapOwnership` is required as stated, those observation-only
Watcher fixtures panic unless they receive an explicit compatible owner; only
the two new/updated handlercontract test files are leased. If the callback is
optional instead, the design must say so and define the nil behavior rather
than claiming it is invoked exactly once.

Required correction: choose one exact compiling intermediate for all existing
`SpawnWatcher` calls. For the non-nil-stdout substrate branch, name either a
substrate reap handoff whose winner calls the one `SubstrateSession.Wait`, or
an explicit legacy Watcher mode with its separate Wait owner and precise
HC/PL intermediate status. Cover wrapper nil/panic, Watcher-construction
failure, successful watcher completion, and tap close for that branch. State
whether `TakeReapOwnership` is required or optional, and expand the production
and test lease to every literal/caller that must change.

## Round-5 criteria assessment

| Change Design criterion | Assessment |
| --- | --- |
| Current state is accurate | **Partially met.** The direct launch path is now precise; constructor-internal post-start failures and the second production SpawnWatcher call remain omitted. |
| Every C1-C6 requirement has a target | **Partially met within R8 scope.** The direct handoff has a target; post-start constructor abandonment and non-nil-stdout substrate ownership do not. |
| No unrequired target exists | **Met within R8 scope.** |
| Target is specific enough for spec drafting | **Not met.** A writer must invent two cleanup owners and decide whether the new Watcher callback is required or optional. |
| Rationales use research evidence | **Met within R8 scope.** |
| No cross-area contradictions | **Partially met.** R9 and watcherless substrate behavior remain sound, but the claimed universal SpawnWatcher transfer conflicts with the existing watcher-backed substrate and test fixtures. |

## Round-5 review boundary

This review changes only this review artifact. It does not authorize Kerf
advancement or edits to designs, specs, production, tests, task cards, or the
task index. Keep the work at `change-design`.
