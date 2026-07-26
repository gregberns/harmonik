# Core queue, job/run, and Pi execution plan

**Priority:** first. The goal is a reliable local execution substrate: accept a supported queue item,
persist it, claim it once, make one Run, launch Pi, observe its lifecycle, and reach one recoverable
terminal state. The authoritative task inventory is [`TASK-INDEX.yaml`](TASK-INDEX.yaml).

## Scope

```text
CLI/socket admission → durable queue record → eligibility/claim → Run creation
→ local Pi LaunchSpec/process → ready/identity/input/exit → terminalization/release → recovery
```

The primary daemon is down, so its runtime proof is deferred. Hermetic local scenarios and controlled real
subprocesses are the immediate evidence. Remote SSH, crew, and scheduling are explicitly out of this wave.

## Architecture rules

1. Characterize real supported boundaries and crash cuts before refactoring. Stale plans and method names
   are not evidence.
2. Put I/O behind small consumer-shaped ports: persistence, claim/ledger, process start/wait, event
   observation, clock, and workspace/context provisioning. Do not introduce a generic dependency bag.
3. Use a functional core: eligibility, recovery, outcome mapping, and launch validation take immutable
   values and return typed decisions/reasons. Adapters perform the resulting I/O once.
4. Use state-transition tables and property tests: terminal stays terminal, recovery is idempotent, and no
   event sequence creates two owners.
5. Correlate Pi events with immutable run/process identity; never synthesize readiness or acknowledgements.
6. Make cleanup idempotent with explicit wait, cancellation, terminal-persistence, and claim-release owners.

## DAG

```text
CQ-00 ─┬─ CQ-01 ─┬─ CQ-03 ─┬─ CQ-04 ─┐
       │         │         └─ CQ-05 ─ CQ-06 ─ CQ-07 ─┐
       └─ CQ-02 ─┘                                    │
                                                          ├─ JR-05 ─ PI-06 ─ E2E-01
CQ-00 ─ JR-00 ─┬─ JR-01 ─┬─ JR-03 ─ JR-04 ──────────────┘             ├─ E2E-02 ─ E2E-04
               └─ JR-02 ─┘                                            └─ E2E-03 ─┘

PI-00 ─┬─ PI-01 ──────────────────────────────────────────────────────┘
       ├─ PI-02 ─┬─ PI-04 ─┐
       └─ PI-03 ─┘         ├─ PI-05 ───────────────────────────────────┘
                            └──────────────────────────── PI-06
```

Before the general evidence wave, `CQ-DEF-01` is a ready P0: a persisted queue `DefaultHarness=pi` currently
falls out of the selection/snapshot path before production dispatch, allowing an unlabeled item to resolve
Claude. Its lease is narrow and it must add a tier-2 production-path characterization test. Start the other
three non-overlapping evidence tasks in parallel: `CQ-00`, `JR-00`, and `PI-00`. Then allow at most one
builder in each queue, run, and Pi ownership area, keeping a reviewer/planner slot free.

Pi has a separate ready safety wave: `PI-A1` secret/key-file representation, `PI-A2` current Pi disk-auth
guard, `PI-L1` terminal-signal ownership, `PI-F0` harness-aware ProcessExit finalization, and `PI-Q1`
queue-default propagation. Its DAG is `{PI-A1,PI-A2,PI-L1,PI-F0,PI-Q1} →
{PI-R1,PI-F1,PI-F2,PI-Q2} → {PI-R2,PI-O1} → PI-E1`.

## Continuous bead-worker protocol

1. Coordinator verifies a ready task's dependencies, base, file lease, and acceptance criteria; only then
   creates/claims the bead.
2. Worker receives one isolated worktree, one exclusive lease, one atomic outcome, and targeted checks.
   A discovered cross-boundary defect becomes a new task rather than an opportunistic patch.
3. After independent review and integration checks, record commit/test evidence in the YAML index and
   unblock dependents. The next worker uses the documented contract and scenario fixtures, not oral context.

## Review gates

Every planned section and every completed implementation task gets a review from a different agent before it
is accepted. The reviewer checks missing cases, task boundaries, dependency direction, acceptance tests, and
whether the lease is actually exclusive. Before a group begins consuming another group—or is declared
complete—a separate cross-group reviewer checks contract compatibility, duplicate ownership, durable-state
and recovery consistency, error propagation, scenario coverage, and DAG ordering. Findings change the index;
they are not merely advisory comments.

## Two Pi Ralph workers and frontier routing

Operate two Pi workers continuously only on `model:pi-ralph` cards: low-complexity deterministic tasks with
an explicit contract, exclusive small lease, bounded checks, and a defined escalation point. Good Pi work is
fixture/test expansion, evidence inventory, a narrow pure function, an isolated adapter, or a proven
mechanical defect. Pi must not decide security policy, create a cross-subsystem contract, refactor shared
spines, or integrate broad changes.

Operate two Codex workers (and later Claude when available) on `model:frontier` cards: high-ambiguity
ownership, concurrency/recovery, security, shared-spine integration, cross-group review, and Pi escalation.
One frontier overseer continuously reviews Pi checkpoints, turns ambiguous findings into bounded cards,
handles failing tests that escape the card, and keeps the Pi queue replenished. Use labels
`complexity:low|medium|high` plus `model:pi-ralph|frontier`; route by ambiguity and blast radius rather than
subsystem name. Medium tasks require a frontier-written/reviewed task card before Pi starts.

`E2E-04` is the first-wave completion bar: bounded local admission, claim, Run, Pi, recovery, race, and
fault proof. It does not claim SSH or primary-daemon proof.
