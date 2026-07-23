# E5 — staffing & sequence (the one document that decides what runs next)

**Written:** 2026-07-22. **Reconciles:** `_plan.md`, `E5-dot-runloop.md` (RT13→RT19 outline),
`RT14-dispatchsegment-conversion.md`, `RT15-chunks.md`, `RT16-emitterport-conversion.md`,
`RT19b-stranded-run-path-helpers.md`, `E3-queue-wiring.md`, `E4-ssh.md`, `E4d-remote-orchestration.md`,
`E2b-OPERATOR-DECISION.md`, `PROGRESS.md`, `00-test-oracle-baseline.md`, plus the projectconfig
blocker resolution.

**Read this instead of re-deriving.** Where two plan files disagree, §7 records the reconciliation and
this document governs.

---

## 0. State of the tree right now

| Fact | Value |
|---|---|
| Nine P2 slices landed | `805a9d76` … `ffc5415a` (gitprobe, transport/tunnel, transport/codesync, queuewiring, crewrun, harness/{shared,codex,claude,pi}) |
| `internal/daemon` at baseline `34509e60` | **126 non-test files / 57,197 LOC** |
| `internal/daemon` after the 9 slices (`66e0041d`) | 105 / 50,804 (**−11.2%**) |
| `internal/daemon` **measured in the working tree now** (RT13 applied, uncommitted) | **103 / 49,064** |
| `workloop.go` now | **6,854** lines (was 8,378) |
| In flight, uncommitted, by another workflow | **RT13** (merge path → `internal/runmerge`) and **E4c** (`buildWorkerRegistry` → `internal/workers/bootwire.go`) |
| Being re-planned by a third workflow | **E4d** (operator already ruled: land E4d-0/1/2, park E4d-3) |
| Ports seam reality | `RunPorts` 7 reference sites; `RunEnv` + `SharedHandles` **dead code** (zero references). ~10% of the designed ports work exists. |

Everything below assumes **RT13 and E4c land and the tree builds green**. Nothing in the RT stream may
start before `go build ./internal/... ./cmd/...` is clean.

---

## 1. The critical path

Ordering is not "depends on" hand-waving. Each edge below names its mechanism.

```
RT13 ──┐
E4c ───┴──► RT19b ──► RT14 ──► PC ──► RT16 ──► RT15(C1..C7) ──► RT17 ──► RT18 ──► RT19 ──► RT20+ (THE LIFT)
                                                                                    │
                                                              RT19c (watchdog clocks) ┘  [LANDED f839121ff; see §3.6]
```

**Landed as of 2026-07-23: RT13, E4c, RT19b, RT14, RT15 (all seven chunks), and RT19c.** RT15 ran BEFORE PC
and RT16, which the diagram draws after them. That is correct, not a violation: §7.6 already records
that PC is a *value* ordering for RT15 and not a compile ordering, and RT16 → RT15 is not an edge in
the table below at all — RT16's edges are to RT17/RT18. Both `RunEnv` and `SharedHandles` stayed in
`package daemon` for all seven chunks, so `ProjectCfg ProjectConfig` and `*RunRegistry` type-checked
with zero work. **PC and RT16 are still worth landing before RT17/RT18**, and nothing about them
changed. Remaining in the chain: PC, RT16, RT17, RT18, RT19, RT20+.

