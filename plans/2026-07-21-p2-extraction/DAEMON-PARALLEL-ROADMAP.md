# internal/daemon — parallel execution roadmap (LIFT + residue + quality)

**Written:** 2026-07-24 (alpha session), synthesizing three parallel planning passes (LIFT leaf-move,
post-LIFT residue extraction, quality-debt drain). **HEAD at authoring:** `b7767a4fd` (RunRegistry
consumer-port landed = LIFT crit 4). **Governing rule:** `_plan.md` §1 — extract only behind seams
that ALREADY EXIST; every unit ships its depguard deny-back edge + grep freeze gate in the same commit.

## The unifying model
`workloop.go` (6,664 LOC, ~356 commits/90d) plus the run-path files form a **single-writer SPINE** —
two writers there = merge warfare. Everything off the spine shards into independent lanes. Where a file
carries BOTH structural debt (needs extracting) AND quality debt (errcheck/nilerr), **one agent owns it
end-to-end** (extract + clean in one slice) so the lanes never collide on it.

## Stale-doc corrections found this session (verified against git/live linter)
- LIFT depguard allow-list in `E5-CHUNK-CATALOGUE.md` §4.2 crit 8 **omits `internal/runlaunch`** —
  imported by reviewloop.go (L8), dot_gate.go (L9), dot_cascade.go (L12). Without it the first mode-file
  commit fails depguard. ADD IT. (Over-claims `sessiondata` + bare `lifecycle` — those are L13's.)
- The reversible LIFT leaf set is **10 chunks (L0–L9), not 12**: `codesync_rs_b8.go` already left to
  `internal/transport/codesync` (`222ff36f`) and `codexnowork_hk368i4.go`'s concern left with the codex
  harness (`5f7762c3`). They are no longer lift chunks.
- Quality audit (2026-07-22) stale in 3 places: `cmd/harmonik/handler.go` errcheck **67→0** (cleaned);
  **hk-8dtiv CLOSED** and the `.golangci.yml` Close-errcheck flip already landed (`f1f96c99`) — there is
  **no "config flip lands last"** to sequence; daemon nolint **869→742**; two named daemon nilerr bugs
  (`commscursor.go:246`, `pasteinject.go:2267`) already fixed.
- LIFT entry criteria 1/2/6 all MET on the live tree (deps reads 0/0/0 in the mode files — residual grep
  hits are comments; export_test.go gone). Criteria 4 (RunRegistry) MET. Only **crit 5 (projectconfig
  leaf)** outstanding, in flight.

## Master concurrency map

| Lane | Work | Files | Runnable |
|---|---|---|---|
| **SPINE** (serial, 1 writer) | The LIFT: L0 → L1–L9 → **stop before the irreversible tail** (LIFT.12 dot_cascade+sub_workflow_runner atom; LIFT.13 beadRunOne drain) | run-path files | **L0 unblocks when PC lands** (RunRegistry done) |
| **X1** | Extract quiesce → daemonmaint + drain its linters | quiesce.go | now (PC-free, spine-free) |
| **X2** | Rehome orphansweep + drain | orphansweep.go | now |
| **Q1** | Quality drain reconciliation (~38 errcheck) | reconciliation*.go | now (in flight) |
| **Q2** | Quality drain misc, shardable by file | restartbackoff, notifystream, statedisk, handlerpause_persist, eagerfill | now |
| **X3/Q3** | Extract subscribe + drain; cmd/harmonik quality bucket | subscribe.go, cmd/* | AFTER PC (PC edits these) |
| **B** | stalewatch, handlerpause extractions | touch workloop.go | serialize INTO the spine |
| **GATED** | tmuxsubstrate, crewstart | — | no-new-seam rule / E2b operator-gated |

**The pivot is PC (projectconfig extraction, crit 5):** landing it simultaneously opens the LIFT (L0) and
frees the subscribe/cmd lanes. **Disk is the live limiter** — each building agent refills GOCACHE; cap
concurrent builders at ~2–3 until the tree quiesces, then re-clear cache and open the fan-out.

## LIFT leaf-move execution (SPINE, after PC)
Order (leaves-first; the mode-file DAG's apparent cross-edges are all comments — only real edge is
dot_cascade→reviewloop's `rl*` symbols):
- **L0** (foundational): split `runports.go` — port interfaces + RunPorts/RunEnv/SharedHandles → new
  `internal/runloop/ports.go`; the daemon* adapters + the `workLoopDeps.runPorts()/runEnv()/
  sharedHandles()` constructors STAY in daemon. Create the package, the depguard block (allow-list below,
  **incl. `runlaunch`**), the `scripts/runloop-freeze-gate.sh`, wire it into check-fast+check-short, and
  the testhelper hookup — all in L0. **L0 is the only chunk gated on the two preconditions.**
- **L1** workloopeventsource.go · **L2** postreadyhang.go · **L3** waitsocketgrace.go · **L4** runshell.go
  + dispatchsegment.go (PAIRED — shared test helpers `recordingEffectors`/`pumpUntilDone`; or pre-lift
  those two helpers to `internal/testhelpers` first) · **L5** scenariogate.go (before L6) · **L6**
  runbridge.go (reads L5's `sgr` result) · **L7** reviewerharness_hkiv748.go · **L8** reviewloop.go
  (temporarily export the 5 `rl*`/`emitReview*` symbols dot_cascade still consumes; un-export at LIFT.12)
  · **L9** dot_gate.go (last leaf; split `dot_gate_heartbeat_hkvjsv_test.go` — the one test violating
  leaves-first).
- **depguard allow-list for `internal/runloop`** (verified union of real imports, L0–L9): `$gostd, core,
  handler, handlercontract, substrate, runexec, runmerge, mergeq, gitprobe, queue, workers, brcli,
  lifecycle/tmux, runlaunch, harness/shared, harness/claude, transport/tunnel, transport/codesync,
  policy, workflow/dot, workspace, google/uuid`; deny `internal/daemon`. Draft with `workflow` (bare) +
  `harness/codex` too so LIFT.12/.13 don't re-touch `.golangci.yml`.
- **Schedule FIRST (behavior-neutral prep):** intra-package split of `dot_cascade.go` inside package
  daemon into `dot_cascade_core.go` (the newDotSubWorkflowRunner↔dispatch cycle members only, ~1,300 LOC
  = the residual LIFT.12 atom) + `dot_cascade_helpers.go` (~1,600 LOC, liftable as an ordinary leaf).
  Shrinks the irreducible atom from 3,296 → ~1,300 LOC. Land fast (dot_cascade.go is 104 commits/90d).
- All of L0–L9 are strictly sequential (shared caller files + the shared freeze-gate/`.golangci.yml`/
  Makefile edits). The one worktree-parallel exception: the optional test-helper pre-lift (test-only).

## Residue extraction (X-lanes) — behind existing config seams
Ready now (0 workloop refs, existing `*Config` seam): **X1 quiesce** (1,003 LOC → daemonmaint), **X2
orphansweep-rehome** (finish the split into `internal/lifecycle`). After PC: **subscribe** (683). Serialize
into the spine: **stalewatch** (consumes the RunRegistry port; 3 workloop refs), **handlerpause** (7 gate
call-sites in workloop). GATED on new seams: **tmuxsubstrate** (substrateWithAdapter escape hatch +
capability-interface family), **crewstart** (E2b, needs `crewSpawner` port — operator-deferred).
`runWorkLoop` (1,667 LOC) DECOMPOSES IN PLACE into named methods on a loop-local runtime struct
(`selectDispatchSource`, `spawnRun` [= the LIFT boundary], `tickPeriodicMaintenance` [absorbs diskcheck],
etc.) — highest-leverage single bead, but single-writer on workloop.go.

## Quality drain — shard by FILE (6 disjoint buckets, zero collisions)
Non-LIFT, non-hot buckets each = one parallel agent, fixing ALL of a file's linter classes at once
(errcheck + nilerr + noctx + contextcheck) so a follow-up edit doesn't re-trip the `--new-from-rev` gate:
- **A reconciliation** (reconciliation.go 22 + cadence 16) — Q1, in flight.
- **B event/lifecycle** (quiesce 10+nilerr, subscribe 10, notifystream 6+4 nilerr) — subscribe waits for PC.
- **C maintenance** (orphansweep 9+ctx, stalewatch 8, restartbackoff 5+ctx).
- **D config/registry** (projectconfig — moves with PC; branching 6+ctx; harnessregistry 7).
- **E state/gate** (statedisk 5, stategather 5, handlerpause_persist 9, eagerfill 6, scenariogate 6).
- **F cmd/harmonik** (keeper_enable_doctor 14, comms 9, eval_cmd 6+noctx, goalkeeper noctx) — after PC.
- **SERIAL bucket G (LIFT-owned, DO NOT open standalone):** workloop 83, reviewloop 24, dot_cascade 27,
  dot_gate 10, pasteinject 24, tmuxsubstrate 11 — fold errcheck cleanup into the RT/LIFT chunk that
  already rewrites the region, or sweep after that file's stream lands.
True bugs to fix regardless: `cmd/harmonik/{eval_cmd.go:486,goalkeeper_cmd.go:147}` real-IO noctx →
`exec.CommandContext`; the 11 daemon nilerr sites need per-site judgment (most are intentional best-effort
needing an explanatory guard, not urgent fixes). Follow-up bead **hk-lvf3w** = the modelpreference.go
live-os.Getenv → read-at-startup fix (deferred from PC, RT17-owned).
