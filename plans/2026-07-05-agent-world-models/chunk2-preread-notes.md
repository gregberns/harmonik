# HANDOFF TO stilgar (2026-07-07, admiral OPTION B — schmidhuber reverted to research-only)
The scripted-twin lane (epic hk-ynyd3) is now **stilgar's**. schmidhuber is OFF it. State at handoff:
- **ST1 hk-xeqtv** — DONE on main (twin-scripts scenarios/regression/merge-race-samefile/twin-scripts/alpha.yaml + beta.yaml).
- **ST3 hk-q3u57** — DONE on main (commit_on_cue sentinel_name, commit 07b5ddad).
- **ST2 hk-xybb7** — SALVAGED: captain cherry-picked the fixture to main (009c552f, incl 8c73298b fixture-create). **scenarios/regression/merge-race-samefile.yaml is LIVE on main.** Bead hk-xybb7 likely still status=open → may need `harmonik reconcile` to close so the chain unblocks. It twice-failed on reviewer-stall/verdict-absent (NOT a fixture bug — work was complete).
- **ST4 hk-ifu19** — register fixture in §10.1 conformance floor (internal/scenario/conformancecorpus_test.go). Dep ST2 (met by salvage) — verify br-ready.
- **ST5 hk-psrnc** — run merge-race vs REAL daemon (scratch-daemon clean-reset, zero tokens) = **the Layer-1 twin PROOF + captain's redeploy gate.** Captain explicitly wants this fired (comms). Dep ST2+ST4.
- **ST6 hk-pnjgh** — epic acceptance + assessor gate (--topic gate at boundary). Dep ST5.
- Targeting convention: **main via merge-base** (NOT integration/scripted-twin — that branch is vestigial).
- Known fleet hazards this lane hit: post-commit-non-exit wedge + reviewer-stall/verdict-absent (salvage via cherry-pick of the run/ branch commits); phantom-gridlock on daemon restart (do NOT churn-resubmit — one clean dispatch, wait).

---

# Chunk 2 (scripted-twin) — pre-read notes (loaded, awaiting T2-green handoff)

*My design (daemon-testbed-design.md) became admiral's Phase-2 plan. My assigned chunk = **Chunk 2
`scripted-twin`**, integration branch **`epic/scripted-twin`**, gated on chunk-1's assertion package
landing green on `integration/core-loop-proof`. Plan: plans/2026-07-06-quality-system/06-phase2-plan.md.*

## Corrections absorbed (vs my original design note)
- Local twin covers the **same-file merge race** (two twins commit the SAME FILE → loser fails safe
  `non_ff_merge`), **NOT** the remote worktree-CREATE race (hk-5qp7z/hk-lt091 — that's a remote
  multi-worker git-worktree-add HEAD race, deferred to Phase 3 / remote substrate).
- Scenario fixtures live at repo-root **`scenarios/<group>/<name>.yaml`** (NORMATIVE per
  scenario-harness.md). `internal/scenario/` is the Go harness package, NOT a fixture dir.
- **Extend the existing seam, do NOT fork/rebuild it.**

## The existing seam (already in-tree — build ON this)
- **`cmd/harmonik-twin-claude/scriptdriver.go`** — twin driver. Types `ScriptFile` / `ScriptMessage`
  (yaml), `loadScriptFile`, `runScript`. Step primitives already exist: `call_stop_hook`,
  **`commit_on_cue`** (← the primitive the merge-race needs: each twin commits a file on cue),
  `signal_interrupt`, `hold`. heartbeat_mode wall_clock|scripted per HC-036a.
- **`cmd/harmonik-twin-claude/scenarios.go`** — canned scenarios (`scenarioSingleHappyPath`,
  `scenarioReviewLoop3Iter`, `scenarioCommitOnCueStartupDelay`, …) to pattern-match a merge-race one.
- **`cmd/harmonik-twin-claude/wire.go`** + `wire_ndjson_test.go` — NDJSON wire framing (also the
  chunk-4 replay-capture format target).
- **`internal/scenario/`** — the Go harness: `agentoverride.go` (SH-008 twin injection via
  `agent_overrides`), `asserteval.go` / `assertionresult.go` (assertion consumption — hooks chunk-1's
  event-stream assertion lib), `conformancecorpus_test.go` (§10.1 floor — REGISTER the new fixture here),
  `crashrecovery.go`.
