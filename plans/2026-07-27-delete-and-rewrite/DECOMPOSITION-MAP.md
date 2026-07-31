# Decomposition map — the rotten center of harmonik

**Date:** 2026-07-28
**Scope:** `internal/daemon/workloop.go` and the run path around it. Read-only analysis.
**Inputs:** the file itself (read end-to-end), `plans/2026-07-27-delete-and-rewrite/` (`_plan.md`,
`CARRY-FORWARD.md`, `NEXT_STEPS.md`), and the superseded-but-evidentiary
`plans/2026-07-24-code-health-audit/` (`_plan.md`, `CORE-QUEUE-PI-PLAN.md`, task cards).

---

## 0-CORRECTION (2026-07-28, late). The four are not peers. One of them already works.

**Read this before §0.** The headline below — "one algorithm written out four separate times" — is
true about the *code shape* and misleading about *what to do*. Measured against the live event log
(`.harmonik/events/events.jsonl`, 100 MB, runs through 2026-07-22):

- **The graph engine is the DEFAULT and has really run.** `moderesolve.go` tier 4 returns
  `WorkflowModeDot`; `single` is reachable only via an explicit `workflow:single` per-bead label and
  has been used **twice, ever** (2 of 2,153 `run_started`). The engine dispatched **1,697 `implement`
  nodes, 1,376 `commit_gate` nodes, and made 6,846 edge-routing decisions** across **three distinct
  graph files** — the project's own `workflow.dot` (`review`, `qa`), `sonnet-triple-review.dot`
  (`review_correctness`, `review_design`, `review_tests`, `consolidate`) and `eval-bead.dot` (`grade`,
  `judge`). All three node sets appear in the log. This is not a demo path.
- **Measure it with `node_dispatch_*`, NOT with `workflow_mode`.** §6 of `PRIOR-ART.md` is right that
  the `workflow_mode` field on `run_started` is stamped *before* the graph→review-loop demotion and so
  cannot distinguish them. The node-dispatch events can: `node_dispatch_requested` is emitted only from
  `dot_cascade_core.go` / `dot_cascade_helpers.go`, and `reviewloop.go` emits zero. Independently, the
  demotion branch is **unreachable on this project** — it is gated on `<projectDir>/workflow.dot` not
  existing, and it exists.
- **Graph-mode failures are not engine failures.** 866 started / 417 completed / 451 failed. Of the
  451: 144 orphaned by a daemon restart, ~138 the implementer agent not committing or not starting,
  ~52 merge/vet/format, ~25 a reviewer producing no verdict file — and ~32 that are the engine working
  correctly (retry cap hit, no-progress detected, routed to `close-needs-attention`). Review-loop's
  failure mix is the same shape (545 of 1,282).

**So Phase 3 is a deletion, not a rewrite of the run machine.** The engine exists; two hand-written
shadows of it do not need to. Removing the shadows needs a named amendment to `execution-model.md`
EM-012a-FLOOR / `process-lifecycle.md` PL-004a / `beads-integration.md` BI-009a, which all restate the
demotion. See `PRIOR-ART.md` §6.

**CORRECTION (2026-07-29): "kept alive by ONE line" is wrong as literal reachability.** It is right
about which path fires *unbidden*, and that distinction is the useful one. Review-loop mode is produced
at **five** non-test sites: the `EM-012a-FLOOR` demotion in `beadRunOne`; the `--review-loop` flag and
`--workflow-mode review-loop` in `cmd/harmonik/run.go`; `harnessApplyWorkflowDOT` in
`cmd/harmonik/harness.go`; and `DriveOrchestration` in `internal/scenario/orchdrive.go`. The last two
default to review-loop when a scenario declares no `workflow_path` — but all six shipped scenario
definitions under `scenarios/` do declare one, so both are **dormant**. `runBridgeConfig` in
`internal/runloop/runbridge.go` is a *consumer* of the mode (it grants review-loop `MaxMergeAttempts=3`
where dot gets 1), not an entry point — but retiring the mode must decide that retry's fate rather than
drop it silently.

So: **the floor is the only path that reaches review-loop when nobody asked for it.** The other four are
deliberate opt-ins. Deleting the floor does not free `reviewloop.go` on its own; the opt-ins must be
retired too.

**And the migration was already specified and never executed.** `specs/examples/review-loop.dot` draws
the hardcoded review loop as five nodes, and its own header says: *"once the C2 dispatch driver
subsumes the hardcoded Go path, this file becomes the source of truth and the Go path is deleted."*

**Caveat that made this task zero, not a conclusion — NOW DISCHARGED.** The event log ends
**2026-07-22** and Phase 1 deleted ~225,000 lines afterwards, so everything above rested on six-day-old
evidence.

**Task zero is GREEN, re-measured 2026-07-29 on the current tip (`cde2f54e`).** A real ticket ran
end-to-end through graph mode in a fully isolated scratch daemon (own socket, own ledger, own tmux
session, own binary built from the tip; the fleet daemon was never touched — `scripts/scratch-daemon.sh`).
The graph routed **six nodes** — `start → implement → commit_gate → review → qa → close` — in 2m32s, and
the run landed a **real commit** on the target branch, verified on disk and not merely inferred from
events. Codex ran the implementer; the reviewer node fired twice and a valid verdict file was produced
and consumed both times. So the engine is not merely historically real — it works on today's binary.

Two things worth carrying forward from that run:

- **The event-type names in this doc are approximate.** There is no `node_dispatch_started` /
  `_completed` / `_failed`. The real emissions are `node_dispatch_requested`, `node_dispatch_decided`,
  `node_started`, `node_completed`. A monitor filtered on the wrong names goes silent, and silence here
  is indistinguishable from "the engine did nothing" — which is exactly the false-negative this project
  keeps getting bitten by. Confirm event names against `internal/core` before filtering on them.
- **`working_tree_refresh_failed` fired on this run and is benign.** The scratch checkout sat on a
  different branch than the merge target, so the post-merge scoped refresh had nothing to check out. The
  registry already classifies EM-054 as *"ordinary — informational; merge already durable"*, which is
  precisely what happened. Do not read it as a failed merge.

---

## 0. The headline, before the detail

> **⚠ SUPERSEDED 2026-07-30. Do not brief anyone off the original headline below.** It said one
> algorithm was "written out four separate times", in 6,459 lines. Two of the four are gone. Measured
> at HEAD on 2026-07-30:
>
> | Function | File | Lines | State |
> |---|---|---:|---|
> | `runAgentLaunch` | `internal/daemon/agentlaunch.go` | 836 | **the ONE launch path.** All three remaining sites call it. A CI gate (`scripts/readywait-freeze-gate.sh`) allows exactly one `runloop.DispatchSegment` in the tree, and it must be here |
> | `beadRunOne` | `internal/daemon/workloop.go` | 1,773 | run driver. Its single-mode tail is about 722 lines |
> | `driveDotWorkflow` | `internal/daemon/dot_cascade_core.go` | 997 | the graph walker. **The default and the traffic** |
> | `dispatchDotAgenticNode` | `internal/daemon/dot_cascade_core.go` | 543 | per-node dispatch inside the walker |
> | `executeCognitionGate` | `internal/daemon/dot_gate.go` | 234 | **cannot run.** No code sets `daemon.Config.CPRegistry` and no graph declares a `type="gate"` node |
> | ~~`runReviewLoop`~~ | ~~`internal/daemon/reviewloop.go`~~ | ~~2,194~~ | **deleted 2026-07-28 (`3cec5afd7`)** |
>
> `runWorkLoop`, the outer scheduler, also left this file — `756b6604c` (2026-07-29) moved it to
> `internal/daemon/scheduler.go`. That is why `workloop.go` reads 3,389 lines and not 6,656.
>
> What is still duplicated, and what to do about it, is §2b. The short version: the launch step is
> one function; "decide whether it did the work" is still written twice.

The named center is `workloop.go`. It is not the only one, and the thing that makes it rotten is not
its size.

The rotten center is one algorithm — "launch an agent, watch it, decide whether it did the work,
merge, terminalize". The duplication was measurable, not inferred. These symbols each used to appear
in three or four files independently:

```
pasteInjectOnLaunch          → workloop, reviewloop, dot_cascade_core
pasteInjectQuitOnCommit      → workloop, reviewloop, dot_cascade_core
runloop.DispatchSegment{}    → workloop, reviewloop, dot_cascade_core, dot_gate
WaitWithSocketGrace          → workloop, reviewloop, dot_cascade_core, dot_gate
EffectiveAgentReadyTimeout   → workloop, reviewloop, dot_cascade_core, dot_gate
ForceTeardownSession         → workloop, reviewloop, dot_cascade_core, dot_gate
HookStore.RegisterHookSession→ workloop, reviewloop, dot_cascade_core, dot_gate
gitprobe.ResolveWorktreeHEADVia → workloop, reviewloop, dot_cascade_helpers
```

**All eight rows are now single-site.** Every symbol above except `gitprobe.ResolveWorktreeHEADVia`
appears once, inside `runAgentLaunch`. HEAD resolution is the one step the collapse deliberately left
with the caller, and it is therefore the one still written twice.

And the drift that duplication predicts is already present, in two places I confirmed:

- **`runmerge.CheckMainWorkingTreeDirty` (the implementer-escaped-worktree guard) has exactly one
  production caller: `beadRunOne`.** DOT runs do not run it. The run state
  machine has a slot for it (`runexec.ActCheckEscape` / `EvEscapeDetected`, `RunEffectors.CheckEscape`)
  but `RunBridge.WireSpine` wires that slot to a function that unconditionally returns
  `EvGuardsPassed`. The guard is imperative, single-mode, and the machine records a pass it never
  performed. **Correction 2026-07-30: this is not drift.** `specs/run-state-machine.md` RSM-008
  requires the DOT path to skip both guards. See §2b.
- ~~**`sandboxSpawnForRun` (the srt sandbox gate) is called from `workloop.go` and
  `dot_cascade_core.go` only.**~~ **FIXED 2026-07-29 by `30b5cf02c`.** There is one gate, and
  `sandbox.harnesses` in the config is the only switch. Every launch asks it.

`CARRY-FORWARD.md` names this exact bug class from the pasteinject archaeology: *"4× single-mode and
review-loop paths drifted"*, *"3× gate applied to one of N call sites"*. The bug class is real. The
two instances above are now one specified asymmetry and one closed defect, so read `CARRY-FORWARD` as
the reason the collapse was worth doing, not as a live inventory.

---

## 1. What is actually in there

### 1a. Whole-file metrics

| Measure | `workloop.go` |
|---|---:|
| Total lines | 6,656 |
| Comment lines | 3,103 (47%) |
| Commits | 370 (first 2026-05-12, last 2026-07-26 — 75 days, ~5/day) |
| Commits by month | May 108, Jun 140, Jul 122 — **not decaying** |
| Distinct `hk-…` bead refs cited in comments | **256** |
| Distinct spec requirement IDs cited | 81 |
| `//nolint` directives | 19 |
| `workLoopDeps` struct | 81 fields, spanning lines 189–931 (~740 lines, ~89% comment) |
| Top-level functions | 39 |
| Two functions | `runWorkLoop` 1,670 + `beadRunOne` 2,289 = **60% of the file** |

256 distinct bead references is the number that matters. This is not a program; it is a changelog
with executable annotations. Nearly half the file is prose explaining why the other half is shaped
the way it is, and much of that prose describes code that is no longer there.

### 1b. `runWorkLoop` — the outer scheduler (lines 1402–3058, 1,670 lines)

One goroutine. Owns the poll loop, all admission policy, the claim write, and goroutine spawn.

| # | Responsibility | ~Lines | External world |
|---|---|---:|---|
| 1 | Merge-queue ownership (create/Start/cancel `mergeq`), claim semaphore, ~12 pieces of loop-local state | 135 | — |
| 2 | `exitClean`: bounded drain (10 s), kill orphan tmux windows, cancel-drain every active queue | 38 | tmux, filesystem (queue.json) |
| 3 | Spawn-substrate readiness gate after restart-backoff boot | 14 | tmux |
| 4 | Adopt live run sessions surviving a prior daemon (spawns a monitor goroutine per record) | 24 | tmux, filesystem (`.harmonik/runs/`) |
| 5 | Dispatch-halt + governor-halt checks | 17 | — |
| 6 | Schedule tick — fire due recurring jobs (spawn-crew, comms-send, shell command) | 6 (delegates to `scheduletick.go`, 433) | tmux, `harmonik comms`, shell |
| 7 | Periodic flywheel-coordinator tmux session reaper (5 min) | 22 | tmux |
| 8 | Periodic disk watermark probe + reactive `go clean -cache` (10 min) | 21 | filesystem, `go` toolchain |
| 9 | Split local/remote capacity gate | 23 | — |
| 10 | Dashboard staleness forcing gate + `dashboard_stale`/`dashboard_refreshed` edges | 47 | filesystem (dashboard.json, lanes.json, config.yaml), bus |
| 11 | EM-062 eager refill | 8 | **kerf CLI**, br |
| 12 | Sentinel governor OBSERVE mode | 46 | br, `events.jsonl` scan, bus |
| 13 | Sentinel governor ACT mode: trip/clear/halt, ack files, **spawn an adversary crew** | 148 | filesystem (ack files), tmux (crew spawn), `harmonik comms`, bus |
| 14 | Queue selection: bootstrap pending groups, re-evaluate deferred items, cross-queue round-robin | 214 | filesystem (queue.json persist) |
| 15 | Cooldown guard, handler-pause gate, decision-required gate, sentinel queue gate | 66 | bus |
| 16 | Pre-claim `ShowBead` + bounded retry + BI-013c skip + stranded auto-reset + deferred stamping | 172 | **br**, filesystem, bus |
| 17 | `needs-greenlight` label gate | 23 | — |
| 18 | Hoisted secondary local-cap guard | 22 | — |
| 19 | Phase-3 dispatch stamp: cross-queue bead dedup, attempts bound, persist | 138 | filesystem |
| 20 | Bead-record hydration from the pre-claim read | 26 | — |
| 21 | br-ready fallback path (no-auto-pull, operator pause, `Ready` poll, attempt bound, re-gates) | 109 | **br**, bus |
| 22 | RunID mint (uuid v7), queue RunID patch + second persist, TID mint | 43 | filesystem |
| 23 | br-ready pre-claim `ShowBead` + label hydration | 49 | **br** |
| 24 | Claim semaphore → `ClaimBead` → blocked detection → stale-blocker auto-close → item revert | 83 | **br**, git (`MainHistoryHasRefsTrailer`), filesystem |
| 25 | Queue-path label hydration (a third `ShowBead`) | 16 | **br** |
| 26 | `RunHandle` construction + registry Register under the cache-reap read lock | 51 | — |
| 27 | Worker pre-selection + local/remote in-flight accounting | 20 | worker registry (ssh) |
| 28 | Goroutine spawn: build `RunEnv`, call `beadRunOne`, group-advance, staged-bead generator | 38 | **br** (staged bead create) |

Note responsibilities 6, 7, 8, 10, 11, 12, 13 — schedule ticks, tmux reaping, disk reaping, dashboard
gating, kerf refill, and a governor that can *spawn an LLM crew* — are all inside the bead-dispatch
poll loop. None of them are about dispatching a bead. That is roughly 300 lines of unrelated
cadenced maintenance riding the dispatcher's tick.

