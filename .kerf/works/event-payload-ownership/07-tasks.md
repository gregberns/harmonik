# Tasks — Event Payload Ownership

Status: ready for implementation. This plan implements the five complete target
drafts. It does not change their contract.

## Implementation boundary

Step 13 has one outcome. A resolver selects and validates one immutable
`core.WorkflowDescriptor` before it emits `run_started`. The event, the active
run, replay, and the DOT executor use that same descriptor. New execution uses
DOT. It never creates a helper workflow UUID.

The resolver maps both old no-review inputs to one registered graph:

- `workflow:single` maps to `no-review-bead` version `1.0` with
  `legacy_single_label`.
- Queue item `workflow_mode=single` maps to that graph with
  `queue_item_single_mode`.

Both paths execute with resolved mode `dot`. Only the exact registered graph,
when selected through one of those paths, has `review_policy=no_review`.
Arbitrary DOT cannot choose that policy. The reviewed `standard-bead.dot`
remains the default.

The target drafts are:

- `05-spec-drafts/event-model.md`
- `05-spec-drafts/execution-model.md`
- `05-spec-drafts/workflow-graph.md`
- `05-spec-drafts/process-lifecycle.md`
- `05-spec-drafts/beads-integration.md`

The `sub-workflow-dispatch.md` forward reference is outside Step 13. It does
not block this implementation plan. It does block final publication of the
workflow-graph source spec until its owner resolves or re-anchors it.

## Ownership and handoff

The Step 13 directive in `LANES.md` overrides the usual package split for this
work. Alpha owns the final `internal/core` decision and all Step 13 production
composition. Alpha also owns `internal/daemon`, final validation, and the
source-spec change. Bravo owns Step 13 planning and may review or perform a
scoped, non-daemon test task after Alpha gives a handoff. Bravo must not edit
an Alpha-owned emitter.

Before Alpha changes an imported Bravo-owned package or an externally visible
core symbol, Alpha announces the package and exported symbol. The planned core
contract is `WorkflowID`, `WorkflowDescriptor`, and version-2
`RunStartedPayload`.

## Dependency graph

```text
T1 core identity and event record
 ├── T2 typed DOT identity and graph registry ──┐
 │                                               ├── T4 resolver and queue compatibility
 ├── T3 durable non-run payload records ────────┤       └── T5 start emitter and DOT driver
 └── T6 strict read and N-1 replay support ─────┘                │
                                                                ├── T7 focused proof suite
T2 ────────────────────────────────────────────────────────────┤
T3 ────────────────────────────────────────────────────────────┤
T6 ────────────────────────────────────────────────────────────┘
T7 ── T8a four-source-spec finalization ── T8b workflow-graph publication
 │                                               │
 └── T9 scenario test ── T10 explore test ── T11 delete old tail ──┴── T12 final regression proof
```

T2, T3, and T6 can proceed in parallel after T1. T4 starts when T1 and T2 are
stable. T5 starts when T4 is stable. No task deletes the imperative single
tail until the scenario and explore tasks prove the DOT replacement.

The Beads edge `hk-9ji6p` depends on `hk-rq6x6` is live. It enforces T10 after
T9. `br dep cycles --json` reported no active dependency cycle on 2026-08-02.

## Tasks

### T1 — Define the durable workflow and start-event contract

**Owner:** Alpha. **Depends on:** none.

Update `internal/core/workflowid.go`, `internal/core/runstartedpayload.go`,
and `internal/core/eventreg_hqwn59.go`.

1. Make `WorkflowID` a validated logical graph identifier. Accept the named
   identifier grammar and lower-case UUID text only as legacy input.
2. Add `WorkflowDescriptor{WorkflowID, WorkflowVersion}`. Make invalid or
   missing values fail at construction or decode.
3. Define the version-2 `RunStartedPayload` as the one public durable record.
   It carries the descriptor, resolved mode, review policy, selection source,
   queue and worker fields, run ID, and start time.
4. Register the version-2 record and its strict current decoder in the core
   event registry. Do not retain a private daemon event shape.

Acceptance checks:

- Unit tests accept valid named IDs and UUID legacy input.
- Unit tests reject missing IDs, malformed names, malformed UUID text, missing
  version, invalid mode, invalid policy, and invalid policy binding.
- Encoding then strict decoding preserves every version-2 field.
- New code has no `uuid.New()` or equivalent workflow-identity allocation.

### T2 — Parse typed graph identity and register the two embedded graphs

**Owner:** Alpha. **Depends on:** T1.

Update `internal/workflow/dot/parser.go`, the DOT graph model that now stores
`workflow_id` in unknown attributes, and the workflow load and validation path.
Update `internal/daemon/standardgraph.go` and its graph-sync tests. Add
`internal/daemon/no-review-bead.dot` and `specs/examples/no-review-bead.dot`.