| Edge | Mechanism (why it is strict, not preference) |
|---|---|
| RT13 → everything | **Same file, uncommitted.** RT13 has `workloop.go` and `runports.go` dirty and cuts lines 6397–8011. Every downstream slice edits `workloop.go`. Two uncommitted rewrites of one file is the `_plan.md` R3 failure. |
| E4c → everything | **Line-number invalidation.** E4c's target is `workloop.go:1227-1322`; it shifts every anchor in RT14/RT15/RT16/RT19b/PC. Also `workloop.go:1127` (a PC edit site) is 8 lines from E4c's `buildWorkerRegistry` at `:1135`. |
| RT19b → RT14 | **One produces a whole-file delete for the next.** RT19b cuts the HC-056 deadline family (`defaultAgentReadyTimeout`, `defaultRemoteAgentReadyTimeout`, `effectiveAgentReadyTimeout`, `ErrAgentReadyTimeout`) out of `agentready.go` into `internal/runlaunch`. What remains is exactly `waitAgentReady` + `agentEventSource` — which is precisely what RT14 deletes. Run in this order and `agentready.go` is a clean `git rm`; run it the other way and RT14 must do a 90-of-207-line partial deletion and RT19b must then re-derive 4 symbols out of a half-file. **This reverses `RT14 §6`'s instruction; see §7.1.** |
| RT14 → RT15 | **Same lines.** RT15-C1..C5 rewrite ~40 `deps.*` reads inside `beadRunOne` 3167–5397; RT14 restructures 272 contiguous lines at 4685–4956, inside that span. RT14 first also makes RT15 cheaper — `deps.agentReadyTimeout`/`remoteAgentReadyTimeout`/`clock`/`adapterRegistry`/`harnessRegistry` collapse from scattered imperative reads to one `cfg` construction. |
| PC → RT15 | **One produces the type the next consumes.** RT15-C1's `runEnv()` constructor populates `RunEnv.ProjectCfg`. Today that field is typed `daemon.ProjectConfig`, which cannot leave daemon. PC (the `internal/projectconfig` leaf move) retypes it to a daemon-free leaf type. E5 §3a states this must be decided *before* RT15 because RT15 constructs `RunEnv`. |
| RT16 → RT17/RT18 | **Same expressions.** RT16 binds `emit := rp.Emitter` / `deps.emitterPort()` at 5 function heads and converts 108 `deps.bus` sites. RT18's signature flip then rewrites 5 lines instead of 108. Doing RT18 first means paying the 108-site edit inside a signature change — i.e. losing RT16's zero-behaviour-risk property. |
| RT15 → RT18 | **Signature dependency.** RT18 drops `deps workLoopDeps` from `driveDotWorkflow`/`dispatchDotAgenticNode`/`dispatchDotGateNode`/`runReviewLoop`/`runBridge`. It cannot do that until `beadRunOne` already takes `env RunEnv` (RT15-C5) and until the ports carry what `deps` carried (RT16 + RT17). |
| RT17 → RT18 | **Port coverage.** RT18 deletes the copy-mutation idiom. Two of the six sites are `deps.launchSpecBuilder = routedLaunchSpecBuilder(...)`; deleting them requires `LaunchPort` to already carry the builder — that is RT17's job. RT17's own exit gate is `grep -c 'deps\.' {reviewloop,dot_cascade,dot_gate}.go` → `0 0 0` (today 137 / 88 / 41). |
| RT19 → RT20+ | **Test-surface blast radius.** 156 files reference `ExportedWorkLoopDeps`/`WorkLoopDepsParams`, 146 reference `stubEventCollector`, 88 reference `ExportedRunWorkLoop`. The lift moves the packages those shims point at. Splitting the shims *after* the lift means 173 test files broken simultaneously. |
| RT19b ⟂ RT15–RT19 | **NOT an edge.** All sixteen RT19b symbols take explicit parameters; none reads `deps`. E5 §4 step 19 files RT19b at the tail of the stream — that is wrong, and RT19b's own §0 proves it. RT19b is executable the moment RT13+E4c commit. |

**What is NOT on the critical path** (verified, and worth saying because two plan files imply otherwise):
E1a/E1b/E1c and E4a/E4b **have all landed**, so E5 §7.3's hard ordering block on `ensureCodexRefsTrailer` /
`ensurePiRefsTrailer` / `newDaemonHeartbeatEmitter` is **discharged**. E5 §2's "LaunchPort leaks E1b
private types" red flag is **retired** — `BuildSpec` now names `shared.LaunchCtx` / `shared.LaunchArtifacts`
(`runports.go:168`, commit `c3fff27d`).

---

## 2. The parallelism map

### 2.1 The honest headline

**E5's RT stream is inherently sequential. There is no safe intra-E5 parallelism.** Every slice from
RT13 to RT20 edits `internal/daemon/workloop.go` (331 commits/90d) and/or the four run-path files
(`reviewloop.go` 98, `dot_cascade.go` 104, `dot_gate.go` 21) and/or `export_test.go` (229). Two agents in
`workloop.go` at once is exactly the R3 merge-conflict warfare `_plan.md` §0 names, and
`E5-dot-runloop.md` §7.6 already declares these slices "strictly sequential single-writer — do not
parallelise across crews."

Do not staff two RT slices. Staff **one RT agent**, and fill the remaining capacity from Lanes B–E below.

### 2.2 Lane table