Note also **three separate `ShowBead` round-trips per dispatch** on the queue path (#16 pre-claim,
#25 post-claim hydration, plus #24's blocked-status re-read on failure), each with its own retry
budget and its own failure semantics.

### 1c. `beadRunOne` — the per-run driver (lines 3120–5408, 2,289 lines)

| # | Responsibility | ~Lines | External world |
|---|---|---:|---|
| 1 | Alias 13 `env` fields to locals; register local-slot and worker-slot release defers | 55 | — |
| 2 | Owning-epic attribution (find parent edge → look up assignee) | 11 | **br** |
| 3 | Run-terminal emission effector + `sessiondata.Collect` goroutine | 53 | bus, filesystem (`~/.claude/projects`) |
| 4 | Workflow-mode and workflow-ref resolution (4-tier + per-item tier-0) | 20 | bus |
| 5 | Construct the `RunBridge` (run state machine) | 14 | — |
| 6 | Model/effort resolution, Pi provider-profile resolution, `provider_selected` emission | 72 | config, bus, **br** |
| 7 | Cross-repo `activeRepo` resolution + allowed-repos safelist refusal | 48 | bead-body parse, filesystem |
| 8 | Parent-commit (`start_from`) resolution; `lands_on` resolution; protected-branch refusal | 61 | **git** |
| 9 | Remote worker selection (pre-selected or fallback) + slot re-accounting | 86 | worker registry, **ssh** |
| 10 | Reverse SSH tunnel: port alloc, worker mkdir, socket-path-length validation, `ssh -N -R` spawn, connectability probe | 119 | **ssh**, network, filesystem |
| 11 | `notifyWorkerOffline` + `preMergeSync` closures | 37 | **ssh**, **git** |
| 12 | Worktree factory selection (local vs remote-over-SSH) + base-sync + create, inside the merge exclusion domain | 103 | **git**, **ssh**, mergeq |
| 13 | Cleanup defers: run-registry record removal, Pi-failure worktree retention | 50 | filesystem |
| 14 | Pre-run untracked-file snapshot (escape-check baseline) | 10 | **git** |
| 15 | `run_started` emission | 8 | bus |
| 16 | DOT graph preload + review-loop safety floor | 26 | filesystem, DOT parser |
| 17 | **Mode dispatch: review-loop** — remote param assembly, call, `WireSpine`, budget charge, terminal feed | 103 | (delegates 1,586 lines) |
| 18 | **Mode dispatch: DOT** — graph load, goal injection, remote params, call, `WireSpine` w/ carve-out, orphan-salvage tip SHA | 175 | (delegates 1,884 lines), **git** |
| 19 | Single-mode: `shared.LaunchCtx` assembly (26 fields) | 61 | — |
| 20 | Agent-type + Pi flags onto the run handle | 11 | — |
| 21 | Per-run substrate: `perRunSubstrate` wrap, worker session naming, independent-tmux-session election + run-registry record write | 86 | **tmux**, filesystem |
| 22 | srt sandbox gate + **engagement canary verification** + exec-path argv wrap + GOCACHE/GOPATH redirect | 104 | **srt subprocess**, filesystem |
| 23 | SessionIDCaptured handling: stdout interceptor, Pi stdout tee-to-file | 60 | filesystem |
| 24 | Hook-session registration | 4 | unix socket registry |
| 25 | Pre-exec message emission | 9 | bus |
| 26 | Per-run event tap + handler construction | 6 | — |
| 27 | Cold-start spawn semaphore (cap 3, remote only) | 22 | — |
| 28 | D2 credential refusal at the launch boundary | 11 | env inspection |
| 29 | Completion-mode + adapter resolution | 24 | — |
| 30 | **`DispatchSegment` construction** — 7 hooks: Launch, OnLaunchFailed, OnLaunched, Deliver, KillReady, EmitReadyTimeout, KillAbort. Contains: paste injection, `/quit`-on-commit watchdog spawn, heartbeat goroutine, comms presence join, agent-ready callback registration, spawn-cap and tmux-timeout event emission, bounded kill+reap | **279** | **tmux**, unix socket, bus, **git** |
| 31 | Post-launch cleanup defers: Pi stderr capture, force teardown, presence offline, heartbeat close | 63 | filesystem, tmux, bus |
| 32 | Ready-timeout terminal | 8 | — |
| 33 | `WaitWithSocketGrace`, per-run abort check, lifecycle `Terminated` transition, tmux window kill | 44 | unix socket, tmux, bus |
| 34 | `implementer_phase_complete` emission | 22 | **git** |
| 35 | ProcessExit daemon-side commit fallback (`codex.EnsureRefsTrailer` / `pi.EnsureRefsTrailer`) + no-work detection | 56 | **git**, codex/pi decision tables |
| 36 | Wait-return → terminal-event mapping | 27 | — |
| 37 | `WireSpine` for single mode | 16 | — |
| 38 | Implementer-escaped-worktree guard, inside the merge domain | 51 | **git** |
| 39 | No-commit guard | 37 | **git** |
| 40 | Dispatch-terminal classification switch: agent_completed / clean-exit / noChange-subsumed / shutdown-drain / failure-with-stderr-tail | 96 | **git**, bus |

**External-world contact count for `beadRunOne` alone: git (12 distinct places), tmux (5), ssh (5),
br (3), unix socket (3), codex CLI (1), pi CLI (1), srt (1), the event bus (~24 emission sites), and
the filesystem throughout.** Every one of the six external tools named in `CARRY-FORWARD.md` is
touched from inside this one function.

### 1d. The two dead things still in the file — BOTH DELETED, and the comments are not dead

- ~~**`activateFirstPendingGroup` (94 lines) has zero callers**~~ — **deleted 2026-07-28.** Only
  `activateFirstPendingGroupLocked` remains, and `runWorkLoop` calls it.
- ~~**`beadExplicitlyReopened` (39 lines) has zero production callers**~~ — **deleted 2026-07-28**,
  with the one test file that held it, `internal/daemon/predispatch_reopen_hkwcv_test.go`. The claim
  above said "two test files". A content search over all history finds one.

**Re-measured 2026-07-29 — the three comment blocks are NOT gravestones, and deleting them would
lose knowledge this program exists to keep.** Each was read in full. **Settled 2026-07-29 — the fact
now lives in the doc that owns it, and the disposition differs per block.** Two of the three kept a
comment in the source, so the one-line "move the fact out, then delete the copy" instruction below
was wrong as a general rule, and it is corrected here.

Note before reading further: **only two of the three are in `workloop.go`.** The Seam A split carried
both `hk-l5saf` comments into `internal/daemon/scheduler.go`.

- **The `sandboxOSTmpDirs` block — fact moved to `CARRY-FORWARD.md`, source copy DELETED.** It did
  not merely record that a function was removed. It stated why the sandbox must never grant recursive
  write access to the shared temp directory, and it named the mechanism that made the hole reachable:
  srt expands every temp-dir entry into a recursive write rule, and `os.TempDir()` falls back to the
  shared `/tmp` whenever no per-user temp dir is set. `CARRY-FORWARD.md` now carries that as a new
  **srt sandbox** section of six facts, which also holds the parts that lived nowhere else — srt's
  hardcoded `TMPDIR=/tmp/claude` for sandboxed children, the consumers that hardcode a host temp root
  instead of reading `TMPDIR` (C's `tmpfile()`, tmux's `TMUX_TMPDIR` socket), the load-7.53
  measurement, and the untested Linux case.
  **Deleting the source copy is safe because the rule already sits at the real edit site and is
  mechanical there.** `GenerateSandboxProfile` in `internal/daemon/sandboxprofile.go` rejects a
  world-shared root in `TmpDirs` and fails the launch with a named error, and the `TmpDirs` field
  comment states the same rule. Nobody was going to reintroduce a function that no longer exists, and
  the block was attached to no code. It had also decayed twice: it cited `Makefile:453` and `:465` for
  a `TMPDIR=/tmp` that the `check-short` and `check-race-full` recipes now carry, and it cited
  `TestSandboxAcceptance_WriteToMainDenied_hki0377` as its evidence, which no longer exists anywhere
  in the tree. Cited by recipe name on purpose — replacing two rotted line numbers with two fresh ones
  reproduces the defect the sentence is describing.
- **The `hk-l5saf` block — KEPT as it stands, in `internal/daemon/scheduler.go`.** It explains why
  there is deliberately **no** guard at that point: the item is already stamped and persisted, so a
  guard there would strand it. Delete the comment and the next reader re-adds the guard. §3 step 3
  states the same ordering constraint and calls it load-bearing, so the fact survives the comment —
  but only the comment says it at the place someone would edit. The comment is already six lines and
  already at that place, so there is nothing to move and nothing to shorten. **This one was never a
  writing task.**
  Two decays to fix when someone next edits that file, and they sit in BOTH `hk-l5saf` comments, not
  only the sibling. The hoisted-guard comment cites "~line 1818" for the Step-2 split gate and
  "~line 3072" for the `localInFlight` increment. The kept post-stamp comment — the "no guard here"
  one — cites "~line 3072" as well. So the block described here as needing no change does carry a
  rotted reference. That does not change the conclusion: the constraint it states is still correct, is
  still at the edit site, and is still recorded in §3 step 3. Only the pointer rotted.
- **The `hk-f38n` block — fact moved to `specs/beads-integration.md` §4.7, SHORT comment kept at the
  edit site.** It records a live false-close: a bare commit-message grep matched an old partial commit
  that carried the same issue ID, so the daemon closed a bead whose remaining work had not run. This
  is a production correctness sensor, which is a third category — phase 1's deletions covered
  prose-grepping test sensors and ticket-named test files, not sensors inside product code. Nothing
  has swept this class. The receiving home is the informative note under BI-022, which owns "git is
  authoritative for completion" and is exactly the claim the incident qualifies. The 18-line block in
  `beadRunOne` became a 9-line "do not add a pre-dispatch already-landed check here" comment that
  names the two runtime paths covering crash-restart and points at the spec note. The same warning
  now sits on `MainHistoryHasRefsTrailer` in `internal/harness/shared/refstrailer.go`, because a
  future caller reaches the primitive rather than the old edit site.
  **Open, and larger than the comment was.** Two things outlive the block. First, `specs/execution-model.md`
  EM-063 Phase 2 (the daemon's eager-refill pre-screen) and EM-064 tier 2 (the orchestrator's guard
  before submit) both still mandate the same bare match as an "already landed" test. Second, and worse,
  `autoCloseStaleBlockersOnClaimFailure` in `internal/daemon/scheduler.go` closes a blocker bead on a
  bare match with no work-presence evidence at all — the same false-close shape, live in the daemon
  today. Both are recorded in `OPEN-DEFECTS.md` and neither is fixed here: narrowing a normative test
  needs adjudication, and the scheduler belongs to another piece of work.

**Disposition, per block rather than in general:** where the fact is about the outside world and
nobody editing that line needs it in front of them, move it out and delete the source copy. Where the
comment's value is its POSITION — it stops a specific edit someone would otherwise make right there —
leave a short comment that states the constraint and points at the doc holding the rationale.

---

## 2. Where the real seams are

A seam is real when the two sides have **different lifetimes, different failure modes, different
external dependencies, and different rates of change**. Below, each proposed seam names the *data*
that crosses it, not a type.

### Seam A — Scheduler ↔ Run  ★ the only seam already load-bearing

**Data crossing:** one immutable dispatch decision — `{run_id, bead_record, queue coordinates
(name/id/group/item), per-item overrides (workflow mode, workflow ref, template params, local-only,
worker target), pre-selected worker or nil, local-slot-held bool, extra context string}`. Back:
one boolean plus a terminal summary.

**Why it is a true joint:**
- *Lifetime:* the scheduler is one goroutine for the daemon's life; a run is one goroutine for
  10–90 minutes.
- *Failure mode:* a scheduler failure wedges the fleet; a run failure reopens one bead.
- *External deps:* scheduler = br + queue.json + kerf + disk. Run = tmux + ssh + git + codex/pi +
  the hook socket. Almost disjoint.
- *Rate of change:* both high, but for different reasons — the scheduler churns on admission policy,
  the run churns on harness behaviour.

**Evidence it is real:** the goroutine boundary already exists (`workloop.go:3025`), the parameter
list is already explicit (a comment there says the explicit parameters exist *because* closure
capture was a bug), and `beadRunOne` has exactly two call sites — the goroutine and a test shim. The
egress analysis confirms it: only four symbols escape `workloop.go` into other production files.

**What is wrong with it today:** the bundle that crosses is `workLoopDeps` copied by value — all 81
fields, of which the run path uses ~25. And the file itself documents the resulting hazard
(lines 830–835): value fields mutated from a run goroutine are silent no-ops, which is why
`loopMaintenanceState` had to be lifted out. That is a design defect preserved by a comment.

### Seam B — Admission ↔ Claim ↔ Reservation

**Data crossing:** *(admission → claim)* a `DispatchCandidate` = `{queue name, queue id, group index,
item index, bead id, per-item overrides}` plus a `permit | delay(reason, wake-condition) | stop`
decision. *(claim → reservation)* `{bead id, run id, transition id}` and, durably, the pair
`(item.status = dispatched, item.run_id)` written **together**.

**Why it is a true joint:** admission is pure — it reads a snapshot and answers a question. Claim is
a single external write to `br` under a semaphore. Reservation is a single durable write to
`queue.json`. Three different failure modes: a wrong admission decision costs a tick; a failed claim
costs a retry; a torn reservation strands an item forever.

**Evidence it is real:** the pure half already exists and is already used —
`internal/orchestrator/select.go` (`SelectNextQueue`, `FleetSnapshot`) is a pure selector that
`workloop.go`'s `selectNextQueue` shell calls after projecting live state via `snapshotFleet`. That
extraction landed and works. The remaining 200 lines of gates (handler-pause, decision-required,
sentinel, greenlight, cooldown, local-cap, cross-queue-dedup, attempts-bound) are the same shape and
have not been moved.

**⚠ Today the reservation is torn, deliberately.** `runWorkLoop` writes
`Items[i].Status = Dispatched` with `RunID = &""` (a placeholder empty string), persists, then mints
the UUID, then re-persists to patch the RunID — and **both persists are non-fatal**. A crash or a
persist failure between them leaves a dispatched item with no run id. The 2026-07-24 audit flagged
exactly this (`CORE-QUEUE-PI-PLAN.md` §"Why the original index was unsafe"). It is unfixed.

### Seam C — Run plan ↔ Resources ↔ Execution ↔ Terminal (inside `beadRunOne`)

Four sub-seams, all inside one function today.

**C1 — Resolution.** *Data out:* an immutable `RunPlan` = `{workflow mode, workflow ref, agent type,
model, effort, pi profile (provider/api/base-url/key-env), active repo, parent SHA, base branch,
merge target, protect-branch set, placement intent}`. *In:* the bead record, project config, queue
defaults, daemon defaults. **True joint:** pure, no I/O except two git rev-parses and one bead-body
parse; deterministic; testable with a table. Everything downstream depends on it and nothing in it
depends on anything downstream. Rate of change is high (harness/model precedence churns constantly)
but the *shape* is stable.

**C2 — Resource acquisition.** *Data out:* a set of leases — worker slot, SSH tunnel (port +
endpoint), worktree (path + cleanup), tmux session/pane, hook session, spawn-semaphore token — each
with an idempotent release. **True joint:** every one of these is an acquire/release pair against an
external system, every one leaks if released twice or not at all, and every one has a distinct
failure mode. This is where CARRY-FORWARD facts tmux-3 (new-window can block forever), tmux-4 (global
command lock), pane-19 (ControlMaster), and codex-5 (`$CODEX_HOME` WAL) live.

The current code proves the joint by how badly it handles it: `beadRunOne` has **at least nine
`defer`s registered at seven different points**, with two of them (`relLocalSlot`, `relWorkerSlot`)
carrying mutable flags that later code flips, and comments explaining LIFO ordering constraints
between them (`hk-3hozm` hoisted one release to the top *because four early returns leaked a worker
slot forever*).

**C3 — Execution.** *Data out:* a `ModeResult` = `{success, summary, needs_attention, approve
verdict or nil, subsumed, advisory_rc, terminal node id, tip SHA}`. **True joint:** the three modes
share nothing but this result; each is a different control flow (one-shot / iterate-until-verdict /
walk-a-graph). **This is the seam that does not exist today and whose absence causes the most bugs.**
See §2b.

**C4 — Terminal.** *Data out:* the ordered effect sequence `gate → pre-merge sync → merge (with
retry) → close-or-reopen → emit run terminal → advance queue group`. **True joint:** these are the
only irreversible writes in the whole run. They must happen exactly once and in order.

**This one is already half-built and is the best structure in the repo.** `internal/runexec`
(`run.go`, `dispatch.go`, `vocab.go` — 1,158 lines) is a pure state machine over the terminal
spine; `internal/runloop/runbridge.go` + `runshell.go` are its effector shell. `beadRunOne` feeds it
classified events (`EvAgentCompleted`, `EvCleanExit`, `EvModeOutcome{success|failure|subsumed|budget}`,
`EvShutdownDrain`) and reads `bridge.Success()`. **Keep this. It is the model for the rest.**

### 2b. Where there is genuinely NO seam — and what must be decided

**No-seam #1: the mode boundary (C3). ⚠ REWRITTEN 2026-07-30 — the section below described a tree
that no longer exists, and the operator was briefed off it. What it said, and why it was wrong, is
kept at the end of this sub-section so a reader can tell decay from a claim that was never true.**

There are **two** workflow modes, not three. `core.WorkflowMode.Valid()` in
`internal/core/workflowmode.go` accepts `single` and `dot` and nothing else. `review-loop` was retired
at v0.10.0 (`specs/execution-model.md` §4.3.EM-015d) and its driver deleted in `3cec5afd7`
(2026-07-28), which removed 3,834 lines: `internal/daemon/reviewloop.go` (2,194 lines),
`internal/daemon/launchspecbuild.go`, `internal/runloop/reviewcycle/` and
`internal/runloop/continuity/`. Eight declarations that were never review-loop-specific moved to
`internal/daemon/runsupport.go` and are still called by the graph cascade.

The default is `dot`. `resolveWorkflowMode` in `internal/daemon/moderesolve.go` returns `dot` at
tier 3 (the daemon default, whose flag default is `dot`) and again at tier 4 (the hard fallback).
`single` is reachable **only** through an explicit `workflow:single` bead label, and taking it emits a
`review_bypassed` audit event. So essentially all real dispatch is graph dispatch.

**The launch step is already ONE function.** `runAgentLaunch` in `internal/daemon/agentlaunch.go`
(836 lines) owns "given a built launch spec plus artifacts, spawn the agent, prove it is alive, drive
it to a dead session, hand back the exit facts". It landed on 2026-07-29 across `6077a24dc`,
`ea96471b3`, `18d59aded`, `159b68e78`, `0743e4dec`, `8346edc2f` and `30b5cf02c`. It has exactly three
callers:

| Caller | Where | Reachable in production? |
|---|---|---|
| single-mode tail of `beadRunOne` | `internal/daemon/workloop.go` | Only via an explicit `workflow:single` bead label |
| `dispatchDotAgenticNode` | `internal/daemon/dot_cascade_core.go` | **Yes — this is the default and carries the traffic** |
| `executeCognitionGate` | `internal/daemon/dot_gate.go` | **No. Dead twice over — see below** |

The cognition gate cannot run. `daemon.Config.CPRegistry` has **zero assignments anywhere in the
tree**, tests included, so `daemonGate.LookupGate` always reports "no registry loaded" and a gate node
returns a structural eval-failure before any launch. And no graph declares one: `type="gate"` appears
zero times in the embedded `internal/daemon/standard-bead.dot`, in this project's `workflow.dot`, in
`sonnet-triple-review.dot` and in `eval-bead.dot`. Counting `dot_gate.go` as a live launch path
overstates the live count by one.

**What is genuinely still duplicated**, after the launch collapse and measured on 2026-07-30:

- **The post-exit interpretation.** The single-mode tail (`internal/daemon/workloop.go`, about 722
  lines from the mode switch to the end of `beadRunOne`) and the graph node
  (`dispatchDotAgenticNode`, about 543 lines) each hand-roll the same four steps after
  `runAgentLaunch` returns: probe worktree HEAD, emit `implementer_phase_complete`, run the
  process-exit commit fallback, then decide what no-commit means. The steps agree in shape and
  disagree in detail.
- **Nothing else.** The terminal spine is shared already: both arms build a `runloop.RunBridge` and
  call `WireSpine`, and `runBridgeConfig` is where the two modes' merge-retry budgets are declared
  side by side.

**The steps that differ, path by path.** Split into two groups, because they need different answers.
This split did not exist in the old text, and its absence is what made the whole set read as one bug.

*Group 1 — the spec says these two paths must differ. Do not "fix" these without amending the spec
first.* `specs/run-state-machine.md` RSM-008 reads: "The single-shot path's post-exit guards — the
escaped-worktree check and the no-commit-guard — MUST run in a `Guarding` state between `Dispatching`
and `Gating` … The review-loop and DOT paths MUST NOT enter `Guarding` (they do not run those
guards)."

| Step | single | dot graph node |
|---|---|---|
| escaped-worktree check (`emitImplementerEscapedWorktree`) | yes | no, **and RSM-008 requires no** |
| `noCommitGuardShouldReopen` | yes | no, **and RSM-008 requires no** — the graph does a bare HEAD-advance compare |

Two things follow. RSM-008 is stale in its own right: RSM-007 still names review-loop as a live fork,
and review-loop was deleted on 2026-07-28. And extending the escape check to the default path is not
obviously an improvement, because `hk-co8g8` root-caused that same check as firing on innocent runs
and killing them. Decide D2 with both facts in hand.

*Group 2 — nothing specifies these. They are real drift, and four of the five leave the DEFAULT path
worse off.*

| Step | single | dot graph node |
|---|---|---|
| Pi provider profile on the launch context (`Provider` / `APIKeyEnv` / `APIKeyFile` / `BaseURL` / `API`) | yes | **no** — `hk-yo9g6`. `resolvedProfile` is resolved once in `beadRunOne` and read only by the single-mode `shared.LaunchCtx`. A Pi node on the default path launches with no provider profile. The model does reach it, because `resolvedModel` is overwritten from the profile before the mode switch |
| implementer comms presence join and leave (`emitImplPresence`) | yes | **no**. No spec requires it on either path |
| Pi stderr capture kept beside the retained worktree on failure | yes | **no** |
| right harness for the process-exit commit fallback | yes | **no** — the graph calls `codex.EnsureRefsTrailer` for every process-exit harness, and Pi is one. Behaviour is the same, because both wrappers call the same `internal/harness/shared/refstrailer.go` primitives. Only the commit message is wrong: a Pi node's daemon-fallback commit says `feat(codex)` |
| independent tmux session, so the run survives a daemon kill and is adopted on the next boot | yes | **no** — `hk-mh3qy`. This is the one capability that makes "just delete single mode" not a one-line change |
| merge retry budget of 3 and the `ChargeReviewLoopFailure` ladder | **no** | yes — deliberate, declared in `runBridgeConfig` |
| `auto_status` work-product inspection | **no** | yes — a graph node attribute, so single has no place to put it |

> **Decision D1 — ANSWERED by the operator, 2026-07-30: collapse the duplication (option A).**
> Do not re-open this. See §7 for what option A now means in practice, which is not what the
> retired text below assumed.

<details>
<summary>Retired text of no-seam #1 (written 2026-07-27/28, false in part on the day it was written,
fully false by 2026-07-29). Kept so a reader can date the decay.</summary>

