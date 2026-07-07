# Quality / Testing-System — kerf state map & gap-to-tasked-out

**Date:** 2026-07-06 · Author: planning subagent (for admiral) · Read-only investigation.

Purpose: reconcile the multiple overlapping quality/testing artifacts into ONE picture so the admiral
can hand medium chunks to the captain. TL;DR — there are **4 kerf works + 2 plan-only docs + a testbed
design** all circling the same goal. Only ONE is fully tasked-out (remote-test-pyramid, and it's DONE).
The two headline plans (quality-loop, quality-process) have **zero kerf work and zero beads** — they are
prose only. The single biggest gap is: turn the quality-process/quality-loop plans into ONE kerf work.

## 1. Artifact table

| Artifact | Type | Current stage | Beads done/open | SINGLE next action to advance |
|---|---|---|---|---|
| **remote-test-pyramid** | kerf work (implementation jig) | `breakdown` (pass 1/… but work is functionally COMPLETE) | 10/0 (all closed) | `kerf square remote-test-pyramid` then `kerf status … finalize` — it's done, just close it out. The L0–L5 runner-seam + twin harness it built is the FOUNDATION the quality-process Lane-1 twin reuses. |
| **testing-strategy-uplift** | kerf work (plan jig) | `integration` (pass 6/8) — has SPEC.md, 05-specs/, integration-review.md on bench | validation beads hk-lz485 / hk-tgiu9 filed at breakdown (not found in current `br` — stale/closed) | Advance pass 6→7: `kerf resume testing-strategy-uplift` and run the **Tasks** pass to emit `07-tasks.md` + beads. It is 3/4 of the way through planning but has produced NO live bead backlog. |
| **test-daemon-harness** | kerf work (plan jig) | `problem-space` (pass 1/8) — only 01-problem-space.md on bench | none (bead_filter: none) | Either advance `problem-space→analyze`, OR **fold into the new quality-system work** (see §2). It overlaps heavily with quality-loop Phase 5 (continuous self-test daemon) and the daemon-testbed design. |
| **eval-program** | kerf work (plan jig) | `problem-space` (pass 1/8) but 30 beads already attached | 14/16 | This is the MODEL-EVAL program, **NOT the software-quality/testing initiative** — different lane (which model, DGX load-scaling, judge rubrics). Keep separate; do not merge. Next: work its 16 open WS beads (WS1 metrics plumbing hk-9jdid is top-ranked). |
| **eval-harness** | kerf work (plan jig) | `problem-space` | 1/4 | Sub/adjacent to eval-program (collector). Same model-eval lane, not software-quality. Keep separate. |
| **quality-process** | **PLAN-ONLY** (no kerf work; `kerf show` → not found) | plan docs only: PLAN.md + 00-forensics + 01-defect-entry + 02-shift-left-design | ZERO beads | **Create a kerf work** (see §2). This is the current HEADLINE plan — P1–P5 shift-left process changes + Lane 1 (epic-branch acceptance gate) + Lane 2 (async fast-follow). |
| **quality-loop** | **PLAN-ONLY** (no kerf work) | single PLAN.md | ZERO beads | Superseded-as-headline by quality-process, but its **Phase 0/1/3 un-suppression tranche is still required and unbuilt** (11 fabricated-green suppression points). Fold Phase 0/1/3 into the new work as tranche 1. |
| **daemon-testbed-design** (agent-world-models dir) | design note (plan-only, no kerf work) | design note, operator-endorsed 2026-07-05 | ZERO beads | This IS quality-process Lane-1 XT with a durable corpus. Layer 0 (Docker substrate) + Layer 1 (Claude Code twin) + Layer 2 (LLM generator). Route into the new quality-system work as the Lane-1 build. |

## 2. Reconciliation — collapse the 4 parallel efforts into a clean shape

**The overlap problem:** quality-loop, quality-process, test-daemon-harness, and daemon-testbed-design
are four descriptions of substantially the same system (isolated scratch/twin daemon runs real workflows,
asserts known failure modes stay fixed, feeds failures back as beads). remote-test-pyramid already BUILT
the runner-seam + twin harness they all assume. Left as-is this is 4 parallel overlapping efforts.

**Recommended consolidation — ONE new kerf work + keep model-eval separate:**

- **CREATE new kerf work, codename `quality-system`** (or `quality-gate`) — the umbrella for the
  software-quality initiative. Its source material: `plans/2026-07-05-quality-process/` (headline P1–P5 +
  Lane 1/2), with `plans/2026-07-04-quality-loop/` Phase 0/1/3 as its first tranche, and
  `daemon-testbed-design.md` + `scenario-harness-framing.md` as the Lane-1 build design.
  - Per BOTH plans' explicit guidance: do NOT file as one mega-epic. Structure as tranches:
    - **Tranche 1 (un-suppression, from quality-loop Ph0/1/3):** stop the 11 fabricated-green suppressions,
      fix the 42 timing-out daemon scenario tests + 110 skipped E2E, then flip gates fail-closed + branch
      protection. Unambiguous, highest-trust-restoration.
    - **Tranche 2 (shift-left process, from quality-process P1–P4):** failure-modes as gated spec artifact,
      independent blocking state-change review, real-boundary tests, canonical liveness contract.
    - **Tranche 3 (Lane-1 acceptance gate + testbed):** epic-on-branch + AT/CR/LT/XT beads; build the
      Layer-1 scripted twin (reusing remote-test-pyramid's seam) → Layer-0 Docker substrate → replay →
      adversarial → (defer Layer-2 LLM generator).
    - **Tranche 4 (Lane-2 async fast-follow):** the `every@15m` scheduler job + `fast-follow.sh` + wire
      orphaned `harmonik smoke`.
- **FOLD `test-daemon-harness`** (stuck at problem-space, no beads) INTO the new work as the Tranche-3/4
  continuous-self-test piece — do not advance it as a separate work; `kerf shelve test-daemon-harness`.
- **ADVANCE or CLOSE `testing-strategy-uplift`** — it's a genuine, near-complete plan (pass 6/8, has a
  SPEC) but predates (2026-05-20) the quality-process reframe. Decide: run its Tasks pass to harvest its
  SPEC into beads, OR explicitly supersede it by quality-system. Do not leave it dangling at `integration`.
- **CLOSE OUT `remote-test-pyramid`** — 10/10 beads done; run `kerf square` + finalize. It's foundation,
  already delivered.
- **KEEP SEPARATE:** `eval-program` + `eval-harness` are the MODEL-evaluation lane (which model / DGX /
  judge rubrics), not software quality. They only *look* adjacent because of the word "eval." Do not merge.

## 3. Scripts + Docker inventory

**EXISTS (shipped, in `scripts/`):**
- `scripts/scratch-daemon.sh` (44 KB) — isolated 2nd daemon on separate clone; `init/build/up/status/down/
  cycle/batch/feedback`. `batch` runs workflows end-to-end + structured pass/fail; `feedback` files scratch
  failures back as deduped MAIN-repo beads. This is the ~80% "already built" primitive both plans cite.
- `scripts/scratch-daemon-smoke.sh` (24 KB) — smoke harness for the scratch daemon.
- `scripts/smoke-scratch.sh` (5 KB) — older/adjacent scratch smoke.
- `scripts/scenario-gate.sh` (16 KB) + `internal/daemon/scenariogate.go` — per-commit scenario gate
  (currently **fail-open by design** — quality-loop suppression point #4).
- `scripts/coverage-gate.sh` (95%/90%) — exists but only in `make check`, which CI never calls.
- `scripts/eval-grade.sh`, `scripts/ubs.sh`, `scripts/secret-scan.sh`, `scripts/validate-commit-msg.sh`.
- Scheduler primitive: `internal/schedule/` (fixed-interval jobs) — shipped, cited as the Lane-2 / Phase-5
  substrate.
- `cmd/harmonik/smoke.go` (`harmonik smoke`) — the 5-signal end-to-end functional check, **orphaned**
  (never invoked by release/redeploy/watchdog). Both plans want it wired in.

**PLANNED-ONLY / MISSING (named in plans, do not exist yet):**
- `scripts/fast-follow.sh` — MISSING. Lane-2 async job (changed-pkg `go test -short` + `harmonik smoke`).
- `scripts/selftest-cycle.sh` — MISSING. quality-loop Phase 5 wrapper (pull main → rebuild → batch corpus
  → feedback → comms alert).
- A **curated, versioned corpus of real production workflows** — MISSING (only throwaway beads today); both
  plans flag this as the one piece of real authoring / the long pole.
- One seeded `every@15m` (or hourly/nightly) schedule job — PLANNED, not seeded.

**DOCKER:**
- **No Dockerfile / docker-compose exists anywhere in the repo** (`find` for Dockerfile* / docker-compose*
  → zero results). The only docker mentions in `plans/` are pi-sandbox (`srt`/bwrap OS-sandbox, a different
  effort) and eval-program's `~/models docker-compose` (external DGX box, model-eval lane).
- The **"docker process to run things in"** the operator referred to = **Layer 0 of
  `plans/2026-07-05-agent-world-models/daemon-testbed-design.md`**: a dockerized controllable substrate
  (disk/CPU/network/clock dials, clean reset) the scratch daemon runs inside. It is **DESIGN-ONLY, not
  built** — no container artifact exists. Build order in that doc puts it as step 2 (after the Layer-1
  scripted twin).