| Lane | Slices | Files touched | Safe to run beside |
|---|---|---|---|
| **A — RUN PATH (single writer, exclusive)** | RT13, E4c, RT19b, RT14, PC, RT16, RT15-C1..C7, RT17, RT18, RT19, E4d-0/1/2, RT20+ | `workloop.go`, `reviewloop.go`, `dot_cascade.go`, `dot_gate.go`, `runbridge.go`, `sub_workflow_runner.go`, `runports.go`, `agentready.go`, `export_test.go`, `.golangci.yml`, `Makefile`, `scripts/*-gate.sh` | **Nothing else in Lane A.** One agent, one slice, commit before the next starts. |
| **B — QUEUE WIRING** | E3b-PREP, then E3b-MOVE | `perqueuespendmeter_tigaf11.go`, `spendmeter_hkk3f8g.go`, `bootstate.go`, `daemon.go`, `testopts_test.go`, `cognition_loop_scenario_hkc7lxc_test.go`, **`export_test.go`** (8 shims), **`.golangci.yml`** | **Genuinely parallel with Lane A — but only while Lane A is on a slice that touches neither `export_test.go` nor `.golangci.yml`.** That is RT16 and RT15 (RT15's exit gate is a *zero diff* on `export_test.go`, and it adds no depguard block). It is NOT safe beside RT19b (touches both) or RT14 (deletes 22 `export_test.go` lines). |
| **C — COLD TEST MOVES (RT19 front half)** | `git mv internal/daemon/runshell_test.go` and `dispatchsegment_test.go` → `internal/runlooptest/`; scaffold the package | those 2 test files only | **Parallel with anything.** Both are already free of daemon internals (E5 §4 step 19). The rest of RT19 (relocating `stubEventCollector`, 146 consumers, and splitting `export_test.go`) is Lane A and must not be split off. |
| **D — NON-P2 (fully free)** | Everything in `PROGRESS.md` §4: `cmd/harmonik` (26,830 LOC, no coverage gate), `internal/{codexwire,keeper,lifecycle,workspace,eventbus}`, the small dense packages, the 8 `nilerr` bugs outside daemon, the 1,736 dead `//nolint:gosec` directives outside daemon, the `core`→yaml/expr layering breach | none of Lane A's | **Parallel with everything.** This is the real second crew. **Exception:** if PC's write-lock escalation (§3.1) is granted, `cmd/harmonik/**` becomes Lane A for the duration of that one slice. |
| **E — RT19c (watchdog clock-port)** | the 7 raw wall-clock sites RT14 descoped (`pasteInjectQuitOnGateFile` in `dot_gate.go`, `waitsocketgrace.go`, `postreadyhang.go`) plus the 22 siblings in `pasteinject.go`, and two `time.Since` reads in `pasteInjectQuitOnReviewFile` the inventory grep missed — 32 in all | `pasteinject.go`, `dot_gate.go`, `waitsocketgrace.go`, `postreadyhang.go` | **LANDED** `f839121ff`. Was NOT safe beside RT14/RT16/RT17 (all edit `dot_gate.go`); lane closed — see §3.6. |

### 2.3 What looks parallel and is not

- **E4d-0/1/2.** Marked "three cheap daemon-internal slices". All three edit `beadRunOne` inside
  `workloop.go` (the 7 `ReopenBead` calls, the `remoteBeadCtx` hoist, the SSH runner literal). **Lane A.**
  Sequence them adjacent to RT16 — E4d-0 drains `LedgerPort` debt RT16/RT17 would otherwise pay.
- **Splitting RT16 by file** (workloop / reviewloop / dot_cascade / dot_gate as four slices). Rejected by
  `RT16 §6` with a good argument: the edit is ~90 min, the *gate* is ~45 min and every sub-slice pays it
  in full (≈3 extra hours of exposure for an identical end state), and the freeze-gate script asserts a
  per-file budget of 0 so it cannot be armed until all files are converted.
- **Splitting RT15 across agents.** The 7 chunks are sequential on one file; C6/C7 are a detachable
  *tail*, not a parallel branch.
- **The quality audit's rec 1/2/5.** rec 2 (`.golangci.yml` allow-list fix) and rec 5 (`beadRunOne`'s
  `//nolint` at `workloop.go:3175`) are Lane A. rec 1 is Lane D **except** inside `internal/daemon`.

---

## 3. Gate / decision points — operator input required

### 3.1 PC (projectconfig lift): a WRITE-LOCK escalation, not an architecture one — **OPEN, blocking**