It claimed:

- Single mode was "lines 4232–5407 (~1,175 lines)". **Wrong by about 1.6x today.** The single-mode
  tail is about 722 lines. Two things shrank it: the review-loop retirement, and `756b6604c`
  (2026-07-29) which moved the dispatch scheduler out to `internal/daemon/scheduler.go`.
  `beadRunOne` now spans about 1,773 lines in total, not 2,289.
- `runReviewLoop` existed with 19 parameters. **Gone since `3cec5afd7`, 2026-07-28.**
- "Each of the three then re-implements: build launch spec → attach substrate → register hook
  session → `DispatchSegment` with 7 hooks → paste inject → wait with socket grace → probe worktree
  HEAD → force-teardown → emit presence. Four times." **False since 2026-07-29.** Every step in that
  list except "probe worktree HEAD" now happens once, inside `runAgentLaunch`.
- "DOT needs ... a ControlPoint registry." **Never true in the sense implied.** No production code
  ever set one, so the graph path has never consulted a ControlPoint registry.
- D1 asked "is one agent launch one thing, or three?" and treated the answer as unbuilt. The launch
  half of option A **shipped on 2026-07-29**, one to two days after the question was written, and the
  document was not updated.

</details>

**No-seam #2: the guards and the state machine disagree about who owns them.**

`runexec` has `ActCheckEscape` / `EvEscapeDetected` / `EvGuardsPassed`. `RunBridge.WireSpine` wires
`CheckEscape` to a constant `EvGuardsPassed`. The actual escape check and no-commit guard are
imperative in `beadRunOne` immediately before the classification switch, with a comment explaining
that they "run BEFORE the dispatch-terminal classification for EVERY class (the pre-RT7 order); by
the time the machine traverses Guarding they are known-green."

Result: the machine's `Guarding` phase is theatre for single mode, and **does not exist at all** for
DOT, because DOT reaches `WireSpine` without ever running the guard. (Corrected 2026-07-30: this
sentence used to say "for review-loop and DOT". Review-loop was deleted on 2026-07-28. The claim
holds for DOT, which is the default path, so the finding got worse, not better — the one mode without
the guards is now the one carrying the traffic.)

**⚠ Added 2026-07-30 — this section never cited the spec, and the spec disagrees with its framing.**
`specs/run-state-machine.md` RSM-008 says: "The single-shot path's post-exit guards — the
escaped-worktree check and the no-commit-guard — MUST run in a `Guarding` state between `Dispatching`
and `Gating` … The review-loop and DOT paths MUST NOT enter `Guarding` (they do not run those
guards)." So the asymmetry is **specified**, not accidental drift, and D2 is a request to change the
spec. Two further facts belong in that decision. RSM-007 and RSM-008 both still name review-loop as a
live fork, so they are stale and need editing whichever way D2 goes. And `hk-co8g8` root-caused the
escaped-worktree check as firing on innocent runs and killing them, so moving it into the machine
would put a known-broken guard on the path that carries all the traffic. Fix `hk-co8g8` first.

> **Operator decision required (D2): do the guards belong to the machine or to the caller?**
> If the machine — then both modes get the escape check and the no-commit guard automatically, and
> behaviour *changes* for DOT (runs that pass today may start failing). If the caller — then delete
> `ActCheckEscape` from `runexec`, because it is a lie.
> This is a behaviour change either way and needs an explicit call.

**No-seam #3: `SharedHandles` is an admitted escape hatch.**

Its own godoc says adding a field to it "is not a new seam ... only a new PORT INTERFACE would be."
It carries 17 fields, of which 10 are concrete daemon pointers and raw sync primitives:
`*atomic.Int32`, `chan struct{}`, `*sync.Mutex` ×2, `map[core.BeadID]struct{}`,
`*workers.Registry`, `*handlercontract.HarnessRegistry`, `*handlercontract.AdapterRegistry`,
`*core.TransitionIDGenerator`, and a raw `WorktreeFactory` func. Anything reached through it is
untestable without the daemon.

Worse, three dependencies exist **twice** across the bundles:
- ledger: `RunPorts.Ledger` (3 methods) *and* `SharedHandles.BrAdapter` (5 methods)
- worktree: `RunPorts.Worktree` (port) *and* `SharedHandles.WorktreeFactory` (raw func)
- launch: `RunPorts.Launch` (port) *and* `RunPorts.LaunchBuilder` (the identical closure, stored twice)

**No-seam #4: `RunPorts` is constructed in three places, one of them inside the consumer.**
`deps.runPorts()` fills 5 fields; `buildRunBundles` fills `Launch` + `LaunchBuilder`; and
**`beadRunOne` itself assigns `rp.Worktree` at line 3743**, because the choice between the local
worktree factory and the SSH one is not knowable until after remote-worker selection. So `RunPorts`
crosses the seam half-valid and the consumer completes it. Any rewrite must either move worker
selection before port construction, or accept that "resources" is a separate phase that *produces*
ports rather than receiving them — which is what C1→C2 ordering says anyway.

**No-seam #5: `workLoopDeps` is not constructed, it is assembled in four stages.**
Production requires `newWorkLoopDeps` → `seedGovernorDeps` → an inline boot-seed →
`injectWorkLoopDeps`, all in `bootworkloop.go`. **~25 of the 81 fields are set after
construction.** There is no point at which the compiler can tell you the bundle is complete. And
`bootworkloop.go`'s `wireStaleWatcherReapSeams` captures a `*workLoopDeps` in a long-lived closure
that a background watchdog dereferences by value — so the bundle outlives the boot function and is
co-owned by two goroutines.

---

## 3. The order

The ordering rule is: **carve out things whose data flows one way and whose absence the compiler can
prove.** Anything requiring a behaviour decision goes late and gets flagged.

### Step 0 — prerequisites (already in the plan, restated because they gate everything)

**Re-measured 2026-07-29: items 1 and 2 are DONE. Only item 3 is outstanding.**

1. ~~Reconcile `origin/integration/phase-reviewloop-20260725` (44 stranded commits).~~ **DONE.** Its
   contents are known, its worthwhile spec clauses were harvested by hand and checked against the code
   rather than taken on trust, and its tip is preserved at
   `origin/salvage/reviewloop-kernels-20260729`, whose tip is byte-identical to the integration tip.
   `NEXT_STEPS.md` item C carries the detail, and the harvest is settled rather than hopeful — the
   v0.9.6 changelog row in `specs/execution-model.md` records that the prose was checked against the
   code before amending, and names what was deliberately left behind. The branch itself still exists
   and is now safe to delete. That is an operator action, not a prerequisite, and it is not a one-liner:
   a worktree is still checked out on that branch at `/private/tmp/harmonik-main-integration-20260725`
   and has to be removed first.
2. ~~Finish the test-mass deletion, including the 4 unit-test files that bind `beadRunOne`'s
   7-parameter signature.~~ **DONE as written, but read the caveat.** Exactly **one** test file calls
   `beadRunOne` today — the shim at `internal/daemon/export_workloop_test.go`. Against one production
   caller, that is the shape §2 Seam A wants: two call sites, the dispatch goroutine and one test shim.

   ⚠ **"Two call sites" reads as more freedom to restructure than you actually have.**
   `internal/daemon/conformance_m4c7_test.go` does not merely mention `beadRunOne` — it parses the
   daemon source, finds the function's declaration, and asserts that a credential guard is a top-level
   statement dominating the launch call. That binds `beadRunOne`'s **internal statement structure**, not
   its signature, so the compiler will not warn you. Its own header records that it already broke once
   when the launch path was collapsed. Expect to update it whenever statements move inside that
   function.
3. **Make the scenario tier merge-blocking.** The rewrite's *only* oracle is
   `internal/daemon/scenario_*` (26 files, 54 test funcs) + `test/scenario/` (11 tests, 27 s). Today
   `.github/workflows/scenario.yml` carries `continue-on-error: true` and neither `check-fast` nor
   `check-short` runs the tier. **You cannot validate a rewrite against a gate that blocks nothing.**
   This is one line of YAML and it is the single highest-leverage item on the list.

