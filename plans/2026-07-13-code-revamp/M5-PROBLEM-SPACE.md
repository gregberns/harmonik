# M5 — daemon god-package decompose — PROBLEM-SPACE (DRAFT)

> **STATUS: DRAFT problem-space (first kerf "problem-space" pass output).** Written
> alongside the other code-revamp phase docs; a formal kerf bench for M5 can be created
> later by the planner. NOT yet reviewed, NOT signed off. Session constraints in effect:
> daemon OFF, NO beads (operator no-beads directive), signoffs waived.
>
> The un-hold trigger for M5 is MET: M3's first slice (the merge-queue split `internal/mergeq`)
> has merged, so M5 can be speccced against the *as-built* extraction template rather than a
> moving target (ROADMAP §"Hold", line ~120). This document is opened in parallel with M4.

---

## 1. Problem statement

`internal/daemon/` is a god-package: **56,954 non-test LOC across ~50 files**, and the
single file `workloop.go` is **7,998 LOC** on its own. It carries at least six distinct
responsibilities in one flat `package daemon` namespace with no internal import boundaries,
so every one of them can reach every other. The concrete pain:

- **Change-amplification.** The worst offenders live in one file. `beadRunOne`
  (`workloop.go:3121`, ~2367 lines), `runWorkLoop` (`workloop.go:1531`, ~1544 lines) and
  `mergeRunBranchToMain` (`workloop.go:6412`, ~525 lines) sit beside ~90 free functions in
  the same file — merge helpers, event emitters, epic-completion logic, worktree teardown,
  fmt-gate passes. A change to any of them recompiles and re-tests the whole package and
  risks touching unrelated concerns.
- **Test isolation is impossible.** Because everything is `package daemon`, a unit test for
  (say) hook-receipt dedup drags in the whole daemon: the work loop, the socket server, the
  merge path, tmux substrate. There is no seam to test a single responsibility in isolation.
  The proof that a seam *is* achievable already exists next door: `internal/runexec`
  (1,158 LOC) and `internal/mergeq` (146 LOC) are both fully unit-testable *because* they
  were cut out with a hard import boundary.
