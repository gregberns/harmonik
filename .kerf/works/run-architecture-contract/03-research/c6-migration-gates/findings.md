# C6 research — Migration DAG and structural gates

## Questions

1. What are the reproducible hotspot baselines?
2. Which task DAG is already fixed?
3. Which lease families serialize work?
4. What is a valid intermediate/rollback boundary?
5. What must ARCH-GATE measure literally?

## Findings

### Reproducible baselines

ARCH-00 records:

| Symbol/type | Span | Cognitive complexity | Reach |
| --- | ---: | ---: | --- |
| `runWorkLoop` | 1667 | 888 | only outer-loop caller; dispatch spine |
| `beadRunOne` | 2298 | 396 | called by `runWorkLoop`; broad three-bundle graph |
| `runReviewLoop` | 1586 | 331 | called by `beadRunOne`; 26/48 daemon-private relocation census |
| `driveDotWorkflow` | 1002 | 195 | DOT core |
| `dispatchDotAgenticNode` | 882 | 185 | DOT core plus sub-workflow runner |
| `workLoopDeps` | 743 | n/a | 81 fields; seven direct type-use files |

Commands:

```sh
gocognit -test internal/daemon | rg \
  ' (runWorkLoop|beadRunOne|runReviewLoop|driveDotWorkflow|dispatchDotAgenticNode) '
rg -n '^func ' internal/daemon/workloop.go internal/daemon/reviewloop.go \
  internal/daemon/dot_cascade_core.go
go list -f '{{range .Imports}}{{println .}}{{end}}' ./internal/daemon
```

ARCH-00 also records file import counts: workloop 49 total/34 project,
reviewloop 25/15, DOT core 25/15. The daemon has 47 direct internal-package
imports. Field reach must be measured by declared fields plus direct type-use
files; forbidden direction is any `internal/runloop -> internal/daemon`
import.

### Existing migration order

CQ-02 already defines 21 queue/JR/WL slices with prerequisites, files/symbols,
leases, intermediate states, rollback boundaries, proofs, and conflicts. RAC
must import rather than reorder that graph. Key spine:

```text
CQ-02I -> CQ-01 -> CQ-RECEIPT -> CQ-RUN-WAIT
CQ-01 + CQ-02I + WL-03 -> CQ-03 -> JR-01
CQ-RECEIPT + JR-01 -> CQ-04
CQ-RUN-WAIT + CQ-04 + JR-01/JR-02 + BR-03 -> JR-03
JR-03 + CQ-04 -> JR-04 -> WL-REC-01
```

ARCH-01/ARCH-GATE precede structural WL tasks. RL work must reconcile its
staged line before reviewloop-spine implementation.

The indexed task pack already owns the remaining decomposition:
PS-01/PS-02 own shared lifecycle, BR-00/BR-01/BR-02A/B/C/BR-03 own the
beadRunOne plan/resource/mode chain, WL-02A/B/WL-MUT-01/WL-03 own the outer
loop, RL-02A/B/C/RL-03 own review, and DOT-01/02/03/DOT-GATE-01 own DOT.
Parallel ARCH-prefixed copies would be conflicting shadow owners.

Two genuine gaps remain: a coordinated HC/RSM/AIS Ack-contract amendment
before RAC drafting, and a production HC/PL conformance slice between PS-01
and PS-02. Final deletion of `workLoopDeps` after every indexed consumer moves
is also not owned by an existing card and must be proposed in evidence only.

### Collision leases

ARCH-00 fixes `dispatch_spine`, `workloop_recovery_spine`,
`reviewloop_spine`, `dot_spine`, `process_phase_scope`, `queue_rpc`,
`pi_launchspec`, and `composition_spine`. One semantic writer may hold a spine
at a time. A task touching multiple spines must wait for all prerequisites; a
shared file does not justify merging semantic owners.

### Accepted intermediate states

A valid node:

- compiles and has focused tests;
- preserves current behavior or fails closed;
- exposes one new target boundary without enabling downstream writers early;
- names exact writable paths/symbols;
- can be rolled back as a unit without leaving a caller on an absent API.

Examples from CQ-02 include a transaction substrate with callers still gated,
receipt primitives with terminal producers disabled, and a recovery owner
whose workloop call sites remain unchanged. These are the pattern for run
construction and lifecycle migration.

### ARCH-GATE implications

The card requires literal integer targets for span, cognitive complexity,
field/dependency reach, and forbidden imports. Research establishes that gates
must be symbol-based, reproducible, and ratcheted from the table above.
Completion also needs:

- no required field populated after constructor return;
- no nullable queued/direct discriminator combination;
- zero `workLoopDeps` use in per-run/mode functions;
- zero daemon-private undefined symbols before any package move;
- zero reverse daemon imports;
- a bounded number of fields per replacement value/handle type;
- each hotspot below its individual span and cognition ceiling.

The actual integer ceilings are a design decision; percentages or “shrink”
language are nonconforming.

## Risks and decision status

Parallel edits to shared spines, gates based only on file size, and function
relocation without responsibility reduction are the primary false-progress
risks. Existing tasks cover most queue/JR/RL work; missing construction or
HC/PL-conformance slices must be proposed in ARCH-01 evidence only. No
unresolved blocker prevents design.
