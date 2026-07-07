# Quality-System — admiral synthesis (2026-07-06)

Consolidates the four scoping passes in this dir (01 kerf-state · 02 bug-corpus · 03 build-chunks ·
04 org-model). Operator directive: build a WHOLE system that vets the daemon after changes BEFORE it
replaces the live binary. Single focus. Don't over-plan — medium chunks, captain delegates, parallelize.

## 1. Consolidation decision (kills the 4-overlapping-efforts problem)
- **Create ONE kerf work `codename:quality-system`**, sourced from `plans/2026-07-05-quality-process/`
  (P1–P5 + Lane 1/2). Tranche it — NOT a mega-epic.
  - Fold in: `quality-loop` Phase 0/1/3 (un-suppress fabricated-green CI) as tranche 1 prereq;
    `daemon-testbed-design.md` as the Lane-1 build spine; `test-daemon-harness` (shelve it, folded).
- **Close out `remote-test-pyramid`** (10/10 done — it already built the runner-seam + twin foundation
  the testbed assumes; `kerf square` + finalize).
- **Decide `testing-strategy-uplift`**: run its Tasks pass to harvest its SPEC into beads, or supersede.
- **Keep `eval-program`/`eval-harness` SEPARATE** — that's the model-eval lane, not software quality.
- Build backlog that exists only as design: **no Dockerfile anywhere** (Layer-0 greenfield),
  `fast-follow.sh`, `selftest-cycle.sh`, curated workflow corpus. Exists: `scratch-daemon.sh`,
  `internal/schedule`, orphaned `harmonik smoke`.

## 2. Build chunks (integration-branch epics; honor build-in-own-worktree rule)
Serial spine: **1 → (2 ‖ 3) → 5 → 6**, with 4 off 2. Two crews concurrent once chunk 1 lands.

1. **`core-loop-proof`** (FIRST, blocks all) — live-verify the real task-processing loop on a scratch
   daemon: bead→queue→harness starts with the CORRECT model→makes real changes→talks to provider through
   the sandbox→DOT reviewer feeds verdict back, across {claude,codex,pi}×{local,remote}. Non-Claude rows
   token-cheap; Claude row flag-gated. **Directly closes bug-gaps 1,2,3,5.**
2. **`scripted-twin`** — Layer-1 deterministic agent twin + concurrent-same-file-merge scenario. No tokens.
3. **`scratch-substrate`** (‖ with 2) — Layer-0 Docker + clean-reset + disk dial; port cache-wipe scenario.
4. **`twin-replay`** (after 2) — record a real Claude session, replay token-free.
5. **`adversarial-corpus`** (needs 2+3) — adversarial overlays + failure-corpus regression tests + Lane-1
   XT break-testing. Largest; splittable.
6. **`chaos-generator`** (LAST, gated on 4+5) — Layer-2 LLM generator, judged by the real daemon.

