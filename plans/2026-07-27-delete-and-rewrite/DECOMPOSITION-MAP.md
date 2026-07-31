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

The named center is `workloop.go`. It is not the only one, and the thing that makes it rotten is not
its size.

**The rotten center is one algorithm — "launch an agent, watch it, decide whether it did the work,
merge, terminalize" — written out four separate times**, in four functions totalling **6,459 lines**:

| Function | File | Lines | What it is |
|---|---|---:|---|
| `beadRunOne` | `internal/daemon/workloop.go` | 2,289 | single-mode driver + the shared prologue/epilogue for the other two |
| `runReviewLoop` | `internal/daemon/reviewloop.go` | 1,586 | implementer↔reviewer iteration driver |
| `driveDotWorkflow` | `internal/daemon/dot_cascade_core.go` | 1,002 | DOT-graph cascade driver |
| `dispatchDotAgenticNode` | `internal/daemon/dot_cascade_core.go` | 882 | per-node agent dispatch inside the cascade |

Plus `runWorkLoop` (1,670 lines), which is a *different* algorithm — the outer scheduler — living in
the same file.

The four-way duplication is measurable, not inferred. These symbols each appear in three or four of
those files independently:

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

And the drift that duplication predicts is already present, in two places I confirmed:

- **`runmerge.CheckMainWorkingTreeDirty` (the implementer-escaped-worktree guard) has exactly one
  production caller: `beadRunOne`.** Review-loop runs and DOT runs do not run it. The run state
  machine has a slot for it (`runexec.ActCheckEscape` / `EvEscapeDetected`, `RunEffectors.CheckEscape`)
  but `RunBridge.WireSpine` wires that slot to a function that unconditionally returns
  `EvGuardsPassed`. The guard is imperative, single-mode, and the machine records a pass it never
  performed.
- **`sandboxSpawnForRun` (the srt sandbox gate) is called from `workloop.go` and
  `dot_cascade_core.go` only.** `reviewloop.go` and `dot_gate.go` do not call it — so with
  `sandbox.backend: srt` configured, reviewer and gate agents launch unsandboxed.

`CARRY-FORWARD.md` names this exact bug class from the pasteinject archaeology: *"4× single-mode and
review-loop paths drifted"*, *"3× gate applied to one of N call sites"*. Those are not historical.
They are the current state of the tree.

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

**No-seam #1: the mode boundary (C3). This is the central defect.**

There is no `ModeExecutor` interface. Instead:

- Single mode is written **inline inside `beadRunOne`**, lines 4232–5407 (~1,175 lines).
- Review-loop mode is `runReviewLoop(ctx, env, rp, handles, runID, beadID, title, description,
  wtPath, headSHA, model, effort, extraContext, baseBranch, runner, workerBinary, workerHookSock,
  workerSession, workerCwd)` — **19 parameters**.
- DOT mode is `driveDotWorkflow(...)` — **19 parameters**, nearly the same list.

Each of the three then re-implements: build launch spec → attach substrate → register hook session →
`DispatchSegment` with 7 hooks → paste inject → wait with socket grace → probe worktree HEAD →
force-teardown → emit presence. Four times (dot_gate.go is the fourth, for cognition gates).

The tangle is genuine, not cosmetic: the three modes need *different subsets* of the resource set at
*different times*. Review-loop needs a reviewer substrate and a per-iteration worktree diff hash;
DOT needs a per-node harness override and a ControlPoint registry; single mode needs the
noChange-timeout channel that `pasteInjectQuitOnCommit` closes. So a naive "extract the common part"
produces a function with a union-of-all-three parameter list — which is what the 19-parameter
signatures already are.

> **Operator decision required (D1): is one agent launch one thing, or three?**
>
> Option A — **one `AgentTurn` primitive**: `{launch spec, substrate, ready policy, delivery policy,
> completion policy, budget}` → `{exit code, stderr tail, socket outcome, session id, HEAD before/after,
> duration}`. All three modes become loops over it. This is the correct shape and is what the
> CARRY-FORWARD "act → verify by observation → retry with bound" pattern demands. Cost: every
> harness-specific special case (`SessionIDCaptured` stdout interception, `CompletionProcessExit`
> skipping the ready handshake, Pi stdout tee, srt argv wrap) has to be expressed as *policy on the
> primitive*, not as an `if` in the driver. That is a real design exercise, not a mechanical move.
>
> Option B — keep three drivers, share only the resource scope. Cheaper, preserves the drift.
>
> This decision gates everything below §3 step 5. It cannot be deferred, because the whole point of
> the rewrite is that structure-caused drift stops being representable.