The slice moves `internal/daemon/projectconfig.go` (2,139 LOC, only harmonik import is `internal/core`)
wholesale to `internal/projectconfig`, which permanently clears E5 §3a's `RunEnv.ProjectCfg` blocker and
clears three more projectconfig-typed run-path couplings (`PiHarnessConfig`, `PiProfileConfig`,
`SandboxConfig`) for free. **No new seam. Pure move under `_plan.md` §5.1.**

It must edit ~24 files under `cmd/harmonik/**`, which is outside the P2 stream's declared fence
(`internal/daemon/**` + `.golangci.yml`) and where another agent currently holds dirty files
(`main.go`, `keeper_enable_doctor_cmd.go`, `supervise/start.go`).

**The question:** extend the P2 stream's exclusive hold to `cmd/harmonik/**` for this one slice (other
agent commits and stands down first) — or re-scope it daemon-only by keeping `daemon.ProjectConfig` et al.
alive as type aliases?

**Recommendation: extend the hold.** The alias variant leaves a second name for the same type alive
indefinitely and makes the freeze gate unenforceable — the exact accretion freeze-then-strangle exists
to prevent.

### 3.2 New seams — E2b's waiver was DENIED and that rule is in force for every unit

`_plan.md` §1 / §5.1: an extraction that invents a seam is a review-gate rejection. Status per slice:

| Slice | New seam? | Note |
|---|---|---|
| RT13, RT14, RT16, RT19b, RT15, PC, E3b, E4d-0/1/2 | **No** | RT14 rides `dispatchSegment` (already the mechanism at 3 of 5 launch sites). RT16 rides `EmitterPort`, a *type alias* — identity wiring. RT19b creates leaf packages (`internal/runlaunch`) but injects nothing. PC is a `git mv`. |
| **RT17** | **Widening, in charter — but this is the tripwire line** | `runports.go:156-162` already documents `LaunchPort` as covering launch-spec build, spawn, registries, hook-store, delivery, ready timeouts and sandbox. Widening to that documented scope is in charter. **Adding an eighth port is not.** If an implementer reports needing one — ESCALATE, do not decide. |
| **RT18** | **ONE OPEN QUESTION — decide before RT18 starts** | `runBridge.deps.tidGen` has 10 reads in `beadRunOne`. E5 §4 offers two resolutions: a **`RunPorts.TIDGen` port (= an eighth port, = escalation)** or a **pre-minted `TransitionID` on `RunEnv` (= no new seam)**. RT15-chunks explicitly refuses to pre-empt this. **Default to the `RunEnv` field**; escalate only if it provably breaks per-transition ID semantics. |
| **RT20+ (the lift)** | **Two decisions** | (a) `SharedHandles.RunRegistry` must stop being `*RunRegistry` (a daemon-resident concrete type, or the lift creates `runloop → daemon`) and become an interface of exactly `SetAgentType`/`SetMachine`/`SetResolvedProvider`. Consumer-defined narrowing interface — arguably a seam; get it blessed before the lift, not during. (b) **One package or three?** E5 §7.13 shows the three-way split manufactures 5 new `dot_cascade → reviewloop` cross-package exports for zero gain. **Recommend ONE `internal/runloop`.** |

### 3.3 E2b — settled, do not reopen

Deferred 2026-07-22, option A. `crewstart.go` stays in daemon as sanctioned residue. Revisit only after
E5's substrate/ports work matures, when the required seam may already exist.

### 3.4 The runtime proof (`_plan.md` §5.4) is standing debt

The daemon is DOWN, so no slice can drive a real bead end-to-end. E5 §7.11 requires **every slice's close
comment to explicitly flag the deferred §5.4 proof rather than omit it**. The mechanical substitutes are
`go test ./internal/runexectest/... -count=10` (RT11 fault matrix + relaunch oracle) and
`./internal/replay/... -count=1` (RT10 run-keyed divergence checkers) — both mandatory, both already
outside `internal/daemon`. **Needs a bead owning the real proof for when the daemon comes back.**

### 3.5 The disk watermark invalidates the differential oracle — **OPEN, environmental**