### Step 1 — free deletions — ⚠ RETIRED 2026-07-29. Nothing here is both free and outstanding.

Every item was re-checked against the tree. **All five were wrong or already satisfied**, so this step
no longer gates the structural work below and **step 2 is now the first thing to do.**

- `activateFirstPendingGroup` — **already deleted.** Only the `…Locked` variant remains, with callers.
- `beadExplicitlyReopened` + its one test file — **already deleted.** The claim above said two files.
- `RunPorts.LaunchBuilder` — **not a zero-caller duplicate.** The graph path
  (`dot_cascade_core.go`) and the cognition gate (`dot_gate.go`) both read it, and
  `internal/runloop/ports.go` documents it as the raw resolved spec builder that sub-drivers reach
  after the deps drop. Deleting it breaks two callers. The claim was never true.
- The `sandboxOSTmpDirs`, `hk-l5saf` and `hk-f38n` comment blocks — **real knowledge, not
  gravestones. DONE 2026-07-29, and the disposition was not uniform.** See §1d. One fact went to
  `CARRY-FORWARD.md` and its source copy is deleted, one fact was already in a doc and its comment
  stays untouched, and one fact went to `specs/beads-integration.md` §4.7 with a short comment kept at
  the edit site. Only one of the three was in `workloop.go` by the time the work ran, and only one of
  the three ended in a deletion.

**The general lesson, which is the part worth keeping:** decay is the smaller half of the problem here.
Only one entry — `activateFirstPendingGroup` — was true when written and then went stale. The other
four were wrong on the day they were written. `LaunchBuilder` already had two callers. The "two test
files" count was one file. The "42 lines" count was 49, and it did not even match the line range
printed beside it. And calling three blocks of live rationale "where this used to be" comments was a
misreading, not a fact that expired.

So the instruction is not only "re-derive the list before acting on it", though do that too. It is
that a list which says the compiler will prove it invites you to skip reading the thing you are about
to delete. §5 already says plan estimates do not survive contact. A deletion inventory is an estimate,
and it is one that reads as a fact.

### Step 2 — lift the cadenced maintenance out of the poll loop (~300 lines, low risk)

Responsibilities 6–13 of `runWorkLoop` (schedule tick, coordinator reap, disk check, dashboard gate,
eager refill, sentinel observe, sentinel ACT) are already *mostly* in their own files
(`scheduletick.go`, `diskcheck_hksxlb.go`, `dashboardgate.go`, `eagerfill_em063.go`,
`internal/sentinel`). What remains in `runWorkLoop` is the cadence arithmetic and the call.

**Why first:** these have a different lifetime (periodic, not per-bead), a different failure mode
(a failed reap is logged and forgotten), and they are the only part of `runWorkLoop` that is not on
the dispatch critical path. Removing them makes the remaining loop readable.

**What crosses:** a `MaintenanceObservation` = `{disk_low bool, blocked_queues map[string]bool,
governor_signal, halt bool}`. Note the existing `loopMaintenanceState` already isolates exactly the
mutable state this needs.

**Risk:** low. One caveat — sentinel ACT mode can spawn a crew and halt the daemon. That is a
dispatch effect emitted from a maintenance pass, so the observation type must carry `halt`
explicitly rather than the maintenance code calling `exitClean` itself.

### Step 3 — the admission decision — ⚠ SPLIT REQUIRED. Re-measured 2026-07-29.

**Read this correction before the original text below.** Every one of the eight gates was read in the
source. **Four of the step's five claims are false.** Only the ordering claim holds. Do not start this
step from the original description.

**The claim, and what the code says:**

| Claim | Measured |
|---|---|
| All eight are `snapshot → allow\|delay(reason)\|stop` | **False for four.** cross-queue-dedup and attempts-bound(a) write durable state and fire the group-advance effect chain. handler-pause emits an event on its hold path, and on BOTH paths it may clear the dedup map when the pause epoch has advanced. cooldown's arm site mutates its own input. **Four fit the shape** — decision-required, sentinel-queue, local-cap and greenlight — but greenlight does not fit it *over the same snapshot*, which is the real finding. |
| None does I/O except greenlight | **False, and backwards.** handler-pause writes the event log. cross-queue-dedup and two of the three attempts bounds write `queue.json` and emit events. greenlight writes stderr exactly as decision-required and sentinel-queue do, so it is not the I/O exception — local-cap is the only gate that touches nothing, reading an atomic and sleeping. What is true, and is the point worth keeping: greenlight is the one gate that cannot run without a `br show` result in front of it. |
| ~200 lines | **305 code lines** for the eight gate bodies on both dispatch paths plus the helpers the fold must re-own (`heldDedupKey`, `emitHeldEvent`, `pruneHeldDedupOnEpochChange`, `dispatchBlocked`, `markQueueItemFailureReason`), excluding `evaluateGroupAdvanceWithOutcome`. State that region set with any raw-line figure or do not quote one — an earlier draft of this table said "435 raw" and a re-measure of the same regions gives 411. The 305 excludes the **115-code-line** pre-claim `br show` block the sequence is built around, and the step cannot be done without deciding that block's fate. |
| Low risk | **No.** The two gates that cannot be reordered are the concurrency-critical ones. See constraints 4 and 5. |
| The sequence is load-bearing, local-cap before the stamp | **True.** The only claim that survived. |

**Every gate named below lives in `internal/daemon/scheduler.go`**, moved there from `workloop.go` by the
Seam A split. Their own source comments still say `workloop.go`. None of them is a named symbol — each is
an inline statement block inside `runWorkLoop`, and its only greppable handle is its bead-tag comment.

**The eight are not a list, because they straddle a subprocess.** cooldown exists to suppress repeated
`br show` calls at poll cadence, so it must stay BEFORE the pre-claim `br show`. greenlight reads
`preClaimRecord.Labels`, which is the zero value until `ShowBead` returns, so it must stay AFTER it.
Those two requirements are directly contradictory for a single pre-claim predicate list over one
snapshot. Worse, moving greenlight earlier **compiles clean and the gate silently never fires**, because
`preClaimRecord` is declared above the block. That hazard holds for the window between the declaration
and the assignment — above the declaration it would not compile, so the trap is narrower than "anywhere
earlier" and no less real.

**The three attempts bounds, named once so the constraints below can refer to them:** **(a)** the
`Attempts++` / `max_attempts_exceeded` site inside the Phase-3 lock, **(b)** the `hk-pina9` pre-claim
`ShowBead` bound, **(c)** the br-ready `readyPathAttempts` bound.

**Ordering constraints that exist only as the current source order:**

1. cooldown before the pre-claim `br show` — else the subprocess it exists to suppress runs every tick.
2. greenlight after the pre-claim `br show` — else it reads a zero value and never fires, silently.
3. local-cap before the Phase-3 dispatch stamp (`hk-l5saf`). Its safety also depends on `localInFlight`
   not being incremented until the post-claim site, so the fold must not move that increment earlier.
   **These are two clauses and only the first is pinned.** `TestL5saf_LocalOnlyItemNotStrandedByCapGuard`
   catches the gate moving after the stamp, but it cannot catch the increment moving earlier: it preloads
   `localInFlight` to `gateMax`, so the guard reads `1 >= 1`, and hoisting the increment makes it `2 >= 1`
   — the same branch, the item still Pending, the test still green. Nothing in the tree pins the
   increment's position, and that position is the hoist's own safety argument.

   **Correction, 2026-07-30: the first clause was not pinned either, and had not been for as long as
   this machine has been low on disk.** `TestL5saf_LocalOnlyItemNotStrandedByCapGuard` did not stub the
   disk-free reading. The dispatch loop holds a tick when disk is below the watermark, and that hold
   sits BEFORE queue selection, so the fixture never reached the guard. Proven by deleting the guard
   outright: the test stayed GREEN. With a one-line disk stub added and the guard still deleted, it
   goes RED. Both clauses are pinned now — the first by the repaired fixture, the second by
   `TestAdmissionOrder_LocalCapGuardReadsThePreIncrementCount`, which always stubbed the reading and so
   never rotted. The wider finding, that the whole `internal/daemon` package is green on a machine
   above the watermark and that 32 of its 36 loop-driving fixtures share this exposure, is in
   `OPEN-DEFECTS.md`.
4. cross-queue-dedup must run inside the same write-lock hold as the stamp. The lock is what makes the
   winning queue's stamp visible. A pure pre-claim predicate cannot hold it, and without it two
   implementers run one bead again — the bug `hk-a11re` fixed.
5. attempts-bound(a) must stay fused to the stamp. `Attempts++` happens only when the item is found
   Pending under the live lock, so only real stamp attempts consume the budget. A pre-lock predicate
   either double-counts or loses that property.
6. `governor.tick` must run before the sentinel-queue gate in the same tick. This couples Step 3 to
   Step 2: either the maintenance observation carries the trip state, or the gate keeps reading the
   blocker directly. **Step 2 as BUILT keeps the direct read**, through `sentinelBlocksDispatch`, because
   the blocker is a dispatch-time question and a verdict captured during the maintenance pass would be
   answered hundreds of lines before it is used. Step 2's own description above still lists
   `governor_signal` as a field of the crossing type. That field was never added — it has no consumer in
   the loop — so read the code, not that field list.

   **This constraint protects a code shape, not a behavior — established 2026-07-29 by enumerating every
   writer, not by reading a region.** The `sentinel` subject has exactly one steady-state writer,
   `movementGovernor.onTrip`/`onClear`, on the dispatch goroutine, plus one boot-time writer with no
   reload path. `DecisionBlocker`'s mutators are declared by no interface anywhere in the repo — it is
   only ever held as a concrete type, so there is no structural back door — and `internal/sentinel`
   cannot reach it at all, because the import would be a cycle. That is why the subject constant is
   duplicated in the daemon package. So nothing between the maintenance pass and the gate can change what
   the gate returns, and a snapshot would be equivalent to today's live read.

   **With one condition that is the whole point:** `governor.tick` is the LAST statement of
   `tickBeforeSelect`, so a snapshot is equivalent only if taken AFTER it. Taken at the top of the pass,
   the daemon dispatches one extra bead on the tick a trip first fires — the same hazard
   `loopmaintenance.go` already documents for the `halt` field. So the fold may snapshot this, but where
   the snapshot sits is load-bearing.

   This constraint is now **tested**, in `internal/daemon/sentinelgate_test.go`. The seam it needed is one
   field, `WorkLoopDepsParams.GovernorState`, wired straight to `workLoopDeps.governorState`; nil keeps the
   subsystem absent, so no existing fixture changed. ACT mode was not needed — `dispatchBlocked` reduces to
   `decisionBlocker.IsQueueBlocked("sentinel")` and the trip is injected through `AddQueueBlock`. The tests
   pin the gate on BOTH dispatch paths, pin that an absent subsystem does not gate dispatch, and pin the
   ordering clause: a trip armed inside `governor.tick` gates the SAME tick. A snapshot taken at the top of
   `tickBeforeSelect` produces exactly one claim instead of zero, so the fold will hear about it.
7. The two dispatch paths order the same gates differently. The br-ready path puts attempts-bound
   before handler-pause. The queue path has no early attempts bound at all. One merged order therefore
   changes one path: today a ready bead over its budget is skipped with no held event, where the queue
   path would hold it and emit one.
8. `delay` is two different outcomes, and **the counts here were wrong on both sides until 2026-07-29**
   — this entry said twelve sleeping sites and named three no-sleep ones. Re-derived twice, by two
   methods: `runWorkLoop` has **31 outer-loop `continue` statements**, of which **26 wait** and **five do
   not**. The five: the queue bootstrap, the `hk-pina9` pre-claim `ShowBead` bound, cross-queue duplicate,
   the `hk-6pspu` max-attempts **stamp** bound, and the `hk-n91y0` claim-blocked path. Keep the word
   "stamp" — `hk-6pspu` tags two sites and the br-ready one sleeps.

   **The 26 are not one behavior, and a merged `delay` must carry a bound plus a wake-set rather than a
   duration.** The nothing-selected `continue` is a single statement with **three wait shapes**: a
   2-second `workloopSleep` when deferred items remain; a 2-second wait that also selects on the schedule
   channel when an enabled scheduled job is loaded; and `workloopIdleWait` with **no timer** when neither
   holds. Shape 2 is bounded by a flat `time.After(workloopPollInterval)` — the same 2 seconds as shape 1,
   with no next-fire-time arithmetic — so never describe it as waiting until the next scheduled job time.
   Shape 3 is untimed but wake-interruptible: the daemon parks, it does not stall, and putting a timer
   there restores the busy-poll `PL-013` forbids. By timeout semantics there are only two shapes; the
   third appears only when you count select shape. Counting immediate-continue, the loop has **four**
   distinct delay outcomes.

   That same branch has a fourth outcome that is not a wait at all: when no queues are loaded it falls
   through with neither a wait nor a `continue`.

   **Do not merge the two variants into one on the assumption that a merge is only slower.** That holds
   only toward the sleeping variant. The other direction busy-spins the `hk-403fw` cooldown — the
   `bead_claim_skipped` storm the cooldown exists to stop.

   And the sleeping direction is not free either, because the cost is not symmetric across the five.
   `evaluateGroupAdvanceWithOutcome` calls `queueStore.Wake()` unconditionally on its
   not-all-succeeded branch, so four of the five already have a wake token pending and merging them
   toward sleep costs **zero** latency, not one interval. The bootstrap site is the exception:
   `activateFirstPendingGroupLocked` writes through `LockedSetQueueByName`, which does not signal the
   wake channel, so merging **that one** site costs a full poll interval on every queue submit.

   The wake-token fact is now pinned by an executable test with no wall clock in it, so this entry no
   longer rests on reading the code.
9. decision-required and sentinel-queue are freely swappable. The only difference is the stderr string.
   This is the one pair with no real constraint.

**Nothing asserts any of this except one edge.** `TestL5saf_LocalOnlyItemNotStrandedByCapGuard` drives
a real tick and fails if local-cap moves after the stamp. Its own header records that the sibling unit
test missed the bug because it only re-stated the boolean. Five of the eight gates have no test at all —
cooldown, decision-required, sentinel-queue, greenlight and attempts-bound. The freeze gate asserts file
placement and symbol counts, not order.

That test header is itself decaying: the sibling it names, `TestSplitGate_LocalOnlyBypassFix`, was
deleted in the signature-pinning sweep, and the header still places the gate in `workloop.go`.

**And the comments that hold the reasons are already rotten. This rot is ours, and it is one commit old.**
All six line references in the gate region are wrong. Read them as symbols in
`internal/daemon/scheduler.go`: the split capacity gate is the `if` on `deps.localInFlight.Load() >=
gateMax`, the twice-cited reference is the `deps.localInFlight.Add(1)` increment, the third is the
`beadRecord = core.BeadRecord{` construction, and the fourth — also cited twice, and uncorrected until
now — is the queue-path post-claim `ShowBead` label hydration.

The `localInFlight.Add(1)` reference is the load-bearing half of the local-cap hoist argument, so the
only record of why the order is safe points at coordinates that no longer exist. **At the commit that
wrote them, two of these numbers were exact.** The Seam A split moved the code and updated none of them
— which means four of the six were broken by this program's own commit, one commit before a commit
titled "cite the symbol instead of a line number." This is a defect fixable today, not slow decay.

**The eight are also not the set.** The same loop holds **four** more admission gates: the primary split
capacity gate (`hk-hs7ex`), the disk-low gate, the no-auto-pull gate (`hk-exd7m`) and the operator-pause
gate (`hk-ry8q1`). Two more sit outside the loop: the `needs-attention` exclusion at adapter read time in
`internal/brcli/ready.go` beside greenlight, and `HandlerPauseChecker` in `internal/queue/validation.go`
at submit time, which is a sixth handler-pause site. If the goal is "the admission decision lives in one
place", name the real set first.