- **Complexity ceilings are breached and grandfathered.** Track C's `funlen`/`cyclop`/`gocognit`
  ratchet is live but only caps *new* lines; the daemon's existing giants are grandfathered.
  Two are explicitly M5-tagged to retire: `startWithHooks` (`daemon.go:672`, ~1675 cognitive
  complexity) and `handleSocketConn` (`socket.go:387`, ~421). They cannot be lowered by editing
  their interiors (revgrep won't re-flag an untouched signature line); they must be *decomposed*.
- **The other big files are each a latent subsystem.** `tmuxsubstrate.go` (2,769),
  `pasteinject.go` (2,671), `dot_cascade.go` (2,668), `daemon.go` (2,346), `reviewloop.go`
  (2,159), `projectconfig.go` (2,027), `stalewatch.go` (1,189), `orphansweep.go` (1,049),
  `quiesce.go` (1,024), `handlerpause_9hwbw.go` (929), `socket.go` (855), `crewstart.go` (780),
  `dot_gate.go` (767) — each a coherent responsibility trapped in the flat package.

The goal of M5 is to convert those responsibilities into bounded packages with
depguard-enforced import edges, following the *proven* extraction discipline (§4), so that
the daemon becomes a thin composition-and-effects **shell** over a set of pure/leaf domains.

---

## 2. Scope — candidate target subsystems

The ROADMAP names ≥8 subsystems (line 93): orchestrator / policy / agentrunner / hook /
memory / improvement / adapters. The depguard scaffolding is already **staged (inert)** in
`.golangci.yml` for most of them. **None of the eight packages exists yet** under `internal/`
(verified: all `absent`) — except that the *adapters* target is already partly built (§4).

For each, the first-cut "narrow pure domain to extract vs. effect-shell to leave" line —
this is the mergeq/runexec discipline, NOT a whole-file lift:

| Subsystem | Daemon responsibilities / functions it absorbs | Narrow pure domain to EXTRACT | Effect-shell to LEAVE in daemon |
|---|---|---|---|
| **hook** | `hookrelay_chb025.go` (438), `hookSessionStore` + `SetAgentReadyCallback`, CHB-025 last-received-wins outcome dedup | the per-`(run_id, claude_session_id)` dedup + session-registry state machine (pure) | socket receipt plumbing, emitter wiring, callback firing |
| **policy** | `quiesce.go` QuiesceArbiter (1,024, self-described "policy layer"), `dot_gate.go` gate evaluator (767), `handlerpause_*` (929+438), `verdictexecutor_rc025a.go` (483) | the quiesce/gate/pause *decision* functions (drained? open? pause? which verdict?) as pure predicates over snapshots | the drain oracle reads, git/registry effects, the actual pausing |
| **agentrunner** | `tmuxsubstrate.go` (2,769), `pasteinject.go` (2,671), `claudelaunchspec.go` (543), `pilaunchspec.go` (487), `claudeharness.go`, agent-launch portion of `beadRunOne` | launch-spec construction (pure), substrate driver contract | the tmux/paste I/O, process spawn — **but see §3: M2+M4 already own most of this** |
| **orchestrator** | `runWorkLoop` (1,531), `selectNextQueue` (1,421), `effectiveQueueWorkers`, `eagerfill_em063.go` (521), `draindetect.go` (524), `activateFirstPendingGroup*` | the queue-selection / worker-cap / group-advance *decisions* as pure functions over queue+registry snapshots | the loop's timers, slot acquisition, dispatch effects |
| **hook / review cascade** (part of orchestrator or its own) | `reviewloop.go` (2,159), `dot_cascade.go` (2,668), `dot_gate.go` (767) | the workflow-graph walk + node-dispatch *decision* (which node next, which model) | the actual agent dispatch, git reads |
| **memory** | (staged rule at `.golangci.yml:538`, core+eventbus) | — see §3: **little-to-no existing daemon code**; this is a greenfield Phase-2 feature | n/a |
| **improvement** | (rule COMMENTED at `.golangci.yml:571`) | — see §3: greenfield; not a decomposition target | n/a |
| **adapters** | `adapter/br`, `adapter/ntm` | — **already extracted**; live depguard rules at `.golangci.yml:489/:492`, not "reserved" | already done |

Two orthogonal socket/boot cuts (not "subsystems" but load-bearing for the debt retirement):
- **socket op-dispatch** — split `handleSocketConn` (`socket.go:387`) per-op so the 421-complexity
  switch retires as ops move to their subsystems.
- **boot wiring** — split `startWithHooks` (`daemon.go:672`, 1675) as each subsystem's
  construction moves behind a small `New…` in its own package.

---

## 3. The AR-3 reconciliation — mergeq landed THIN; what that implies

AR-3 (`ALIGNMENT-REVIEW.md:16`) requires M5's problem-space to reconcile against the
*as-built* `runexec`/mergeq extraction, not the as-planned "merge subsystem." The load-bearing
observation, confirmed by reading the code:

- **`internal/mergeq` is 146 LOC** — a strictly-FIFO single-executor exclusion queue that split
  *only* the `mergeMu` mutex (`mergeq.go:1-16`). It did **not** lift `mergeRunBranchToMain`
  (~525 LOC) or its ~20 helpers (`commitMerge`, `prepareRebase`, `runMergeBuildGate`,
  `runMergeFmtGate`, …) — those all **stayed in `workloop.go`** as the effect-shell, threading
  their prepare/commit closures *into* `mergeq.Submit` as `critical func`s. The dependency
  direction is `daemon → mergeq`, never the reverse.
- **`internal/runexec` is 1,158 LOC** — two *total, pure* reactors (Dispatch + Run machines
  over a flat event/action vocabulary, `doc.go:1-8`). The effects (`runshell.go`, 437 LOC, plus
  the rest of `beadRunOne`) stayed in the daemon shell, which drives each reactor to a terminal.

**The discipline both prove:** extract the *narrow pure invariant* (single-writer FIFO; a total
lifecycle reactor), leave the *effectful orchestration* in the daemon. The package's LOC is a
fraction of the daemon code it disciplines. **A "subsystem" is a bounded pure domain + a hard
import edge — not a folder you move whole files into.**

**Implications for M5's ambition (the honest answer: the "≥8 subsystems" count is soft):**

1. **`adapters` is already done.** `adapter/br` and `adapter/ntm` exist with *live* depguard
   rules (`.golangci.yml:489/:492`), described as "core only," not "reserved for M5." Strike
   adapters from the M5 target list — it is not decompose work.
2. **`memory` and `improvement` are greenfield, not decompose.** There is no meaningful daemon
   code implementing a memory store or an improvement loop today (the `go-subsystem-add` skill
   frames both as "Phase-2-and-beyond subsystems" *to be added*). A god-package *decompose*
   cannot extract code that isn't there. These belong to a future feature phase, not M5. Their
   staged depguard rules (memory `:538`; improvement commented `:571`) are forward-reservations,
   not decompose targets.
3. **`agentrunner` is largely subsumed by M2 + M4.** Its heavy files — `pasteinject.go`,
   `tmuxsubstrate.go` — are *already being deleted/rebuilt* by M2 (agent-input-substrate:
   "delete pasteinject/tmuxsubstrate," ROADMAP:77), and M4 (remote-substrate) collapses the
   `runner != nil` dual paths and rebuilds the launch/review/dot flow. M5 must NOT re-lift this;
   whatever residue survives M2+M4 is the only agentrunner decompose work, and it is gated on
   them landing (§5 merge-order).
4. **The subsystems that survive as genuine M5 narrow cuts are:** `hook` (thin, mergeq-like),
   `policy` (the quiesce/gate/pause decision predicates), and the **residual `orchestrator`**
   shell (the work-loop/queue-selection brain that remains after everything else is peeled — the
   largest and most-coupled, therefore last).

So the realistic M5 shape is **~3 real decompose cuts (hook, policy, orchestrator) + 2 debt
retirements (socket dispatch, boot wiring)**, not 8 fresh packages. The staged depguard list
over-counts because it reserves future *feature* packages (memory/improvement) and packages
already built (adapters) alongside genuine decompose targets.

---

## 4. Constraints

- **Depguard edges (hard, enforced).** Every extracted subsystem MUST NOT import
  `internal/daemon`; the daemon shell threads closures *in* (the mergeq pattern). Staged
  allow-lists cap each: policy → core only (`:450`); hook → core+eventbus (`:530`); memory →
  core+eventbus (`:538`); agentrunner → core+eventbus+handlercontract+adapter/ntm (`:520`);
  orchestrator (commented `:580`) → everything but daemon/cmd. Pure reactors: no I/O, no clock
  reads, no ID minting (the runexec rule, `doc.go:5`). New packages follow the `go-subsystem-add`
  scaffold skill (layout + depguard matrix entry + test-helper hookup).
- **Two grandfather functions to retire** (`track-c-enforcement.md §4`): `startWithHooks`
  (`daemon.go:672`, ~1675 cognit) and `handleSocketConn` (`socket.go:387`, ~421). They cannot be
  lowered in place; each subsystem extraction should shave a slice off one of them (a subsystem's
  boot-construction leaves `startWithHooks`; its socket op leaves `handleSocketConn`). M5 is
  "done" on these when both drop under the ceilings without `//nolint`.
- **Backward-compat surfaces that must not break:** the **socket wire protocol** (all ops in
  `handleSocketConn` — external `harmonik` CLI clients depend on it), the daemon's **public
  handler interfaces** (`RequestHandler`, `HookRelayHandler`, `QueueHandler`, `SubscribeHandler`,
  `CrewHandler`, etc.), and the **event shapes** on the bus. Extraction is internal refactor;
  the wire and event contracts are frozen.
- **Test-migration burden.** `package daemon` tests are numerous and stack-wide. Each cut must
  migrate the relevant tests to the new package (as `runexec`/`mergeq` did — mergeq shipped 344
  test LOC for 146 impl LOC) and prove the extracted domain is testable *without* the daemon.
  The `scenario` harness is depguard-DEFERRED (`.golangci.yml:556`, hk-uyxg0) because it drives
  the full stack — M5 cuts must not make it worse.

---

## 5. Open questions for the planner / operator

1. **Sequencing — which subsystem first?** Recommend **hook** (see §6): narrowest, most
   mergeq-like, zero overlap with M2/M3/M4. `policy` second, `orchestrator` last (it is the
   residual brain and only cleanly separable once the others are peeled).
2. **Does M5 get its own kerf bench + beads?** Currently the **no-beads directive is in effect**
   this session. M5 is net-new (`ROADMAP:132`) and warrants a formal kerf work
   (`codename:daemon-decompose` or similar) when speccing resumes — but under no-beads it rides
   the code-revamp plan docs for now. **Operator call:** create the bench + beads now, or hold?
3. **M4 merge-order dependency (load-bearing).** M4 (remote-substrate) collapses the
   `runner != nil` dual paths and rebuilds the agent launch / review-loop / dot-cascade flow —
   exactly the `agentrunner` target's files (`reviewloop.go`, `dot_cascade.go`, `claudelaunchspec.go`,
   `tmuxsubstrate.go`). M2 deletes `pasteinject.go`/`tmuxsubstrate.go`. **Constraint:** M5's
   agentrunner cut CANNOT run before M2 and M4 land, or M5 and M4 will fight over the same files.
   The hook and policy cuts have no such overlap and can proceed in parallel with M4. **Confirm
   at M5 design:** is agentrunner deferred to *after* M4, or dropped from M5 entirely (folded into
   M4/M2 as their natural endpoint)?