## 3. Bug corpus → what the harness must prove (task-processing lens)
8 clusters; the 5 top coverage gaps (supported but NO e2e proof) are chunk-1's acceptance criteria:
1. model actually reaches the harness per family (the whole pi-model-leak week — zero coverage)
2. remote(tcp://) path == local path (gb-mbp critical path, repeatedly diverges)
3. provider comms through the sandbox (harness→sandbox→provider→tool_call→commit never driven)
4. queue-submit → dispatch field fidelity (custom workflow/model overrides silently dropped)
5. real Claude-worktree startup → agent_ready (PR-19 fleet outage; green unit tests, unproven)
Testbed corpus already covers C1/C3/C5/C6-partial; ADD C2, C4, C6-provider, C7, C8.

## 4. Org model (RECOMMENDED — needs operator sign-off; reshapes manifests)
**New dedicated `assessor` agent-manifest = the gate EXECUTOR**, spawned per epic at the gate boundary,
DIRECTED by the admiral, structurally separate from the captain that built the work.
- admiral = gate AUTHORITY (decides when gate fires, receives verdict, holds the merge/deploy) — keeps its
  read-assess-STOP altitude; does NOT run tests.
- captain = builder (unchanged). assessor = executor (runs LT live-verify + XT break-testing + CR review
  on an isolated scratch clone; files findings as beads; posts PASS/BLOCK).
- Why not the operator's literal "admiral runs validation": bolting execution onto the hourly oversight tier
  collapses its neutrality + cadence. Why not captain-owns-both: it's the P2 same-frame-reviewer violation
  (a cleared epic is the captain's own scorecard).
- The block is DETERMINISTIC: the set of open P0/P1 `found-by:*` beads on the epic branch IS the gate.
- Hand-off chain: crew posts `--topic gate` when epic branch fully closed → captain verifies + relays to
  admiral → admiral spawns assessor on the branch → assessor runs LT+XT+CR, files bead findings, posts
  PASS/BLOCK → admiral holds the single human `epic→main` PR until PASS. Lane-2 small merges stay
  agent-light (scheduled `fast-follow.sh`, no assessor).

## 4b. `assessor` charter (operator-confirmed 2026-07-06 — admiral-overseen crew member)
The assessor is a crew-member reporting up the admiral chain. It REPORTS/VERDICTS on:
1. **Merge-gate** — can this integration branch be merged to main? (runs LT live-verify + XT break-testing +
   CR independent review on an isolated scratch clone; open P0/P1 `found-by:*` beads on the branch = BLOCK).
2. **Deploy-gate (GATE-0)** — is a specific commit reliable ENOUGH to deploy as the live daemon? (isolated
   e2e reproducing the changed behavior must be green; this is the enforcement point for the 24h rule).
3. **Own + grow the regression corpus** — every new confirmed bug becomes a permanent testbed scenario.
4. **Run the exploratory break-testing fan-out** (Lane-1 XT); file findings as `found-by:assessor` beads.
5. **Deploy-readiness report** — what was tested, what passed, residual risk — admiral uses it to authorize.
Admiral = gate AUTHORITY (holds the human epic→main PR + the deploy decision); assessor = executor;
captain = builder. Lane-2 small merges stay agent-light (scheduled `fast-follow.sh`, no assessor spawn).

**Assessor manifest STATUS (2026-07-06):** authored at `.harmonik/agents/assessor/{soul,operating,
manifest}.yaml` + independently reviewed → **APPROVE, no edits** (`harmonik agent check assessor` → ok;
full verdict + 4 resolved flags in `05-assessor-manifest-notes.md`). Two wire-up notes for when it's
spawned: (i) the deterministic block must query beads with `--label-any` over the known `found-by:` sources
(`br list --label` is EXACT-match, `found-by:*` does NOT glob-expand); (ii) settle the mission-handoff
schema at wire-up. Not wired into any launcher yet.

**TODO (operator 2026-07-06 — not needed yet, but required before the gate is trustworthy): the assessor
needs a "GOOD ENOUGH" severity/decision FRAMEWORK, applied to BOTH releases AND local redeploys.** Rules:
a MAJOR regression BLOCKS everything (no merge, no release, no redeploy). A SMALL issue found in testing
does NOT block — one or more of merge/release/redeploy may proceed, recorded as a tracked KNOWN ISSUE
(a `found-by:assessor` bead that stays open, not a block). So the framework = a severity rubric
(major/blocking vs minor/known-issue) + a per-action (merge | release | local-redeploy) allow/block decision
+ a known-issue ledger the deploy-readiness report surfaces. Design this as part of the assessor's gate
mechanics (Phase 1 gate-bootstrap or early Phase 2).

## 4c. PHASING (operator 2026-07-06 — plan-a-phase-then-hand-off, less waterfall)
Admiral defines the 3-phase skeleton; captain runs the FULL kerf cycle on ONE phase at a time, so the
captain never waits for the whole plan. Admiral plans Phase N+1 while the captain builds Phase N.

- **Phase 1 — Core-loop acceptance + gate bootstrap** (PLAN FULLY + TASK NOW):
  chunk `core-loop-proof` (the real task-processing loop across {claude,codex,pi}×{local,remote}; acceptance
  = the top-5 coverage gaps in §3) + close `remote-test-pyramid` + stand up the `assessor` manifest and the
  deterministic merge-gate/deploy-gate mechanics (found-by-beads = block). This is the "start with the core
  system" phase the operator named.
- **Phase 2 — Deterministic testbed** (plan when Phase 1 is building):
  `scripted-twin` (Layer-1) + `scratch-substrate` (Layer-0 Docker) + `twin-replay`; port the failure corpus.
- **Phase 3 — Exploratory + generative + fast-follow** (plan later):
  `adversarial-corpus` + XT break-test fan-out + `chaos-generator` (Layer-2) + Lane-2 async fast-follow.

Parallel admiral-owned track (NOT captain's build queue): author the `assessor` agent-manifest now so it's
ready for Phase 1's first epic boundary.

## 5. Sequencing for the captain (does NOT block on the org decision)
Building can start now; the assessor/gate only matters at the FIRST epic boundary. So:
- Immediately: create `quality-system` kerf work + drive its problem-space/design tranche for chunk 1;
  spin `integration/core-loop-proof`; staff it (prefer non-Claude fleet path under the token crunch).
- In parallel: close `remote-test-pyramid`; decide `testing-strategy-uplift`.
- Operator decides the org model meanwhile; assessor manifest gets authored before chunk 1 reaches its gate.
