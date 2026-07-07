---
schema_version: 1
crew_name: jamis
queue: jamis-q
epic_id: hk-420yr
goal: "Subsystem proofs: file-disjoint unit lanes for DOT-verdict, promote/reconcile, and br-adapter (codename:subsystem-proofs)"
captain_name: captain
model: sonnet
---

# Mission: Subsystem proofs (crew jamis) — codename:subsystem-proofs

You are crew member **jamis**, owning the **subsystem-proofs** (Lane-4) lane on
queue **jamis-q**. Report status to **captain**. Epic **hk-420yr**.

## On boot
1. Confirm `$HARMONIK_AGENT == jamis`. If not, STOP and report.
2. `harmonik comms join` + confirm identity = jamis.
3. `br update hk-420yr --assignee jamis` (mirror for attribution — load-bearing).
4. Arm inbox: `harmonik comms recv --agent jamis --follow --json`.
5. Post a boot status to **captain** (`--topic status`) + a `br comment` on hk-420yr.

## What this lane is

A **lightweight batch of three FILE-DISJOINT sub-batches** turning scattered
subsystem cases into repeatable acceptance + regression suites. Pure Go/shell.
**NO Docker, NO twin, NO coreloop, NO scratch-daemon. Zero infra risk.** Does
**NOT** wait on alia (hk-1yxhh coreloop assertion library).

**READ FIRST:** `plans/2026-07-06-quality-system/13-substrate-and-lane4-plan.md`
— the **LANE B — `codename:subsystem-proofs`** section (the file-disjointness
table, and the B1/B2/B3 tranche detail). It is the authoritative design.

## Step 1 — RUN YOUR OWN KERF PASSES to emit the sub-batches as beads

Do NOT trust a pre-baked task list — **run your own kerf passes** over the
design doc to emit the **3 file-disjoint sub-batches B1/B2/B3** as beads under
label `codename:subsystem-proofs` (so they roll up to epic hk-420yr). The three,
per the plan:

- **B1 — DOT verdict-parsing (pure unit).** `internal/workspace/reviewverdict.go`
  (`parseReviewVerdict`, `retryVerdictReadOnMalformed`, ErrMalformed mid-write
  salvage) + `internal/workflowvalidator` DOT parse/validate. Pure in-process
  unit tests over byte/string literals — no files, no daemon. **Hand FIRST.**
- **B2 — promote/reconcile on a temp git repo.** `cmd/harmonik/promote_cmd.go`
  (push-mode `runPromotePush`, bead-ID trailer, non-ff rebase-retry) +
  `internal/lifecycle/orphansweepbeads.go` `GitMergeCommitScanner` against a real
  temp repo. Reuse the proven `setupPromoteRepo` fixture in
  `promote_cmd_hkpk3p1_test.go`. PR-mode needs `gh` — scope to push-mode + PR-mode
  arg-construction unit test; leave live-`gh` out.
- **B3 — br-adapter on a temp `.beads/`.** `internal/brcli` `Adapter` against a
  real `br` binary via `NewForProject(brPath, t.TempDir())` + `br init`. Read ops
  + terminal-transition writes (Claim/Close/Reopen/ResetBead). Guard with
  `LookPath("br")` skip (pattern: `internal/daemon/t5_realdb_concurrent_test.go`).
  Prove daemon-owns-terminal-transitions + the ResetBead stranded-reset primitive
  (adapter primitive only; daemon-loop wiring stays in alia's lane).

The three targets are **confirmed file-disjoint** from each other and from the
dispatch (alia), keeper, and comms lanes — no merge contention.

## Step 2 — build discipline (C-model)

- Build on branch **`integration/subsystem-proofs`** in your **OWN worktree**.
  Commit to that branch — **NEVER main**. The **daemon executes** the queued
  work; you orchestrate. `integration/subsystem-proofs → main` is **one
  assessor-gated human PR** at the end.
- **PREFER the CODEX build path** (Claude cap ~98%). Do **NOT** use pi — it is
  blocked (hk-4ir08).
- The 3 sub-batches are file-disjoint, so you **MAY run them as parallel internal
  sub-agents** / concurrent queue dispatch on jamis-q with no cross-batch
  contention.

## Bug discipline

On ANY defect you find (in code under test or elsewhere), append a **terse
block** to repo-root `BUGS.md` (what, where, repro, severity). Do not derail the
lane to fix out-of-scope bugs — log and continue.

## Progress feed

Post progress to **captain** (`--topic status`) AND as `br comment`s on the
sub-beads / epic hk-420yr: on every bead close, on a ≤10-min timer while
dispatching (≤15-min when idle/draining), and at boot + drain bookends.

## Done

All B1/B2/B3 beads closed with green tests on `integration/subsystem-proofs`;
post a drain status to captain naming the branch ready for the assessor-gated PR.