`df`: 228 Gi volume, 96% full, ~7.5 Gi free, against the daemon's 10 GiB dispatch watermark. Below it the
daemon logs `disk-check: … dispatch paused` and every dispatch/throughput/timing test fails for reasons
unrelated to any code change (147 such messages contaminated the Phase-5 differential run). ~10 G sits in
other sessions' scratchpad worktrees plus `.claude/worktrees/agent-*` abandoned 4–5 days ago **inside the
repo** (which is also why `make fmt-check` fails). **Nobody will delete another session's data without
your say-so.** Until it is resolved, every timing failure in a gate run is *suspect, not real*.

### 3.6 RT19c — RESOLVED, landed in the RT stream

RT14 §0 correctly descoped 7 of the 8 raw wall-clock sites E5 §4 step 16 claimed, because they live in
`pasteInjectQuitOnGateFile` and the Working-phase completion wait — code `dispatchsegment.go`'s header
explicitly places *outside* the RT8 segment boundary, with 22 further sibling sites in `pasteinject.go`.
Converting one copy of three watchdogs closes nothing and diverges them. The open question was whether
RT19c belonged in the RT stream's definition of done or post-lift.

**Answered by landing it: `f839121ff` (2026-07-23), inside the stream.** All four files now take an
injected `substrate.ClockPort` (32 sites, including two `time.Since` reads the inventory grep did not
cover), the four files are pinned in `scripts/readywait-freeze-gate.sh` check (2), and the three
minutes-to-hours timeout branches have FakeClock tests that run in microseconds of wall time. Row 7 of
§4 is therefore closed rather than re-filed.

---

## 4. Definition of done for the RT stream

RT20+ (the lift) may not be attempted until **all** of the following are measurably true. Each row has a
command that returns a number.

