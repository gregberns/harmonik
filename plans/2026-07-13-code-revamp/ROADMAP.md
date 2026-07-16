# Code Revamp — Phasing & Sequencing Roadmap

> **Companion to `PLAN.md`.** PLAN decides *what "good" is + the first vertical*. This ROADMAP
> decides *the order of everything after Phase 1, where the orphaned items live, and what to
> prepare now.* Authored 2026-07-13 by admiral at operator direction ("figure out the best
> plan"). Built from `PLAN.md`, the six dossiers, and the census
> (`plans/2026-07-12-codebase-census/{REPORT,PLAN}.md`).

## Corrections (verified 2026-07-13, post planning fan-out)

> Five planning agents + independent Fable verification refined this roadmap. The body below is
> preserved as authored; these corrections override it where they conflict. Detail + evidence:
> `PLANNING-LOG.md`.
>
> 1. **`subsystem-proofs → M5` is a NAMING COLLISION — struck.** That work is DONE (12/12 beads
>    closed) and is per-subsystem *test lanes*, not the daemon god-package breakup. **M5
>    (daemon-decompose) stays genuinely net-new** — no existing work to reconcile against.
> 2. **`quality-system` is the acceptance ORACLE / DOGFOOD gate, not a Track C source.** It is DONE
>    (15/15) and its `core-loop-proof` matrix harness IS the offline acceptance oracle — reference
>    it, don't rebuild. **Track C enforcement has NO existing owner → it is direct config work.**
> 3. **The record→replay seam is already BUILT, not to-be-extracted.** P1 landed
>    `internal/substrate` (generic `EventSource[E]`/`Effector[A]`/`Run[E,A]` + `ClockPort`),
>    `internal/replay`, and the `replay-substrate` + `session-keeper` specs. M2/M3 **instantiate**
>    the proven seam. This resolves PLAN §8 decisions #2 (generics — landed) and #3 (typed-decode
>    ADOPT — landed, EV-048 normative at `specs/event-model.md:720`).
> 4. **The enforcement ratchet already exists.** CI gates on `golangci-lint --new-from-rev=origin/main`
>    + `scripts/coverage-gate.sh`; enabling complexity linters auto-grandfathers legacy code.
> 5. **`validation-net` rebase = RE-FILE beads.** 12 of its 13 spec'd VN beads (incl. flagship VN4
>    `hk-ukhzu`) are absent from the `br` DB — not merely unlabeled.

## Operating fact that drives the whole sequence

**The daemon is stopped for the duration of the rebuild.** All work is done out-of-pipeline
(Claude Code sub-agents, single-writer, human-reviewed merge to a branch) — the same model Phase 1
is running under. Nothing flows through the daemon pipeline until the rebuilt core passes the
acceptance oracle offline. This single fact resolves the one real disagreement between the two
prior plans (below) and sets the ordering.

## Resolving the census-vs-PLAN disagreement (STEP-0)

The census made **STEP-0** (resume-hang, false-close, honest-probe) *prerequisite #0* —
"make the pipeline trustworthy before anything flows through it." The code-revamp PLAN demoted it
to a parallel "Track B / fold into A3's SR9." They conflict only because they assumed different
execution models:

- The census assumed **the daemon keeps running** and must be trustworthy to dogfood the rebuild.
- The PLAN chose to work **entirely out-of-pipeline**.

**The operator's decision (daemon down for the rebuild) removes the census's assumption.** There is
no live pipeline to keep trustworthy, so STEP-0 is not a gate. Resolution:

- **STEP-0a (resume-hang) folds into M3.** The hang lives inside `beadRunOne` — the exact function
  M3 rewrites. Fixing it standalone is throwaway; it becomes a **required invariant of the M3 state
  machine** (bounded liveness on the resume branch — the daemon analog of Phase 1's keeper SR9).
- **STEP-0b (false-close) + queue.json two-writer** are small, seam-independent data-integrity
  fixes. Do them anytime out-of-pipeline. No gate, no kerf.
- **STEP-0c (honest-probe)** is a `createworktree.go` guard that **M4 must carry forward** — track
  it as an M4 acceptance item, not a standalone move.
- **The daemon returns to service only after the rebuilt core passes the acceptance oracle offline
  (census Oracle #1–#4).** That is the dogfood gate — a finish line, not a prerequisite.

Net: the PLAN's out-of-pipeline stance wins; the census's STEP-0 items are preserved but **relocated
into the phases that own their files** instead of blocking the front of the line.

## The phase map

Ordering rule: **prove the method → turn on the protective net → rebuild the god-core and the two IO
channels behind the proven seam → dogfood.** Seam-independent hygiene runs in parallel from day one.

| Phase | Scope | Kerf-first? | Home | Depends on | Status |
|---|---|---|---|---|---|
| **P1 — session-restart-substrate** | substrate seam + ClockPort + 4 keeper events + keeper vertical + adopt typed-decode | yes | `session-restart-substrate` (exists, **Ready**; T6 landed, T7–T14 impl remaining) | — | **in flight** |
| **C — enforcement** | complexity linters + coverage floor + daemon `deny` ceiling + fix inert/mismatched depguard rules | no (direct) | direct config work — NO existing owner (`quality-system` is the acceptance oracle, not a source; reference `core-loop-proof`, don't rebuild) | — (seam-independent) | ready to do |
| **B — data-integrity** | queue.json two-writer fix; noChange false-close (STEP-0b) | no (direct) | checklist / small beads | — (seam-independent) | ready to do |
| **M1 — test-theater** | delete `operatornfr`/`specaudit` self-assert mass, prune `scenario`, delete dead event-registry surface | no | reconcile into `testing-strategy-uplift` (integration) | — (seam-independent) | ready to scope |
| **M3 — run-state-machine** | extract `beadRunOne` → `runexec` state machine; `mergeMu` → explicit merge queue; **absorbs STEP-0a resume-hang** | **yes** `codename:2026-07-14-run-state-machine` | **NEW kerf** | P1 method proven; C ratchet live; M1→M3 coverage audit | prepare problem-space now |
| **M2 — agent-input-substrate** | structured-protocol driver behind `handler.Substrate`; tmux → observation-only; delete `pasteinject`/`tmuxsubstrate` | **yes** `codename:2026-07-14-agent-input-substrate` | **NEW kerf** | P1 method proven | prepare problem-space now |
| **M4 — remote-substrate** | daemon (mac-mini) drives agent PROCESS on remote box (gb-mbp) via SSH `CommandRunner` behind `handler.Substrate`; **all 3 harnesses remote** (Claude/Codex/Pi), **Claude-first v1 slice**; F4 merge-`push` relocation; **carry STEP-0c guard**. Runner-threaded (Option A); worker-resident network agent = Phase-3. DEC-A dual-path cleanup **DEFERRED** (not v1). _(Decisions operator-locked 2026-07-16 — see `.kerf/works/remote-substrate/01-problem-space.md`.)_ | **yes** `codename:remote-substrate` | rewritten onto as-built M2/M3 seams | M2 (InputPort/Ack) + M3 (mergeq) — both DONE | **✅ CODE-COMPLETE** (T1–T8 landed+reviewed, tip `ac6091ca`, 2026-07-16; COORD c041). Operator gates remain: real-box T4 proof + integration→main PR. |
| **DOGFOOD** | rebuilt core passes acceptance oracle offline → daemon back on | — | — | M2 + M3 (+ M4 for remote) | gate |

**M2 ‖ M3 are near-fully parallel — one real edge.** M3-phase-1 (C1 ClockPort, C2 `mergeMu` →
merge-queue split) can start as soon as P1 is proven + M1-1 lands; it needs neither M2 nor the rest
of M1. M3-3 (workLoopDeps → ports) also does NOT need M2. The **only** M3→M2 dependency is
**M3-4 (reactor `Step`) → M2-1 (seam input/ack contract)**; everything else runs concurrently.
M4 hard-depends on the M3-phase-1 merge-queue split. (B2 — confirm this single edge at M3 design.)

## Orphan homing — items in the dossiers/census with no phase in PLAN §4

Assign each to a host so nothing falls through:

| Orphan item | Source | Host |
|---|---|---|
| Full daemon **god-package** breakup (≥8 subsystems: orchestrator/policy/agentrunner/hook/memory/improvement/adapters) | dossier 06 / census | **larger than M3** — its own follow-on phase **M5 — daemon-decompose** (net-new; `subsystem-proofs` is a naming collision, DONE, and NOT reconciled against it). M3 is the first slice. |
| `apptap` never wired to a **production capture path** (record→replay has no live recorder) | dossier 03 | fold into **M2** (the input rebuild is where a real capture tee gets spliced in) + a P1 follow-up to record keeper live |
| Instrument **event-dark `internal/workspace`** (zero events on remote materialization) | dossier 04/05 | **M4 prerequisite** (instrument before rebuild — "the least observable path") |
| **JSONL rotation/retention** (single 85 MiB file, no rotation) | dossier 04 | small standalone under **Track B/C hygiene** |
| Systemic **discarded emit errors** (`_ = Emit`), `Type string`→enum hoist, `SourceSubsystem` stamping | dossier 04 | event-model follow-up; ride alongside **M-phases** touching those emit sites |
| queue **`HandlerAdapter` eviction**; lifecycle-reconcile **intent-log (BI-031)**; codex **WAL-guard** | census carve-and-protect | `HandlerAdapter`→Track B; intent-log→its own small work; WAL-guard→M2 |
| **Real-Claude-process** restart integration test | dossier 02 | P1 follow-up (L3 tier) or M2 |

## What to prepare NOW (getting ahead of the implementer) vs hold

**Do now (seam-independent, zero risk of rework):**
1. **Track C enforcement** — implement it. It's the protective ratchet for *all* code P1 and the
   M-phases produce; the sooner it's on, the more it catches. Grandfather existing violations.
2. **Track B data-integrity** — queue.json two-writer + false-close. Small, direct.
3. **M1 test-theater** — scope the keep/delete classification (needs operator sign-off on the
   ambiguous set before bulk delete — census Q4).

**Prepare the problem-space + decompose for (design passes de-risk as P1 lands):**
4. **M3 `2026-07-14-run-state-machine`** and **M2 `2026-07-14-agent-input-substrate`** — new kerf works. Their
   *problem-space* is fully known today (dossiers 01 + 05); write it now. Hold their *design* passes
   until P1 proves the reactor method generalizes — M3 in particular mirrors the same
   functional-core/reactor shape, so its design should follow the proven keeper template.
5. **M4** — reconcile the **existing** `remote-substrate` / `remote-substrate-phase2` works against
   this framing (do not create a duplicate); they predate the revamp and need rebasing onto M3's
   merge-queue + the proven seam.

**Un-held — BUILD-READY (2026-07-16, COORD c042):**
6. **M5 daemon-decompose** — god-package breakup. Un-hold trigger MET (M3 mergeq slice landed → as-built
   extraction template exists; M4 landed → agentrunner files settled). **Scope locked to the HONEST target,
   NOT "≥8 packages":** 3 real cuts (`hook`→`policy`→`orchestrator`) + 2 debt retirements (socket op-dispatch,
   boot wiring). `adapters` struck (done); `memory`/`improvement` struck (greenfield, future feature phase);
   `agentrunner` folds into `orchestrator` (M2+M4 own its files). Success = daemon shell shrinks + the two
   grandfather giants (`startWithHooks`, `handleSocketConn`) retire under ceilings. **Slice 1 = `internal/hook`,
   GO now.** Full handoff: COORD c042. Problem-space: `M5-PROBLEM-SPACE.md`.

## Kerf reconciliation checklist (before creating anything new)

Several existing works overlap the future phases — **reconcile, don't duplicate**:
- `remote-substrate` (analyze) + `remote-substrate-phase2` (problem-space) → **M4**.
- `subsystem-proofs` — DONE (12/12); a naming collision with M5, NOT reconciled into it. **M5 (daemon-decompose) is net-new.** (Its depguard subsystem-edge detail may inform, but does not own, any phase.)
- `quality-system` (DONE, 15/15) is the acceptance ORACLE / dogfood gate — reference its `core-loop-proof` matrix, don't rebuild. **Track C enforcement has NO existing owner → direct config work**, not a reconcile.
- `testing-strategy-uplift` (integration) → **M1** test-theater + the L0–L3 taxonomy already being
  defined in P1's substrate spec.

**M3 (`2026-07-14-run-state-machine`)**, **M2 (`2026-07-14-agent-input-substrate`)**, and **M5 (`daemon-decompose`)**
are the genuinely net-new codenames (M5 un-held + BUILD-READY as of 2026-07-16 — see item 6 above).
