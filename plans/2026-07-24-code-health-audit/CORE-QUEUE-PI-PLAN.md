# Core queue, Run, and local Pi recovery plan

**Priority:** first.
**Machine authority:** [`TASK-INDEX.yaml`](TASK-INDEX.yaml).
**Worker handoffs:** [`tasks/`](tasks/README.md).

The completion bar is a reliable local path:

```text
supported admission
  → durable queue record
  → one durable reservation carrying Run ID
  → one Bead claim and one Run
  → local Pi launch/lifecycle/finalization
  → one terminal/release result
  → fixed-point restart recovery
```

The primary daemon remains down. Acceptance uses hermetic scenarios, real local
git/Beads fixtures, twin processes, and controlled local Pi subprocesses.
Functional SSH, crew launch, scheduling, and primary-daemon deployment are
deferred.

## Why the original index was unsafe

The 2026-07-25 deep review found:

- the prose DAG still referenced superseded `PI-01`…`PI-06` and `PI-Q1`;
- `E2E-02` depended on superseded `PI-05`;
- CQ and JR both claimed overlapping ownership of the same dispatch transaction;
- queue dispatch persisted `dispatched` without a Run ID, treated durable-write
  failures as nonfatal, patched the Run ID in a second nonfatal persist, and
  then claimed the Bead;
- existing pure queue/Run kernels and scenario harnesses were incorrectly
  planned as greenfield work;
- one “ready” Pi secret task contradicted normative specs;
- local Pi proof was blocked by speculative rate-limit and functional-remote
  work;
- the Pi callback/process/finalizer item combined distinct parser-lock,
  signal-before-session, unbounded-kill, and per-workflow integration defects.

Schema v2 removes stale `unlocks`, uses only `hard_requires`, adds exact model and
lease families, splits those ownership boundaries, and keeps workers from
editing the coordinator-owned index.

## Critical DAG

```text
FIRST WAVE (four total agents)
  coordinator/overseer: Sol xhigh
  ├─ CQ-DEF-01: Sol high, exclusive dispatch spine
  ├─ CQ-00A: Pi/Nemotron inventory
  └─ CQ-00B: Pi/Nemotron inventory

QUEUE
  CQ-00A + CQ-00B → CQ-00
  CQ-00B → CQ-MIG-01 ─────────────┐
  CQ-00 → CQ-02 contract → CQ-02I ├→ CQ-01 admission → CQ-03 reservation
                                   └───────────────────────────────────────┐
  CQ-03 → CQ-04 recovery ─┬→ CQ-07 restart scenario                     │
  CQ-03 → CQ-05 closure → CQ-06 ports ────────────────────────────────────┘

RUN
  JR-00 → JR-02 closure ─────────────────────┐
  JR-00 + CQ-03 → JR-01 claim/Run → JR-03 terminal/release
                                              └→ JR-04 recovery → JR-05 scenario

PI SECURITY
  PI-00 → PI-SPEC-01 → PI-A1 ────────────────┐
                         └→ PI-A2A → PI-A2B → PI-R1

PI PROCESS / FINALIZATION
  PI-00 → {PI-L1A, PI-L1B, PI-L1C, PI-F0}
             └──────────────────────→ {PI-F1, PI-F2S, PI-F2D}

PI ADMISSION / LOCAL-ONLY
  CQ-DEF-01 + CQ-00 → PI-Q2A → PI-Q2B → PI-Q2D
  CQ-DEF-01 + PI-SPEC-01 → PI-R2 local placement fence

  {PI-A2B, PI-R1, PI-F1, PI-F2S, PI-F2D, PI-Q2D, PI-R2}
      → PI-E1 local lifecycle matrix

END TO END
  CQ-07 + JR-05 → E2E-BASE
  E2E-BASE + PI-E1 → E2E-PI
  E2E-BASE + JR-04 → E2E-FAULT
  E2E-BASE → E2E-STRESS
  all four → E2E-GATE
```

Rate-limit fixture/classifier work (`PI-O0`, `PI-O1`) is P1 and deferred. It
does not gate the local happy path. Functional remote Pi remains `REMOTE-00`;
`PI-R2` proves refusal and absence of secret transport only.

## Architecture rules

1. Characterize supported production boundaries before changing them.
2. The queue transaction has one durable owner. Reservation persists
   `dispatched + RunID` together before Bead claim; persistence failure causes
   zero claim and zero launch.
3. Logic is pure where decisions are possible; adapters perform durable writes
   and events once.
4. Existing kernels (`internal/queue/state.go`, `internal/orchestrator/select.go`,
   `internal/runexec`, and the handler outcome mapper) are reviewed/extended,
   not replaced.
5. Existing queue and restart scenarios are extended; no second harness is
   invented.
6. Pi security policy is spec-first. No worker implements around a normative
   contradiction.
7. Spawn proof, captured identity, genuine readiness, terminal signal,
   finalization, wait, and reap are distinct facts.
8. Primary-daemon deployment still requires new isolated real-runtime end-to-end
   proof per the standing orchestrator rule.

## Four-agent coordination without comms

The coordinator is the fourth agent and sole index/worktree/integration owner.
Three workers receive immutable cards and exclusive worktrees. Their durable
return is a commit or `tasks/evidence/<task>.yaml`, so progress is recoverable
from Git even if chat/comms fail. See [`tasks/README.md`](tasks/README.md).

Do not attempt the old “two Codex + two Pi + overseer” shape: that is five roles.
With four total slots, use one overseer, one frontier builder, and two bounded
workers. Once Pi cards are exhausted, replace a Pi slot with Terra rather than
manufacturing low-value work.

## Model settings

- GPT-5.6 Sol `xhigh`: security/spec policy, concurrency, durable recovery,
  shared-spine ambiguity, and cross-group reviews.
- GPT-5.6 Sol `high`: a Sol-reviewed shared-wiring or bounded security
  implementation such as `CQ-DEF-01`.
- GPT-5.6 Terra `high`: bounded multi-file implementation and scenarios after
  the contract is explicit.
- Local Nemotron via Pi Ralph, high thinking: inventories, fixtures, exhaustive
  pure tables, and narrow mechanical fixes only. One corrected retry; then
  escalate to Terra. Sol reviews every Pi result.

## Done means

- `E2E-GATE` is green from a clean tree;
- queue, Run, and Pi groups each have an approved cross-group review;
- required local targeted/race/fault/real-process proofs are green;
- no task is complete with pending required evidence;
- primary-daemon and functional-SSH proofs remain explicitly deferred until
  their own prerequisites and pre-deploy gate are satisfied.