1. Parse `workflow_id` and `version` as typed graph properties. Do not leave
   them in `UnknownAttrs`.
2. Apply typed attribute substitution before graph validation. Validate graph
   identity through the T1 constructors.
3. Keep `standard-bead.dot` as the reviewed default graph.
4. Embed and register `no-review-bead.dot`, with logical ID
   `no-review-bead` and version `1.0`.
5. Keep the daemon artifact and `specs/examples/no-review-bead.dot`
   byte-identical. Extend the existing graph-sync proof to enforce this.

Acceptance checks:

- Parser tests prove substitution happens before identity validation.
- Parser tests reject a missing or invalid graph ID and version.
- Registry tests resolve each exact descriptor and reject an unregistered
  descriptor.
- The sync test detects a one-byte difference between the no-review artifacts.

### T3 — Register complete durable payloads at producer boundaries

**Owner:** Alpha. **Depends on:** T1.

Update `internal/core/agentevents_hqwn59.go`, `internal/core/eventtype.go`,
and `internal/core/eventreg_hqwn59.go`. Update the watcher producer under
`internal/handlercontract/` and the current liveness and stale-bead producers
in `internal/daemon/`.

1. Convert raw watcher `supported_versions []int` at `registerAgentEvents` to
   `HandlerCapabilitiesPayload.ProtocolVersionsSupported []string`, in source
   order and decimal form.
2. Preserve the optional Claude-session field as an additive version-1 field.
3. Define and register typed durable payloads for `liveness_halt` and
   `stale_open_bead_detected`.
4. Make each producer emit its registered core record. Do not write a local
   wire-shape duplicate.

Acceptance checks:

- A watcher-boundary test proves `[1, 2, 10]` becomes `["1", "2", "10"]`.
- Tests prove a capability event decodes as its registered core payload.
- Tests prove liveness and stale events have registered record types and retain
  their producer facts through encode and decode.

### T4 — Resolve one graph before validation and event emission

**Owner:** Alpha. **Depends on:** T1, T2.

Update `internal/daemon/workloop_runplan.go`, `internal/daemon/moderesolve.go`,
the queue-item handoff in `internal/daemon/scheduler.go`, and the resolver
tests in `internal/daemon/`.

1. Introduce one resolved-workflow value that contains the parsed validated
   graph, immutable descriptor, resolved mode, policy, selection source, and
   audit-safe raw queue values.
2. Resolve the full queue item. Keep tier-0 `WorkflowMode` and `WorkflowRef`
   through the complete path.
3. Map `workflow:single` to the registered no-review graph with
   `legacy_single_label`.
4. Map queue `WorkflowMode=single` to that same graph with
   `queue_item_single_mode` before validation or emission. Keep the raw item
   value for audit.
5. Derive `no_review` only for the exact registered descriptor selected by one
   of those two compatibility paths. Reject an arbitrary DOT policy claim.
6. Keep `dot` as the resolved mode for both mappings. Keep standard DOT as the
   fallback and reviewed default.

Acceptance checks:

- Resolver unit tests cover normal default, named graph, both legacy inputs,
  retired `review-loop`, duplicate labels, invalid reference, and tier order.
- Both legacy inputs produce equal descriptor and policy values but distinct
  selection sources.
- A direct DOT graph cannot obtain `no_review` from graph attributes or queue
  parameters.
- The resolver result is complete before the caller can create a run.

### T5 — Use the resolved value for event emission and DOT execution

**Owner:** Alpha. **Depends on:** T4.

Update `internal/daemon/workloop.go`, `internal/daemon/dot_cascade_core.go`,
and their focused tests.

1. Delete `workloopRunStartedPayload` and use only
   `core.RunStartedPayload` version 2.
2. Pass the resolved workflow value through validation, run creation,
   `emitRunStarted`, and the DOT-driver entry point.
3. Make the DOT driver accept the selected descriptor. Delete its random
   workflow-ID helper.
4. Assert that the emitted event and the executing graph use the same
   descriptor, resolved mode, policy, and selection source.

Acceptance checks:

- A normal bead emits one version-2 `run_started` record with standard-graph
  descriptor and reviewed policy.
- Each no-review compatibility input emits the registered no-review descriptor,
  `dot`, `no_review`, and its own provenance.
- The event descriptor equals the descriptor passed to the DOT driver.
- Search and focused tests show no private start payload and no random
  workflow-identity creation on new execution.

### T6 — Keep strict current reads and one legacy replay reader

**Owner:** Alpha. **Depends on:** T1.

Update the event compatibility registration and the replay and restart
reconciliation readers under `internal/replay/` and `internal/daemon/`.

1. Decode new writes only through the strict version-2 core record.
2. Provide one N-1 private version-1 decode path for historical replay and
   restart reconciliation.
3. Convert legacy data at the read boundary into the internal replay form. Do
   not let version-1 records reach a new event writer.

