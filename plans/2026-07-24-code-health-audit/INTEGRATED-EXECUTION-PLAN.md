# Integrated code-health execution plan

**Status:** L0 committed; independent takeover verification and integration pending  
**Takeover authority:** `HANDOFF-alpha.md` after L0 completes  
**Inputs:** this folder's `_plan.md`, `plans/2026-07-21-p2-extraction/DAEMON-PARALLEL-ROADMAP.md`,
`E5-CHUNK-CATALOGUE.md`, and `E5-dot-runloop.md`

This plan integrates the existing P2 extraction program with the independent code-health audit. It does
not start implementation while L0 is still owned by another agent. All L0-dependent facts must be
refreshed from Git and tests at takeover.

**Live planning checkpoint:** L0 has produced a clean two-commit candidate stack, directly descended from
current main: reviewed L0 implementation `95e248cb`, followed by trivial prerequisite cleanup `24a61f40`
removing a stale integration-test import exposed by tagged vet. It remains exclusively owned by the current
agent until takeover. Independent gate verification, classification/acceptance of both commits, depguard
reconciliation, and merged-tree verification remain outstanding; this checkpoint is not an acceptance claim.

## 1. Operating constraints

1. The session can run four agents total: this orchestrator plus three sub-agents.
2. The machine can safely sustain only **two concurrent Go builders** at the current disk watermark.
   The third sub-agent slot remains useful for planning, review, static analysis, or non-building edits.
3. The LIFT spine is single-writer:
   `workloop.go`, `runports.go`, `reviewloop.go`, `dot_cascade.go`, `dot_gate.go`,
   `runbridge.go`, `sub_workflow_runner.go`, and shared runloop gates/config.
4. Every implementation agent works in its own worktree under `/Users/gb/github/harmonik-wt`.
5. The primary daemon remains down. Mechanical, unit, scenario, race, fault, and isolated real-process
   tests are available; a primary-daemon runtime proof is deferred and must never be claimed.
6. LIFT.12/LIFT.13 require explicit operator approval and a fresh fleet-quiesce check.
7. E2b remains blocked on E2-P plus explicit operator waiver for the new `crewSpawner` seam.
8. Numeric disk headroom is oscillating around the 10 GiB builder threshold; every builder acquisition
   must re-measure it. Broad/race/differential/SSH gates require stable headroom, not one transient pass.

## 2. Integration decision

Continue the reversible LIFT spine through L0–L9, but do not let package movement monopolize the quality
program. Run three file-disjoint safety programs beside it:

- **Restart R — live P0 restart/runtime defects:** re-derive and reproduce the current P0 backlog; a
  proven P0 fix preempts the next LIFT chunk if files overlap.
- **Safety A — supervisor and durable state:** concrete, bounded correctness defects with no spine overlap.
- **Safety B — eventbus:** systemic concurrency/durability ownership, isolated from the spine.
- **Safety C — lifecycle/workspace/process cleanup:** start with specifications and tests outside spine
  call sites; defer edits to `dot_cascade`, `tmuxsubstrate`, or handler/run wiring until their owner releases
  those files.

L0–L9 retain their already approved behavior-neutral move/freeze-gate acceptance contract. The audit
findings—partial-valid `RunPorts`/`RunEnv`/`SharedHandles`, temporal `workLoopDeps` initialization,
giant in-place state machines, and production-graph fidelity—form a **post-L9 backlog to re-measure**.
They must not silently ride inside reversible `git mv` chunks. Any semantic/ownership response is new,
kerf/spec-first work.

The independent programs below are a priority queue, not simultaneously staffed lanes. Steady state is
one spine implementer, one verified-P0 investigator/implementer, and one rotating planner/reviewer. Safety
A/B/C/D and X/Q work take turns in that rotating slot.

## 3. Workstream DAG

```text
L0 takeover verification
  └─ L0 merge
      ├─ L1 → L2 → L3 → L4 → L5 → L6 → L7 → L8 → L9
      └─ dot_cascade behavior-neutral pre-split
          (earliest safe exclusive spine window; never concurrent with another spine chunk)
      both complete
          └─ L12/L13 design reconciliation + decision packet
              └─ STOP: explicit operator gate + fresh fleet quiesce
                  └─ L12 dot_cascade + sub_workflow_runner atom
                      └─ L13 themed drains → final beadRunOne relocation

Safety A0: supervisor contract tests/fix plan ───────────────┐
Safety A1: durable-write/crash model ─┬─ schedule sleep/wake │
                                      ├─ branch-tip sensor   │ independent of L0–L9
                                      ├─ replay corruption   │
                                      └─ sessiondata policy  │
Safety B0: eventbus invariant/spec pass ── bounded delivery/drain/crash tests
Safety C0: process ownership model ─────── remote SSH + handler + cleanup contexts
Safety D0: queue transaction model ─────── CAS/ownership + migration/crash tests

Restart R0: verify live P0 backlog ── deterministic isolated reproducers ── localized fixes

After LIFT file release:
  process call-site integration + tmux substrate ownership
  run graph immutable assembly/decomposition
  keeper temporal-state lane
  pure-logic parallel drains
```

