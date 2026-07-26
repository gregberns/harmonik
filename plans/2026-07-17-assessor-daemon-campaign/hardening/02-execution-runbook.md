# Hardening input 2 — §6 Execution Runbook (executability pass)

> Produced 2026-07-18 by a hardening agent. Ready-to-paste as PLAN.md §6.
> Angle: make the campaign executable, pinned to a moving tree, re-runnable, and resumable.

**Executor:** assessor · **Sandbox:** isolated scratch daemon via `scripts/scratch-daemon.sh`
· **Tree:** moving (`phase1-session-restart-substrate`, captain landing mediums separately).

All artifacts under `plans/2026-07-17-assessor-daemon-campaign/runs/<PASS_ID>/`. Operate from
`$HARMONIK_PROJECT` (repo root) only — **never `cd` into the scratch clone**; drive it via
`scripts/scratch-daemon.sh <sub> "$SCRATCH"` and `git -C "$SCRATCH"`.

## §6.0 Two-pass model (pinned to a moving tree)

| Pass | Trigger | PASS_ID | Meaning |
|---|---|---|---|
| **Exploratory** | starts NOW, current HEAD | `exploratory-<sha8>` | Shakes the daemon out against a moving tree. Findings real + filed, but PASS/BLOCK **provisional**. |
| **Baseline** | captain posts `mediums-complete` | `baseline-<sha8>` | Authoritative. Full re-run S1–S7 from fresh clone at the clean SHA. **Only a baseline pass produces the ASSESSMENT.md the admiral gates on.** |

Re-run policy: when `mediums-complete` arrives mid-exploratory, finish the current suite,
checkpoint, then **start a fresh baseline pass from a new clone** — do not hot-swap the binary
under a running pass (a moving SHA mid-pass poisons attribution).

## §6.1 STARTUP (exact first commands)

```bash
# (a) IDENTITY + CWD guard
test "$HARMONIK_AGENT" = assessor || { echo "NOT ASSESSOR — abort"; exit 1; }
cd "$HARMONIK_PROJECT" && git rev-parse --show-toplevel   # must == $HARMONIK_PROJECT
harmonik comms join --name assessor
harmonik comms recv --agent assessor --follow --json &     # arm inbox; re-join on <=90s timer

# (b) PIN THE BUILD
SRC="$HARMONIK_PROJECT"
PIN_SHA=$(git -C "$SRC" rev-parse HEAD)
PIN_BRANCH=$(git -C "$SRC" rev-parse --abbrev-ref HEAD)
PASS_KIND=exploratory                                      # or: baseline
PASS_ID="${PASS_KIND}-${PIN_SHA:0:8}"
RUN_DIR="plans/2026-07-17-assessor-daemon-campaign/runs/$PASS_ID"
SCRATCH="/tmp/h-assessor/scratch-$PASS_ID"
mkdir -p "$RUN_DIR"
export TMPDIR=/tmp/h-assessor CODEX_HOME=/tmp/h-assessor/codex GOCACHE=/tmp/h-assessor/gocache
unset OPENAI_API_KEY

# (c) STAND UP + ISOLATION-VERIFY (clone pinned to PIN_SHA)
scripts/scratch-daemon.sh init  "$SCRATCH" "$SRC"
git -C "$SCRATCH" checkout --detach "$PIN_SHA"
scripts/scratch-daemon.sh build "$SCRATCH"
scripts/scratch-daemon.sh up    "$SCRATCH"
scripts/scratch-daemon.sh status "$SCRATCH"
# run the §C pre-flight isolation check (hardening doc 03) — ALL must hold before any suite.
# (d) BEGIN S6 (background watcher) THEN S1 (foundation) — see §6.2
```

Write RUN-LOG header (§6.4) and CAMPAIGN-STATE.json (§6.5) **before** the first assertion.

## §6.2 EXECUTION ORDER (dependency-ordered)

```
[ ] SANDBOX   up + isolation pre-flight green (doc 03 §C)     HARD GATE
[ ] S6 ARM    adversarial log watcher -> BACKGROUND, always-on
                 (subscribe --json / jq, NEVER hand-grep run_id)
[ ] S1 LIFECYCLE  FOUNDATION -- IF S1 FAILS, STOP (nothing below meaningful).
[ ] S2 HANDLERS   per-harness handshake->dispatch->review->terminal, local+remote. Needs S1.
[ ] S3 MATRIX     DOT x single x review-loop  x harness x substrate x local/remote. Needs S2.
---- S4 & S5 CONCURRENT (disjoint: comms bus vs keeper) ----
[ ] S4 COMMS      Hamlet 1.1 over comms; ordered transcript, no drop/dupe. Needs S1.
[ ] S5 KEEPER     ~30k -> WARN->ACT->handoff->/clear->resume; document C4. Needs S1.
[ ] S7 FAULTS     LAST -- destructive; needs S1-S3 green baseline for attribution.
[ ] CRITIC    completeness critic -> another round until dry.
[ ] S6 STOP   collect LOG-WATCH-FINDINGS.md
[ ] ASSESS    reconcile claimed-done vs commits/diff/tests; write ASSESSMENT.md; mark every cell.
[ ] TEARDOWN  scratch-daemon.sh down; run teardown verification (doc 03 §D).
```