**No-seam #2: the guards and the state machine disagree about who owns them.**

`runexec` has `ActCheckEscape` / `EvEscapeDetected` / `EvGuardsPassed`. `RunBridge.WireSpine` wires
`CheckEscape` to a constant `EvGuardsPassed`. The actual escape check and no-commit guard are
imperative in `beadRunOne` immediately before the classification switch, with a comment explaining
that they "run BEFORE the dispatch-terminal classification for EVERY class (the pre-RT7 order); by
the time the machine traverses Guarding they are known-green."

Result: the machine's `Guarding` phase is theatre for single mode, and **does not exist at all** for
review-loop and DOT, because those modes reach `WireSpine` without ever running the guard.

> **Operator decision required (D2): do the guards belong to the machine or to the caller?**
> If the machine — then all three modes get the escape check and the no-commit guard automatically,
> and behaviour *changes* for review-loop and DOT (runs that pass today may start failing). If the
> caller — then delete `ActCheckEscape` from `runexec`, because it is a lie.
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
deterministic, all decided before any resource is acquired.

**Why here:** it is the largest purely-decidable chunk of `beadRunOne` and it removes four of the
five refuse-before-launch early returns — the ones that caused the `hk-3hozm` worker-slot leak
because they returned before the release defer was registered. Resolving *before* acquiring makes
that class of bug unrepresentable.

**Risk:** low, provided the precedence tables are transcribed exhaustively. The precedence walks are
subtle (`hk-pkugu`: the model default must be resolved against the harness that will actually be
selected, or a Pi run asks the Pi provider for a Claude model). Write them as tables and test them
as tables.

### Step 6 — resource leases (~450 lines, MEDIUM risk)

C2: worker slot, tunnel, worktree, tmux session, hook session, spawn token. Each an acquire
returning a lease with one idempotent release; composed into a scope closed in reverse order.

**Why here:** it depends on step 5 (placement is a plan output) and it is what unblocks step 7 (the
mode boundary needs a complete resource scope to receive).

**Risk:** medium. This is where the CARRY-FORWARD facts bite hardest — tunnel readiness must be a
*connectability* probe not an existence check (pane fact / hk-ege6), tmux window creation must be
externally bounded (tmux fact 3), and every kill must target `-pgid` and probe the group (tmux fact
7). Get these wrong and runs die silently.

### Step 7 — the mode boundary  ⛔ **BLOCKED on decision D1**

Cannot start until the operator answers "one `AgentTurn` primitive, or three drivers?" Everything
before this step is a move; this step is a design.

If D1 = one primitive: this is the biggest single piece of work in the program and it subsumes
`runReviewLoop`, `driveDotWorkflow`, `dispatchDotAgenticNode`, and `beadRunOne`'s single-mode body —
6,459 lines collapsing to one primitive plus three thin loops.

If D1 = three drivers: extract the shared prologue/epilogue only, accept that drift remains, and
compensate with a conformance test that asserts all three call the same guard set.

### Step 8 — the terminal spine (LAST, and mostly already done)

`beadRunOne` becomes a phase coordinator: resolve → acquire → execute → classify → feed the machine.
The machine (`internal/runexec` + `RunBridge`) already owns gate → merge → close/reopen.

**Why last:** everything depends on it, and it is the only part of the current code that is already
right. Touching it early means re-doing it.

> **⚠ Operator decision (D2) lands here**: whether the guards move into the machine's `Guarding`
> phase (behaviour change for review-loop and DOT) or the machine's escape slot is deleted.

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