## 4. Priority lanes

### Lane R — daemon restart/runtime P0s, preemptive correctness lane

The handoff names eight P0s across Codex timeout/reaper behavior, Claude trust/isolation, tmux buffer
naming, captain comms, and related restart failures. Those labels and statuses are claims, not ground
truth. At takeover:

1. re-run the live P0 inventory;
2. group duplicate symptoms by production root cause;
3. locate the production call site and build an isolated deterministic reproducer;
4. distinguish localized bugs from lifecycle-contract changes;
5. fix localized proven defects in their current owner with failing-first tests;
6. route cross-subsystem lifecycle changes through kerf/spec work before implementation.

This lane does **not** require the LIFT to finish. If a proven P0 fix touches the currently active spine
chunk, finish/revert that reversible chunk, then give the P0 the next exclusive spine window. Never run two
writers on the same files.

No live primary-daemon canary is allowed. Recovery proof uses the scratch/isolated real-runtime harness.

### Lane S — LIFT spine, one writer

**Owner files:** all run-path files named in §1.3 plus `.golangci.yml`, Makefile runloop wiring, and
`scripts/runloop-freeze-gate.sh` while a chunk is active.

Sequence:

1. Assess L0 from Git, commit metadata, diff, and real gates.
2. Merge L0 only after reviewer approval and required trailer verification.
3. Execute L1–L9 strictly in the existing leaves-first order.
4. Schedule the behavior-neutral `dot_cascade` intra-package pre-split at the earliest safe exclusive spine
   window after L0. It may occur before all L1–L9 chunks but never concurrently with another spine writer.
5. Stop before L12/L13. Obtain the explicit operator decision and re-run fleet-quiesce.
6. L12 is the remaining `dot_cascade` + `sub_workflow_runner` atom after the pre-split.
7. Design reconciliation and the operator decision packet may proceed before approval; implementation
   and file mutation may not.
8. L13 is not one giant commit. It is a serial themed-drain sequence—branching/workflow resolution,
   model/profile resolution, sandbox, guards, emitters—followed by final `beadRunOne` caller relocation.

There are intentionally no L10/L11 chunks: `codesync` and `codexnowork` already moved. Do not recreate
them.

Audit observations during L0–L9 are non-expansion checks only: package movement must not worsen measured
complexity/span or create new partial initialization. After L9, re-measure bundle completeness, graph
construction, staged initialization, and state-machine size; then create/reconcile kerf/spec work before
semantic changes.

### Lane A — safety hypotheses and proven durability defects

This lane is subdivided so its pieces can run in successive independent worktrees.

#### A1 Supervisor concurrent state

Files: `internal/supervise/**`.

Static review found a concrete double-close path and stale load-copy-store risk. First independently
reproduce or prove the invariant breach with a concurrency contract test; do not design a test merely to
force the conclusion. If not demonstrated, downgrade this behind verified P0s and systemic eventbus/
lifecycle evidence. Any localized confirmed fix remains one small reviewed commit with no LIFT dependency.

#### A2 Durable-state contract

Begin with a kerf work or reconciliation into the normative persistence owner covering atomic durable writes:
unique temp → write → file sync → close → rename → parent sync, plus explicit corrupt/torn-read behavior.
Then split implementation work by file ownership:

- schedule sleep/wake transaction and sidecar;
- lifecycle branch-tip persistence;
- replay strict corruption accounting;
- sessiondata durability/corruption visibility.

Do not create four bespoke atomic-write variants. Settle the shared contract first, then parallelize
file-disjoint consumers.

### Lane B — eventbus concurrency and durability

Files: `internal/eventbus/**`; no run-path ownership.

This is cross-subsystem behavior and is kerf/spec-first. Required outcomes before code:

- bounded delivery/backpressure behavior;
- single ownership point for global/per-run drain counters;
- ordering and cancellation contract for consumers and observers;
- JSONL/fsync failure behavior;
- close/publish/drain lifecycle.