4. **Is "≥8 subsystems" still the bar, or is ~3 real cuts the honest target?** §3 argues adapters
   is done, memory/improvement are greenfield-not-decompose, agentrunner is subsumed. The planner
   should decide whether M5's success metric is "8 packages" or "the daemon shell shrinks to N kLOC
   and the two grandfather giants retire."

---

## 6. Recommended first slice

**Extract `internal/hook` — the CHB-025 outcome-dedup + hook-session registry.**

Rationale:
- **Narrowest, most mergeq-like domain.** `hookrelay_chb025.go` (438 LOC) is a self-contained
  last-received-wins dedup keyed by `(run_id, claude_session_id)`, plus the `hookSessionStore`
  state (`newHookSessionStore`, `RegisterHookSession`, `SetAgentReadyCallback`,
  `CloseHookSession`). That is a pure state machine wrapped in thin plumbing — the exact shape
  mergeq proved at 146 LOC.
- **Its depguard rule is already staged** (`.golangci.yml:530`, core+eventbus) — no new policy
  design needed, just un-comment and populate.
- **Zero overlap with M2/M3/M4.** Unlike agentrunner it touches none of the files those phases
  rebuild, so it can land immediately and in parallel with M4.
- **Proves the M5 pattern end-to-end at low blast radius:** a pure domain + a hard `daemon → hook`
  edge + migrated tests + a shave off `startWithHooks` (its construction moves behind
  `hook.NewSessionStore`) — demonstrating the whole M5 loop before the larger, riskier
  policy/orchestrator cuts.

