# Code-Revamp Forward-Planning Log

> **Owner:** admiral, autonomous (signoffs waived, NO DAEMON). Started 2026-07-13.
> **Goal:** get the post-P1 plan fully prepared *ahead* of the implementer (who is on P1/T7).
> Documents every planning move so the next session (or the operator) can pick up cold.
> Companion to `ROADMAP.md` (the phase map) + `PLAN.md` (what "good" is).

## Ground rules in force
- **NO DAEMON.** Everything out-of-pipeline: kerf + Claude Code sub-agents only. Do not enable the daemon.
- **Signoffs waived.** Advance autonomously; independent-reviewer sub-agent instead of operator gate.
- Another agent owns **P1 impl** (`session-restart-substrate`, on T7). Do not touch its bench or branch work.
- Kerf CLI works without the daemon (verified: `kerf show`/`kerf map` ran clean).

## The forward-planning task list (from ROADMAP §"What to prepare NOW")
| # | Stream | Kind | Deliverable | Status |
|---|--------|------|-------------|--------|
| 1 | Kerf reconciliation | audit | rebase/supersede recommendation per existing overlapping work → `reconciliation.md` | dispatched |
| 2 | M3 `run-state-machine` | new kerf | work created + problem-space (pass 1) authored | dispatched |
| 3 | M2 `agent-input-substrate` | new kerf | work created + problem-space (pass 1) authored | dispatched |
| 4 | Track C enforcement | scope | linter config diff + ceilings + grandfather list → `track-c-enforcement.md` | dispatched |
| 5 | Track B + M1 | scope | data-integrity fix locations + test-theater keep/delete classification → `track-b-m1.md` | dispatched |

**Hold (do NOT spec yet):** M5 daemon-decompose (depends on M3 landing its first slice).
**Design passes held:** M3/M2 stop at problem-space+decompose until P1 proves the reactor seam.

## Log
- **2026-07-13 (start):** Re-hydrated as admiral on keeper-restart. Confirmed P1 planning fully
  saved (session-restart-substrate at Pass 8/Ready) and impl on T7. Read ROADMAP+PLAN. Confirmed
  M3/M2 net-new via `kerf map`. Set up this log; creating M3/M2 kerf works; fanning out 5 planning agents.
- **2026-07-13 — Stream 1 (reconciliation) DONE** → `reconciliation.md`. Key results (PENDING Fable verify):
  - **ROADMAP correction A:** `subsystem-proofs → M5` is a NAMING COLLISION. subsystem-proofs is DONE
    (12/12 beads closed) and is per-subsystem *test lanes*, not the god-package breakup. **M5 stays net-new.**
  - **ROADMAP correction B:** `quality-system` is DONE (15/15) and IS the acceptance-oracle / DOGFOOD gate —
    reference it, don't rebuild. It is **NOT** a Track C source. **Track C enforcement has NO existing owner → direct work.**
  - `remote-substrate` → REBASE onto M4 (phase-1 SSHRunner/dual-path code already in-tree).
  - `remote-substrate-phase2` → SUPERSEDE-and-fold into M4 (partial) + operator call (its DEC-A *rejected*
    handler.Substrate, conflicting with the revamp seam; container/egress out of M4 scope).
  - `validation-net` → REBASE (ready, whole) — but only 1/13 spec'd beads carry the codename label; attachment needs fixing.
  - `testing-strategy-uplift` → SUPERSEDE (stalled, 0 beads); harvest coverage-gate→Track C, 5-layer taxonomy→M1.
  - **M3/M2 both CONFIRMED net-new.** M3 cross-ref `stall-sentinel` (SR9-analog) + `reap` (orphan-run reconcile); M2 cross-ref P1 seam + phase2 DEC-A reversal.
  - Cross-cutting: 3 "overlapping" works have ALL beads closed while pass-status lags (impl landed ahead of pass advance).
  - **3 operator calls** captured (handler.Substrate adoption; container/egress split; supersede testing-strategy-uplift).