Tests must cover race, stress, goroutine bounds, I/O faults, replay, and drain invariants. This is a named
workstream, not a miscellaneous complexity cleanup.

### Lane C — process and cleanup ownership

Primary files:

- `internal/lifecycle/tmux/runner.go`;
- `internal/handler/session.go`;
- `internal/workspace/createworktree.go`;
- promotion cleanup and lifecycle/workspace recovery helpers.

Reserved until the spine releases them:

- `internal/daemon/dot_cascade.go`;
- `internal/daemon/tmuxsubstrate.go`;
- run-path paste/liveness call sites.

Kerf/spec reconciliation and read-only harness design may begin immediately. No shared-contract or harness
implementation begins until that work defines ownership and compatibility. Integrate spine call sites only
after the active LIFT chunk commits and relinquishes those files. A localized bug with a failing regression
test may proceed without a new work only when it does not change lifecycle semantics.

Lane C explicitly excludes X2-owned `internal/daemon/orphansweep.go`, `RunOrphanSweep`, and their direct
move/provenance tests.

Required invariants:

- every `Start` site has one Wait owner;
- cancellation owns the full local or remote process tree;
- teardown uses an independent bounded cleanup context;
- restart semantics distinguish owned detached jobs from fire-and-forget work;
- no cleanup deletes ambiguous or dirty worktree state.

### Lane D — queue transaction and recovery

Files: `internal/queue/**` first; daemon callers are integration points and remain reserved by the spine.

This is kerf/spec-first. Plan around one mutation/persistence ownership API or persisted generation/CAS
invariant. Cover:

- direct `Persist` callers;
- legacy unlocked handler fallback;
- migration with one valid-copy invariant;
- completion/cancellation memory-before-disk behavior;
- crash/restart and generated transition sequences.

Pure `queue.Validate` complexity is a later parallel cleanup, not the primary risk.

### Lane E — lifecycle, keeper, workspace, and worker follow-ons

These begin after higher-priority ownership contracts are staffed:

- residual lifecycle recovery files/tests explicitly outside X2's lease, after X2 lands;
- keeper watcher/cycle temporal model and hermetic tmux/comms action tests;
- workspace remote/reviewer materialization and cleanup;
- remote worker telemetry fault injection.

Keep these separate by package unless a shared invariant is explicitly identified.

### Existing X/Q lanes

Retain the already reconciled non-spine work rather than duplicating it:

- X1 quiesce → daemon maintenance package;
- X2 orphan sweep → lifecycle, augmented with the audit's crash/PID/provenance criteria;
- Q2 file-sharded quality drain outside active spine files;
- X3/Q3 subscribe and command quality now that projectconfig has landed, but below restart P0s and systemic
  correctness lanes.

Stale-watch and handler-pause still serialize into the spine. Tmux-substrate remains gated.
Crew-start/E2b is blocked on preparatory slice E2-P **plus** explicit operator waiver of the no-new-seam
rule.

### Lane P — parallel deterministic drains

Only after the complexity, coverage, suppression, and mutable-global ratchets exist:

- DOT parser/validator;
- queue validators;
- cohesive CLI command handlers;
- stale/subscribe helpers;
- explicit production complexity suppressions;
- narrowly justified duplicate groups.

Each slice must lower a stored baseline. It cannot close by moving code or adding another suppression.

## 5. Four-slot staffing cadence

At most two rows marked **build** may run concurrently.

| Slot | L0–L9 cadence | After reversible LIFT |
|---|---|---|
| main | verify/merge, maintain leases, synthesize reviews, refresh disk and conflict map | same |
| agent 1 | LIFT spine implementer (**build**) | spine or post-LIFT graph owner (**build**) |
| agent 2 | restart-P0 investigator/implementer (**build only after proof**) | next verified P0 or isolated recovery proof (**build**) |
| agent 3 | X1/X2, supervisor/eventbus/durable-state planning, or reviewer; no build while two builders run | planner/reviewer/non-building editor while agents 1+2 hold tokens; may build only after one token is released |

Use waves rather than long-lived generalists. A worktree agent owns one bounded commit, returns its evidence,
and releases the slot. The next agent starts from the newly integrated HEAD.

The main orchestrator's integration build/check-fast consumes one of the two builder tokens; pause one
agent build while integration gates run.

## 6. Conflict and ownership matrix