| # | File | LOC | Commits | Longest function | Mutable globals | External world |
|---|---|---:|---:|---|---:|---|
| 1 | `internal/daemon/reviewloop.go` | 2,194 | 116 | `runReviewLoop` **1,586** | 1 | tmux, claude, codex, git, ssh, fs, bus, hook socket |
| 2 | `internal/daemon/dot_cascade_core.go` | 1,989 | 123† | `driveDotWorkflow` **1,002** (+`dispatchDotAgenticNode` **882**) | 0 | tmux, claude, codex, git, ssh, fs, bus |
| 3 | `internal/daemon/pasteinject.go` | 2,691 | 56 | `pasteInjectQuitOnCommit` ~575 | **23** | tmux, ssh, raw `exec.Command` (pgrep/ps/git), git, fs, bus |
| 4 | `internal/daemon/tmuxsubstrate.go` | 3,023 | 67 | `spawnWindowVia` 194 (76 funcs) | 1 | tmux (385 refs), ssh, `syscall.Kill` |
| 5 | `internal/keeper/watcher.go` | 2,110 | 78 | `(*Watcher).Run` 531 | 1 | fs, tmux, `exec.Command`, events.jsonl |
| 6 | `internal/eventbus/busimpl.go` | 1,647 | — | max 118 | — | fs (JSONL), 7 `go func` fan-out sites |
| 7 | `cmd/harmonik/main.go` | 1,517 | 128 | `run` 1,298 | 0 | ~50 verbs, all delegated |
| 8 | `internal/daemon/stalewatch.go` | 1,189 | 15 | `(*StaleWatcher).checkRun` 347 | 6 | git rev-parse, process liveness, bus |

† `dot_cascade_core.go` shows 2 direct commits because it was recently split out of `dot_cascade.go`;
`--follow` gives 123.

Runners-up: `internal/daemon/dot_gate.go` (888, `executeCognitionGate` 367),
`cmd/harmonik/comms.go` (2,212, `runCommsRecvFollowIO` 300, six hand-rolled socket dial sites),
`cmd/harmonik/run.go` (942, `runBeadSubcommandIO` 701).

### Genuinely part of the rotten center (ranks 1–3)

**#1 `reviewloop.go` and #2 `dot_cascade_core.go` are the same disease as `workloop.go`, not
neighbours of it.** `dot_cascade_core.go` has the worst concentration in the repo — **two functions
are 94% of the file**, with no intermediate decomposition at all. `runReviewLoop` at 1,586 lines is
the second-largest function in the repo after `beadRunOne`, and it is structurally *the same code*
as `beadRunOne`'s single-mode body, re-implemented. **These three files must be rewritten as one
unit or not at all** — rewriting `workloop.go` alone leaves two-thirds of the duplicated dispatch
loop in place and guarantees the drift continues.

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
| **D1** | **One `AgentTurn` primitive, or three mode drivers?** | Rewrite step 7; determines whether 6,459 lines collapse or 3 copies persist | It is the whole thesis of the rewrite. Choosing "three drivers" means accepting that the drift bug class stays representable. |
| **D2** | **Do the escape check and no-commit guard belong to the Run machine or to the caller?** | Rewrite step 8 | Moving them into the machine *changes behaviour* — review-loop and DOT runs that pass today would start being guarded. Deleting `ActCheckEscape` admits the machine's `Guarding` phase is decorative. |
| **D3** | **Is a failed queue-reservation persist fatal to the dispatch?** | Rewrite step 4 | Correct answer is yes (no claim, no launch). But under disk pressure it converts silent inconsistency into visible dispatch stall. |
| **D4** | **Are `reviewloop.go` and `dot_cascade_core.go` in scope with `workloop.go`, or after it?** | Scoping of the whole program | Rewriting `workloop.go` alone leaves 3,470 lines of the same duplicated dispatch loop in two other files. The stated contract (`runWorkLoop` + `workLoopDeps`) does *not* cover them — they are called from inside `beadRunOne` with 19-parameter signatures. **My reading says they are one unit.** |
| **D5** | **Does the scenario tier become merge-blocking before the rewrite starts?** | Everything | It is one line of YAML. Without it there is no oracle. It goes red immediately (the `hk-zobns` branch-protection guard fails open) — which is the point, but it is a visible red build the operator has to accept. |

---

## 8. One-paragraph answer

`workloop.go` is not one file that got too big; it is two unrelated programs (a scheduler and a run
driver) sharing a file, where the run driver is one of **four** copies of the same
launch-watch-verify-merge algorithm spread across `workloop.go`, `reviewloop.go`,
`dot_cascade_core.go`, and `dot_gate.go` — 6,459 lines in four functions. The duplication has already
produced the drift it predicts: the escaped-worktree guard runs in one of the four, and the sandbox
gate in two of the four. The seams are real and mostly already identified — the 2026-07-24 audit
found the same five, independently — and a good state machine for the terminal spine already exists
in `internal/runexec`. What does not exist, and what no amount of extraction will produce, is a
single primitive for "launch one agent and find out whether it did the work." Deciding whether to
build that is the operator call that gates the entire program.