- **2026-07-13 — Stream 5 (Track B + M1) DONE** → `track-b-m1.md` (PENDING Fable verify):
  - **B1** bead: `fix(queue): route append RPC through LockForMutation — close queue.json two-writer lost-update (rpc.go:1006-1016)`. Append RPC does Load→AppendItems→Persist→SetQueue outside `queueMu`; fix = locked read-then-write on live in-memory state; add `-race` regression test.
  - **B2** bead: `fix(lifecycle): Cat-3c false-close — require real Harmonik-Bead-ID trailer + non-docs diff, not a git-log --grep body mention (orphansweepbeads.go:230)`. One scanner, two callers → single fix point.
  - **M1:** DELETE/RELOCATE-safe = 1 (`specaudit`); AMBIGUOUS operator-call = 2 (`operatornfr`, `scenario`).
  - **RESOLVES PLAN §8 decision #3 (adopt-vs-delete typed-decode):** the "dead event-registry path"
    (`DecodePayload`/`Dispatch*`/`ValidateEnvelopeSchemaVersion`/`pertypecompat`) is **NO LONGER DEAD** — P1 landed
    `internal/replay/replay.go` consuming it; bench D6 `[OPERATOR-LEAN CONFIRMED]` + spec EV-048 = ADOPT. **M1 must KEEP it** (census DELETE was correct only on `main`, superseded on the P1 branch).
- **2026-07-13 — Stream 3 (M2 agent-input-substrate) DONE.** Problem-space + decompose authored on bench,
  held at `decompose` (design deferred). 7 goals / 5 non-goals / 7 components (C1 ack contract → C7 WAL-guard). (PENDING Fable verify):
  - **RESOLVES PLAN §8 decision #2 (generics vs any):** P1's `internal/substrate/` already ships the full generics
    spine green (`EventSource[E]`/`Effector[A]`/`Run[E,A]`/`ReplayCodec[E]`/`Twin[E]`/`FaultConfig`/`ClockPort`/`FakeEffector`/`SyntheticSource`).
    → **generics, already landed.** M2 (and M3) INSTANTIATE the seam; they do not extract it. ROADMAP's "extract substrate" framing is partly already done by P1.
  - Honest reframe: the "~48 sleep sites" motivation was a *test-file* count; live path has ~1 prod `time.Sleep`.
    Rebuild case rests on **ack-freeness + 44 tmux-incident beads / 4 workaround generations**, not sleeps.
  - Deletion boundary caveat: `handler.Substrate` also spawns crew/consolidate windows (`crewstart.go:180/290`) — input-stack deletion must NOT touch the spawn path.
  - Orphan-homing folded in: apptap production capture path = C4; codex WAL-guard = C7.
- **2026-07-13 — Stream 4 (Track C enforcement) DONE** → `track-c-enforcement.md` (PENDING Fable verify):
  - **HEADLINE: the ratchet already exists.** Merge gate = `make check-short` → `golangci-lint run --new-from-rev=origin/main`
    (`.github/workflows/ci.yml:42`, `Makefile:239`); full lint is deliberately non-gating (~5666 legacy issues, `Makefile:341`).
    → enabling complexity linters **auto-grandfathers every existing violator, zero `//nolint`, no baseline file**. Least-noisy mechanism, already wired.
  - **Coverage gate also already exists** — `scripts/coverage-gate.sh` (90% floor `internal/**`, 95% spec-cores, 0.3pp regression guard),
    in `make check`/`check-full` but not the merge gate. Track C = extend + add scoped merge-time step + prune stale package refs (not build).
  - Thresholds: `funlen` 100 lines/60 stmts, `cyclop` 15, `gocognit` 20; drop `gocyclo`; exclude `_test.go`+`scenario`/`specaudit`.
  - Blast radius (non-test, brace-matched): 355 funcs ≥50 lines, **104 ≥100**, 44 ≥150, 13 ≥300, 4 ≥1000 (beadRunOne 2367 worst). All grandfathered.
  - **Latent bug caught:** `queue` depguard allow-list omits `github.com/google/uuid` which `rpc.go:32` imports — Track C adds the edge.
  - depguard dead rules (orchestrator/policy/agentrunner/…) confirmed absent on disk → comment-mark "reserved for M5". daemon ceiling = inverse-edge (carved pkgs deny daemon); `runexec` rule ready for M3.
  - Reconciliation: complexity+depguard → fold into `validation-net`; coverage → `quality-system`. One umbrella grandfather bead `codename:complexity-grandfather`.
- **2026-07-13 — Stream 2 (M3 run-state-machine) DONE.** Problem-space + decompose on bench, held at `decompose`. (PENDING Fable verify):
  - Scope: extract `beadRunOne` (`workloop.go:3072–5438`, ~2366 lines, 17 params incl `runSucceeded *bool`) → `runexec` state machine;
    split global `mergeMu` (`:384`, held across rebase→build→push→reset→br-sync) → explicit merge queue; absorb resume-hang as bounded-liveness (daemon SR9 peer).
  - 6 components in 2 phases: C1 daemon ClockPort, C2 merge-queue split (=census "M3-phase-1", startable before M2, HARD prereq for M4),
    C3 workLoopDeps→ports, C4 runexec Step reactor, C5 resume-hang liveness+test, C6 terminal-spine factoring.
  - **Consistent with M2's headline:** P1 already landed `internal/substrate` + `internal/replay` + `specs/replay-substrate.md` + `specs/session-keeper.md`;
    **SK-INV-005 (SR9) is the exact bounded-liveness template M3 mirrors.** M3 builds on a proven seam.
  - Surprise: `worktreeCreateMu` (`:398`, hk-5qp7z) is ALREADY a separate lock → create/merge split partially begun; C2 finishes the merge path.