| Boundary | Spine | Supervisor | Durable state | Eventbus | Process/cleanup | Queue | Keeper |
|---|---:|---:|---:|---:|---:|---:|---:|
| run-path files | owns | — | — | — | reserve call sites | reserve callers | reserve callers |
| `.golangci.yml` / Makefile | owns during each chunk | schedule merge | schedule merge | schedule merge | schedule merge | schedule merge | schedule merge |
| `internal/supervise` | — | owns | — | — | — | — | — |
| `internal/eventbus` | — | — | replay consumer only | owns | — | — | — |
| `internal/lifecycle` | imports/tmux only | — | branch-tip owns exact file | — | owns other exact files | recovery integration later | — |
| `internal/workspace` | read dependency | — | — | — | owns | — | — |
| `internal/queue` | imports | — | — | — | — | owns | — |
| `internal/keeper` | — | — | — | — | tmux integration coordination | — | owns |

The file lease, not the package name, is authoritative. Shared configuration/gate files are merged in a
short orchestrator-owned integration commit or reserved for one worktree at a time.

## 7. Gates by work class

Every non-trivial commit:

- scoped build/vet/test;
- `.tools/golangci-lint run --new-from-rev=HEAD~1`;
- `ubs` on changed source files;
- independent reviewer approval;
- valid `Reviewed-By` and `Review-Verdict` trailers;
- `make check-fast` after commit where applicable.

Additional gates:

| Class | Extra proof |
|---|---|
| LIFT move | runloop freeze gate, depguard, differential oracle when disk permits |
| concurrency | `go test -race`, repeated stress, goroutine bound/leak assertion |
| durable state | injected failure at every write/rename/sync boundary; restart/readback |
| process lifecycle | real local process group; real isolated SSH worker where required |
| queue/recovery | generated transition sequences and crash/reload invariant |
| keeper/tmux | hermetic isolated tmux namespace and deterministic clock/input |

No primary-daemon test is permitted. A deferred live proof is recorded explicitly.

Race, broad scenario, differential, and real-SSH proofs are required before final lane completion but may
be explicitly deferred when numeric disk headroom is at/below the threshold. At that point only narrow
targeted tests/scoped builds run under the two-token scheduler, and no lane may claim fully green without
recording the deferred gate.

## 8. Takeover checklist after L0 completes

1. Re-read `HANDOFF-alpha.md`; do not trust its commit/state claims.
2. Inspect L0 branch head and worktree status.
3. Verify and classify both candidate commits separately: the L0 implementation and the trivial
   integration-test import cleanup outside nominal L0 ownership. Explicitly decide whether the prerequisite
   cleanup is accepted in the fast-forward stack.
4. Reconcile the L0 depguard discrepancy before merge: the candidate intentionally allows only current
   `ports.go` imports and omits `internal/runlaunch`, while the committed roadmap/handoff says L0 should
   pre-arm the measured L0–L9 union. Either amend/follow up the candidate or record a deliberate revised
   decision and correct the roadmap/handoff; green tests alone do not settle the contract.
5. Verify build, vet, check-fast/delta lint, runloop freeze gate, and reviewer trailers.
6. For L0, merge by `--ff-only` if it still directly descends current main. Do not rebase the L0 candidate.
   If main actually advances, use cherry-pick only after exact file-disjointness is proven.
7. Re-run build and freeze gate on the integrated branch.
8. Remove the L0 worktree and branch only after the commit is reachable from the main branch.
9. Refresh disk headroom, worktree registry, open branches touching the spine, and audit-plan assumptions.
10. Commit this plan folder with the required review trailers before any new implementation worktree
    depends on it.
11. Start the next LIFT worktree plus one independent safety worktree; reserve the third sub-agent for review.
12. Re-derive the P0 restart inventory and give a proven overlapping P0 preemption authority over the next
    reversible LIFT chunk.

## 9. Operator decisions

Already required:

1. explicit approval before L12/L13;
2. E2b preparatory slice E2-P plus explicit `crewSpawner` seam waiver.

Potential new decisions should be surfaced only after planning proves they are real:

- whether detached scheduled commands are durable owned jobs or intentionally fire-and-forget;
- durability/sync policy for session metrics;
- whether destructive dead worktree probes are deleted or given a production contract.

These do not block L0–L9 or the independent supervisor/eventbus/durable-state planning lanes.

## 10. Done means

- L0 takeover was verified from live Git/test evidence;
- every workstream has exact file ownership, dependency gates, test class, and merge order;
- the reversible LIFT and independent P0 safety lanes can remain staffed without overlapping writes;
- no plan requires the broken primary daemon for its acceptance evidence;
- operator-gated work remains stopped at the gate;
- the final implementation program has honest disk/build concurrency limits and recoverable worktree state.
