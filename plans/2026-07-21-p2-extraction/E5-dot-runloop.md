# Unit E5 — DOT run-loop → `internal/runloop`, riding the RT ports seam

**Status:** NEEDS PREPARATORY SLICE — and the preparatory work is a **stream (RT13→RT19), not a slice**.
Exactly one of those steps (**RT13, the merge-path carve-out to `internal/runloop/../runmerge`**) is
landable today, on its own, with a complete recipe in §4. Everything after RT13 is gated on units
E1a/E1b/E1c and E4 landing first, for reasons proved in §3a. **The lift itself (`git mv` of the run
machine into `internal/runloop`) is explicitly OUT of this document's executable scope** — it is
described in §4 step 20 as a scoping note so nobody mistakes it for a next step.

**Depends on:** E1a (codex harness) + E1b (claude harness) + E1c (pi harness) + E4 (ssh/transport)
must ALL land before any lift. RT13 alone depends on **nothing** and can be executed immediately.

**Size:** ~13,950 LOC of production code across 18 non-test files, against ~59,300 LOC of test mass
across 173 daemon test files. Broken down:

| Scope | Non-test | Test |
|---|---|---|
| RT13 (executable now) | ~1,770 LOC across 4 files touched, 2 files deleted from daemon | ~1,750 LOC across 6 test files moved |
| Full E5 unit (eventual) | ~13,950 LOC across 18 files | ~59,300 LOC across 173 files |

**Risk:** **HIGH.** `workloop.go` took 318 commits in 90 days and `export_test.go` 221 — every step in
this stream edits one or both, so these slices are strictly sequential single-writer work; and the
`workLoopDeps` god-struct (82 fields) still travels **by value, and is mutated in the callee's copy**
at 4 run-path entry points, which means the "ports seam" the plan says E5 rides is roughly 10% built.

---

## 0. Which input won, where they disagreed

I ran both the recon and the adversarial challenge against the tree. **The challenge wins on every
contested point** — each of its corrections reproduced under direct verification, and several of the
recon's numbers were off by 5–8x. Specifics, with the command that settled each:

| Disputed claim | Recon | Challenge | Verified | Taken |
|---|---|---|---|---|
| `scenariogate.go` uses `deps.mergeMu` | "confirm the field exists; may be dead code" | zero `deps` refs; `mergeMu` only in prose at :85/:87 | `grep -n 'deps\.' internal/daemon/scenariogate.go` → 1 hit, a comment | **Challenge.** scenariogate.go is the cleanest mover in the unit. |
| `export_test.go` shim count | ~60 | 183 funcs + 59 exported types = 242 | `grep -cE '^func Exported[A-Za-z]'` → 183; `grep -cE '^type [A-Z]'` → 59 | **Challenge.** |
| Files on `ExportedWorkLoopDeps`/`WorkLoopDepsParams` | 19 | 155 | 156 | **Challenge** (156). |
| Files on `stubEventCollector` | 20+ | 146 | 146 | **Challenge.** |
| Files on `ExportedRunWorkLoop` | "a small residue" | 87 | 88 | **Challenge.** |
| Move `mergetomain_hkftyvo_test.go` into runmerge | yes (RT13 step i) | **cannot** — it uses `daemon.ExportedRunWorkLoop`, `daemon.ExportedWorkLoopDeps`, `daemon.WorkLoopDepsParams`, `daemon.ExportedProductionWorktreeFactory` | confirmed, all four | **Challenge.** Fixture is duplicated, never moved. See §4 step 9. |
| Fixture consumers | 8 | 20 | 20 | **Challenge.** |
| `dot_gate.go` is the only wall-clock file left | yes | no — `waitsocketgrace.go` and `postreadyhang.go` too | `dot_gate.go` 6, `waitsocketgrace.go` 1, `postreadyhang.go` 1; `dot_cascade/reviewloop/dispatchsegment/runbridge/runshell` all 0 | **Challenge.** |
| RT13 depguard allow-list needs `workspace` | omitted; listed `mergeq` | needs `workspace`, does not need `mergeq` | region uses `workspace.TaskBranchName` (:6547), `workspace.WorktreePath`+`NoWorktreeRootOverride` (:6556); `mergeq` appears only in comments at :6458/:6460/:6863 | **Challenge.** |
| Verification gate `go build ./...` | prescribed | red at repo root regardless of E5 | root build fails on `plans/2026-07-15-agent-substrate-v2/investigate/10-zeromq-experiments/*.go` (zmq4/mangos unvendored); `go build ./internal/... ./cmd/...` green | **Challenge.** |
| `sessioncontext_chb023.go` edge direction | inbound (it reaches into `reviewLoopState`) | outbound (reviewloop calls into it) | `reviewLoopState` in that file is a doc comment at :26; `reviewloop.go` calls `newSessionIDInterceptor`, `persistClaudeSessionID`, `emitClaudeSessionIDPersisted`, `sendVersionSelectedACK` | **Challenge.** |
| `gitRevParse` merge-region call count | 6 | 8 (+4 in branching.go) | region hits at :6707 :6713 :6754 :6757 :6938 :6961 :7185 :7720 = 8; branching.go call sites :332 :338 :533 :536 = 4 | **Challenge.** |
| `emitOutcomeEmitted` destination | `runloop.EmitOutcomeEmitted` | it sits at :7450, **inside** the RT13 cut, and `runbridge.go` calls it | confirmed both | **Challenge.** |
| E1a→E5 breakage | generic "E1 must land first" | concrete: `ensureCodexRefsTrailer` (an E1a file's symbol) is called from `workloop.go:5135` and `dot_cascade.go` | `grep` confirms `codexcommit.go` + `workloop.go` + `dot_cascade.go` | **Challenge.** |

**Where the recon held up and I kept it** (each re-verified): `workLoopDeps` really is 82 fields at
`workloop.go:188-943`; `RunEnv` and `SharedHandles` really are dead code — the ONLY references in all
of `internal/` are their own declarations at `runports.go:292` and `:322`; the merge region really is
nearly free-standing (exactly 8 `deps.` references, all inside `emitBeadClosedAndMaybeEpic` /
`maybeEmitEpicCompleted`); `waitAgentReady` really has exactly 2 production callers
(`workloop.go:4897`, `dot_gate.go:474`) plus one `export_test.go:1569` shim; `runregistry.go` really
must stay; the `mergeq:` depguard block really exists so "append after it" is a valid instruction.

**Two couplings NEITHER input found, both verified, both blocking:**

1. **`internal/specaudit/wminv003_task_branch_append_only_test.go` walks all of `internal/` for
   history-rewriting git exec patterns** (`"git" … "rebase"`, `"--amend"`, `"push" … "--force"`) and
   allowlists them **by file path**. Its map contains the literal keys `"internal/daemon/workloop.go"`
   and `"internal/daemon/reviewtrailers_hkdyim.go"`. Moving those `git rebase` / `git commit --amend`
   calls to `internal/runmerge/*.go` **fails `TestWMINV003PartBCorpusCheck` immediately** until the
   allowlist gains the new paths. Handled in §4 step 11.
2. **`escapedetect_hkooexj_test.go` (499 LOC) CAN move** — the recon marked it "CANNOT MOVE — names
   `beadRunOne` directly", but both `beadRunOne` mentions (:377, :435) are comments; its only real
   daemon surface is `daemon.ExportedCheckMainWorkingTreeDirty` and
   `daemon.ExportedSnapshotUntrackedFiles`, which RT13 exports on `runmerge`. Handled in §4 step 9.

**Also corrected: the plan's own baseline numbers.** `_plan.md` §0 says `internal/daemon` is "631
non-test .go files, 56,583 non-test LOC". Measured 2026-07-22: **134 non-test files, 58,971 non-test
LOC, 517 test files.** (The 631 figure counted test files.) The success metric in §6 is stated against
the measured number, not the plan's.

---

## 1. What moves

### 1a. Moves in RT13 (executable today)

| File / region | LOC | Destination | Notes |
|---|---|---|---|
| `internal/daemon/workloop.go` lines **6397–8011** (`isRetryableMergeReason` … `isHarmonikChurn`) | 1,615 | `internal/runmerge/merge.go` (+ split as noted) | **Except** `emitBeadClosedAndMaybeEpic` (:7761) and `maybeEmitEpicCompleted` (:7772-7850), 90 lines, which take `deps workLoopDeps` and STAY. Net ~1,525 lines leave. |
| `internal/daemon/workloop.go` `removeWorktree` (:5767–5790) | ~25 | `internal/runmerge/worktree.go` | Called at `:5705` (beadRunOne) and `:6567` (merge region). Imports `workspace.PruneWorktreeTrust` — that is why it goes to `runmerge`, **not** `gitprobe` (gitprobe's allow-list has no `workspace`, and widening it would weaken the E1a leaf). |
| `internal/daemon/stripruncontext_hk4je.go` | 105 | `internal/runmerge/stripruncontext.go` | Single caller is inside the merge block. Whole file leaves daemon. |
| `internal/daemon/reviewtrailers_hkdyim.go` | 96 | `internal/runmerge/reviewtrailers.go` | `appendReviewTrailersToHEAD`, the `spineArgs.amendTrailers` callee. Whole file leaves daemon. |
| `internal/daemon/branching.go` `gitRevParse` (:362–380) | ~20 | `internal/gitprobe/gitprobe.go` as `gitprobe.RevParse` | Pure read-only `git rev-parse`, no daemon state — exactly gitprobe's charter. 12 call sites to rewrite (8 in the merge region, 4 in `branching.go` at :332 :338 :533 :536). |

**Test files that move in RT13** (all verified to reference only symbols RT13 exports):

| Test file | LOC | Package | Why it can move |
|---|---|---|---|
| `mergetomain_residualdelta_hkrljho_test.go` | 298 | `daemon_test` | only `ExportedCommitResidualDelta`, `ExportedDiscardDirtyChurn` |
| `mergetomain_dirtyledger_hk3yz2d_test.go` | 250 | `daemon_test` | only `ExportedDiscardDirtyChurn` |
| `mergetomain_integrationartifacts_hkg9zz_test.go` | 212 | `daemon_test` | only `ExportedCleanUntrackedFiles` |
| `mergetomain_hksfy7f_test.go` | 220 | `daemon_test` | only `ExportedMergeRunBranchToMain` |
| `escapedetect_hkooexj_test.go` | 499 | `daemon_test` | only `ExportedCheckMainWorkingTreeDirty`, `ExportedSnapshotUntrackedFiles` (the `beadRunOne` mentions are comments) |
| `reviewtrailers_hkdyim_test.go` | 172 | `daemon` (internal) | moves with its file; becomes `package runmerge` |
| `mergepath_commitsubject_hkr1v2n_test.go` | 99 | `daemon_test` | pure commit-subject string-shape assertions; zero `daemon.` references |

### 1b. Moves in the eventual lift (RT20+, NOT executable here)

| File | LOC | Destination | Blocked by |
|---|---|---|---|
| `internal/daemon/dot_cascade.go` | 2,814 | `internal/runloop/dotrun` (**not** `.../dot` — name collision, see below) | takes `deps workLoopDeps` **by value and mutates the copy** at :215 |
| `internal/daemon/reviewloop.go` | 2,182 | `internal/runloop/reviewloop` | same by-value `deps`; 137 `deps.` reads |
| `internal/daemon/dot_gate.go` | 795 | `internal/runloop/dotrun` | 41 `deps.` reads; 6 raw-time sites; open-codes `waitAgentReady` at :474 |
| `internal/daemon/workloop.go` lines 3167–5397 (`beadRunOne`) | 2,230 | `internal/runloop/run.go` | needs RT15's `RunEnv`/`RunPorts`/`SharedHandles` re-signature |
| `internal/daemon/sub_workflow_runner.go` | 463 | `internal/runloop/dotrun` | 3 real `r.deps.` reads (:174 projectDir, :290 bus, :317 handlerEnv) |
| `internal/daemon/runshell.go` | 437 | `internal/runloop` | **already clean** — 0 `deps.` refs, imports only core/runexec/substrate |
| `internal/daemon/scenariogate.go` | 428 | `internal/runloop` | **already clean** — 0 `deps.` refs; imports stdlib + `gitprobe` + `lifecycle/tmux` only |
| `internal/daemon/runbridge.go` | 375 | `internal/runloop` | struct field `deps workLoopDeps` at :31; imports `internal/brcli` (must be in the allow-list) |
| `internal/daemon/runports.go` | 342 | **SPLIT**: the port *interfaces* + `RunPorts`/`RunEnv`/`SharedHandles` → `internal/runloop/ports.go`; the `daemonLedger`/`daemonMerge`/`daemonGate`/`daemonBudget`/`daemonWorktree`/`daemonLaunch` adapters **stay** (they close over `*workLoopDeps`) | see §3a `ProjectConfig` / `*RunRegistry` rows |
| `internal/daemon/dispatchsegment.go` | 295 | `internal/runloop` | **already clean** — 0 `deps.` refs |
| `internal/daemon/codesync_rs_b8.go` | 258 | `internal/runloop` **or** E4/transport | 0 `deps.` refs; DD1 pre-merge code-sync driven by the runbridge mergeHook |
| `internal/daemon/workloopeventsource.go` | 230 | `internal/runloop` | 0 `deps.` refs; wraps `handlercontract.EventEmitter` + `core.EventEnvelope` only |
| `internal/daemon/waitsocketgrace.go` | 170 | `internal/runloop` | 0 real `deps.` refs (the one hit at :95 is a comment); has 1 raw `time.After` |
| `internal/daemon/reviewerharness_hkiv748.go` | 135 | `internal/runloop/dotrun` | 0 real `deps.` refs (3 hits are comments) |
| `internal/daemon/codexnowork_hk368i4.go` | 105 | `internal/runloop` | 0 `deps.` refs |
| `internal/daemon/postreadyhang.go` | 102 | `internal/runloop` | 0 `deps.` refs; 1 raw `time.NewTimer` |
| `internal/daemon/agentready.go` | 207 → 131 → 0 | **RESOLVED — converted, not moved. DELETED by RT14 (`7d448afb`).** | RT19b-3 first moved the four surviving policy symbols to `internal/runlaunch`; RT14 then converted both `waitAgentReady` call sites onto `dispatchSegment` and deleted the empty remainder. The "only remaining reference is the export_test shim" claim was FALSE when written — see §4 step 17. |

**Package-name warning (from the challenge, verified):** `internal/workflow/dot` is already
`package dot`, and **both** `dot_cascade.go` and `dot_gate.go` import it. A new `internal/runloop/dot`
would collide at every such site and force an alias. **Name it `internal/runloop/dotrun`.**

### 1c. Files that STAY in `internal/daemon`, and why

| File | LOC | Why it stays |
|---|---|---|
| `workloop.go` lines 188–943 (`workLoopDeps`, 82 fields) | 755 | The daemon composition root. 16 non-unit prod files reference it (`agentready.go`, `bootsocket.go`, `bootworkloop.go`, `codexnowork_hk368i4.go`, `concurrencycontroller.go`, `daemon.go`, `diskcheck_hksxlb.go`, `eagerfill_em063.go`, `followup_ledger_ac1.go`, `harnessregistry.go`, `hookrelay_chb025.go`, `moderesolve.go`, +4). It must never be exported. |
| `workloop.go` lines 1547–3167 (`runWorkLoop`) | 1,620 | The **outer** queue-claim/poll/maintenance loop. This is unit E3's neighbourhood, not E5's. Its symbols (`runWorkLoop`, `newWorkLoopDeps`, `evaluateGroupAdvanceWithOutcome`, `selectNextQueue`, `snapshotFleet`, `workloopSleep`, `workloopIdleWait`, `loopMaintenanceState`, `diskCheckInterval`) are read by 17 non-unit daemon files. **If an implementer treats "workloop.go" as the unit, the move fails.** |
| `runregistry.go` | 336 | `RunRegistry`/`RunHandle` are read by 14 non-unit prod files (`bandwidthtuner`, `bootstate`, `bootworkloop`, `diskcheck_hksxlb`, `draindetect`, `handlerpause_9hwbw`, `handlerpause_policy_37zy8`, `perqueuespendmeter_tigaf11`, `projectconfig`, `queuestore_hkj808w`, `stalewatch`, `statedisk`, `statetypes`, `subscribe`). Shared daemon state, not run-loop-private. |
| `stalewatch.go` | 1,189 | Outer-loop bus observer over `RunRegistry`. |
| `runinflightreconcile_hkr73qr.go` | 280 | Boot-time reconcile driven by `bootreconcile.go`. |
| `run_session_adoption.go` | 105 | Boot-time session adoption driven by `bootreconcile.go`. |
| `pasteinject.go` | 2,637 | 21 symbols of E5's outbound coupling land here, **but** `specs/agent-input.md` T8 is demoting the tmux-input surface to observation-only in favour of `SubmitInput`. Bind it behind `LaunchPort.Deliver`; do not move it. |
| `reversetunnel.go` | 361 | Unit **E4**. 11 symbols of E5 outbound coupling. |
| `tmuxsubstrate.go` | — | Unit **E4** / `handler.Substrate`. 15 non-method symbols *plus* a method surface (`tmuxSubstrate.runSessionName`, `workerSpawnSessionName`, `perRunSubstrate.SendEnterToLastPane/WriteLastPane/SendQuitToLastPane`, `tmuxSubstrateSession.Kill/Wait/Outcome`) that the recon's count omitted. |
| `branching.go` | 672 | Stays as a whole (only `gitRevParse` leaves in RT13). **Do not create `internal/branching` for the rest** — that package already exists as the `.harmonik/branching.yaml` loader and `branching.go` already imports it; merging bead-body parsing into it would give one name two responsibilities. |
| `export_test.go` | 3,628 | Stays, but RT19 must split it. 183 `Exported*` funcs + 59 exported types are the ONLY route 173 external `daemon_test` files have into the run loop. |
| `mergetomain_hkftyvo_test.go` | — | **Cannot move** (uses 4 `daemon.Exported*` symbols including `ExportedRunWorkLoop`) even though 20 test files consume its fixture family. |
| `mergetomain_hk1u4wp/hkcwxow/hkj1aq5/stripruncontext_hk4je`, `worktreerefresh_hk4goy3`, `worktreerefreshscope_hk7qmpp`, `branchguard`, `mergetomain_mergestepretry_hkf9xzs` `_test.go` | — | All drive `ExportedRunWorkLoop` end-to-end. They stay in daemon as integration tests even after the merge path leaves. |

---

## 2. The seam it exits behind

**Pre-existing seam, no new seam invented:**

- `RunPorts` — `internal/daemon/runports.go:280`. Bundle of:
  - `LedgerPort` — `runports.go:44`
  - `EmitterPort` — `runports.go:38` (a **type alias** for `handlercontract.EventEmitter`, so wiring it is identity)
  - `WorktreePort` — `runports.go:152`
  - `MergePort` — `runports.go:90`
  - `LaunchPort` — `runports.go:163`
  - `GatePort` — `runports.go:118`
  - `substrate.ClockPort` (external, already a leaf)
- `BudgetPort` — `runports.go:172`
- `RunEnv` — `runports.go:292`; `SharedHandles` — `runports.go:322`
- Downstream leaves already outside daemon and already depguard-fenced: `internal/mergeq`
  (`.golangci.yml:647`), `internal/runexec`, `internal/gitprobe` (`.golangci.yml:171`).

**RED FLAG — the seam is far shallower than `_plan.md` §2 assumes.** `_plan.md` says E5 = "finish
threading the run machine through these ports". Measured:

- `RunPorts` is referenced at **7 sites in the entire tree**: `workloop.go:3779` (comment), `:3982`
  (comment), `runports.go:273/280/330/334/335`, `runbridge.go:32/75`, `runshell.go:15` (comment).
- `RunEnv` and `SharedHandles` are referenced at **zero** sites outside their own declarations. They
  are **dead code** — designed in `ports-design.md §1`, declared by RT4, never constructed, never
  consumed.
- Meanwhile `deps.` is read **372 times in `workloop.go`, 137 in `reviewloop.go`, 88 in
  `dot_cascade.go`, 41 in `dot_gate.go`, 12 in `runbridge.go`.**

So roughly **10% of the ports work is done**. RT16/RT17 must **widen** `LaunchPort` (see §3a) — that
is not "inventing a new seam", because `runports.go:156-162` already documents `LaunchPort` as
covering "launch-spec build, agent spawn (substrate), harness/adapter registries, hook-store outcome
wait, brief delivery, agent-ready timeouts, and sandbox" — only `BuildSpec` was ever cut. **Widening a
port to its already-documented scope is in charter. Adding an eighth port is not — escalate if a step
seems to need one.**

**Second RED FLAG — `LaunchPort` currently leaks E1's private types.** `runports.go:166`:

```go
BuildSpec(ctx context.Context, rc claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error)
```

`claudeRunCtx` and `claudeRunArtifacts` are private types owned by `claudelaunchspec.go` — an **E1b**
move target. `internal/runloop` cannot compile against them. E1b must land (and promote those two
types into a shared home) before `LaunchPort` can leave daemon.

---

## 3. Coupling to break

### 3a. Outbound (unit → daemon)

**165 non-method daemon symbols across 55 files (234 including methods).** The rows below are the ones
that decide sequencing, not the whole census.

| Symbol | Defined in | Used by | Resolution |
|---|---|---|---|
| `workLoopDeps` (82 fields) | `workloop.go:188-943` | `driveDotWorkflow`, `dispatchDotAgenticNode`, `dispatchDotGateNode`, `runReviewLoop`, `runBridge.deps`, `beadRunOne` — **all by value; 4 mutate the copy** | **BLOCKER until RT15/RT18.** Replace at the 5 entry points with `(RunEnv, RunPorts, SharedHandles)`. The copy-mutation idiom (`deps.clock = substrate.SystemClock{}` at `dot_cascade.go:215`/`:1254`, `reviewloop.go:222`; `deps.launchSpecBuilder = routedLaunchSpecBuilder(...)` at `workloop.go:3967-3979`) smuggles a port through a mutated by-value field and **must be deleted, not preserved**. |
| `RunEnv.ProjectCfg` typed `ProjectConfig` | `projectconfig.go:1235` (in a 1,300-LOC file read by many non-unit files) | `runports.go:302` | **BLOCKER for lifting `RunEnv`.** Either narrow to a run-scoped view struct owned by `runloop`, or move `ProjectConfig` out of daemon. Decide **before** RT15, because RT15 constructs `RunEnv`. Neither input flagged this; the challenge found it. |
| `SharedHandles.RunRegistry` typed `*RunRegistry` (concrete) | `runports.go:323` / `runregistry.go` | nothing yet | **BLOCKER for lifting `SharedHandles`.** `runregistry.go` correctly stays in daemon, so a lifted `SharedHandles` yields `runloop → daemon`: the exact cycle + depguard violation this unit exists to prevent. Must become a narrow interface (`SetAgentType`/`SetMachine`/`SetResolvedProvider` are the methods `beadRunOne` calls). Same for `SharedHandles.Workers *workers.Registry` (that one is fine — `workers` is already a leaf). |
| `deps.bus` (EmitterPort bypass) | `workloop.go:190` | 115 direct reads (workloop ×34, reviewloop ×51, dot_cascade ×23, dot_gate ×7); `EmitterPort` reached only at `runports.go:337` and `workloop.go:7762` | **inject-as-port.** `EmitterPort` is a type *alias*, so this is a pure rename with zero behavior risk. Highest-count / lowest-risk item in the unit — do it first inside RT16. |
| `deps.brAdapter` (LedgerPort bypass) | `workloop.go:189` | 20 direct calls; `LedgerPort` exposes 3 methods | **inject-as-port**, but only the ~9 sites **inside** `beadRunOne` (3167–5397) convert. `:2393`, `:2753`, `:2946`, `:1920`, `:1971` are OUTER-loop (`runWorkLoop`) sites that legitimately keep the raw adapter. Widen `LedgerPort` with `ClaimBead` + `Ready`. |
| `deps.substrate` / `deps.reviewerSubstrate` | `workloop.go` fields | workloop ×12 (in beadRunOne), `reviewloop.go:338-340`, `dot_cascade.go:1476-1478/:1804`, `dot_gate.go:383-385` | **inject-as-port** onto `LaunchPort` (already in its documented charter). `handler.Substrate` is already an interface — mechanical. |
| `deps.hookStore` | `hookrelay_chb025.go` (`hookStoreIface`, 5 methods) | ~30 sites: workloop ×4, `dot_gate.go` :400/:401/:417/:418/:448/:450/:488/:489/:502/:509/:510, `dot_cascade.go` :1633/:1634/:1791/:1792/:1845/:1848/:1871/:1872/:1940/:1941, `reviewloop.go` :385/:386/:785/:1506 | **inject-as-port.** Already an interface — pass-through onto `LaunchPort`. |
| `deps.harnessRegistry` / `deps.adapterRegistry` | `workloop.go` fields; types are `handlercontract.HarnessRegistry`/`AdapterRegistry` | ~38 sites across all four mode files | **inject-as-port** as `LaunchPort.HarnessFor`/`AdapterFor`. Types are already core-only leaf types. **COORDINATION: this is E1's seam — E1a/b/c must land or the run loop keeps a direct edge to the harness impls.** |
| `deps.launchSpecBuilder` | `workloop.go` field; `routedLaunchSpecBuilder` + `pinnedHarnessLaunchSpecBuilder` in `harnessregistry.go` | `workloop.go:3967-3986` (assigned into the by-value copy, read at :4338 via `rp.Launch`), `reviewloop.go:306/:1252`, `dot_cascade.go:1423`, `dot_gate.go:339` | **inject-as-port.** Resolve into `rp.Launch` ONCE at :3986; sub-drivers read `ports.Launch.BuildSpec`. `pinnedHarnessLaunchSpecBuilder` (recon missed it) also needs a port route. |
| `ensureCodexRefsTrailer` | `codexcommit.go` (**an E1a move target**) | `workloop.go:5135`, `dot_cascade.go:2016` | **HARD ORDERING CONSTRAINT.** E1a is the FIRST unit in the plan's ordering and it moves this file. E1a must export this symbol (or route it behind `LaunchPort`) or E1a breaks the run path. Say so in E1a's plan. |
| `ensurePiRefsTrailer` | `picommit.go` (**an E1c move target**) | `beadRunOne` | Same pattern as above, for E1c. |
| `newDaemonHeartbeatEmitter` | `claudeheartbeat.go` (**an E1b move target**) | 5 run-path sites: `workloop.go:4838`, `dot_cascade.go:1843` and `:2265`, `dot_gate.go:437`, `reviewloop.go:640` | Same pattern, for E1b. **Absent from the recon entirely.** |
| `loadStandardGraph` | `standardgraph.go` | `workloop.go:3946` (in `beadRunOne`) | **Absent from the recon.** Either move `standardgraph.go` with the unit or thread the graph in on `RunEnv`. |
| `ResolveModelPreference` | `modelpreference.go` | `workloop.go:3340` (in `beadRunOne`) | **Absent from the recon.** Same choice. |
| `resolveWorkflowMode` / `resolveWorkflowRef` | `moderesolve.go` | `workloop.go:3294` | Recon listed `moderesolve.go` only as a `workLoopDeps` *consumer*; it is also a dependency. |
| `resolveHarnessAgentTypeQuiet` | `harnessresolve.go` | `beadRunOne`, `dot_cascade.go`, `reviewerharness_hkiv748.go` | **Absent from the recon.** |
| `resolvePiProfile`, `hasSingleModelLabel`, `emitProviderSelected`, `PiProfileConfig` | `pi_profile_resolve.go` (E1c), `projectconfig.go` | `workloop.go:3356/:3371/:3390` | Recon listed this file only as a *caller*. |
| `SandboxProfileInput`, `sandboxSpawnForRun`, `sandboxWrapExecArgv`, `verifySandboxEngaged`, `resolveGateAgentType`, `srtEngagementCanaryPath`, `srtClaudeTmpDir` | `sandboxprofile.go`, `sandboxgate.go` (264) | `beadRunOne`, `dot_cascade.go`, `dot_gate.go` | **inject-as-port** as `LaunchPort.SandboxFor(agentType, SandboxProfileInput)` — `ports-design §1` names sandbox as part of LaunchPort. |
| `newSessionIDInterceptor`, `persistClaudeSessionID`, `emitClaudeSessionIDPersisted`, `sendVersionSelectedACK` | `sessioncontext_chb023.go` | `reviewloop.go:458/:474/:485/:501` | **OUTBOUND, not inbound** (recon had the direction backwards). Four real edges to drain. |
| `emitReviewFixupStalled`, `emitReviewerBudgetExceeded`, `priorVerdictSummaryMaxBytes`, `rlComputeDiffHashVia`, `rlTruncateUTF8` | `reviewloop.go` | `dot_cascade.go` | **Cross-destination edge.** If `dot_cascade.go` → `runloop/dotrun` and `reviewloop.go` → `runloop/reviewloop`, these 5 become new cross-package exports. **Consider putting both in one `internal/runloop` package** and revisiting the sub-package split later. |
| `isHarmonikChurn` (`workloop.go:7986`), `commitResidualDelta` (`:7335`) | inside the RT13 cut | `reviewloop.go` ×3 and ×2 respectively | **A production `reviewloop → runmerge` edge the recon listed as tests-only.** Fine (both land in `runmerge` as exported), but RT13 must export them and rewrite those 5 `reviewloop.go` sites. |
| `emitOutcomeEmitted` (`:7450`) | inside the RT13 cut | `runbridge.go:133` | **RT13 must export it** or `runbridge.go` fails to compile. Same class: `emitBeadClosed` (:7746), `emitWorkingTreeRefreshFailed` (:7470), `emitMergeBuildFailed` (:7527), `emitBeadSyncFailed` (:7551), `emitWorkingTreeLocalEditsOverwritten` (:7492), `filterIgnoredPaths` (:7951), `gitRebaseAbort` (:6793), `isMergeBuildColdCacheError` (:7516). Verified: only `emitOutcomeEmitted` and `isHarmonikChurn` have external callers today, but all are in-region and must be exported if referenced. |
| `emitBeadClosedAndMaybeEpic` (`:7761`), `maybeEmitEpicCompleted` (`:7772`) | inside the RT13 line range | `runbridge.go:363` (closeHook) | **RT13 TRAP.** These are the ONLY 2 functions in 6397–8011 that take `deps workLoopDeps` (reading `deps.emittedEpics`/`emittedEpicsMu`/`deps.bus`/`deps.runPorts()`). A naive `sed -n '6397,8011p'` cut will not compile. **They stay behind**; `runmerge` takes an `OnBeadClosed func(ctx, runID, beadID)` callback. |
| "Third band" helpers stranded in `workloop.go` outside both regions | `clockAfter` (:1217), `agentReadyKillReapTimeout` (:147), `beadAlreadySubsumedInMain` (:5440), `forceTeardownSession` (:5760), `emitPreExecMessage` (:5807), `emitPreExecBeforeLaunch` (:5841), `emitImplementerPhaseComplete` (:8012), `emitSpawnCapBlocked` (:8044), `emitTmuxNewWindowTimeout` (:8077), `artifactAgentType` (:8106), `emitAgentReadyTimeout` (:8117) | called by files this unit moves | **Eleven back-edges that survive RT19.** Note `artifactAgentType(a claudeRunArtifacts)` is signature-coupled to an **E1b** private type. Neither the recon's two-region model nor RT13–RT19 addresses these; a dedicated slice (call it RT19b) must. |
| `allocateReverseTunnelPort`, `buildReverseTunnelArgs`, `workerTCPEndpoint`, `resolveAgentDaemonSocket`, `waitWorkerSocketLive` +6 | `reversetunnel.go` (361) | `beadRunOne` remote branch, `driveDotWorkflow:214`, `runReviewLoop:229` | **BLOCKER until E4 lands.** 11 symbols. |
| `ErrSpawnCapTimeout`, `ErrTmuxNewWindowTimeout`, `newPerRunSubstrate`, `substrateSpawnStats`, `perRunSubstrate`, `SrtSpawnConfig`, `exitCodeClean`, `runSessionSpawner`, `sessionCreator`, `tmuxSubstrate`, `defaultNewWindowTimeout`, `defaultSpawnAcquireTimeout` + methods | `tmuxsubstrate.go` | `reviewloop.go:338`, `dot_cascade.go:1804`, `dispatchsegment.go:288-291`, `workloop.go` | **E4 owns this.** The two sentinel errors go to a tiny shared errors leaf (`dispatchSegment.classifyLaunchFailure` needs only `errors.Is`); the rest is reached via `LaunchPort`. |
| `readAutoStatusMarkerVia` | `dot_cascade.go` | `pasteinject.go` — a **non-unit** file reaching INTO the DOT cascade | Invert to a callback so `pasteinject.go` stops reaching in. |
| `time.After`/`time.Now`/`time.NewTimer` on the run path | stdlib | `dot_gate.go` ×6 (`:484` `:751` `:758` `:767` `:773` `:786` + a `NewTimer`), `waitsocketgrace.go` ×1, `postreadyhang.go` ×1 | **inject-as-port** (`substrate.ClockPort`). `beadRunOne` (3167–5397), `dot_cascade.go`, `reviewloop.go`, `dispatchsegment.go`, `runbridge.go`, `runshell.go` are all verified **clean** of raw time. Closing all **eight** sites (not six) is what makes the run path FakeClock-drivable. |
| `internal/policy`, `internal/workflow`, `internal/workflow/dot`, `internal/brcli` | external leaves | `dot_gate.go` (policy, workflow/dot), `dot_cascade.go` (workflow, workflow/dot), `runbridge.go` (brcli) | **allow-list gap** — the eventual `internal/runloop` depguard block must include these four or the lift fails lint on the first commit. |

### 3b. Inbound (daemon → unit)

**Total symbols needing export across the whole unit: 201.** Of those, **RT13 exports 21** — the rest
are the eventual lift's problem.

**RT13's 21 exports** (name → exported name; every call site listed):

| Current (unexported, in daemon) | Exported as | Call sites to rewrite |
|---|---|---|
| `mergeRunBranchToMain` | `runmerge.RunBranchToTarget` | `runbridge.go:278`, `runbridge.go:321`, `codesync_rs_b8.go`, `scenariogate.go`, `daemon.go` |
| `mergeSubmit` (func type) | `runmerge.Submit` | `runports.go:93` (MergePort.Submit return type), `runbridge.go` |
| `inlineMergeSubmit` | `runmerge.InlineSubmit` | `runports.go:108` |
| `mergeOutcome` | `runmerge.Outcome` | `runbridge.go`, `export_test.go` |
| `isRetryableMergeReason` | `runmerge.IsRetryableReason` | `runbridge.go`, `reviewloop.go` (`spineArgs.retryable`) |
| `emitOutcomeEmitted` | `runmerge.EmitOutcomeEmitted` | `runbridge.go:133` |
| `isHarmonikChurn` | `runmerge.IsHarmonikChurn` | `reviewloop.go` ×3 |
| `commitResidualDelta` | `runmerge.CommitResidualDelta` | `reviewloop.go` ×2 |
| `discardDirtyChurn` | `runmerge.DiscardDirtyChurn` | tests only |
| `cleanUntrackedFiles` | `runmerge.CleanUntrackedFiles` | tests only |
| `checkMainWorkingTreeDirty` | `runmerge.CheckMainWorkingTreeDirty` | tests only |
| `snapshotUntrackedFiles` | `runmerge.SnapshotUntrackedFiles` | tests only |
| `parsePorcelainPaths` | `runmerge.ParsePorcelainPaths` | in-package only (export for parity with its siblings' tests) |
| `stripRunContextFromMerge` | `runmerge.StripRunContextFromMerge` | in-region only |
| `appendReviewTrailersToHEAD` | `runmerge.AppendReviewTrailersToHEAD` | `spineArgs.amendTrailers` in `reviewloop.go` |
| `removeWorktree` | `runmerge.RemoveWorktree` | `workloop.go:5705`, in-region `:6567` |
| `gitRevParse` | `gitprobe.RevParse` | `branching.go` :332 :338 :533 :536 + 8 in-region sites |
| `mergeRunBranchToMainPayload`, `beadClosedPayload`, `epicCompletedPayload`, `workingTreeRefreshFailedPayload`, `mergeBuildFailedPayload` | `runmerge.{MergeRunBranchToMainPayload, BeadClosedPayload, EpicCompletedPayload, WorkingTreeRefreshFailedPayload, MergeBuildFailedPayload}` | payload structs read by tests via `mergeToMainPayloadKind` | 

**Exports the eventual lift needs** (recorded so nobody re-derives them):
`beadRunOne` → `runloop.Run(ctx, env, ports, shared) runexec.RunState`; `driveDotWorkflow` →
`dotrun.DriveWorkflow`; `dispatchDotAgenticNode` → `dotrun.DispatchAgenticNode`;
`dispatchDotGateNode`/`dispatchDotToolNode` → `dotrun.DispatchGateNode`/`DispatchToolNode`;
`runReviewLoop`/`reviewLoopState`/`notifySubstrateRunner` →
`reviewloop.Run`/`State`/`NotifySubstrateRunner`; `productionWorktreeFactory` →
`runloop.ProductionWorktreeFactory`; `emitRunCompleted`/`emitSpawnCapBlocked`/
`emitTmuxNewWindowTimeout`/`artifactAgentType` → `runloop.*`.
**Never exported:** `workLoopDeps`, `runWorkLoop`, `newWorkLoopDeps`, `selectNextQueue`,
`snapshotFleet`, `evaluateGroupAdvanceWithOutcome`, `loopMaintenanceState`,
`reconcileOrphanedRunsOnResume`, `resetGuardedBead`, `beadStatusReader`, `adoptDeadRunSessions`,
`forceTeardownSession`, `windowCleaner`.

---

## 4. Step-by-step recipe

### Ground rules for every step

- **Single writer.** These slices all edit `workloop.go` (318 commits/90d) and/or `export_test.go`
  (221 commits/90d). **Never parallelise across crews.** One crew, one slice at a time.
- **Pure move.** The diff must be `git mv` + package rename + import fixups. **No emission-string
  changes, no signature "improvements", no gofumpt reflow of untouched code.** Any logic delta is
  called out in the commit body and justified, else rejected at review.
- **Never `cd` into a worktree.** Operate from `/Users/gb/github/harmonik` with absolute paths.

### RT13 — carve the merge path out to `internal/runmerge` (EXECUTABLE NOW)

**1.** Create the package directory:

```bash
mkdir -p /Users/gb/github/harmonik/internal/runmerge
```

**2.** Move `gitRevParse` to gitprobe. Cut `internal/daemon/branching.go` lines 362–380 (the doc
comment + `func gitRevParse`) into `internal/gitprobe/gitprobe.go` as:

```go
// RevParse runs `git rev-parse <ref>` in repoRoot and returns the trimmed SHA.
func RevParse(ctx context.Context, repoRoot, ref string) (string, error) { … }
```

Then rewrite the 4 call sites in `branching.go` (`:332`, `:338`, `:533`, `:536`) to
`gitprobe.RevParse(...)`. `branching.go` already imports `gitprobe`? Check with
`grep -n gitprobe internal/daemon/branching.go`; add the import if absent.
**Checkpoint:** `go build ./internal/... ./cmd/...` green.

**3.** Move the two whole files:

```bash
cd /Users/gb/github/harmonik
git mv internal/daemon/stripruncontext_hk4je.go       internal/runmerge/stripruncontext.go
git mv internal/daemon/reviewtrailers_hkdyim.go       internal/runmerge/reviewtrailers.go
git mv internal/daemon/reviewtrailers_hkdyim_test.go  internal/runmerge/reviewtrailers_test.go
```

Change `package daemon` → `package runmerge` in all three. Export
`stripRunContextFromMerge` → `StripRunContextFromMerge` and
`appendReviewTrailersToHEAD` → `AppendReviewTrailersToHEAD`.
**Do not** `git mv` `workloop.go` — it is a region cut, not a file move.

**4.** Cut `internal/daemon/workloop.go` lines **6397–8011** into `internal/runmerge/merge.go`,
**leaving behind** `emitBeadClosedAndMaybeEpic` (:7761–7771) and `maybeEmitEpicCompleted`
(:7772–7850). Split for readability into `merge.go` (the spine + prepare/commit),
`fmtgate.go` (`runMergeFmtGate`/`runMergeFmtCheck`/`runFmtPassesOnce`/`fmtGofumptPass`/`fmtGciPass`/
`commitFmtChanges`/`readGoModule`), `worktreestate.go` (`discardDirtyChurn`/`commitResidualDelta`/
`cleanUntrackedFiles`/`snapshotUntrackedFiles`/`parsePorcelainPaths`/`checkMainWorkingTreeDirty`/
`filterIgnoredPaths`/`isHarmonikChurn`/`mergedCommitPaths`/`refreshMergedPaths`/`locallyEditedPaths`/
`writeRecoveryPatch`) and `events.go` (the 8 `emit*` helpers + the 5 payload structs).

**5.** Also move `removeWorktree` (`workloop.go:5767–5790`, doc comment included) into
`internal/runmerge/worktree.go` as `RemoveWorktree`, and rewrite `workloop.go:5705` to
`runmerge.RemoveWorktree(...)`.

**6.** Apply the 21 renames from §3b. Rewrite the 8 in-region `gitRevParse` sites
(`:6707 :6713 :6754 :6757 :6938 :6961 :7185 :7720`) to `gitprobe.RevParse`.

**7.** Break the `workLoopDeps` trap. Give `runmerge` an explicit callback instead of the god-struct.
In `internal/runmerge`, add to the merge config:

```go
// OnBeadClosed is invoked after CloseBead lands. The daemon binds it to
// emitBeadClosedAndMaybeEpic, which stays in internal/daemon because it reads
// deps.emittedEpics / deps.emittedEpicsMu (workloop.go:7815-7821).
OnBeadClosed func(ctx context.Context, runID core.RunID, beadID core.BeadID)
```

At `runbridge.go:363` (closeHook), pass `func(ctx, runID, beadID) { emitBeadClosedAndMaybeEpic(ctx, deps, runID, beadID) }`.
`runmerge` never sees `workLoopDeps`.

**8.** Rewrite the daemon-side call sites: `runports.go:93/:104/:108` (`MergePort.Submit()` now returns
`runmerge.Submit`, `daemonMerge.Submit` returns `runmerge.InlineSubmit`), `runbridge.go:133/:278/:321/:363`,
`codesync_rs_b8.go`, `scenariogate.go`, `daemon.go`, and the 5 `reviewloop.go` sites
(`isHarmonikChurn` ×3, `commitResidualDelta` ×2).

**9.** Move the 7 clean tests, converting `daemon.Exported*` to the direct `runmerge.*` call:

```bash
cd /Users/gb/github/harmonik
git mv internal/daemon/mergetomain_residualdelta_hkrljho_test.go      internal/runmerge/residualdelta_test.go
git mv internal/daemon/mergetomain_dirtyledger_hk3yz2d_test.go        internal/runmerge/dirtyledger_test.go
git mv internal/daemon/mergetomain_integrationartifacts_hkg9zz_test.go internal/runmerge/integrationartifacts_test.go
git mv internal/daemon/mergetomain_hksfy7f_test.go                    internal/runmerge/mergetomain_hksfy7f_test.go
git mv internal/daemon/escapedetect_hkooexj_test.go                   internal/runmerge/escapedetect_test.go
git mv internal/daemon/mergepath_commitsubject_hkr1v2n_test.go        internal/runmerge/commitsubject_test.go
```

Change `package daemon_test` → `package runmerge_test`, drop the
`github.com/gregberns/harmonik/internal/daemon` import, add
`github.com/gregberns/harmonik/internal/runmerge`.

**DO NOT move `internal/daemon/mergetomain_hkftyvo_test.go`.** It uses `daemon.ExportedRunWorkLoop`,
`daemon.ExportedWorkLoopDeps`, `daemon.WorkLoopDepsParams` and
`daemon.ExportedProductionWorktreeFactory`; moving it would make `runmerge`'s external test package
import `internal/daemon` — precisely what step 12's deny edge forbids. Its `mergeToMain*` fixture
family is consumed by **20** test files, **8 of which cannot move**. **Duplicate** the ~4 helpers the
moved tests actually need (`mergeToMainFixtureGitRepo`, `mergeToMainEventOrder`, `mergeToMainFindEvents`,
`mergeToMainPayloadKind`) into a new `internal/runmerge/fixture_test.go`. Duplication is correct here:
the alternative is a `runmerge → daemon` edge.

**10.** Delete the now-orphaned `export_test.go` shims: `ExportedMergeRunBranchToMain`,
`ExportedMergeOutcome`, `ExportedIsRetryableMergeReason`, `ExportedDiscardDirtyChurn`,
`ExportedCommitResidualDelta`, `ExportedCleanUntrackedFiles`, `ExportedCheckMainWorkingTreeDirty`,
`ExportedSnapshotUntrackedFiles`. **Keep** any shim still referenced by a test that stayed —
`grep -rn 'ExportedInlineMergeSubmit\|ExportedMergeOutcome' internal/daemon/*_test.go` before deleting each.

**11.** Update the spec-audit allowlist — **this test WILL fail without it.** In
`internal/specaudit/wminv003_task_branch_append_only_test.go`, `wmInv003FixtureAllowlist` maps file
paths to authorisation citations and is keyed on `"internal/daemon/workloop.go"`,
`"internal/daemon/reviewtrailers_hkdyim.go"`, `"internal/daemon/mergetomain_dirtyledger_hk3yz2d_test.go"`,
`"internal/daemon/mergetomain_residualdelta_hkrljho_test.go"`,
`"internal/daemon/mergetomain_integrationartifacts_hkg9zz_test.go"`. Add the moved paths, carrying the
**identical** citations:

```go
"internal/runmerge/":                                "EM-052/EM-053 pre-merge rebase of run-branch onto target (WM §4.5 merge-back); not a workspace_leased task-branch rewrite — moved from internal/daemon/workloop.go by P2 E5 RT13",
```

(The scanner matches by `relPath == allowedRel || strings.HasPrefix(relPath, allowedRel)`, so the
trailing-slash directory prefix covers every file in the package. Keep the `internal/daemon/workloop.go`
entry — the outer loop still has rebase-adjacent code paths; verify with a run of the test whether it
can be dropped, and drop it in a separate commit if so.)

**12.** Add the depguard block. Append to `/Users/gb/github/harmonik/.golangci.yml` **immediately after
the `mergeq:` block** (which ends at `.golangci.yml:653`), matching the `gitprobe`/`mergeq` idiom
exactly, with the same indentation (8 spaces for the key):

```yaml
        # runmerge: the run-branch merge / commit / fmt-gate path carved out of
        # workloop.go lines 6397-8011 (P2 unit E5, RT13). Param-in/value-out git
        # plumbing: it takes ctx + paths + an EventEmitter + a runmerge.Submit
        # closure + an OnBeadClosed callback and returns an Outcome. The daemon
        # threads its mergeq exclusion-domain submit and its bead-closed callback
        # IN, so the direction is daemon -> runmerge; runmerge MUST NOT import
        # daemon back. workspace is required: the spine calls
        # workspace.TaskBranchName / WorktreePath / NoWorktreeRootOverride
        # (workloop.go:6547,:6556). mergeq is deliberately NOT allowed — it
        # appears in this region only in comments, and the queue handle stays on
        # the daemon side of the seam.
        # Rationale: plans/2026-07-21-p2-extraction/E5-dot-runloop.md; RSM-015.
        runmerge:
          files: ["**/internal/runmerge/**"]
          allow:
            - "$gostd"
            - "github.com/gregberns/harmonik/internal/core"
            - "github.com/gregberns/harmonik/internal/handlercontract"
            - "github.com/gregberns/harmonik/internal/workspace"
            - "github.com/gregberns/harmonik/internal/gitprobe"
            - "github.com/gregberns/harmonik/internal/lifecycle/tmux"
            - "github.com/gregberns/harmonik/internal/runmerge"
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/daemon", desc: "runmerge MUST NOT import daemon (leaf merge path; daemon drives it) — P2 E5 RT13" }
```

**13.** Run the full §6 verification gate. RT13 ends here; commit and release as one bead.

### RT14 — retire the open-coded ready wait (LANDED 2026-07-22)

**LANDED** in three commits: `229e6e91` (phase A, single-mode), `cb89e35e` (phase B, cognition gate),
`7d448afb` (phase C, retirement + freeze gate). The recipe is
[`RT14-dispatchsegment-conversion.md`](RT14-dispatchsegment-conversion.md); it governs, and it
corrects steps 14–18 as originally written below. Three of those corrections are recorded here so the
next reader does not re-derive them.

**14 (as landed).** `workloop.go`'s single-mode `waitAgentReady` became a `&dispatchSegment{…}`,
`reviewloop.go`'s implementer as the template. **One behaviour delta the template did NOT encode:**
`killAbort` fires on the ctx-cancel edge and performs the same `sess.Kill` that
`runlaunch.ForceTeardownSession` performs — but at THIS site that teardown is guarded by hk-o85ye
(`!useIndepSession || ctx.Err() == nil`) so an independent-session run survives daemon shutdown for
the next boot's adoption pass. An unguarded `killAbort` would strand the bead. The hook carries the
same guard. `reviewloop.go` / `dot_cascade.go` need none — their teardown is unconditional.

**15 (as landed).** `dot_gate.go`'s cognition gate, `dot_cascade.go` as the template. Its
site-specific differences (no `errors.Is` emissions in `onLaunchFailed`, plain `tap.Emit` not
`EmitWithRunID`, an UNBOUNDED reap `Wait`, `SkipReadyHandshake: false` per the hk-01vs0 claude pin)
were all re-derived from this site rather than copied. It also needed the local `substrate` shadow
renamed to `runSubstrate`, and the `deps.clock == nil` backstop the other three consumers already carry.

**16 — WRONG AS WRITTEN. The eight-site count conflated two concerns.** Only **one** of the eight was
in RT14's blast radius: `dot_gate.go`'s `time.After(runlaunch.KillReapTimeout)`, which sits INSIDE
the dispatch segment and converted for free. The other seven are **Working-phase watchdogs** —
`dot_gate.go`'s `pasteInjectQuitOnGateFile`, `waitsocketgrace.go`, `postreadyhang.go` — which
`dispatchsegment.go`'s header places explicitly OUTSIDE the RT8 segment boundary until the M5
reactorization. Converting one copy of a watchdog without its two siblings in `pasteinject.go` (22
further sites) would diverge three implementations of one pattern and close nothing. **The remaining
30 sites are re-filed as [RT19c](RT19c-workingphase-watchdogs.md).** The step-16 grep asserting
`0 0 0` on those three files would fail today and is expected to.

**17 — RIGHT ANSWER, WRONG REASON, and it was unsafe when written.** `rm agentready.go` is what
landed, but NOT because "its only remaining reference is the export_test.go shim." At the time this
step was written the file declared six symbols, four of them live in production, and the `daemon.go`
references that made it LOOK dead were comments — a `grep -c`-driven deletion would have broken the
build. What made the delete safe is that **RT19b-3 (`fd608c01`) had already moved the four survivors**
(`DefaultAgentReadyTimeout`, `DefaultRemoteAgentReadyTimeout`, `EffectiveAgentReadyTimeout`,
`ErrAgentReadyTimeout`) to `internal/runlaunch`, leaving only `agentEventSource` + `waitAgentReady`.
Re-derive with the comment-and-string-filtered grep in the recipe's §3c before trusting any claim
here. `ErrAgentReadyTimeout` is emphatically NOT deletable — it is live at six production sites.

**18 (as landed).** Plus `scripts/readywait-freeze-gate.sh`, wired into `check-fast`/`check-short`:
no re-declaration of the retired symbols, no raw wall-clock in the run-path files that are clean
today or inside `beadRunOne`, and all four production launch sites still construct a
`dispatchSegment`.

**Metric — publish this, not a LOC delta.** RT14 is a seam-uniformity slice: agent-launch sites
hand-rolling their own ready wait **2 → 0**; raw wall-clock sites on the dispatch path **1 → 0**;
`dispatchSegment` consumers 3 → 4 files / 5 segments. Non-test LOC moved only −52.

### RT15–RT19 — the prep STREAM (NOT executable until E1a/b/c and E4 land)

**19.** These are sequenced, not scheduled. Do **not** start them before E1 and E4, because three of
E5's dependencies (`ensureCodexRefsTrailer`, `ensurePiRefsTrailer`, `newDaemonHeartbeatEmitter`) are
literally E1's move targets and `LaunchPort.BuildSpec`'s signature names two E1b private types.

- **RT15 — build `RunEnv`/`SharedHandles` (currently dead) and re-signature `beadRunOne`.**
  Resolve the two blockers first: narrow `RunEnv.ProjectCfg` off the daemon `ProjectConfig` type, and
  turn `SharedHandles.RunRegistry` from `*RunRegistry` into an interface exposing exactly
  `SetAgentType`/`SetMachine`/`SetResolvedProvider`. Then construct both bundles at the `runWorkLoop`
  dispatch site, change `beadRunOne` to `(ctx, env RunEnv, ports RunPorts, shared SharedHandles) runexec.RunState`,
  and rewrite the ~40 `deps.projectDir`/`targetBranch`/`brPath`/`protectBranches`/`allowedRepos`/
  `workflowModeDefault`/`defaultHarness`/`projectCfg` reads inside 3167–5397 to `env.*`.
  **Test blast radius: 156 files** reference `ExportedWorkLoopDeps`/`WorkLoopDepsParams` — *not* 19.
  Budget accordingly; the shim must keep emitting a compatible `RunEnv` so those 156 keep compiling.
- **RT16 — `LaunchPort` part 1: substrate + registries + builder.** Widen `LaunchPort` (already its
  documented charter, `runports.go:156-162`) with `Substrate()`, `ReviewerSubstrate()`, `HarnessFor()`,
  `AdapterFor()`, `HandlerBinary()`, `HandlerArgs()`, `HandlerEnv()`, `DaemonBinaryPath()` — each a
  pass-through preserving the exact nil-defaulting of `ports-design §3`. Rewrite the 12 `deps.substrate`
  sites and the ~38 registry sites. **Delete the copy-mutation at `workloop.go:3967-3979`**; resolve the
  builder into `rp.Launch` once at `:3986`; the sub-drivers read `ports.Launch.BuildSpec`. Also route
  `pinnedHarnessLaunchSpecBuilder`, which the recon missed.
- **RT17 — `LaunchPort` part 2: hookStore, timeouts, sandbox, delivery.** Add
  `RegisterHookSession`/`CloseHookSession`/`SetAgentReadyCallback`/`WaitForOutcome` (pass-throughs onto
  the existing 5-method `hookStoreIface`), `ReadyTimeout()`/`RemoteReadyTimeout()`/
  `PostReadyHangTimeout()`/`CodexNoWorkFloor()`, `SandboxFor(core.AgentType, SandboxProfileInput)`, and
  `Deliver(ctx, sess, kind)` bound to `pasteinject.go`. Do **not** move `pasteinject.go`. Exit gate:
  `grep -c 'deps\.' internal/daemon/{reviewloop,dot_cascade,dot_gate}.go` → `0 0 0`
  (today: 137, 88, 41).
- **RT18 — drop `deps workLoopDeps` from every run-path signature.** `driveDotWorkflow`
  (`dot_cascade.go:185`), `dispatchDotAgenticNode` (`:1221`), `dispatchDotGateNode` (`dot_gate.go:72`),
  `runReviewLoop` (`reviewloop.go:180`) → `(env, ports, shared)`. `runBridge.deps` (`runbridge.go:31`)
  → `env RunEnv` + the existing `rp RunPorts`, rewriting `b.deps.tidGen` (:111), `b.deps.bus`
  (:133/:278/:321), `b.deps.clock` (:83/:141), `b.deps.targetBranch` (:276/:319), `b.deps.brPath`
  (:278/:321) — `tidGen` needs either a `RunPorts.TIDGen` port or a pre-minted `TransitionID` on
  `RunEnv`. **Delete the three `if deps.clock == nil { deps.clock = substrate.SystemClock{} }` blocks**
  (`dot_cascade.go:215`, `dot_cascade.go:1254`, `reviewloop.go:222`). Break the two reach-ins:
  `pasteinject.go` → `readAutoStatusMarkerVia` (invert to a callback), and the **four outbound**
  `reviewloop.go` → `sessioncontext_chb023.go` calls (`:458 newSessionIDInterceptor`,
  `:474 persistClaudeSessionID`, `:485 emitClaudeSessionIDPersisted`, `:501 sendVersionSelectedACK`) —
  note the recon had this edge's direction backwards.
- **RT19 — split the test surface.** Create `internal/runlooptest/` (mirroring `internal/runexectest`
  and `internal/keepertest`) holding `RunEnv`/`RunPorts`/`SharedHandles` fake builders. Move
  `internal/daemon/runshell_test.go` and `internal/daemon/dispatchsegment_test.go` **now** (both are
  already free of daemon internals). Relocate `stubEventCollector` (`workloop_test.go`, **146**
  consumers) and `NewSealedAdapterRegistryForTest` (`testhelpers_adapterregistry_test.go`) into
  `internal/testhelpers`. Move the run-loop half of `export_test.go`'s shims out.
  **Do not use the recon's "export_test.go under ~1,500 LOC" gate** — it was calibrated against a
  60-shim estimate when the real count is 183 funcs + 59 types. Set the gate from a measured
  post-split target instead.
- **RT19b — the third band (NEW; neither input planned it).** Drain the eleven helpers stranded in
  `workloop.go` outside both regions (§3a, "third band" row) plus `standardgraph.go`,
  `modelpreference.go`, `moderesolve.go`, `harnessresolve.go`, `sandboxprofile.go`. Note
  `artifactAgentType(a claudeRunArtifacts)` cannot be resolved until E1b re-homes
  `claudeRunArtifacts`.

**20. THE LIFT (RT20+) — explicitly NOT in this batch.** Only after RT13–RT19b **and** E1a/b/c **and**
E4: `git mv` `reviewloop.go` / `dot_cascade.go` / `dot_gate.go` / `runbridge.go` / `runshell.go` /
`dispatchsegment.go` / `waitsocketgrace.go` / `postreadyhang.go` / `workloopeventsource.go` /
`sub_workflow_runner.go` / `scenariogate.go` / `codesync_rs_b8.go` / `codexnowork_hk368i4.go` /
`reviewerharness_hkiv748.go` + the `beadRunOne` region into `internal/runloop{,/dotrun,/reviewloop}`;
split `runports.go` (interfaces out, daemon adapters stay); add the `internal/runloop` depguard block
with allow-list `$gostd, core, handler, handlercontract, workspace, substrate, runexec, runmerge,
mergeq, gitprobe, queue, workers, policy, workflow, workflow/dot, brcli, lifecycle/tmux, self` and
deny `internal/daemon`. **This is itself a multi-slice stream, never one PR.** Strongly consider
landing `internal/runloop` as ONE package rather than three, to avoid manufacturing the 5 new
`dot_cascade → reviewloop` cross-package exports (§3a).

---

## 5. Freeze tripwire

### 5a. The deny edge (RT13, literal — this is the same YAML as §4 step 12)

```yaml
        runmerge:
          files: ["**/internal/runmerge/**"]
          allow:
            - "$gostd"
            - "github.com/gregberns/harmonik/internal/core"
            - "github.com/gregberns/harmonik/internal/handlercontract"
            - "github.com/gregberns/harmonik/internal/workspace"
            - "github.com/gregberns/harmonik/internal/gitprobe"
            - "github.com/gregberns/harmonik/internal/lifecycle/tmux"
            - "github.com/gregberns/harmonik/internal/runmerge"
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/daemon", desc: "runmerge MUST NOT import daemon (leaf merge path; daemon drives it) — P2 E5 RT13" }
```

The allow-list **is** the isolation gate (same reasoning as the `bootconfig:` block's comment at
`.golangci.yml:~700`): anything not listed is rejected as "not allowed from list". The explicit
`deny` row is belt-and-braces and carries the human-readable rationale into the CI failure message.

### 5b. "No new files in daemon for this concern" — grep guard

depguard cannot express "this *file name* may not be created", so this is a shell guard. Add it as a
step in the repo's lint job (alongside the existing `golangci-lint run` invocation) and as a
pre-commit check:

```bash
#!/usr/bin/env bash
# scripts/freeze-guard-runmerge.sh — P2 E5 RT13 freeze tripwire.
# The merge-to-main / commit / fmt-gate concern left internal/daemon. It does not
# come back. Any NEW daemon file whose name claims that concern is a hard failure.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

violations=$(git ls-files 'internal/daemon/*merge*.go' 'internal/daemon/*fmtgate*.go' \
             'internal/daemon/*stripruncontext*.go' 'internal/daemon/*reviewtrailer*.go' \
             | grep -v '_test\.go$' || true)
if [ -n "$violations" ]; then
  echo "FREEZE VIOLATION (P2 E5 RT13): merge-path files must live in internal/runmerge, not internal/daemon:" >&2
  echo "$violations" >&2
  echo "If this is genuinely daemon-side merge *wiring* (not the merge path), rename it so the name does not claim the extracted concern, and say why in the commit body." >&2
  exit 1
fi

# The run path must not re-acquire a raw merge exec. Any new `git merge` /
# `git rebase` exec in internal/daemon outside the allowlisted outer loop is a
# regression of the carve-out.
if git grep -nE '"git"[^)]*"(merge|rebase)"' -- 'internal/daemon/*.go' ':!internal/daemon/*_test.go' \
   | grep -v '^internal/daemon/workloop.go:' ; then
  echo "FREEZE VIOLATION (P2 E5 RT13): new git merge/rebase exec in internal/daemon — it belongs in internal/runmerge." >&2
  exit 1
fi
echo "freeze-guard-runmerge: OK"
```

**Severity: hard CI failure**, per the resolved architecture decision. A warning erodes — 939 commits
touched `internal/daemon` in 60 days and would walk straight past one.

### 5c. The boundary test

`internal/specaudit/wminv003_task_branch_append_only_test.go` already functions as a second, independent
boundary test for this concern: it walks all of `internal/` for history-rewriting git execs and fails on
any un-allowlisted hit. After step 11 it pins the merge path to `internal/runmerge/` **by path**. Treat a
new hit in `internal/daemon` as the same violation class as 5b.

---

## 6. Verification gate

Run in this order, from `/Users/gb/github/harmonik`. **Serialize the test runs** — do not run them
concurrently with an agent fan-out or a parallel build; that is what manufactured five of the six
failures in the measured baseline.

| # | Command | Pass criterion |
|---|---|---|
| 1 | `go build ./internal/... ./cmd/...` | exit 0. **Scoped deliberately:** `go build ./...` is RED at repo root regardless of E5 — `plans/2026-07-15-agent-substrate-v2/investigate/10-zeromq-experiments/*.go` import unvendored `go-zeromq/zmq4`, `pebbe/zmq4`, `nanomsg/mangos`. Never make an unscoped root build a gate. |
| 2 | `go vet ./internal/...` | exit 0 |
| 3 | `go vet -tags=scenario ./internal/daemon/` | exit 0. **Required, and absent from both inputs' recipes:** 28 daemon test files are behind `//go:build scenario` and **12 of them drive the run-loop exports** (`branchguard_test.go`, `mergetomain_perbead_target_hklgykq_test.go`, `scenario_commit_gate_cap_hki8g59_test.go`, `scenario_em012a_unlabeled_bead_dot_default_hk982_test.go`, `scenario_multibead_mergeconflict_serial_hktijaj_test.go`, `scenario_gate_efficacy_hkv5dyg_test.go`, `scenario_remote_substrate_t4_claude_test.go`, `scenario_remote_substrate_localhost_dot_test.go`, `scenario_remote_substrate_localhost_test.go`, `scenario_subworkflow_dispatch_hkx9l_test.go`, `scenario_reviewloop_em015de_hkintln_test.go`, `scenario_reviewloop_verdict_absent_salvage_hknhmbk_test.go`). Plain `go test ./...` never compiles them. Also run `go vet -tags=e2e_real_claude ./internal/daemon/` (3 files) and `go vet -tags=integration ./internal/daemon/`. |
| 4 | `go test ./internal/specaudit/... -count=1` | exit 0. Specifically `TestWMINV003PartBCorpusCheck` — this is the one that fails if §4 step 11 was skipped. |
| 5 | `go test ./internal/runmerge/... ./internal/gitprobe/... -count=1` | exit 0. The newly-carved packages must be green on their own. |
| 6 | `go test ./internal/daemon/... ./internal/harness/... -count=1 -timeout 25m` | **Differential green**, per `00-test-oracle-baseline.md`: capture `--- FAIL` names before and after; `comm -13 before-failures.txt after-failures.txt` must be EMPTY. The suite is **already red at HEAD** — `TestThroughput_TenBeadsAtMaxFour` is a hard pre-existing failure and five others are load-sensitive flakes. Do not chase them; do not "fix" them inside an extraction commit. |
| 7 | `go test ./internal/runexectest/... -count=10` | exit 0. The RT11 fault-matrix + N=10 relaunch oracle is the **behavior-preservation gate**. `-count=1` is not sufficient. |
| 8 | `go test ./internal/replay/... -count=1` | exit 0. The RT10 run-keyed replay checkers detect event-stream divergence — i.e. a "pure move" that silently reordered or renamed an emission. |
| 9 | `golangci-lint run` | exit 0, depguard green. If the `runmerge` allow-list is missing `internal/workspace` this is where it fails. |
| 10 | `bash scripts/freeze-guard-runmerge.sh` | prints `freeze-guard-runmerge: OK` |
| 11 | `ubs $(git diff --name-only HEAD)` | exit 0 |
| 12 | LOC/file measurement (below) | numbers published in the bead close comment |

**Measurement commands (the unit's success metric):**

```bash
cd /Users/gb/github/harmonik
find internal/daemon -type f -name '*.go' ! -name '*_test.go' | wc -l          # file count
find internal/daemon -type f -name '*.go' ! -name '*_test.go' -exec cat {} + | wc -l  # non-test LOC
wc -l internal/daemon/workloop.go
```

**Measured BEFORE (2026-07-22, working tree with the staged gitprobe/harness-shared work):**

| Metric | Value |
|---|---|
| `internal/daemon` non-test files | **134** |
| `internal/daemon` non-test LOC | **58,971** |
| `internal/daemon` test files | **517** |
| `workloop.go` | **8,378** |

**Expected AFTER RT13:**

| Metric | Expected | Delta |
|---|---|---|
| `internal/daemon` non-test files | **132** | −2 (`stripruncontext_hk4je.go`, `reviewtrailers_hkdyim.go`) |
| `internal/daemon` non-test LOC | **~57,220** | **−~1,750** (1,525 net from the region cut + 25 `removeWorktree` + 201 whole files + ~20 `gitRevParse`) |
| `workloop.go` | **~6,830** | **−~1,550** |
| `internal/daemon` test files | **511** | −6 moved to `internal/runmerge` |
| New `internal/runmerge` package | ~1,750 non-test LOC + ~1,750 test LOC | new leaf, depguard-fenced |

**Expected AFTER the full E5 unit (RT13 → lift, informational only):** `internal/daemon` non-test LOC
drops to roughly **45,000** (−~14,000) and `workloop.go` to roughly **2,500** (the outer `runWorkLoop`
+ `workLoopDeps` composition root, which correctly stay). Do not treat this row as a commitment — it
is what the arithmetic implies if the whole stream lands.

---

## 7. Risks and how each is mitigated

1. **The RT ports seam is ~10% built, not "nearly done".** `_plan.md` §2 says E5 = "finish threading
   the run machine through these ports". Measured: `RunPorts` appears at 7 sites tree-wide, `RunEnv`
   and `SharedHandles` at **zero** outside their own declarations, while `deps.` is read 372/137/88/41
   times in the four mode files. **Mitigation:** this document states the real number in §2 and marks
   RT15 as "construct the dead bundles" rather than "finish threading". Do not accept a status report
   that says "the ports are in".

2. **`workLoopDeps` travels by value and callees mutate the copy.** `dot_cascade.go:215`/`:1254` and
   `reviewloop.go:222` do `deps.clock = substrate.SystemClock{}`; `workloop.go:3967-3979` does
   `deps.launchSpecBuilder = routedLaunchSpecBuilder(...)` to smuggle a port to its callees.
   **Any port work that does not delete this idiom is cosmetic.** **Mitigation:** RT16 step and RT18
   step (c) name the exact line numbers to delete, and RT17's exit gate is a literal `grep -c 'deps\.'`
   → 0.

3. **Ordering dependency on E1 and E4 is HARD, not soft.** `LaunchPort.BuildSpec` (`runports.go:166`)
   literally names `claudeRunCtx` and `claudeRunArtifacts` — E1b's private types. `beadRunOne`,
   `driveDotWorkflow:214` and `runReviewLoop:229` reach 11 symbols in `reversetunnel.go` (E4). And
   three concrete symbols the run path calls (`ensureCodexRefsTrailer` from E1a's `codexcommit.go`,
   `ensurePiRefsTrailer` from E1c's `picommit.go`, `newDaemonHeartbeatEmitter` from E1b's
   `claudeheartbeat.go`) are themselves E1 move targets. **Mitigation:** RT13 and RT14 are declared
   independent of E1/E4 (verified — neither touches those symbols); everything from RT15 on is gated.
   **And E1a's own plan must be told about `ensureCodexRefsTrailer`, or E1a — the first unit in the
   whole P2 ordering — breaks the run path on landing.**

4. **Test mass is the dominant cost, and the recon undercounted it 5–8x.** Real numbers: 183 `Exported*`
   funcs + 59 exported types in `export_test.go`; **156** files on `ExportedWorkLoopDeps`/
   `WorkLoopDepsParams`; **146** on `stubEventCollector`; **88** on `ExportedRunWorkLoop`; 173 daemon
   test files (~59,300 LOC) reaching the run loop. **Mitigation:** §4 RT15 and RT19 carry the corrected
   numbers; the "export_test.go under 1,500 LOC" gate is explicitly retired as mis-calibrated.

5. **The `mergeToMain*` fixture family cannot move, and 20 test files consume it.**
   `mergetomain_hkftyvo_test.go` uses `daemon.ExportedRunWorkLoop`, `ExportedWorkLoopDeps`,
   `WorkLoopDepsParams`, `ExportedProductionWorktreeFactory`; 8 of its 20 consumers are files that
   themselves cannot move. Moving it would create the exact `runmerge → daemon` edge the deny rule
   forbids. **Mitigation:** §4 step 9 duplicates 4 helpers into `internal/runmerge/fixture_test.go`.
   Duplication is the correct trade here, and the reviewer should be told so up front so it does not
   read as sloppiness.

6. **Churn conflict risk is the highest in the tree.** 90-day commit counts: `workloop.go` 318,
   `export_test.go` 221, `daemon.go` 171, `dot_cascade.go` 99, `reviewloop.go` 92. Every step in this
   stream edits `workloop.go` or `export_test.go`. **Mitigation:** the RSM tasks doc already declares
   `internal/daemon` RT tasks strictly sequential single-writer. **Do not parallelise these slices
   across crews.** RT13 in particular should be landed fast, in one sitting, not left open.

7. **RT13's `sed`-shaped trap.** `emitBeadClosedAndMaybeEpic` (`:7761`) and `maybeEmitEpicCompleted`
   (`:7772-7850`) sit inside the cut range and are the only 2 functions there that take
   `deps workLoopDeps` (they read `deps.emittedEpics`/`emittedEpicsMu` at `:7815-7821` and `deps.bus`
   at `:7833`). A naive `sed -n '6397,8011p'` will not compile. **Mitigation:** §4 step 4 names them;
   step 7 gives the `OnBeadClosed` callback that replaces them.

8. **Nine in-region helpers with external callers were omitted from the recon's export list.**
   `emitOutcomeEmitted` (`:7450`, called by `runbridge.go:133`) and `isHarmonikChurn` (`:7986`, called
   by `reviewloop.go` ×3) are the two with verified external callers; `emitBeadClosed`,
   `emitWorkingTreeRefreshFailed`, `emitMergeBuildFailed`, `emitBeadSyncFailed`,
   `emitWorkingTreeLocalEditsOverwritten`, `filterIgnoredPaths`, `gitRebaseAbort`,
   `isMergeBuildColdCacheError` are the same omission class. **Mitigation:** §3b's export table is the
   authoritative list; step 6 applies all 21.

9. **The prescribed verification gate was red before any E5 work, and blind to a whole test tier.**
   `go build ./...` fails at repo root on the zeromq experiment files; plain `go test ./...` never
   compiles the 28 `//go:build scenario` files, **12 of which drive the run-loop exports**.
   **Mitigation:** §6 scopes the build/vet/test commands and adds explicit `-tags=scenario` /
   `-tags=e2e_real_claude` / `-tags=integration` vet passes.

10. **The daemon test suite is already red — a naive "green" gate is unreachable.**
    `TestThroughput_TenBeadsAtMaxFour` fails at HEAD in isolation; five others are load-sensitive
    flakes. **Mitigation:** §6 uses `00-test-oracle-baseline.md`'s **differential** oracle (no NEW
    failure names), and its known-flaky allowlist. If an after-set entry is on that allowlist, re-run
    it in isolation before calling it a regression.

11. **The runtime proof (`_plan.md` §5.4) cannot be run — the daemon is DOWN.** The mechanical
    substitutes are `internal/runexectest` (RT11 fault matrix + N=10 relaunch oracle) and
    `internal/replay` (RT10 run-keyed checkers), both already outside `internal/daemon`.
    **Mitigation:** §6 steps 7–8 make both mandatory. **Every slice's close comment must explicitly
    flag the deferred §5.4 runtime proof as an outstanding gap** rather than quietly omit it.

12. **Package-name collision.** `internal/workflow/dot` is already `package dot` and both
    `dot_cascade.go` and `dot_gate.go` import it; a new `internal/runloop/dot` would force an alias at
    every dual-use site. **Mitigation:** the destination is named `internal/runloop/dotrun` throughout
    this document.

13. **The proposed three-way split manufactures new cross-package exports.**
    `dot_cascade.go` calls 5 symbols owned by `reviewloop.go` (`emitReviewFixupStalled`,
    `emitReviewerBudgetExceeded`, `priorVerdictSummaryMaxBytes`, `rlComputeDiffHashVia`,
    `rlTruncateUTF8`). Splitting them into `dotrun` and `reviewloop` forces 5 new exports across a
    brand-new boundary — extra surface for zero gain. **Mitigation:** §4 step 20 recommends landing
    `internal/runloop` as ONE package first and revisiting the sub-split later, if ever.

14. **`RunEnv.ProjectCfg` and `SharedHandles.RunRegistry` are typed on daemon-private/daemon-resident
    types.** Lifting either bundle as written drags `ProjectConfig` (a 1,300-LOC file's type) out of
    daemon, or creates `runloop → daemon` for `*RunRegistry` — the exact cycle this unit exists to
    prevent. Neither input's recipe caught the `*RunRegistry` contradiction (the recon says "reach it
    as an interface" and then writes the concrete type into its own RT15 recipe). **Mitigation:** §4
    RT15 makes resolving both a precondition, not a cleanup.

15. **Roughly twenty back-edges survive RT19.** The eleven "third band" `workloop.go` helpers, the four
    `sessioncontext_chb023.go` calls, `standardgraph`/`modelpreference`/`moderesolve`/`harnessresolve`/
    `sandboxprofile`, the three E1-owned harness calls, `ProjectConfig`, `*RunRegistry`, and the 5
    `dot_cascade → reviewloop` symbols. Each is a `runloop → daemon` import the deny rule must reject.
    **Mitigation:** RT19b exists in §4 step 19 specifically to drain them. **The honest shape of E5 is:
    RT13 is a genuine near-term win; everything downstream is a prep stream whose length is not yet
    fully measured.** Say that in status reports; do not report E5 as "in progress, ports nearly done".

---

## 8. Rollback

**RT13 (the only slice executable now) is trivially reversible.** It has landed nothing outside its own
commit and creates no runtime state.

1. **Mid-flight, uncommitted:**
   ```bash
   cd /Users/gb/github/harmonik
   git checkout -- internal/daemon internal/gitprobe internal/specaudit .golangci.yml
   git clean -fd internal/runmerge
   rm -f scripts/freeze-guard-runmerge.sh
   ```
   Then confirm the baseline: `go build ./internal/... ./cmd/...` and
   `go test ./internal/specaudit/... -count=1`.

2. **Committed but not released:** RT13 is a single commit on its own bead branch, and it is a pure
   move — `git revert <sha>` restores `workloop.go`'s line count, both deleted daemon files, the
   `export_test.go` shims, and the `.golangci.yml` block atomically. Because nothing else depends on
   `internal/runmerge` (the daemon calls it, never the reverse), a revert cannot orphan a caller.
   **Verify the revert with the same §6 gate**, not by eye.

3. **Released, and a regression surfaces later:** revert the commit, then re-run steps 6–8 of §6 with
   `-count=10` on `internal/runexectest`. If the regression *reproduces after the revert*, it was not
   RT13 — check the differential oracle's known-flaky allowlist before re-opening.

4. **Abandoning the whole E5 unit** (decision that the prep stream is not worth it): keep RT13 and
   RT14. Both are net-positive on their own — RT13 removes ~1,750 LOC and 2 files from the god package
   behind a real deny edge; RT14 deletes `agentready.go` and closes the last wall-clock site ON THE
   DISPATCH PATH (one, not eight — the other seven are Working-phase watchdogs, now RT19c),
   which is what makes the run path FakeClock-drivable and is worth having whether or not the machine
   ever moves. **Do not** abandon partway through RT15–RT18: a half-threaded `RunEnv` leaves the tree
   with two parallel dependency-passing idioms, which is strictly worse than the single ugly one it
   has today. If RT15 must be abandoned mid-flight, revert it whole.

5. **What is NOT reversible and must not be started casually:** the lift (§4 step 20). Once
   `reviewloop.go` / `dot_cascade.go` / `dot_gate.go` move packages, every open branch touching those
   files conflicts, and there are 92–99 commits/90d on each. **Do not begin the lift while any other
   crew has an open branch touching the run path.** Confirm with
   `git branch -r --contains $(git rev-parse HEAD) ` plus a survey of open bead branches before
   starting it.