| # | Condition | Today | Required | Owner |
|---|---|---|---|---|
| 1 | `deps.` reads in the three mode files: `grep -c 'deps\.' internal/daemon/{reviewloop,dot_cascade,dot_gate}.go` | **137 / 88 / 41** | **0 / 0 / 0** | RT16 → RT17 (RT17's own exit gate) |
| 2 | Run-path `deps.bus` bypass: same grep for `deps\.bus` over `reviewloop`, `dot_cascade`, `dot_gate`, `runbridge`, `sub_workflow_runner` | 51 / 23 / 7 / 3 / 2 | **0 across all five**; `workloop.go` stays at exactly **10** (the outer `runWorkLoop` sites, which correctly never move) | RT16 (108 of 118 sites) |
| 3 | **The by-value copy-mutation idiom is gone.** `grep -n 'deps\.clock = \|deps\.launchSpecBuilder = ' internal/daemon/*.go` | **6 sites** — line numbers shift every slice, grep for them; RT15 confirmed all six are still present and untouched (E5 §3a said 4; RT15-chunks re-measured 6) | **0** | the 2 builder sites = RT17; the 4 clock sites = RT18. **Any port work that does not delete these is cosmetic** (E5 §7.2). |
| 4 | `RunEnv` / `SharedHandles` are live | **DONE (RT15, 2026-07-22).** `runEnv()` / `sharedHandles()` constructors exist; `RunEnv` is built at the dispatch site and passed as a parameter, `SharedHandles` is built inside `beadRunOne` as a local named `handles` (`shared` is the harness/shared package — do not use that name) | remaining: `SharedHandles` becomes a PARAMETER when `deps` is dropped | RT15-C1/C5 (env) and RT15-C6/C7 (shared, as a local) — **DONE**; the `shared` parameter is RT18 |
| 5 | `RunEnv.ProjectCfg` is a daemon-free type | `daemon.ProjectConfig` (`projectconfig.go:1235`, a 2,139-LOC daemon file) | `projectconfig.ProjectConfig` (leaf) **or** a narrowed run-scoped view | PC — **needs §3.1 first** |
| 6 | `SharedHandles.RunRegistry` is not a daemon concrete type | `*RunRegistry` | 3-method interface | RT20 precondition (§3.2) |
| 7 | Raw wall-clock on the run path | **DONE.** RT14 closed the one in-segment site; RT19c (`f839121ff`) closed the other 7 plus the 22 `pasteinject.go` siblings and two `time.Since` reads. All ten pinned run-path files are wall-clock **clean** and `scripts/readywait-freeze-gate.sh` check (2) now ratchets them, over a regex widened to include `Since`/`Until`/`AfterFunc` | **0** | RT14 + RT19c — **closed**, not re-filed |
| 8 | **Export drain: 201 symbols need exporting across the whole unit** | RT13 drains **21**; RT19b drains **15** → **36 / 201 (18%)** | the remaining ~165 are the lift's, and each must appear in RT19's or RT20's enumerated list before the lift starts. A symbol with no named slice is a build break waiting at lift time. | RT19 (enumerate), RT20 (apply) |
| 9 | **Back-edge drain: ~20 back-edges survive RT19** (E5 §7.15) | RT19b drains 11 of the "third band" + the 4 HC-056 deadline symbols. The 3 E1-owned harness calls are **already discharged** (E1a/b/c landed). | remaining and each with an owner: the 4 `sessioncontext_chb023.go` calls (RT18), `standardgraph`/`modelpreference`/`moderesolve`/`harnessresolve`/`sandboxprofile` (RT17), `ProjectConfig` (PC), `*RunRegistry` (RT20), the 5 `dot_cascade → reviewloop` symbols (**dissolved by landing one package, §3.2b**) | — |
| 10 | `export_test.go` split | 3,628 LOC; 183 `Exported*` funcs + 59 exported types; the only route 173 external test files have into the run loop | split, with a target set from a **measured** post-split number. The recon's "under 1,500 LOC" gate is retired as mis-calibrated. | RT19 |
| 11 | `internal/runloop` depguard block drafted and reviewed **before** the lift | not written | allow `$gostd, core, handler, handlercontract, workspace, substrate, runexec, runmerge, mergeq, gitprobe, queue, workers, policy, workflow, workflow/dot, brcli, lifecycle/tmux, self`; deny `internal/daemon`. Missing `policy`/`workflow`/`workflow/dot`/`brcli` fails lint on the lift's first commit. | RT20 prep |
| 12 | No other crew holds an open branch touching `workloop.go` / `reviewloop.go` / `dot_cascade.go` / `dot_gate.go` | unknown | verified. Once those files move packages, **every** open branch touching them conflicts, at 98–331 commits/90d each. This is the one irreversible step in P2. | operator |
| 13 | The differential oracle is trustworthy | `df` shows ~7.5 GiB free vs a 10 GiB watermark | `df -h` > 10 GiB free; before/after runs at **identical package scope** on a **clean detached worktree**, serialized, never the shared tree | §3.5 |

---

## 5. Recommended immediate next action

## → **RT19b** (`/Users/gb/github/harmonik/plans/2026-07-21-p2-extraction/RT19b-stranded-run-path-helpers.md`), the moment RT13 and E4c are committed and green.

Why RT19b and not RT16 or RT14:

1. **It is the only ready slice that actually shrinks the daemon.** −357 non-test LOC and 15 back-edges
   gone. RT16 moves zero lines by design; RT14 nets ~−140 and is MEDIUM-HIGH risk.
2. **It has no dependencies beyond RT13+E4c and needs no operator decision.** Verified property: not one
   of the sixteen symbols reads `deps`. E5 §4 step 19 files it at the tail of the stream; that is wrong.
3. **It makes RT14 strictly cheaper and safer.** After RT19b, `agentready.go` is a whole-file `git rm`
   instead of RT14 performing a 90-of-207-line partial deletion of a file whose remaining four symbols
   have 4 production call sites. RT14 §0 records that getting that deletion wrong would have been
   unrecoverable.
4. **It banks progress even if the window closes.** Three independent commits, cheapest first:
   RT19b-1 `clockAfter → substrate.After` (5 sites, cold files); RT19b-2 `artifactAgentType` +
   `beadAlreadySubsumedInMain` → `internal/harness/shared` (28 sites); RT19b-3 the launch effects →
   new leaf `internal/runlaunch` (55 sites + package + fence). Commits 1 and 2 stand alone.
5. **It proves a leaf is required, not optional** — `bootstate.go:275/:278` (a file that never moves)
   calls two of the moved symbols, so they cannot land inside `internal/runloop`. Doing this now avoids
   discovering it at lift time.

**In parallel, immediately:** put §3.1 (the PC write-lock escalation) in front of the operator so PC can
run the moment RT14 finishes; start Lane D with a second crew; and resolve §3.5 (disk) before any slice
tries to run its differential gate.

**Window discipline for RT19b:** all three commits in one sitting, target < 4 hours, hard stop at one
working day. At 331 commits/90d, `workloop.go` changes about every 6.5 hours of project wall time and
RT19b has 42 substitution points in it.

---

## 6. Estimated shrink and running total

Baseline `34509e60`: **126 non-test files / 57,197 LOC**. Measured now (RT13 applied in-tree): **103 / 49,064**.

| Slice | Files | Non-test LOC | Running total | Basis |
|---|---:|---:|---|---|
| *(9 landed slices)* | −21 | −6,393 | 105 / 50,804 | measured, `PROGRESS.md` |
| **RT13** *(in flight)* | −2 | **−1,740** | **103 / 49,064** | **measured in the working tree** |
| **E4c** *(in flight)* | 0 | −96 | 103 / 48,968 | `E4-ssh.md` (`buildWorkerRegistry` + `…WithRunner` + `bootHealthRunner`, `workloop.go:1227-1322`) |
| **RT19b** | 0 | **−357** | 103 / 48,611 | RT19b §1a (285 from `workloop.go`, 72 from `agentready.go`) |
| **RT14** | −1 | ~−140 | 102 / ~48,471 | RT14 §1: `agentready.go` −90 (whole file, if RT19b ran first), `workloopeventsource.go` −46; `workloop.go`/`dot_gate.go` restructure ≈ neutral. `export_test.go` −22 is test LOC. |
| **PC (projectconfig)** | **−1** | **−2,139** | **101 / ~46,332** | blocker resolution; **also −12 test files / −2,216 test LOC**, and `cmd/harmonik/supervise` drops its daemon import entirely |
| **RT16** | 0 | **0** | 101 / ~46,332 | RT16 §5: "this slice moves nothing — say so explicitly rather than reporting a shrink of 0 as if it were a miss." Its payoff is turning RT18 from a 108-line change into an 8-line one. |
| **RT15 (C1–C7)** | 0 | ~+60 | 101 / ~46,392 | RT15-chunks: 193 lines changed, mostly substitution; the two constructors in `runports.go` are net-new |
| **RT17 / RT18** | 0 | ≈0 | 101 / ~46,392 | port widening + signature flips; no plan supports a number |
| **E4d-0/1/2** | 0 | **0** | 101 / ~46,392 | E4d §"Be honest about what they are not" — they move zero lines out of daemon; they buy RT16/RT15 debt relief |
| **E3b** *(Lane B, parallel)* | −2 | −637 | 99 / ~45,755 | `E3-queue-wiring.md` §E3b |
| **Subtotal vs baseline** | **−27 files** | **−11,442 LOC** | **99 / ~45,755 (−20.0%)** | |
| **RT19** | 0 | 0 | 99 / ~45,755 | test-surface only (moves test LOC, not production) |
| **RT20+ THE LIFT** *(arithmetic, not a commitment)* | ~−14 | ~−10,900 | ~85 / ~34,900 (**−39%** vs baseline) | E5 §1b: `dot_cascade` 2,814 + `reviewloop` 2,182 + `beadRunOne` 2,230 + `dot_gate` 795 + `sub_workflow_runner` 463 + `runshell` 437 + `scenariogate` 428 + `runbridge` 375 + `dispatchsegment` 295 + `codesync_rs_b8` 258 + `workloopeventsource` 230 + `waitsocketgrace` 170 + `reviewerharness` 135 + `codexnowork` 105 |

`workloop.go` trajectory: 8,378 → **6,854** (RT13, measured) → ~6,760 (E4c) → ~6,475 (RT19b) → ~6,400
(RT14) → ~2,500 post-lift (the outer `runWorkLoop` + the `workLoopDeps` composition root, which
correctly stay in daemon forever).

---

## 7. Reconciliations — where the plan files disagreed

1. **RT19b vs RT14 ordering.** RT19b §6 says "land RT19b first, then RT14"; RT14 §6 says "RT19b must be
   re-derived after RT14 lands." **RT19b wins.** RT14's claim rests on RT14 adding 2 call sites to 4 of
   the moved symbols — a trivial re-derive of *new code RT14 is writing anyway*. RT19b's claim rests on
   turning a hazardous partial-file deletion into a whole-file one; RT14's own §0 documents that the
   partial deletion is where E5's recipe was dangerously wrong. Order: **RT19b → RT14.**
2. **Who owns the copy-mutation sites.** RT15-chunks §"Do not touch" says "RT16 owns them"; RT16 §0 says
   "they are RT18's." **Neither RT15 nor RT16 touches them.** Ownership: the 2 `launchSpecBuilder` sites
   → RT17 (they need `LaunchPort` widened first); the 4 `deps.clock` sites → RT18. Count is **6**, not
   E5 §3a's 4.
3. **Copy-mutation site count and line numbers.** E5 §3a: 4 sites at `dot_cascade.go:215/:1254`,
   `reviewloop.go:222`, `workloop.go:3967-3979`. Post-RT13 measured: **6** at `workloop.go:3179/:3977/:3987`,
   `dot_cascade.go:219/:1259`, `reviewloop.go:229`.
4. **`deps.bus` count.** E5 §3a says 115. RT16 measured **113 code + 2 comments** in the four mode files,
   **plus 5 more on movers** E5 never counted (`runbridge.go:134/:279/:322`, `sub_workflow_runner.go:290/:317`)
   → **118 total, 108 converted, 10 deliberately left in the outer loop.**
5. **RT15's test blast radius.** E5 §4 step 19 says 156 files. RT15-chunks: those 156 drive
   `ExportedRunWorkLoop(ctx, deps)`, never `beadRunOne`. Because `env` is a *derived local*, the real
   figure is **7 call sites**, and `export_test.go` must show a **zero diff** for all of RT15 — an exit
   gate, not a preference.
6. **RT15's two "blockers".** E5 §4 makes narrowing `RunEnv.ProjectCfg` and `SharedHandles.RunRegistry`
   preconditions of RT15. RT15-chunks is right that they are not: both bundles stay in `package daemon`
   for all seven chunks. **They block THE LIFT, not RT15.** PC is still worth landing before RT15 — it is
   the single largest remaining shrink and it removes the question permanently — but it is a *value*
   ordering, not a compile ordering.
7. **RT14's raw-clock scope.** E5 §4 step 16 says delete 8 raw-time sites. Only **1** is in RT14's blast
   radius (`dot_gate.go:486`). The other 7 belong to `pasteInjectQuitOnGateFile` and the Working-phase
   completion wait, with 22 further siblings in `pasteinject.go` → re-filed as **RT19c**, which has since
   **landed** (`f839121ff`, §3.6).
8. **`artifactAgentType` is no longer blocked on E1b.** E5 §3a says it "cannot be resolved until E1b
   re-homes `claudeRunArtifacts`". E1b-prep (`c3fff27d`) already did: the signature is now
   `func artifactAgentType(a shared.LaunchArtifacts) core.AgentType` over a daemon-free leaf type. It is
   the *easiest* symbol in RT19b, not the blocked one.
9. **All E5 line numbers predating RT13 are stale.** RT13 cut `workloop.go` from 8,378 to 6,854, and E4c
   will shift it again. **Use the `grep` anchors in each RT plan, never the line numbers in `E5-dot-runloop.md`.**

---

## 8. Standing rules for whoever runs Lane A

- **One writer.** Commit each slice before starting the next. Never leave `workloop.go` dirty overnight.
- **Pure move.** `git mv` + package rename + import fixups. No emission-string changes, no signature
  "improvements", no gofumpt reflow of untouched code. Any logic delta is called out in the commit body
  or the slice is rejected at review.
- **Stage by explicit pathspec.** `git add -A`, `git commit -a`, `git reset --hard`, `git clean` are
  hard-forbidden (`PROGRESS.md` §2 — this stream nearly swept 49 of another agent's files at 08:00).
- **Never `cd` into a worktree.** Operate from `/Users/gb/github/harmonik` with absolute paths.
- **Gate scope:** `go build ./internal/... ./cmd/...` — **never** `go build ./...` (red at repo root on
  unvendored zmq4/mangos files under `plans/2026-07-15-agent-substrate-v2/`). Add
  `go vet -tags=scenario ./internal/daemon/` — 28 scenario-tagged files, 12 of which drive the run-loop
  exports, never compile under plain `go test`.
- **Differential oracle, not "green":** `internal/daemon` is already red at HEAD (1 hard failure + 5
  load-sensitive flakes + 1 isolation-sensitive one; `specaudit` has 7). Gate = **no NEW failure names**,
  identical package scope, clean detached worktree, serialized. See `00-test-oracle-baseline.md`.
- **Every slice ships its freeze gate in the same commit as its depguard block.** The harness unit did
  not, and that hole is still open (`PROGRESS.md` §5 item 7).
- **Every close comment states:** files + non-test LOC that left `internal/daemon`; that the `_plan.md`
  §5.4 runtime proof is DEFERRED because the daemon is down; and which back-edges the slice did *not*
  close, with the slice that owns them.