Acceptance checks:

- Current decoder rejects version-1 data on the new-write path.
- Historical version-1 fixtures replay and reconcile without panic.
- A version-2 record uses no compatibility fallback.
- Tests show no version-1 emission path remains.

### T7 — Run the focused proof suite and prove each claim can fail

**Owner:** Alpha. **Depends on:** T2, T3, T5, T6.

Run the focused tests from T1 through T6. Deliberately break one representative
claim in each new test family, observe its failure, then restore the change.

Required commands, adjusted only if a package name changes during the work:

```sh
go test ./internal/core/...
go test ./internal/workflow/...
go test ./internal/handlercontract/...
go test ./internal/replay/...
go test ./internal/daemon/...
```

Acceptance checks:

- Each focused test family reaches its intended resolver, producer, decoder, or
  DOT-driver path.
- The test names state the contract they defend.
- Record any unrelated existing failure. Do not hide it as a Step 13 result.

### T8 — Finalize the source specs after independent review

**Owner:** Alpha. **Depends on:** T7 and independent review of the five drafts.

These are the target source files:

- `specs/event-model.md`
- `specs/execution-model.md`
- `specs/workflow-graph.md`
- `specs/process-lifecycle.md`
- `specs/beads-integration.md`

Keep `specs/examples/no-review-bead.dot` byte-identical with the embedded
daemon artifact. T8a can finalize the event-model, execution-model,
process-lifecycle, and beads-integration source specs.

T8b finalizes `specs/workflow-graph.md` only after the owner resolves or
re-anchors the disclosed `sub-workflow-dispatch.md` reference. That reference
is outside Step 13. It must not delay T9 through T11 runtime proof.

Acceptance checks:

- A reviewer confirms the four T8a source specs match their target drafts.
- After the external reference resolves, a reviewer confirms the
  workflow-graph source spec matches its target draft.
- The spec registry and revision metadata remain valid.
- Every source-spec statement maps to a T1 through T7 test or a documented
  later work.

### T9 — Run the planned normal-DOT scenario

**Owner:** existing test bead `hk-rq6x6`. **Depends on:** T7.

This is the scenario task named **“DOT start event v2 lifecycle.”** Use a
disposable project, a scripted twin, a valid named DOT graph, and
`harmonik run --beads`.

Acceptance checks:

- The event log has exactly one version-2 `run_started` record.
- Its descriptor, resolved mode, reviewed policy, and provenance match the
  graph that executed.
- The run reaches its normal terminal result.
- The emitted record is not a private shape and has no generated workflow ID.

### T10 — Inspect both resolved start-event routes

**Owner:** existing test bead `hk-9ji6p`. **Depends on:** T9.

The live Beads dependency `hk-9ji6p` → `hk-rq6x6` enforces this dependency.

This is the explore task named **“inspect resolved DOT start event.”** Run a
daemon and inspect `harmonik subscribe` for a normal named DOT run and a
`workflow:single` run.

Acceptance checks:

- Both observed records are readable version-2 records.
- The normal run has the standard reviewed descriptor and policy.
- The compatibility run has the registered no-review descriptor,
  `no_review`, and `legacy_single_label` provenance.
- Each run has terminal correlation with its observed start event.

### T11 — Delete the imperative single dispatcher only after proof

**Owner:** Alpha. **Depends on:** T9 and T10.

Locate the remaining single-tail behavior and dispatch-mode branch in
`internal/daemon/`. Move any still-required behavior to the DOT graph or retire
it. Remove the imperative dispatcher. Keep CLI and queue legacy input support,
because T4 maps it before dispatch.

Acceptance checks:

- There is one DOT executor.
- There is no imperative single dispatch tail.
- `workflow:single` and queue `WorkflowMode=single` still complete through the
  registered no-review graph.
- Re-run T7, T9, and T10 after deletion.

### T12 — Final regression and handoff record

**Owner:** Alpha. **Depends on:** T8a, T8b, and T11.

Run the affected daemon suite and the project checks that cover changed
packages. Include downstream build and vet checks because core changes affect
many packages. Request independent review before merge.

Acceptance checks:

```sh
go build ./...
go vet ./...
go test -short -count=1 ./internal/daemon
```

Record focused test results, scenario and explore outcomes, compatibility
limits, and any unrelated failures in the Kerf session record. Update the
source-spec status only after the review gate passes.

## Completion definition

Step 13 is complete when all of the following are true:

- Every new run resolves one validated graph descriptor before `run_started`.
- The emitted record, replay result, and executing DOT graph share it.
- New durable starts write only core version-2 records.
- Historical version-1 records remain readable only for replay and restart.
- Both old no-review inputs use the registered no-review DOT graph and retain
  distinct provenance.
- The old imperative single path is gone.
- The focused, scenario, explore, downstream build, and daemon regression
  checks have recorded results.