- **`scenarios/`** at repo root EXISTS with groups `_workflows/`, `regression/`, `smoke/` — fixture home.
- Twin-script YAML schema = **HC-036a** (handler-contract.md §4.8): `heartbeat_mode` +
  `messages[]{type,payload,relative_timestamp_ms}`, at `<fixture-root>/<scenario>/twin-scripts/<role>.yaml`.
- Twin injection = **SH-008–SH-011** (scenario-harness.md): `agent_overrides[role]` → twin binary, hash-
  checked, search-path `twins/`. NO new spawn hook, NO runtime real-vs-twin branch in production code.

## Chunk-2 deliverables (from the plan)
1. Extend scriptdriver/scenarios/wire for the merge-race twin behavior (reuse `commit_on_cue`).
2. New fixture `scenarios/<group>/merge-race-samefile.yaml` + twin-scripts per role; register in
   `conformancecorpus_test.go`.
3. Assertion helper consuming chunk-1's event-stream assertion lib: loser `non_ff_merge`, both beads
   terminal, no corrupt merge.
4. Runs green vs the REAL daemon, ZERO Claude tokens, repeatable across `scripts/scratch-daemon.sh`
   clean reset, daemon does NOT special-case the twin.

## CREATED 2026-07-07 (trigger fired, event 019f3b34)
- kerf work **`scripted-twin`**; epic **hk-ynyd3**.
- Branch **`integration/scripted-twin`** = commit 58af0da1 (pinned T2 contract), pushed to origin.
- Tasks (label `codename:scripted-twin`, P1):
  - ST1 **hk-xeqtv** — merge-race twin-scripts (commit_on_cue) — READY
  - ST3 **hk-q3u57** — verify/extend commit_on_cue two-twin same-file — READY
  - ST2 **hk-xybb7** — scenario fixture consuming EvaluateAssertions — blocked by ST1+ST3
  - ST4 **hk-ifu19** — register in §10.1 conformance floor — blocked by ST2
  - ST5 **hk-psrnc** — run vs REAL daemon, scratch-daemon clean-reset, zero tokens — blocked by ST2+ST4
  - ST6 **hk-pnjgh** — epic acceptance + assessor gate — blocked by ST5
- Pre-existing related P2: **hk-zk0v2.3** (scenariotest/ twin-driving helper) — candidate to fold into ST1.
- OPEN blocker for first build: daemon merges to origin/main by default (config.yaml §5.2). BLOCKER
  IDENTIFIED = **hk-lgykq** (OPEN, hawat): "Per-bead/DOT integration-branch targeting: wire dead
  LandsOn/landTaskBranch path into live workloop merge." The per-bead integration-branch-targeting path
  is DEAD, so beads can't merge to integration/scripted-twin. ST1+ST3 now DEP on hk-lgykq (gated — won't
  auto-dispatch into a main-merge). Dispatch ST1+ST3 the moment hk-lgykq closes.

## TARGETING REALITY (2026-07-07, discovered on ST3 completion)
Beads land on **origin/main via merge-base**, NOT on integration/scripted-twin. ST3 hk-q3u57 completed
= commit 07b5ddad on main (correct: adds sentinel_name to commit_on_cue). hk-420yr's beads also landed
direct on main via merge-base ("exactly right" per captain) → main-via-merge-base is the CURRENT fleet
convention, superseding the integration-branch+human-PR model in 06-phase2-plan.md. integration/scripted-
twin branch (created @58af0da1) is VESTIGIAL unless captain wants per-bead LandsOn targeting (asked, event
019f3cfe). ST2/ST4/ST5 will build on ST1+ST3's main commits via merge-base — chain works naturally.
Progress: ST3 hk-q3u57 CLOSED (on main), ST1 hk-xeqtv in_progress.

## Handoff trigger + my move
HOLD until chunk-1's assertion package exists at a stable exported path + green on
`integration/core-loop-proof` (building the twin against an unpinned contract = moving target). On the
handoff: turn this into kerf tasks on `epic/scripted-twin`, build-in-own-worktree, non-Claude fleet path,
assessor gate at epic boundary. Runs concurrently with Chunk 3 `scratch-substrate` (a different crew).