## §6.3 BUILD PINNING & RE-RUN POLICY

- Pin once per pass (`PIN_SHA` at §6.1b); clone `git checkout --detach "$PIN_SHA"`; sandbox binary built from that frozen tree; source may advance freely.
- Stamp every artifact with `PASS_ID`, full `PIN_SHA`, `PIN_BRANCH`, `harmonik --version`.
- Verify binary == pin before any suite (guards a stale `.harmonik/bin/harmonik`).
- Re-run trigger: comms message from captain `--topic status` matching `mediums-complete` → finish current suite → checkpoint → **new baseline pass** = repeat §6.1 with `PASS_KIND=baseline` + a **fresh clone** (do not reuse exploratory scratch; its DB/comms carry residue).
- Exploratory verdicts are PROVISIONAL; only `baseline-<sha8>` produces the admiral-gating ASSESSMENT.

## §6.4 ARTIFACT TEMPLATES

**RUN-LOG.md** (append-only; one block per command): header carries PIN_SHA(full)/PIN_BRANCH/
PASS_KIND/BINARY/SANDBOX/ISOLATION checklist/STARTED. Each block: `## [ts] Sn · <step>` with
CMD / OBSERVED / ASSERT+PASS|FAIL / ARTIFACT path; a FAIL block links the filed bead id.

**COVERAGE-MANIFEST.md**: a Suites table (S1–S7 RUN/SKIPPED+evidence) and the S3 cell matrix
(harness × mode × substrate × local/remote), **no blank cells** — every cell RUN with evidence or
SKIPPED with explicit reason. Codex rows pre-marked `SKIPPED — operator "codex minimal" (§5.3)`.
Assert `RUN + SKIPPED == enumerated total`.

**ASSESSMENT.md** (schema v2): `schema_version: 2`, `spawned_by: admiral`, `pass_id`, full
`pin_sha`, `pin_branch`, `measured_against: good-enough-principles.md`, VERDICT PASS|BLOCK +
one-line rationale (reasoned judgment, NOT a bead tally); Legs (LT/XT/CR); per-suite results;
claimed-done reconciliation (a claim with no commit/diff/test → BLOCK); findings table (evidence,
not the gate); residual risk for the admiral; coverage (RUN N/M, SKIPPED list, critic dry?).

## §6.5 RESUMABILITY (multi-hour; survives keeper restart)

Checkpoint `runs/<PASS_ID>/CAMPAIGN-STATE.json`, rewritten atomically after each suite + each S3
cell: `{pass_id, pass_kind, pin_sha, pin_branch, scratch, binary_sha_verified, suites{S1..S7:
PASS|BLOCK|IN_PROGRESS|PENDING|RUNNING}, s3_cells_done[], s6_watcher_pid, last_checkpoint}`.

**Resume protocol** (keeper restart mid-campaign):
1. Re-read mission file; confirm `$HARMONIK_AGENT == assessor`; `cd "$HARMONIK_PROJECT"`.
2. Re-join comms + re-arm `recv --follow --json` (stream doesn't survive restart).
3. Read newest CAMPAIGN-STATE.json; adopt PASS_ID/PIN_SHA/SCRATCH.
4. Do NOT rebuild if sandbox healthy AND binary --version == PIN_SHA; else `up` (or rebuild from the same pin — never a newer SHA).
5. Re-arm S6 if `s6_watcher_pid` gone (must cover whole campaign).
6. Skip every suite marked PASS/BLOCK; resume IN_PROGRESS; for S3 skip cells in `s3_cells_done`.
7. Append `## [ts] RESUME` block to RUN-LOG noting skipped-as-complete suites.

The pin is the anchor: resume always continues the **same** PIN_SHA pass — a mid-pass SHA change
is a new pass, never a resume.

## Notes for the synthesizer
- `scripts/scratch-daemon.sh` (`init|build|up|status|down|cycle|batch`) auto-derives isolation
  handles from the scratch path — **verify it exists** before relying on it; if absent, the
  first assessor task is to write it (or stand the sandbox up by hand per docs/daemon-redeploy.md).
- Deliverable paths now pass-scoped under `runs/<PASS_ID>/` so exploratory + baseline coexist.
- One open operator item (PLAN §5): harness set = claude + pi; codex minimal (already resolved).