**Do this instead — split the step:**

- **Step 3a (low risk, worth doing).** Fold the genuinely pure predicates — decision-required,
  sentinel-queue, local-cap, and greenlight once the bead record is in hand — into
  `internal/orchestrator` beside `SelectNextQueue`. Encode the order as data, not scattered `if`s.

  **Re-measured 2026-07-30, and the step's own description repeats Step 3's mistake at smaller scale.**
  Three corrections, each of which changes what the work is:

  **The queue-path figure was exact and the both-paths figure was not.** 42 code lines for the four
  gates on the queue path is right, counted body-only with comments and blanks excluded. The
  both-paths figure was 66. It is **60**. The 6-line gap is the `hk-l5saf` guard counted a second
  time, on a path where it does not exist — **the br-ready path has no local-cap guard at all.** Only
  a vestigial comment on the converged path marks where the old post-stamp version sat. That is
  sound rather than a defect: the guard keys on the per-queue local-only routing field, and a
  br-ready bead comes from no queue, so the field has no meaning there. The br-ready path relies on
  the tick-level split capacity gate alone. Record it so nobody "restores symmetry" later.

  **"local-cap" names two gates, and they share a tick-local.** The split capacity gate
  (`hk-hs7ex`) runs once per tick before the path branch and is silent. The `hk-l5saf` guard runs on
  the queue path only, immediately before the dispatch stamp, and is also silent. The cap they
  compare against is derived ONCE per tick, at the first gate, and read again by the second. Fold one
  without the other and that single derivation splits in two, which changes behaviour whenever the
  concurrency controller is adjusted between the two reads. Counting the derivation and the split
  gate, the real region is **72 code lines**, not 60.

  **Greenlight is queue-path only.** The br-ready path excludes those beads earlier, at adapter read
  time in `internal/brcli/ready.go`, beside the needs-attention exclusion. That is a different
  package and a different moment, and it stays out of this step.

  **So the order cannot be one flat list here either, for the same reason it could not be in Step 3.**
  The four gates sit at four points in a tick: before the path branch, before the pre-claim `br show`,
  after it, and immediately before the dispatch stamp. Two subprocess-and-lock boundaries cut through
  them. The step is therefore to make the **stage** data as well as the order, so that a gate asked to
  run before its inputs exist FAILS LOUDLY. Today the reverse is true, and that is the whole point:
  hoisting greenlight above the `br show` compiles clean, reads a zero value, and the gate silently
  never fires.
- **Step 3b (MEDIUM risk, belongs with Step 4).** cross-queue-dedup and attempts-bound(a) are a
  reservation-transaction problem wearing a gate costume. Fold them into Step 4's single durable write,
  not into an admission list.
- **handler-pause needs its event emission split from its predicate before it can move at all.**
- **cooldown stays where it is.** It is a rate limiter on a subprocess, not an admission gate.
- **Before either half, write the tests that pin the order.** Six of the nine constraints above have
  nothing holding them.

---

**Original text, kept because the correction above is a response to it. It is the only place the eight are
named as one list. Every ⛔ sentence below is REFUTED — do not quote one as current fact.**

Fold the remaining gates (handler-pause, decision-required, sentinel-queue, greenlight, cooldown,
local-cap, cross-queue-dedup, attempts-bound) into `internal/orchestrator` beside the existing
`SelectNextQueue`. ⛔ REFUTED: ~~All of them are `snapshot → allow|delay(reason)|stop`. None of them do
I/O except `greenlight`, which reads a label already present on the pre-claim record.~~ Four fit the
shape, not eight, and greenlight is not the I/O exception.

**Why now:** the pure selector already landed there and works; this is the same shape. ⛔ REFUTED:
~~It is table-testable with no daemon.~~ Two of the eight need the live write lock.

⛔ REFUTED on risk, upheld on sequence: ~~**Risk:** low, but~~ note the *sequence* is load-bearing — the
hoisted `hk-l5saf` local-cap guard must stay **before** the dispatch stamp. Encode it as an ordered list
of predicates, not as scattered `if`s, so the ordering is data.

### Step 4 — the reservation transaction (~150 lines, MEDIUM risk — real fix, not a move)

Collapse the dispatch stamp + RunID patch into one durable write, and make persist failure fatal to
the dispatch (no claim, no launch). Today it is two non-fatal writes with an empty-string placeholder
between them.

**Why here:** it must precede any run-path work, because the run path's recovery semantics
(QM-002a on restart, `adoptLiveRunSession`) depend on what a dispatched item means.

> **D3 — ANSWERED 2026-07-30. A failed reservation write aborts the dispatch: no claim, no launch.**
> Operator: *"No more launching and hoping."* The bead stays pending and the next tick re-picks it,
> which is correct, because it never started. Retry with backoff is a later layer, not the first move —
> a full disk or a corrupt queue file does not clear on retry, and retrying quietly rebuilds the failure
> this decision removes. The error must be loud: an event, a visible marker on `queue list`, and a
> message specific enough that an agent investigates rather than shrugs.

**✅ DONE 2026-07-30 — `internal/daemon/scheduler_reservation.go`.** The dispatch reserves through
`QueueStore.Transact` in one write that sets status and RunID together. A write that does not reach
disk abandons the dispatch: no claim, no launch, item stays pending. Pinned end-to-end by
`TestReservationWriteFailure_NeverClaimsAndNeverLaunches`, which drives the real `runWorkLoop`.

Four things found on the way, none of which the step anticipated:

- **The event the spec demands could not be built.** `queue-model.md` QM-001 names
  `failed_prerequisite: queue_write_error`; `event-model.md`, which owns the event type, listed
  seven values and that was not one. Added to both the owner spec and `internal/core`.
- **QM-001 asks for three things and the code did none.** Refuse further mutations, emit, go
  degraded. `Transact` now quarantines on any I/O-failure outcome — but NOT on a pre-I/O refusal,
  which attempted no write. Getting that wrong broke an existing concurrency test.
- **A quarantined queue read as "retry later" and spun silently.** `queuewiring.ErrQueueQuarantined`
  separates "this queue is shut" from "your snapshot is stale". The report is bounded to once per
  queue or a sticky failure emits every poll interval forever.
- **14 test fixtures used non-UUID queue IDs**, which the spec forbids and the durable write rejects.
  Five tests regressed the moment dispatch used the real write. Fixed, not suppressed.

Still open: D3's marker on `queue list` (`hk-ujanf`). Step 3b's cross-queue-dedup fold landed here
via `TransactionRequest.Precondition`; attempts-bound(a) landed in the same reservation write.

**Original assessment, kept because the work confirmed it:**

**⚠ The premise of this step is stale — re-verified 2026-07-30. Step 4 is mostly WIRING, not writing.**

`QueueStore.Transact` in `internal/queuewiring/store.go` already implements the reservation
transaction, backed by `internal/queue/transaction.go` (1,005 lines). Verified today: **all ten
`.Transact(` call sites are in `queuewiring/store_transaction_test.go`. Production callers: zero.**
The dispatch path uses bare `queue.Persist` — ten calls in `internal/daemon/scheduler.go`.

`Transact` supplies each thing this step was going to build:

- **`Mutate func(*queue.Queue) error`** — one closure sets status *and* RunID together, so the
  empty-string placeholder stops existing. The RunID is a UUIDv7, generated locally from a timestamp
  plus randomness; nothing stops it being generated before the stamp instead of after.
- **Generation guard plus a byte-equality check on the snapshot** — a stale or concurrently-mutated
  queue rejects *before* any I/O. This is the defence against a second writer.
- **Quarantine** — refuses further mutations on a queue that failed a write, which is QM-001's
  "refuse further mutations".
- **`TransactionResult.Outcome`** — a value the caller must read, rather than an error that
  `_ =` can discard.