`policy` is the recommended second slice (the QuiesceArbiter is already self-described as a
"policy layer," `quiesce.go`); `orchestrator` last.

---

## Appendix — measured grounding (working tree, this session)

- `internal/daemon/` non-test: **56,954 LOC** / ~50 files.
- `workloop.go` **7,998** · `tmuxsubstrate.go` 2,769 · `pasteinject.go` 2,671 · `dot_cascade.go`
  2,668 · `daemon.go` 2,346 · `reviewloop.go` 2,159 · `projectconfig.go` 2,027 · `stalewatch.go`
  1,189 · `orphansweep.go` 1,049 · `quiesce.go` 1,024 · `handlerpause_9hwbw.go` 929 · `socket.go`
  855 · `crewstart.go` 780 · `dot_gate.go` 767.
- God-functions: `beadRunOne` `workloop.go:3121` · `runWorkLoop` `workloop.go:1531` ·
  `mergeRunBranchToMain` `workloop.go:6412` · `startWithHooks` `daemon.go:672` ·
  `handleSocketConn` `socket.go:387`.
- Extraction template: `internal/runexec` **1,158 LOC** (vocab 233 / run 530 / dispatch 374 /
  doc 21) · `internal/mergeq` **146 LOC** (+344 test).
- Target packages under `internal/`: orchestrator/policy/agentrunner/hook/memory/improvement all
  **absent**; adapters **present** (`adapter/br`, `adapter/ntm`).
- Staged depguard (`.golangci.yml`): policy :450 · agentrunner :520 · hook :530 · memory :538 ·
  adapter-br/ntm :489/:492 (live) · improvement (commented) :571 · orchestrator (commented) :580.