## Consolidated cross-stream findings (for synthesis)
- **PLAN §8 decisions already RESOLVED by P1 landing** (no operator call needed): #2 substrate generics (→ generics, landed) · #3 typed-decode adopt-vs-delete (→ ADOPT, landed via `internal/replay` + EV-048/D6).
- **ROADMAP doc-corrections needed:** (A) drop `subsystem-proofs → M5` (naming collision; it's DONE test-lanes) — M5 net-new;
  (B) `quality-system` = the acceptance ORACLE, not a Track C source; Track C enforcement is direct work with no existing owner.
- **Ratchet + coverage gate already wired** — Track C is a config extension, not a build.
- Two real latent bugs found in passing: queue↔uuid depguard gap; the git-log-grep false-close (B2).

## Fable review verdicts (independent verification)
- **Reconciliation — VERIFIED. All 6 claims CONFIRMED, none fabricated.** One finding is WORSE than reported:
  **`validation-net` is not just a label gap — 12 of its 13 spec'd VN beads (incl. flagship VN4 `hk-ukhzu`) do NOT exist
  in the `br` DB at all** (only `hk-d5twq`, closed WONTFIX, remains). ⇒ **"rebase validation-net" means RE-FILE the beads, not re-label.**
  Also confirmed the lag pattern is broader (named-queues 12/12@tasks, handler-pause 17/17@problem-space, keeper-redesign 42/42@decompose all lag too).
- **Track B + M1 — VERIFIED. All claims CONFIRMED.** B1 (rpc.go two-writer, 0 `LockForMutation` in rpc.go) + B2 (`--grep` matches any body mention,
  even unanchored so `hk-123`⊂`hk-1234` — slightly worse) are real bugs. M1 LOC counts exact. Typed-decode-not-dead AIRTIGHT: D6 at
  `00-decisions.md:170` "[OPERATOR-LEAN CONFIRMED] … Do NOT delete"; **EV-048 landed normative at `specs/event-model.md:720`**.
  Two minor overstatements to fix in the doc: (1) `replay.go` does NOT consume `DispatchSynchronous` (adopted via D6/EV-048, not called) — headline "every one" is loose; (2) specaudit "zero product code" is false — 3/132 test files import product pkgs → the CI-lint relocation needs a 3-file carve-out. Neither overturns a verdict.
- **M3 run-state-machine — VERIFIED. All 6 claims CONFIRMED, no fabrications.** beadRunOne = 17 params (incl `runSucceeded *bool`) / 2366 lines
  (`workloop.go:3072–5438`); `mergeMu:384` holds build+push+reset+br-sync under one global lock, 5 call-sites exact, 26 `time.Now()`, zero daemon ClockPort;
  `worktreeCreateMu` split partial (field :407); SK-INV-005 = SR9 bounded-liveness at `specs/session-keeper.md:264`. Trivial line nits only.
- **Track C — VERIFIED. All 6 claims CONFIRMED airtight.** Ratchet gate `make check-short`→`golangci-lint --new-from-rev=origin/main`
  (`ci.yml:42`, `Makefile:239`); full lint non-gating (~5666 legacy, `Makefile:339-341`); complexity linters currently absent (grep=0) → enabling them grandfathers all existing code.
  Coverage-gate.sh real (95 core / 90 floor / 0.3pp), stale package list confirmed (orchestrator/reconciler absent). uuid bug real (`.golangci.yml:157-161` vs `rpc.go:32`). Blast radius reproduced EXACTLY (≥100→104, beadRunOne 2367). Thresholds sane (all laxer than defaults ⇒ won't trip unchanged code).
  Two impl notes to fold in: (a) a PR touching an existing giant's SIGNATURE line WILL trip funlen at merge (M3 authors: expect it — it's the ratchet working); (b) the `check-carve-coverage` snippet's `go test … || true` silently swallows compile failures — tighten at impl.
- **M2 agent-input-substrate — VERIFIED. All 5 claims CONFIRMED airtight.** `internal/substrate/` real generics
  (`EventSource[E any]` seam.go:7, `Effector[A any]` :13, `Run[E,A any]` :27, `Twin[E any]` replay.go:82, `ClockPort` clock.go:10);
  **`go test ./internal/substrate/` → ok 0.654s.** Ack-free facts exact (one-method `Substrate`/`SpawnWindow` substrate.go:30; no-op SendInput/CloseStdin; exit0≠accepted :131; blind Enter×3 :1795). Sleep reframe honest (1 live `time.Sleep`). Deletion-boundary real (crewstart.go:180/290/535). kerf held at decompose. Cosmetic nits only.

## PLANNING COMPLETE — status
All 5 forward-planning streams delivered AND independently verified (Fable). Every load-bearing claim CONFIRMED; no fabrications.
Deliverables on disk: `reconciliation.md`, `track-c-enforcement.md`, `track-b-m1.md`, kerf works `run-state-machine` + `agent-input-substrate`
(both held at `decompose`, design deferred until P1 proves the seam), ROADMAP corrections applied, `DECISIONS.md` (operator digest), this log.
**Next actionable (when P1 lands / operator confirms):** advance M2+M3 to design (mirror the proven keeper reactor); implement Track C config; file B1/B2 + M1-specaudit beads.
Minor doc-fixes queued for the authoring agents' outputs (all cosmetic, listed per-stream above): replay.go "every one" overstatement (drop DispatchSynchronous); specaudit 3-file product-import carve-out; paneLiveness/paneSizer line-label swap.

## EXECUTION PHASE (operator approved all decisions; C1/C2/C3 confirmed; no beads — task defs only)
- **2026-07-13 — Track C IMPLEMENTATION dispatched** (Opus, shared tree, config files ONLY — disjoint from P1's `internal/*`+`specs/`).
  Applying `.golangci.yml` complexity linters + queue↔uuid fix + depguard daemon-ceiling/`runexec` edges + coverage-carve; verifying the
  `--new-from-rev=origin/main` ratchet mechanism lands green (grandfathers legacy; flags only new/changed code). **Not committing** (avoids
  interleave with P1 agent's commits). Record → `track-c-APPLIED.md`.
- **2026-07-13 — TASKS registry dispatched** (Fable) → `TASKS.md`: consolidated task defs (NO beads) for Track C (in-progress), B1/B2, M1,
  and design-gated M2/M3/M4 seeds. Ready-now shortlist + gated list.
- M2/M3 design remains HELD (operator approved B6) — P1-gated. M4 reconciliation tasks reflect approved C2 (adopt proven seam) + C3 (split container/egress).
- **2026-07-13 — keeper-restart RESUME; both in-flight agents reconciled.** Re-hydrated as admiral; found the two at-handoff sub-agents had been interrupted by the clear. Reconciled:
  - **TASKS registry (Fable): COMPLETE** — `TASKS.md` written (27 task defs / 8 ready-now / 19 gated; NO beads). Survived the clear and landed on its own.
  - **Track C impl (Opus): was killed mid-flight** (only `.golangci.yml` had landed). Re-dispatched a completion agent (config-only scope) → **DONE + verified:**
    - **Config VALID** (`golangci-lint config verify` exit 0) — the shared-file safety check for the live P1 branch passes; P1 builds won't break on a parse error.
    - **Ratchet grandfathers legacy CLEAN** — `golangci-lint run --new-from-rev=origin/main` flags **zero unchanged giants** (beadRunOne et al. all silently grandfathered). Working exactly as designed.
    - **2 new `gocognit` findings on P1's NEW code** — `internal/replay/replay.go:157` (Replay, 48) + `internal/substrate/replay.go:123` (Twin.replay, 31). This is the ratchet WORKING on new code; Track C enabled `gocognit`, so **these WILL surface at P1's `make check-short` merge gate.** Per directive: NOT silenced — relay to the P1 author (refactor or operator-accept). One lone depguard finding on P1's new `rapid` import is pre-existing `core`-rule, not caused by Track C.
    - **Coverage-carve done but LEFT STANDALONE (not wired into `check-short`)** — `internal/substrate` is at 83.1%, below the 90% floor, so wiring `check-carve-coverage` into the live merge gate would RED every merge until substrate coverage rises (a fix that touches `internal/**`, out of Track C's config-only scope). Target exists in the Makefile with the wire-in condition documented; Fable's note-b compile-swallow bug fixed (fail-closed, no bare `|| true`). As left, **Track C does not affect P1's `check-short`** beyond the 2 gocognit findings above.
    - Record: `track-c-APPLIED.md`.
  - **Queued cosmetic doc-fixes applied** (3): replay.go "every one" overstatement softened (`track-b-m1.md:24`, DispatchSynchronous adopted-not-called); specaudit "zero product code" corrected to 129/132 + 3-file carve-out (`track-b-m1.md:160`); paneLiveness/paneSizer line-label swap fixed (`TASKS.md:92`). ADOPT/keep conclusions all intact.
  - **Uncommitted working-tree changes** (deliberate — P1 owns branch commits): `.golangci.yml`, `scripts/coverage-gate.sh`, `Makefile`, `track-b-m1.md`, `TASKS.md`, + new `track-c-APPLIED.md`.
  - **CROSS-VERIFIED by convergence.** The *original* Track C agent (thought killed) had actually survived the clear too and finished ~concurrently with the re-dispatched completion agent — both edited the shared `.golangci.yml`. Independently confirmed the merged on-disk state is coherent: `config verify` exit 0, single settings block per linter (funlen/cyclop/gocognit), no duplicate enable entries. Both agents independently reported the SAME 2 gocognit findings on P1's new replay code — strong agreement, no corruption from the race.
  - **Merge-gate blast-radius clarified:** full-tree `--new-from-rev=origin/main` = 49 findings, but only **2 are Track C's** (the gocognit pair); the other **47 are pre-existing from already-enabled linters** biting the P1 branch's in-flight changed code (baseline pre-Track-C run = 47, zero funlen/cyclop/gocognit). So Track C's true blast radius on P1 is exactly the 2 gocognit hits.
  - **Forward-planning phase is now fully drained.** All remaining actionable work is P1-gated (advance M2/M3 to design once the reactor seam is proven) or operator-gated (wire the coverage gate after substrate coverage rises; accept/refactor the 2 gocognit findings). No un-gated autonomous work remains.
- **2026-07-13 — PRE-EXECUTION INTEGRATION REVIEW + reconciliation** → `REVIEW-FINDINGS.md`. Ran the "review before execute" gate that had never happened: Fable verified each *stream's* claims in isolation, but nobody checked whether the milestones/decisions/deps/tasks **hang together**. 3 parallel adversarial reviewers (dependency/sequencing · completeness/gaps · contradiction/staleness). **No dependency cycle; near-term decomposition sound.** Found ~18 real integration defects — one (TC-4's coverage claim) flagged by ALL THREE. Applied the full reconciliation across the 6 plan docs via 2 disjoint-doc-set agents:
  - **TC-4 corrected** (all-3 finding): docs claimed `check-carve-coverage` was wired into `check-short` and substrate passed the 90% floor — both false (substrate 83.1%, gate left standalone, wire-in gated on ≥90%). Fixed in TASKS + track-c-enforcement.
  - **UNGUARDED DELETION HAZARD closed:** M2-6 deletes `pasteinject.go`, the input path P1's shipped keeper vertical still uses — no dependency edge forced "keeper migrates to the M2-2 driver first." Same class as the event-registry hazard (which WAS caught); this one wasn't. Added the guard to M2-3 + M2-6 + M2-6's deletion boundary. **This was the finding most likely to cost a rebuild.**
  - Staleness fixes where P1/ratification outran the docs: ROADMAP body now matches its own Corrections (dropped `subsystem-proofs→M5`, `quality-system→Track C`; T6 landed); PLAN annotated `SUPERSEDED BY P1` on the dead-event-registry + daemon-resume-hang framing (marked, not reopened); testing-strategy-uplift aligned to operator-approved B5 (SUPERSEDE); specaudit "zero product code" residuals fixed (129 relocate + 3 carve-out); `admiral-initiatives.md` un-frozen (ratified 2026-07-13).
  - Missing edges/tasks added: M4 prereqs stated precisely (M3-2 + M2-1) consistently in ROADMAP+TASKS; M2/M3 parallelism narrowed to the one real M3-4→M2-1 edge; M3's "M1 lands" gate → "M1-1 landed"; new M1-5 coverage-audit producer; deferred-orphans section (9 in-passing findings homed); Track C commit-ownership (TC-7); gocognit-findings task (TC-8); M5 gated placeholder + un-hold trigger; M2/M3 un-hold proof condition; validation-net marked hollow + SCOPE-CALL-PENDING.
  - **3 items surfaced to operator (NOT auto-decided), see `REVIEW-FINDINGS.md` §B:** B1 validation-net scope (in or out?) — the one true scope call; B2 M3-phase-2→M2 edge (design-phase, deferrable); B3 the 3-file specaudit disposition (recommendation: keep under `go test`).
  - **Plan is now execution-ready and internally consistent.** All edits in `plans/` + `admiral-initiatives.md`, disjoint from P1's live branch, uncommitted.