**What `Transact` deliberately does not do**, per its own comment: *"performs no event emission and has
no completion-receipt behavior."* So `infrastructure_unavailable{failed_prerequisite:
queue_write_error}` is still the caller's job. `internal/queue/persistence.go` says the same thing and
names the wiring point. The payload type exists in `internal/core`. **It is emitted nowhere.** That
emission, not the transaction, is the part of Step 4 that must actually be written.

Do not restate this step as "collapse two writes into one" without first re-running
`grep -rn '\.Transact(' --include='*.go' internal/ cmd/`. If production callers are still zero, the
work is to route `scheduler.go` through the owner that exists and to wire the event — not to design a
new transaction. Background: `UNWIRED-INVENTORY.md` row 5, and the 21-slice plan in
`.kerf/works/queue-transaction-contract/07-tasks.md` with one slice built.

### Step 5 — the run-plan resolver (~350 lines, low risk, high value)

Pull C1 out of `beadRunOne`: workflow mode/ref, harness agent type, model/effort, Pi profile,
active repo, parent commit, lands-on, merge target, protect-branch check, placement intent. All
decided before any resource is acquired.

**⚠ The stated justification below was stale — corrected 2026-07-30.** This step used to be sold as
the fix for the `hk-3hozm` worker-slot leak. **That leak is already fixed.** The bead closed
2026-07-18 in commit `3a391dc9`, which hoisted the release to a top-level `defer` in `beadRunOne`
above every early return. Re-verified today: **zero returns leak a slot.** The motivation survives
unchanged — resolving before acquiring makes that class of bug unrepresentable — but this step is a
structural move, not a bug fix. Do not re-price it as remediation.

**Why here:** it is the largest decidable chunk of `beadRunOne`, and it is already contiguous. The
ten decisions occupy one unbroken block, and **nothing between the top of the function and the
worker-selection fallback acquires a resource.** The seam is real and it is clean.

**"Decidable" is the property, not "pure".** Three of the ten do I/O and must not be dressed up as
pure functions:

- **Parent commit** reads `<repo>/.harmonik/branching.yaml` and forks up to two `git rev-parse`
  calls. It is also *time-varying*: the target branch ref moves as sibling runs merge, so the same
  inputs give different answers minute to minute.
- **Lands-on** reads the same file again.
- **Model and effort** read two process environment variables at call time, deliberately, so an
  operator can retune them without a daemon restart.

What they share is that they are all settled *before anything is acquired*. That is the invariant
worth building on.

**The one structural gain: `resolveBranching` runs twice per run.** Once inside `resolveParentCommit`
(which discards `LandsOn`), then again solely to recover `LandsOn`. The code comment admits the
duplication exists to avoid widening a return type. Both calls stat and may read the same file, and
they can disagree if it changes between them. Collapse them into one call that returns what both
callers need. **The trap:** the first call failing is fatal and reopens the bead; the second failing
is non-fatal and silently skips the whole protect-branch check. Collapsing them naively changes
behaviour. This is the real correctness risk of the step — larger than the transcription risk.

**Risk:** low, provided the precedence tables are transcribed exhaustively. The precedence walks are
subtle (`hk-pkugu`: the model default must be resolved against the harness that will actually be
selected, or a Pi run asks the Pi provider for a Claude model). Write them as tables and test them
as tables.

**Two boundaries this step must not cross:**

- **Placement is not a plan output.** `SelectWorker` decides and reserves inside one mutex hold, on
  purpose. Splitting it re-opens the race it was written to close. The plan carries placement
  *intent* — local-only, worker target — and stops there.
- **The run-level model is not the last word for DOT.** The cascade recomputes an effective harness
  per node and corrects the model against it. Collapsing that into one run-level answer re-opens
  `hk-pkugu` one level down, on the node axis. Leave the cascade alone.

**Deliberately out of scope: the refusal-reporting split.** Seven refuse-before-launch returns call
`ReopenBead` directly and emit no `run_failed`. Five ride `failRun` and do. The split is not
principled — the four earliest refusals have `failRun` in scope and choose not to use it. Unifying
them would *add* events operators do not expect today, so it is a decision, not a cleanup. Record
it; do not fold it into a move.

**Found while mapping, recorded not chased:**

- **A pure check sits behind two acquisitions.** The socket path-length check is a `len()` against a
  platform constant and depends only on the project directory, yet it refuses the run *after* the
  worker slot is reserved and *after* a tunnel port is allocated. It exists because `ssh -N -R` never
  validates its local forward destination, so a too-long path lets the tunnel come up, the readiness
  gate false-green, and every hook connection get swallowed. All true, and none of it needs a worker.
  Folded into this step as its own commit.
- **The bead body is parsed three times per run** — once for the target repo, then once inside each
  of the two `resolveBranching` calls. Three parses, three different fields consumed.
- **The terminal diagnostic's model and harness are read from two different places 1,150 lines
  apart.** The model comes from the resolver. The harness is read back off the *launcher*, not from
  the resolved agent type. Nothing asserts the two agree — which is the exact shape of `hk-pkugu`,
  and it would be invisible in the log. Returning both from the resolver makes the disagreement
  checkable.
- **"Is this run local?" changes value 400 lines in.** The local-slot flag is initialised from the
  caller and then flipped when the fallback turns a local dispatch into a remote one, with a
  compensating decrement and a remote-flag write. Any future claim that placement is "decided once"
  has to reckon with this.

### Step 6 — resource leases (~450 lines, MEDIUM risk)

C2: worker slot, tunnel, worktree, tmux session, hook session, spawn token. Each an acquire
returning a lease with one idempotent release; composed into a scope closed in reverse order.

**Why here:** it depends on step 5 (placement is a plan output) and it is what unblocks step 7 (the
mode boundary needs a complete resource scope to receive).

**Risk:** medium. This is where the CARRY-FORWARD facts bite hardest — tunnel readiness must be a
*connectability* probe not an existence check (pane fact / hk-ege6), tmux window creation must be
externally bounded (tmux fact 3), and every kill must target `-pgid` and probe the group (tmux fact
7). Get these wrong and runs die silently.

### Step 7 — the mode boundary  ✅ **UNBLOCKED. D1 is answered, and half of it already shipped.**

Rewritten 2026-07-30. The old text said this step was blocked on "one `AgentTurn` primitive, or three
drivers?", and priced it at 6,459 lines across four functions. Both numbers are stale and the question
is settled.

**Done already (2026-07-29):** the launch half. `runAgentLaunch` in `internal/daemon/agentlaunch.go`
is the one launch path, and all three remaining call sites use it. A guard added there cannot fail to
reach a mode.

**What step 7 is now.** Two pieces, in order.

1. **Collapse the post-exit interpretation.** The single-mode tail and `dispatchDotAgenticNode` both
   hand-roll probe HEAD → emit `implementer_phase_complete` → process-exit commit fallback → decide
   what no-commit means. They disagree in detail, and the guard table in §2b lists exactly how. This
   is the same shape of work the launch collapse was, on a smaller surface.
2. **Make single-shot a graph and delete the single-mode tail.** A one-implementer graph
   (`start → implement → close`) expresses `workflow:single` exactly. Nothing in the graph engine
   stops it. What stops it today is that five capabilities live only in the single-mode tail and have
   no graph equivalent: the independent tmux session and its restart adoption (`hk-mh3qy`, the real
   blocker), the escaped-worktree guard, `noCommitGuardShouldReopen`, the implementer comms presence
   join and leave, and the Pi provider profile on the launch context (`hk-yo9g6`). Port those onto the
   graph node, then the tail is about 722 deletable lines and `core.WorkflowMode` reduces to one
   value.

Do **not** count `dot_gate.go` in this step. Its cognition-gate launch is unreachable — no code sets
`daemon.Config.CPRegistry` and no graph declares a `type="gate"` node. Decide separately whether to
wire it or delete it. Migrating it costs real work and buys nothing until one of those two things is
true.

### Step 8 — the terminal spine (LAST, and mostly already done)

`beadRunOne` becomes a phase coordinator: resolve → acquire → execute → classify → feed the machine.
The machine (`internal/runexec` + `RunBridge`) already owns gate → merge → close/reopen.

**Why last:** everything depends on it, and it is the only part of the current code that is already
right. Touching it early means re-doing it.

> **⚠ Operator decision (D2) lands here**: whether the guards move into the machine's `Guarding`
> phase (behaviour change for review-loop and DOT) or the machine's escape slot is deleted.

---

## Steps 9 onward — added 2026-07-30. The step is no longer "the inside of a function"

Step 8 ends with `beadRunOne` a phase coordinator. **That is where the plan used to stop, and the core
is not done there.** Steps 0–8 carve up the inside of two functions. Everything below is about what is
*around* them: the bundle that builds a run, the packages that write the queue, and the line between
the core and everything the charter switched off.

**The §3 ordering rule stops working here, and it should be replaced rather than stretched.** "Carve
out what flows one way and whose absence the compiler can prove" needs a compiler that can prove
something. It cannot prove anything about a struct whose fields are written after it is returned, and
that shape is what steps 9–15 are about. The rule from here is: **make each unit's dependency set
declarable, then declare it.**

All numbers below measured 2026-07-30 on `15bfdc154`. Two that the rest of this section rests on:
`internal/daemon` is **42,438 production lines in 95 files**, and **106 files in `internal/` are over
400 lines and hold 49% of the tree's production code in 14% of its files**.

Steps 9–15 are the core. **Step 16 is not** — it is named because the operator asked for it, and it is
flagged as sequel work rather than quietly promoted into the core set.

### Step 9 — the live pass (~2 days, LOW risk, and it is the oracle steps 10–15 do not otherwise have)

**This is not a carve-out.** It is here because every step below changes boot-time construction, and
nothing that runs today exercises a real boot.

**What the scenario tier already covers, and what it cannot. Be precise about this, because the tier
is stronger than "unit tests" and weaker than "end to end", and both mislabels have been used.**
`internal/daemon/scenario_*` (26 files) and `test/scenario/` (5) boot the production composition root
by calling `daemon.Start` in a goroutine. Real `git` and real `br` run as subprocesses. The *agent* does
not: `Config.HandlerBinary` points at a twin and `Config.BrPath` at a wrapper. So the tier is genuine
in-process coverage of construction against a fake agent, and it should stay.

What it never does: **start the daemon as a process, run a real agent, run the supervisor, or cross a
process boundary.** A defect in the shipped binary, in supervisor revival, or in state that must
survive a restart is therefore invisible to it. Note also that `scenario_happypath_n1_test.go` pins
`WorkflowModeSingle`, a mode selected twice in the entire event log.

**The tmux substrate is the sharpest gap, and the tier says so itself.** Three of the 32 scenario files
set `Config.Substrate`; the other 29 leave it nil, and `daemon.Config.Substrate`'s own doc says a nil
substrate falls back to `exec.CommandContext` — no panes. All three that do set one wrap
`NewTmuxSubstrate` around a **fake adapter**, and
`scenario_launch_liveness_slotleak_hk40c3y_test.go` records the consequence in a comment: *"the
fake-substrate path cannot reach `run_completed` (no hook-bridge socket relay → agent_ready_timeout)."*
`scenario_concurrent_dispatch_vn4_hkukhzu_test.go` is skipped unconditionally for the same reason, and
its skip text names what is missing: *"agent_ready/outcome arrive over the socket, not stdout… That is
real tmux + real socket altitude."* So `tmuxsubstrate.go` (3,023 lines), `pasteinject.go` (2,691) and
the whole hook-bridge relay are untested by this tier **by construction, not by neglect** — and the one
constraint that stopped it, not touching real tmux on the shared box, no longer applies with the daemon
down and a scratch clone available.

**Two named tiers that would cover the rest are empty.** `test/crash/crash_stub.go` and
`test/integration/integration_stub.go` are 7-line build-tag stubs holding **zero test functions**, so
`check-full`'s `-tags=crash` sub-run tests nothing. Crash, SIGKILL-mid-merge and restart recovery have
no home today.

**A live pass does not duplicate this tier. It covers the half the tier cannot reach.**

**The apparatus for a real pass is already built and is not being run.** `scripts/scratch-daemon.sh`
(959 lines) starts a second, fully isolated daemon — its own clone, socket, pidfile, tmux session,
binary and bead ledger — with `init / build / up / status / down / cycle / batch / feedback`. `batch`
submits beads and writes a structured pass/fail artifact. `feedback` turns failures into beads in the
main repo. `scripts/core-loop-matrix.sh` (481 lines) drives a matrix of cells through it and folds the
results. `docs/scratch-daemon-runbook.md` documents all of it. It was used once, on 2026-07-29, for the
run recorded in §0-CORRECTION: six graph nodes, a real commit on the target branch, 2m32s.

**It is stale at its own default.** `SCRATCH_WORKFLOW_MODE` defaults to `review-loop`, and the daemon
no longer offers that mode — the flag help in `cmd/harmonik/main.go` now reads "single, dot", and
`core-loop-matrix.sh` already carries a comment working around the same default. The first thing a live
pass needs is not new code. It is a default that names a mode that exists.

**Why here and not at the end.** Putting the live pass last repeats the mistake `NEXT_STEPS.md` §5.1
documents: an oracle that is built and never run reports nothing, and a tier nobody runs is
indistinguishable from a tier that passes. Steps 10–15 change the composition root, the queue's writers
and the subsystem switches — the parts a unit test reaches least and a real boot reaches first. Run the
pass before them and it is a baseline. Run it after and it is an autopsy.

**What a pass is:** repair the default, run one bead end-to-end on the tip, keep the batch artifact,
then re-run it as the acceptance check at the end of each step below. No new tier, no new harness,
nothing to keep in sync.

**Risk:** low. The isolation is already designed for — the script refuses to target the fleet daemon
and kills only the PID named in the scratch pidfile, and only after confirming that process's command
line contains the scratch path. The real cost is agent tokens per run, which is why this is a per-step
check and not a per-commit one.

**"The daemon is off" means less than it sounds.** Measured today: no daemon process is running and no
launchd agent is loaded. Nothing gates it off in code. Turning it back on is a command, not a project.

### Step 10 — the composition root (~800 lines, MEDIUM risk — a design, not a move)

`workLoopDeps` holds **81 fields**, and its declaration alone spans **743 of `workloop.go`'s 3,389
lines** — more than a fifth of the file is one type. It is assembled in four stages across
`bootworkloop.go` (`buildWorkLoopDeps`, `seedGovernorDeps`, `injectWorkLoopDeps`,
`startBackgroundLoops`), with **25 post-construction field writes** there and 3 more in `scheduler.go`.
`bootState` (`internal/daemon/bootstate.go`, 22 fields) has the same shape and admits it in its own doc
comment: early phases write the fields, later phases read them, and a missed hand-off surfaces as a
nil-deref at boot.

**Why here:** §3's "What must NOT move" defers this in so many words — *"Replace it when steps 2–6 have
made most of its fields locally owned, not before."* Steps 2–6 are that work. This step is the licensed
successor to that instruction, and the reason it was deferred rather than dropped.

**Why it blocks done:** `PRINCIPLES.md` §2 asks for consumer-owned ports. An 81-field bundle threaded
through every run means no unit of the core has a declared dependency set, and validity is temporal —
which boot phase are we in — rather than something the compiler checks. It also blocks §6's "switching
one back on is a one-line change", because a subsystem's handle is a nullable field on a shared bundle
instead of its own composition.

**Risk:** medium. The bundle is captured by a background watchdog closure, so a partial cleanup
produces a bundle that is neither the old thing nor the new one — the §3 warning still holds and this
step is the only sanctioned way to discharge it. Deciding which fields belong to which extracted stage
is a design decision, not a transcription.

### Step 11 — one writer for the queue (~400 lines, MEDIUM risk — a design)

`queue.Item.Status`, `Group.Status` and `Queue.Status` are set by **direct field assignment at 27
production sites, in 9 files, across 5 packages** — `internal/queue` (`state.go`, `rpc.go`,
`resume.go`), `internal/queuewiring/operatorevents.go`, `internal/lifecycle/startup_pl005_qm002.go`,
and `internal/daemon` (`scheduler.go`, `scheduler_reservation.go`,
`perqueuespendmeter_tigaf11.go`). `queue.Persist` is called from 9 production files in 5 packages, 7 of
those sites in `scheduler.go` alone. `queuewiring.QueueStore.LockForMutation` has 18 production call
sites in 6 files. A real state machine exists — `AdvanceGroup` in `internal/queue/state.go` — but it is
one writer among many rather than **the** writer.

**Why here:** the charter names both halves of this directly. §6's done criteria include "one explicit
state machine with a single writer", and §4 says the queue is the centre and contested effort goes
there. Step 4 landed the reservation transaction; it did not make the store the sole writer.

**Why it is a design:** collapsing writers decides who may pause a queue and when a write must be
durable. That is the same class of question as D3, and it is the shape RA-2 in `NEXT_STEPS.md` is
complaining about from the other end — a queue that enters `paused-by-failure` automatically and that
nothing in the product can leave.

**Risk:** medium. The invariant is held today by a mutex plus convention, so the failure mode of
getting this wrong is a lost status write that still compiles.

### Step 12 — finish the partition (~600 lines, LOW risk — mostly a move)

`knownSubsystems` in `internal/projectconfig/subsystems.go` holds **8 names**, and all 8 are wired at
their construction seams. Against that, `bootState.wireSpendAndQueueConsumers` and
`bootState.wireWatchersAndObservers` construct and subscribe **9 bus consumers with no switch at all**:
`HandlerPausePolicyGoroutine`, `DaemonSpendMeter`, `PerQueueSpendMeter`, `QueueOperatorEventConsumer`,
`SubscribeHub`, `StaleWatcher`, `ReviewGateAnomalyWatcher`, `QuiesceArbiter`, `CatBL2Handler`. Comms,
crew, captain, keeper, live-state and subscribe — six of the names the charter puts *outside* the core
— have no switch of their own either. They ride `SubsystemSocketListener`, which is one coarse switch
over eleven things rather than a partition. And **8,678 production lines inside `internal/daemon`**
(20% of the package) are non-core by the charter's own list.

**Why here:** §6 defines done as "every other subsystem is switched off by configuration and absent at
runtime". Nine ungated consumers is nine subsystems constructed whatever the config says, so §4's
composability test fails for all nine today. Until this closes, "done" cannot be demonstrated — only
asserted.

**Why it is mostly a move:** the mechanism is right and cheap. `SubsystemsConfig.Enabled`'s own doc
states the rule correctly — gate at the construction seam, never make the subsystem inert — and the
`bandwidth_tuner` gate is a worked example of gating a pair of seams consistently. The gap is coverage,
not design. The design residue is the handful of consumers that hold queue state
(`PerQueueSpendMeter`, `QueueOperatorEventConsumer`): switching those off changes queue behaviour, so
each needs its own decision.

**Risk:** low, with one trap. `unknownYAMLKey` in `internal/projectconfig` early-returns "no unknown
keys" for any node that is not a mapping, so a YAML alias hides a typo'd key inside the `subsystems:`
block. That hole is inherited, not new, and it is now on the surface this step widens.

### Step 13 — the event payloads the core actually emits (~300 lines, MEDIUM risk — a design, and it carries a live defect)

`internal/core` registers **181 event types** with typed payloads, and `daemon.startWithHooks` refuses
to boot if `scanRegisteredPayloadsForSecretFields` finds a secret in one. But **25 `…Payload` structs
live outside `internal/core`**, four of them in `workloop.go` — `workloopRunStartedPayload`,
`workloopRunCompletedPayload`, `beadClosedPayload`, `epicCompletedPayload` — and those, not the
registered types, are what the run path marshals. A further **29 sites marshal a bare `map[string]…`**
as a payload with no type at all.

The divergence is not cosmetic. `core.RunStartedPayload` requires `workflow_id` and
`workflow_version` and has a `Valid()`. `workloopRunStartedPayload` omits both and adds `queue_id`,
`queue_group_index`, `worker_name`, `worker_os` and `workflow_mode`. `run_started` is nonetheless
registered to `&RunStartedPayload{}`, and `internal/replay/runcheckers.go` type-switches on it — so
replay decodes the daemon's real wire bytes into a type that zeroes the fields it expects and drops the
fields that are there. `internal/daemon/runinflightreconcile_hkr73qr.go` reads the same events back
through the shadow struct, so two readers of one event disagree by construction.

**Why here:** this is §4's "consolidate by default" in its purest form — two definitions of one wire
format, both compiling, already drifted. And it blocks §6's "tests that fail when behavior breaks": a
payload change breaks a consumer with nothing red in between. `PRINCIPLES.md` §3 wants record→replay to
be the substrate, and replay is currently decoding into the wrong shape.

**Why it is a design:** the fix is a choice. Either amend the spec'd payload to the shape actually
emitted, or make the run path emit the spec'd shape — which needs a `workflow_id` the single-mode path
does not have. Either way it is a named spec amendment.

**Risk:** medium, and note that the replay mis-decode is a live correctness defect, not only a
structural one. **Recorded, not chased** — it is fixed by this step or not at all.

### Step 14 — declare substrate capability instead of asking for it (~250 lines, MEDIUM risk — a design)

`handler.Substrate` has **one** method. Around it, `internal/daemon/tmuxsubstrate.go` declares **16
capability interfaces** — `substrateWithAdapter`, `substrateWithSessionName`, `substrateWithKeepalive`,
`substrateWithSpawnCap`, `substrateWithSpawnCapSetter`, `substrateSpawnReadier`,
`substrateDiagnosticHookSetter`, `paneTargeter`, `paneCaptureAdapter`, `pasteInjecter`,
`sessionCreator`, `sessionEnsurer`, `runnerSwapper`, `crewSessionSpawner`, `crewSessionStopper`,
`runSessionSpawner` — resolved at **21 production type-assertion sites** across eleven files. The event
bus has the same shape at smaller scale: `EventBus` plus four extension interfaces probed at 6 sites.

**Read the contrast, or this gets mis-applied.** The 13 one-method `Emit` interfaces re-declared across
`eventbus`, `lifecycle`, `handlercontract`, `queue`, `brcli` and `daemon` are **not** a tangle — that is
`PRINCIPLES.md` §2 working as designed, and `runloop.EmitterPort` is the documented idiom. The
difference is direction. A consumer narrowing a dependency is the principle. A consumer interrogating
an implementation to find out what it can do is the defect.

**Why here:** the core set names "harness registry + one substrate". A capability that is discovered
rather than declared cannot be switched off honestly — a missing capability fails as a silent
`ok == false` branch, which is the charter §3 prohibition on faking a degraded capability, applied to
tmux. It is also what makes a second substrate expensive: a new implementation has to satisfy 16
undeclared contracts to behave like the first.

**Risk:** medium. Every one of the 21 sites has a fallback branch today, and some of those fallbacks
are the only thing keeping a non-tmux path alive.

### Step 15 — split `internal/daemon` (~large, LOW risk — a move, and it is the last one)

42,438 production lines, 95 non-test files, 183 test files, and a fan-out of **52 internal packages**.
It holds the scheduler, the run driver, the tmux substrate, the paste-inject watchdogs, the DOT
cascade, the boot composition root — *and* comms, crew, dashboard, decisions, subscribe, quiesce,
handler-pause, spend metering and the schedule tick.

**Why last:** the package boundary is what steps 10 and 12 produce, not a prerequisite for them.
Splitting first means guessing the boundary and moving the files twice. `internal/runloop` (2,412
lines, depguard-fenced against importing `daemon`) is the proof the split works — it stopped at the
port surface because nothing had yet made the fields locally owned.

**Why it blocks done:** §4's "segment, then stitch — not entangled-but-documented-as-separate, which is
the current state." Everything in this package can reach everything else's unexported identifiers,
which is the single fact that makes steps 10, 12 and 14 possible in the first place.

**Risk:** low by then, and high if attempted early. That asymmetry is the whole reason it is last.

### Step 16 — the keeper — SEQUEL WORK, and it is separable from crew

**The operator asked for the keeper to be pulled into the program, and suspected it could not come
without the crew system. The second half is not true.** Measured, not argued:

- `internal/keeper` imports exactly five internal packages: `core`, `substrate`, `presence`,
  `dashboard`, `digest`. It does **not** import crew. `.golangci.yml` already allow-lists those five
  and denies `internal/daemon`.
- `internal/crew` imports only `internal/core`. `internal/crewrun` imports `crew`, `handler`, `queue`.
  Neither imports keeper.
- The one edge that touches crew is transitive and accidental:
  `internal/keeper/dashboardnag.go` calls `digest.LoadDashboardGateConfig`, and `internal/digest`
  separately calls `crew.List` from an unrelated file. `LoadDashboardGateConfig`'s own file imports
  nothing but the standard library and a YAML package. Move that one reader to a leaf and the edge is
  gone — which is the same work `NEXT_STEPS.md` RA-6 already decided on for other reasons.
- `specs/session-keeper.md` contains the word "crew" **zero** times.
- Only four daemon files reach into keeper at all, and every call is read-only or a name resolver:
  `crewstart.go` (`LiveKeeperPresent`), `quiesce.go` (`ResolveTmuxTarget`, `HarmonikSessionName`),
  `statedisk.go` and `stategather.go` (`ReadCtxFile`, `ReadSessionIDFile`). The crew one is already
  behind an injectable seam.

**What actually couples keeper and crew is a file protocol, not code:** the markers under
`.harmonik/keeper/` (`.managed`, the flock `.lock`, `.ctx`, `.sid`, `.idle`, `.dispatching`,
`.hold.<sid>`) plus the tmux session and window names. Freeze that set and the two can be worked
independently, in either order.

**What the workstream would have to include:** `internal/keeper` (8,163 production lines, 18 files),
its CLI (`keeper_cmd.go`, `keeper_enable_doctor_cmd.go`, `resolve_keeper_config.go` — about 3,300
lines, and where the staged config assembly actually happens), and the four embedded shell hooks (412
lines) that write the gauge the Go watcher only reads. The named tangles are `WatcherConfig` (**63
fields**, mutated in place by `applyDefaults`), `CyclerConfig` (**56 fields**, about 20 of them
function-pointer seams that duplicate the `ports.go` interfaces — two parallel seam systems over the
same behaviour), and `(*Watcher).Run` at **531 lines** inside an otherwise well-decomposed file.
`internal/keepertwin` and `internal/keepertest` bind to the pure reactor in `step.go` and are the
safety net that makes this affordable.

**Why it is marked sequel work and not folded into the core.** `CHARTER.md` §3 puts the keeper outside
the core set by operator decision, and §6 says nothing else is required to declare the core done.
Scheduling it before step 15 would be adding to the core set, which §3 says needs a reason. **The
evidence above is the reason it is now cheap and safe to schedule — it is not a reason to schedule it
early.** If the operator wants it moved ahead of step 15, that is a charter change, and it should be
made as one.

**One thing worth taking from the keeper before then, in the other direction.** `step.go` / `cycle.go` /
`shell.go` / `ports.go` are a working functional-core-and-shell split, and four files in
`internal/daemon` and `internal/runloop` already carry comments naming `internal/keeper/ports.go` as
the idiom they mirror. `PRINCIPLES.md` §8 says to find the subsystem that already embodies the target
and make the rest of the tree look like it. **That subsystem is the keeper.** Steps 10 and 14 should
read it before designing anything.

### The shape of what is left, in one table

| Step | What it is | Size | Risk | Depends on | Move or design |
|---|---|---|---|---|---|
| 9 | Repair the scratch-daemon default and make a live pass the per-step acceptance check | ~2 days | low | nothing | neither — it is the oracle |
| 10 | Replace the 81-field `workLoopDeps` and the 22-field `bootState` with constructed units | ~800 lines | medium | steps 2–6 | **design** |
| 11 | Make the queue store the only writer of queue status | ~400 lines | medium | step 4 | **design** |
| 12 | Give the nine ungated bus consumers and the six unswitched subsystems their own switches | ~600 lines | low | step 10 | move, with 2 decisions |
| 13 | One definition per event payload, and make replay decode what the core emits | ~300 lines | medium | nothing | **design** + a spec amendment |
| 14 | Replace 16 substrate capability assertions with a declared contract | ~250 lines | medium | step 10 | **design** |
| 15 | Split `internal/daemon` along the boundary steps 10 and 12 produce | large | low | steps 10, 12 | move |
| 16 | The keeper — separable from crew, outside the charter's core set | ~11,500 lines | medium | nothing technical | move + design |

**Honest read on how much is left:** steps 9–15 are on the order of 2,400 lines of production change
over a 42,000-line package, and three of the seven are designs rather than transcriptions. The line
count is not the cost. **Steps 10, 11, 13 and 14 each need a decision before they can start**, and this
program's record is that it runs out of decisions rather than lines. D1 is the case in point in both
directions: it blocked step 7 from the day the map was written until 2026-07-30, and when it was
finally read against the code the answer turned out to be already built. **Ask whether a decision is
still open before waiting on it.**

### What must NOT move, and why

- **`workLoopDeps` field-by-field.** Do not "clean up" the 81-field bundle in place. It is assembled
  in four stages across `bootworkloop.go` and captured by a background watchdog closure; a partial
  cleanup produces a bundle that is neither the old thing nor the new one. Replace it when steps 2–6
  have made most of its fields locally owned, not before.
- **`WorkLoopDepsParams` (47 exported fields) and the 20 `export_*_test.go` files.** Do not attempt to
  preserve this fixture. The field count read 48 here. The struct spans lines 36–373 of
  `export_workloopdeps_test.go` with no embedded and no multi-name fields, and it holds 47.

  ⚠ **The 157-file blast radius is stale by 4.6x. Re-measured 2026-07-29: it is 34 test files.** The
  header of `export_workloopdeps_test.go` still says 157, and that number was written before the
  test-mass deletion removed ~225,000 lines. Counted today, 34 of the 183 test files in
  `internal/daemon` reference `WorkLoopDepsParams` or any of the four exported shims. Those five
  symbols are the whole surface, so the number cannot go up. The file-count claim in that header is
  now wrong and should be corrected when someone next edits it.

  **This changes a decision, not just a number.** 157 files was the stated reason a signature change
  was too expensive to contemplate and had to wait for the deletion. The deletion has happened, and
  the cost is now a fifth of what this section priced. Re-price the change before deferring it again.

---

## 4. What is actually load-bearing

### 4a. Must survive verbatim — external-world knowledge, cross-referenced to CARRY-FORWARD

**Timing constants.** Every one of these is CARRY-FORWARD fact 18 ("each set by a production
false-kill, several twice"). They currently live as **mutable package `var`s** in
`internal/daemon/pasteinject.go` — which is the single biggest rewrite blocker, because tests mutate
them in place:

`commitPollTimeout` (30 m), `commitHardCeiling` (90 m), `heartbeatStalenessThreshold` (8 m),
`launchHeartbeatTimeout` (180 s), `launchSuppressionCeiling` (12 m), `implementerReseedGrace` (75 s),
`postQuitKillGrace` (60 s), `noChangeKillDelay` (30 s), `briefDeliveredTimeout` (2 m),
`reviewFileTimeout` (10 m), `reviewFileHardCeiling` (60 m), `reviewerHeartbeatActiveGrace` (10 m),
`reviewFilePerKLineBudget` (10 m per 1000 lines), `reviewFilePollInterval` (2 s).

And in `internal/runlaunch/deadlines.go`: `DefaultAgentReadyTimeout` (150 s — raised 30→90→150 per
tmux fact 15), `DefaultRemoteAgentReadyTimeout` (210 s), `KillReapTimeout` (10 s).

**Keep the values. Change the mechanism**: one config struct, constructed once, threaded. That is
exactly what `_plan.md` §6 says about pasteinject's 23 globals, and it is right.

**Verification-by-observation, never by return code** (CARRY-FORWARD patterns 1 & 2). Named
symbols that implement it and must not be simplified back to a return-code check:

| Symbol | What it proves | CARRY-FORWARD fact |
|---|---|---|
| `verifySandboxEngaged` + `srtEngagementCanaryPath` | srt exits 0 without applying under fork saturation | hk-5wdon/hk-tch4t |
| `tunnelpkg.WaitWorkerSocketLive` | endpoint is *connectable*, not merely present | pane/tmux — hk-ege6 |
| `lifecycle.ValidateSocketPathLength` | `ssh -R` never validates the local forward until traffic flows | hk-ta6dg |
| `codex.NoWorkSuspected` (duration floor **AND** no-commit, conjoined) | exit 0 in 3.3–5.2 s with clean worktree = no work | codex fact 13 |
| `codex.EnsureRefsTrailer` / `pi.EnsureRefsTrailer` decision table | sandbox blocks codex's own `git commit`; codex exits 0 anyway | codex fact 3 |
| `noCommitGuardShouldReopen` | asks "did **this bead** land?" via `MainHistoryHasRefsTrailer`, not "did main move?" | hk-4ie1z |
| `runmerge.SnapshotUntrackedFiles` → `CheckMainWorkingTreeDirty` | escape detection needs a pre-run baseline | hk-ooexj |
| `pasteInjectQuitOnCommit` + `noChangeTimeoutCh` | Claude's Stop hook fires on session exit only | pane fact 13 |
| `implementerReseedGrace` late re-seed Enter | the TUI swallows keystrokes while absorbing a paste | pane facts 7, 8 |

**Concurrency and resource invariants.** Each of these is a fix for a live incident:

- `tmuxpkg.SSHRunner{Opts: ["-o","ControlMaster=no","-o","ControlPath=none"]}` — pane fact 19. Set at
  **both** worker-selection sites in `beadRunOne`.
- `agentSpawnSem`, cap 3, remote-only, released on agent_ready — bounds concurrent cold starts.
- `worktreeCreateMu` around the *whole* `git worktree add` retry loop — retrying inside a race does
  not help when the siblings are also racing.
- `mergeq` (`internal/mergeq`) as an explicit FIFO exclusion domain replacing a mutex, with the
  ownership rule that `runWorkLoop` creates-and-cancels but never re-Starts an injected queue
  (double-Start = two owners draining one channel + double `close(done)` panic).
- `cacheReapMu` RWMutex: reaper takes W for the whole `go clean -cache`, Register takes R.
- The **`-pgid` kill + probe-the-group** prescription (tmux fact 7) — "branching on a regime you
  cannot detect is the actual hazard."
- `strandedBeadHasOnDiskRun` returns **`true` on a list error** — race-conservative by design.
- `shutdownDrainTimeout` = 10 s bounded drain — without it, N concurrent beads × ~15 s each hangs
  SIGTERM.
- `closeBeadWithHistoryTrim` trimming `.beads/.br_history` before every close — br fact 9 (25 GiB
  across 15,072 snapshots filled a disk) and br fact 15 (write latency scales with ledger size).
- `queuePreClaimShowAttempts` kept **separate** from the item's persisted `Attempts` — sharing the
  budget means two transient `br` blips leave a bead with one real dispatch attempt.
- `claimSkipInProgressCooldown` (5 min) — suppresses a ~2.5 s spin loop emitting hundreds of
  `bead_claim_skipped` events.
- `d2RemoteAPIKeyRefusal` evaluated **after every spec mutation, at the launch boundary** — the
  2026-05-30 credential-leak incident.

**Structural things that are right and should be copied, not rebuilt:**
- `internal/runexec` — the pure Run state machine. The best code in the run path.
- `internal/orchestrator/select.go` — the pure queue selector with the non-resetting round-robin
  cursor (resetting it starves lexicographically-later queues).
- `internal/mergeq` — the explicit exclusion domain.
- `internal/queue/transaction.go` — named in `_plan.md` §4 as surviving; it is the model for step 4.

### 4b. Scar tissue — do not reproduce

- **Three `ShowBead` calls per queue dispatch** with three different retry semantics. One pre-claim
  read is enough; the label-hydration second read exists only because `br ready --format json` omits
  labels, which is a `br` adapter concern.
- **Two dispatch paths** (queue and br-ready) with duplicated pause/decision/attempt gates written
  twice, ~110 lines of near-copy. br-ready is a backward-compat fallback for tests.
- **The placeholder-RunID two-persist reservation** (§2 Seam B). It is a bug, not a behaviour.
- **`workLoopDeps` passed by value** into every run goroutine, with a comment explaining that value
  fields mutated there are silent no-ops. Do not carry the bundle; carry the plan.
- **The nil-means-production-default pattern** on 5 of the 9 func-typed fields
  (`launchSpecBuilder`, `worktreeFactory`, `diskFreeBytesFunc`, `goCacheCleanFunc`,
  `worktreeReclaimFunc`). `buildRunBundles` documents the hazard directly: if a test injects
  `launchSpecBuilder`, harness routing is bypassed and a codex-labelled bead silently runs under the
  Claude builder. Production defaults belong in the constructor, not in `if x == nil` at the use site.
- **`RunPorts.LaunchBuilder`** — the same closure stored twice, described in its own comment as
  replacing "the by-value raw-field smuggle."
- **`SharedHandles` as an explicit non-seam.** Ten concrete pointers and raw sync primitives crossing
  a boundary that documents itself as not being one.
- **`activateFirstPendingGroup` and `beadExplicitlyReopened`** — dead, kept alive by tests
  (the second one *admits* this in its doc comment).
- **19 `//nolint` directives**, including the `//nolint:funlen,gocognit,cyclop` on `beadRunOne`
  itself. Per `NEXT_STEPS.md` §2.5 this suppression is why the complexity ratchet has never once
  fired on the largest function in the repo — it was **born over the ceiling at 119 lines** and is
  2,289 today.
- **3,103 lines of comment, 256 bead references.** Comments explaining what code used to be there,
  what a removed check did, and which of 256 tickets caused which line. In a rewrite this is the
  commit log's job.

---

## 5. The other rot

Ranked. "Rot" = size × churn × longest-function × distinct responsibilities × mutable globals.
`workloop.go` is rank 0 and excluded.

⚠ **Re-measured 2026-07-30.** Rank 1 no longer exists: `internal/daemon/reviewloop.go` was deleted on
2026-07-28. Rank 2's numbers moved when its helpers split out and the launch step left it. The rest of
the table is unverified at HEAD and every LOC figure in it should be re-read as of its 2026-07-28
measurement date, not today.

| # | File | LOC | Commits | Longest function | Mutable globals | External world |
|---|---|---:|---:|---|---:|---|
| ~~1~~ | ~~`internal/daemon/reviewloop.go`~~ | ~~2,194~~ | ~~116~~ | ~~`runReviewLoop` **1,586**~~ | ~~1~~ | **DELETED 2026-07-28 (`3cec5afd7`)** |
| 2 | `internal/daemon/dot_cascade_core.go` | 1,653 at HEAD (was 1,989) | 123† | `driveDotWorkflow` **997** (+`dispatchDotAgenticNode` **543**, was 882 before the launch collapse) | 0 | tmux, claude, codex, git, ssh, fs, bus |
| 3 | `internal/daemon/pasteinject.go` | 2,691 | 56 | `pasteInjectQuitOnCommit` ~575 | **23** | tmux, ssh, raw `exec.Command` (pgrep/ps/git), git, fs, bus |
| 4 | `internal/daemon/tmuxsubstrate.go` | 3,023 | 67 | `spawnWindowVia` 194 (76 funcs) | 1 | tmux (385 refs), ssh, `syscall.Kill` |
| 5 | `internal/keeper/watcher.go` | 2,110 | 78 | `(*Watcher).Run` 531 | 1 | fs, tmux, `exec.Command`, events.jsonl |
| 6 | `internal/eventbus/busimpl.go` | 1,647 | — | max 118 | — | fs (JSONL), 7 `go func` fan-out sites |
| 7 | `cmd/harmonik/main.go` | 1,517 | 128 | `run` 1,298 | 0 | ~50 verbs, all delegated |
| 8 | `internal/daemon/stalewatch.go` | 1,189 | 15 | `(*StaleWatcher).checkRun` 347 | 6 | git rev-parse, process liveness, bus |

† `dot_cascade_core.go` shows 2 direct commits because it was recently split out of `dot_cascade.go`;
`--follow` gives 123.

Runners-up: `internal/daemon/dot_gate.go` (749 at HEAD, was 888; `executeCognitionGate` 234, was 367
— and it cannot execute in production, see §2b),
`cmd/harmonik/comms.go` (2,212, `runCommsRecvFollowIO` 300, six hand-rolled socket dial sites),
`cmd/harmonik/run.go` (942, `runBeadSubcommandIO` 701).

### Genuinely part of the rotten center (ranks 1–3)

**Rewritten 2026-07-30.** This paragraph used to read: "#1 `reviewloop.go` and #2
`dot_cascade_core.go` are the same disease as `workloop.go`, not neighbours of it … These three files
must be rewritten as one unit or not at all." One of the three is gone, so the sentence cannot stand
as written. Its conclusion survives, narrowed to two files: **`workloop.go` and `dot_cascade_core.go`
are one unit.** The launch collapse already treated them that way and proved the point — a change
made in `agentlaunch.go` reaches both. `dot_cascade_core.go` still has a bad concentration: two
functions are about 93% of the file, with no intermediate decomposition.

**#3 `pasteinject.go` is the hardest to move**, despite being smaller. Its 23 mutable package
globals are the empirical budgets from CARRY-FORWARD fact 18, every one of them a test seam mutated
in place. `_plan.md` §6 already has the right answer (split into `paneinject` / `agentwatchdog` /
`paneprobe`, replace the globals with a config struct, drive from fakes). The three raw
`exec.Command` calls that still bypass `CommandRunner` (`pgrep`, `ps`, `git`) are the bug class that
already cost 5 commits and are **still open**.

### Merely large, not rotten

- **#4 `tmuxsubstrate.go`** — biggest daemon file after workloop, but 76 functions and a 194-line
  max. Six cohesive things stapled together (resizable semaphore, spawn-cap admission, bounded window
  spawn, three session-naming schemes, `perRunSubstrate`, kill-with-grace local+ssh). Rot is at the
  *file* level; the seams are visible. Split it, don't rewrite it.
- **#5 `keeper/watcher.go`** — one 531-line function inside an otherwise well-decomposed file
  (33 other functions, all small). Fix the one function.
- **#6 `eventbus/busimpl.go`** — evenly decomposed, max 118 lines, one responsibility. The audit's
  concern (goroutine-per-delivery, no bounded pool) is a *contract* defect, not a structural one.
- **#7 `cmd/harmonik/main.go`** — 1,298-line flat verb switch, zero state, zero globals, hottest
  file in the repo after workloop because every new verb lands there. Cheap to split; not
  architecturally rotten. The 2026-07-24 audit's adversarial pass reached the same conclusion and
  downgraded it. **Agreed — do not put this on the rewrite list.**
- **`internal/daemon/daemon.go`** — 175 commits (2nd-highest churn in the repo) but only ~292 code
  lines (73% comment). It is the composition root; its churn is structural, not decay.
- Also checked and cleared: `internal/core/eventtype.go` (1,463 LOC, 237 code lines, one 3-line
  function — a documented const table), `internal/codexwire/codexwire.go` (1,543 LOC, 8 commits, max
  func 51), `internal/projectconfig/projectconfig.go` (2,144 LOC, flat parse-and-validate),
  `internal/queue/rpc.go` (1,662 LOC, max func 187).

---

## 6. Reconciliation with the 2026-07-24 code-health audit

The audit's *process* is dead. Its *evidence* holds up well.

### Corroborated — independently reached the same conclusion

| Audit claim | My reading | Verdict |
|---|---|---|
| Run graph is 105/105 P0, the top hotspot | Same | **Corroborated** |
| `runWorkLoop` ~1,667 lines, cyclomatic 266, cognitive 888 | 1,670 lines | **Corroborated** |
| `beadRunOne` ~2,288, cyclomatic 217, cognitive 396 | 2,289 | **Corroborated** |
| `runReviewLoop` ~1,582, cyclomatic 136 | 1,586 | **Corroborated** |
| DOT drivers ~883–1,001 | 882 / 1,002 | **Corroborated** |
| `workLoopDeps` is a service locator "assembled in stages; validity is temporal not compiler-enforced" | Four-stage assembly across `bootworkloop.go`; ~25 of 81 fields set post-construction | **Corroborated, and worse than stated** |
| "`runPorts()` deliberately returns required fields nil and a later assembly step fills them" | Confirmed — and the *final* step is inside `beadRunOne` itself (`rp.Worktree`, line 3743) | **Corroborated, and worse than stated** |
| Its proposed phase decomposition: Resolve → Prepare → Launch/Execute → Finalize → Terminal (`BR-00`…`BR-04`) | Independently arrived at the same four sub-seams (C1–C4) | **Strongly corroborated** — two independent passes, same seams |
| Its outer-loop decomposition: maintenance → dispatch-permission → dispatch-source → reservation → spawn (`WL-02A`…`WL-04`) | Same as my steps 2, 3, 4 | **Strongly corroborated** |
| Queue dispatch "persisted `dispatched` without a Run ID, treated durable-write failures as nonfatal, patched the Run ID in a second nonfatal persist, then claimed the Bead" | Confirmed verbatim in `runWorkLoop` | **Corroborated — and still unfixed** |
| `tmuxsubstrate.go` P1: large, many lock domains, but splittable | Same — merely large | **Corroborated** |
| CLI router (`main.run`) downgraded: "less cognitively coupled than its raw cyclomatic number suggests" | Same | **Corroborated** |

### Disagreements — trust the current reading

1. **`workLoopDeps` field count: audit says 86, actual is 81.** Five fields were removed (including
   the `lastCoordinatorReap`/`lastDiskCheck`/`diskLow` lift to `loopMaintenanceState`). Minor, but
   it means the bundle *did* shrink slightly and the audit's number should not be quoted forward.

2. **`RunEnv` field count: audit says 30, actual is 31.** Trivial drift; noted only because the
   audit used it as a baseline metric that later cards ("meet the exact `ARCH-01` metric target")
   were meant to be scored against.

3. **The dot_cascade atom.** The handoff records the audit estimating this atom at ~1,300/~1,600 LOC;
   the real indivisible atom is **1,989** (`dot_cascade_core.go`), of which 1,884 lines are two
   functions. The audit's *per-function* numbers (883/1,001) were accurate — whoever aggregated them
   into an "atom" estimate under-counted by 20–35%. **Any sizing derived from that estimate is
   wrong.** Treat 1,989 as the floor.

4. **The audit's framing is extraction-in-place, and that framing is now retired.** Cards `WL-02A`
   through `WL-04` and `BR-01` through `BR-04` all say "extract the X region of `workloop.go`" under
   an exclusive file lease, with a "sole `dispatch_spine` writer" serialization constraint. The
   2026-07-27 decision is that decomposition-in-place cannot save this code. **The seam *locations*
   in those cards remain valid and are the best independent corroboration available. The execution
   model — leases, worktrees, coordinator-owned index, `sol_xhigh`/`terra_high` staffing — is dead.**

5. **The audit relied on characterization harnesses (`WL-01`, `RL-01`) as the fidelity oracle.**
   Those were to be built from bead-named test files, most of which are now being deleted. The
   rewrite's oracle is the `-tags=scenario` tier, not a characterization harness. This is a real
   change of method, not a refinement.

### Invalidated since the audit

- **`internal/daemon/dot_cascade.go` no longer exists.** It was split into `dot_cascade_core.go`
  (1,989) + `dot_cascade_helpers.go` (956). Every audit reference to `dot_cascade.go` needs
  re-pointing, and the split did **not** reduce the atom — it moved helpers out and left the two
  giant functions intact.
- **Audit finding #4 (supervisor `Stop` double-close panic) is FIXED.** `Supervisor.Stop` now uses
  `s.stopOnce.Do(func() { close(s.stopCh) })`. Do not carry this forward as an open defect.
- **`RunEnv`/`RunPorts`/`SharedHandles` field counts have drifted**, so the audit's "baseline and
  reject growth in run-bundle fields" gate (recommendation 12) has no valid baseline.

### Flagged by the audit, still unactioned

- **Reservation transaction** (torn dispatched/RunID write, non-fatal persists) — open. This is my
  step 4.
- **`internal/lifecycle/branchtip_em024a.go` `WritePersistedTip` uses plain `os.WriteFile`** — no
  temp+rename, no fsync. The rewind detector therefore fails *open* after a crash or disk-full,
  which is precisely when it is needed. Confirmed still present.
- **`internal/schedule/store.go` `SuspendAllForSleep` / `writeSuspendedSet`** — disables jobs
  durably, releases the lock, then separately `os.WriteFile`s the restore set. A crash between the
  two loses the pre-sleep enabled set with no way to reconstruct it. Confirmed still present (the
  file has an atomic `os.Rename` path elsewhere at line 651 — it just isn't used here).
- **`internal/workspace/mergedispatch_wm018a.go` `DetectSquashMergeConflict`** — runs a real
  `git merge --squash` followed by unconditional `git reset --hard HEAD`, with no deadline, no
  cleanliness precondition, and no ownership lock. **Zero production callers, tests only.** Confirmed
  still present. This is a latent data-loss API sitting in the tree; it belongs in the dead-code
  deletion pass, not on a fix list.
- **`internal/eventbus/busimpl.go`** — 1,647 LOC, 7 `go func` fan-out sites, no bounded worker pool.
  Unactioned.
- **Remote process-tree cancellation** (audit #2): killing the local SSH client leaves the worker-side
  `/bin/sh -lc` and its build/test tree running. Nothing in the current plan addresses this, and
  CARRY-FORWARD tmux fact 11 ("a remote worker's pane PID does not exist in the local process table")
  is the same boundary. **A rewrite of `workloop.go` does not fix it** — it lives in
  `internal/handler/session.go` and `internal/lifecycle/tmux/runner.go`.
- **The blanket `_test\.go$` exclusion on `funlen`/`cyclop`/`gocognit`** — ~488k lines of test code
  with no complexity ceiling at all. `NEXT_STEPS.md` §2.5 repeats this. Unactioned.

---

## 7. Decisions the operator must make

| # | Decision | Blocks | Why it cannot be delegated |
|---|---|---|---|
| **D1** | ✅ **ANSWERED 2026-07-30 — collapse the duplication (option A).** See the note below for what option A turned out to mean. | Nothing. Step 7 is unblocked | — |
| **D2** | **Do the escape check and no-commit guard belong to the Run machine or to the caller?** | Rewrite step 8 | Moving them into the machine *changes behaviour* — DOT runs that pass today would start being guarded, and DOT is the default. Deleting `ActCheckEscape` admits the machine's `Guarding` phase is decorative. |
| **D3** | **Is a failed queue-reservation persist fatal to the dispatch?** | Rewrite step 4 | Correct answer is yes (no claim, no launch). But under disk pressure it converts silent inconsistency into visible dispatch stall. |
| **D4** | ✅ **MOOT 2026-07-30.** It asked whether `reviewloop.go` and `dot_cascade_core.go` were in scope with `workloop.go`. `reviewloop.go` was deleted on 2026-07-28, and the launch step of the other two was collapsed into `agentlaunch.go` on 2026-07-29, so the question answered itself by events. The surviving half of it — "are the run driver and the graph cascade one unit?" — is yes, and step 7 above now states it directly. | Nothing | — |
| **D5** | **Does the scenario tier become merge-blocking before the rewrite starts?** | Everything | It is one line of YAML. Without it there is no oracle. It goes red immediately (the `hk-zobns` branch-protection guard fails open) — which is the point, but it is a visible red build the operator has to accept. |

---

## 8. One-paragraph answer — REWRITTEN 2026-07-30

The remaining work is **not** "design one primitive that all modes call". That primitive exists and is
in production. `runAgentLaunch` (`internal/daemon/agentlaunch.go`, 836 lines) owns spawn, liveness
proof, drive-to-dead-session and exit facts, and a CI gate
(`scripts/readywait-freeze-gate.sh`) pins the invariant mechanically: exactly one
`runloop.DispatchSegment` may be constructed in the tree, and it must be in `agentlaunch.go`. There
are two workflow modes left, not three, and one of them is a rounding error — across the whole live
event log (2,153 runs through 2026-07-22) `workflow_mode` reads `dot` 5,890 times, the retired
`review-loop` 1,635 times, and `single` **twice**. The remaining work is therefore the graph program:
finish moving the last shape onto the graph and delete the other. Two things stand in the way. First,
"what the exit means" is still written twice — the single-mode tail and `dispatchDotAgenticNode` each
hand-roll probe-HEAD, `implementer_phase_complete`, the process-exit commit fallback and the
no-commit decision, and they disagree in detail. Second, and worse, the drift now runs the wrong way:
four steps live only on the path used twice ever and are missing from the path that carries
everything — the Pi provider profile on the launch context (`hk-yo9g6`), implementer comms presence,
Pi stderr capture, and the independent tmux session that lets a run survive a daemon restart
(`hk-mh3qy`). Close those, express single-shot as a one-implementer graph, delete the tail, and
`workflow_mode` stops being a branch. Two carve-outs. The escaped-worktree check and
`noCommitGuardShouldReopen` are **not** in that list: `specs/run-state-machine.md` RSM-008 says the
DOT path MUST NOT run them, so aligning them is a spec amendment (D2), not a cleanup — and `hk-co8g8`
found the escape check killing innocent runs, so extending it as-is would spread a live bug. And the
cognition gate in `dot_gate.go` is not part of this either: no code sets `daemon.Config.CPRegistry`
and no graph declares a `type="gate"` node, so it cannot execute today. Wire it or delete it as its
own decision.

<details>
<summary>What §8 said before 2026-07-30, and why each claim was wrong.</summary>

- "**four** copies … spread across `workloop.go`, `reviewloop.go`, `dot_cascade_core.go`, and
  `dot_gate.go` — 6,459 lines in four functions." Two errors. `reviewloop.go` was deleted on
  2026-07-28 (`3cec5afd7`). And of the three that remained, the launch step was collapsed into one
  function on 2026-07-29, leaving two live post-exit copies plus one unreachable gate.
- "the sandbox gate in two of the four." Fixed on 2026-07-29 by `30b5cf02c`. There is one gate,
  `sandboxSpawnForRun`, and config is the only switch.
- "the escaped-worktree guard runs in one of the four." **Still true**, and it is the more important
  half of the finding, because the one is not the default.
- "What does not exist, and what no amount of extraction will produce, is a single primitive for
  'launch one agent and find out whether it did the work.'" False from 2026-07-29. The first half of
  that sentence — launch one agent — was built. The second half — find out whether it did the work —
  is the part still duplicated, and it is the honest statement of what is left.

</details>
