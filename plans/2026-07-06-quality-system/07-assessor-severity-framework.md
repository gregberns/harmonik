# Assessor "GOOD ENOUGH" severity / decision framework (2026-07-06)

*Design for the admiral. Operator requirement: SYNTHESIS §4b lines 78-85. Reconciles with the
deterministic block of §4b line 53 / §2 org-model ("the set of open P0/P1 `found-by:*` beads on the
epic branch IS the gate"). DESIGN ONLY — no code, no beads, no manifest edits. This becomes part of the
assessor's gate mechanics, wired at the first epic boundary.*

---

## 1. Purpose + where it plugs in

The assessor already knows *how* to test (LT live-verify + XT break-testing + CR review on an isolated
scratch clone) and *how* to file findings (`found-by:assessor` beads). What it does not yet have is a
**decision rule** for turning a pile of findings into an allow/block verdict — and, crucially, a rule
that distinguishes "this is bad enough to hold everything" from "this is a real bug but shipping is
still the right call, tracked as a known issue."

Without it, the assessor has only two degenerate modes: block on ANY finding (nothing ever ships — the
fleet stalls on cosmetic defects) or block on nothing (the gate is theater). The operator's rule cuts
between them:

- **MAJOR regression → BLOCK everything** (no merge, no release, no local-redeploy).
- **MINOR issue found in testing → does NOT block** — the action may proceed, recorded as a tracked
  **KNOWN ISSUE**: a `found-by:assessor` bead that stays OPEN (not a block, but not forgotten).

This framework = **(1)** a severity rubric, **(2)** severity→P-level→found-by-bead mapping that
reconciles with the existing deterministic block, **(3)** a per-action decision matrix for
{merge | release/promote | local-redeploy}, **(4)** the known-issue ledger the deploy-readiness report
surfaces, **(5)** edge-case adjudication.

**Where it plugs in:** Phase 1 gate-bootstrap (SYNTHESIS §4c — "stand up the deterministic
merge-gate/deploy-gate mechanics"), or early Phase 2 if Phase 1 ships with the raw
open-P0/P1-block-only rule first. It is the severity-classification layer *on top of* the deterministic
bead block that already exists in soul.md / operating.md §Merge-gate step 6. The block mechanic does not
change; this framework defines **how the assessor assigns the P-level** that feeds it, and **what the
non-blocking findings become**.

---

## 2. Severity rubric

Severity is assigned per finding at file-time, from the **task-processing pipeline** lens (the same lens
the bug corpus uses: queue-submit → worker-select → harness-launch+agent_ready → model-selection →
sandbox+provider-comms → edit+commit → commit_gate → DOT-review → merge). The question is always:
**"does this defect break, silently corrupt, or unbounded-hang the core loop for a plausible real
input — or is it a bounded, narrow, or recoverable degradation?"**

### MAJOR (blocking) — any ONE of these tests is enough

- **Correctness break on the core loop:** a plausible real bead does the wrong thing — no commit, wrong
  model, wrong branch, false close/reopen, dropped field. The pipeline "succeeds" but produces a wrong
  or empty result.
- **Silent failure / false-green:** the failure is not surfaced — a test or gate reports pass while the
  real behavior is broken. (Silent is an *aggravator* — it promotes a would-be minor to major, because
  it defeats detection.)
- **Unbounded hang / wedge:** any path that can block forever or wedge a slot/run/fleet with no timeout
  or safe-fail.
- **Fleet-wide or critical-path blast radius:** breaks every dispatch, or breaks the remote (tcp://)
  throughput path, or starves a whole queue.
- **Regression of a previously-fixed bug** (a corpus scenario goes red): always major regardless of the
  surface symptom — the whole point of the corpus is that these never come back.
- **Data/state corruption:** stranded `in_progress`, corrupted worktree HEAD, cache wipe that kills
  in-flight builds, bead ledger left inconsistent.

### MINOR (known-issue) — ALL of these must hold

- **Bounded blast radius:** narrow input class, single non-critical path, or a mode not on the core loop.
- **Loud, not silent:** the failure is observable (clear error, logged, surfaces to the operator) — it
  does not defeat its own detection.
- **Recoverable / has a workaround:** retry, re-dispatch, safe-fail, or a documented manual step already
  handles it; no data loss.
- **Not a corpus regression:** it is genuinely new, not the return of something the corpus already
  guards.
- **Cosmetic / ergonomic / latency-only** defects, and defects in test-only or scaffold code that do not
  touch the runtime task loop.

> Rule of thumb: **silent + core-loop = always MAJOR. Loud + bounded + recoverable = MINOR.** A finding
> that is loud but on the core loop, or silent but truly bounded, is a **DISPUTE** — assessor proposes a
> severity with rationale, admiral holds final authority (§6).

### Worked examples — MAJOR (from the real corpus, 02-bug-corpus-classification.md)

1. **Pi harness model leak** (hk-pkugu / hk-lfrub / hk-ytzj2 — the whole pi-model-leak week): the
   configured pi model never reaches the harness; a claude default silently shadows it. Correctness
   break + **silent** (green tests all week) → MAJOR.
2. **queue-submit drops per-item `workflow_ref`/`workflow_mode`** (hk-u6zp, open P0): a fully-specified
   bead dispatches with the wrong workflow, silently. Silent field-drop on the core loop → MAJOR.
3. **srt sandbox blocked Pi reaching the loopback model + false-green egress test** (hk-u69my): silent
   no-commit AND the egress test reported green → MAJOR (silent + false-green is the worst combination).
4. **Flagless REQUEST_CHANGES wedges the run forever; later APPROVE ignored** (hk-thbbv/hfmg6, open):
   unbounded wedge on the DOT-review path → MAJOR.
5. **level-2 per-queue gate counts local+remote, starving all-remote queues** (hk-4tjt6): critical-path
   (remote throughput) starvation → MAJOR.
6. **Claude 2.1.201 permissions modal wedges EVERY dispatch pre-agent_ready** (PR-19): fleet-wide
   blast radius, dispatch fully down → MAJOR.

### Worked examples — MINOR (known-issue) (same corpus)

1. **Cold-cache build failure hard-fails the merge-gate; fix adds retry** (hk-44ab2): transient,
   recoverable by retry, loud (the gate reported the failure). Before the retry existed it was an
   annoyance, not a corruption → MINOR / known-issue with the workaround "re-commit."
2. **DOT reviewer verdict read `ErrMalformed` from ssh-cat mid-write** (hk-vv10r, open): loud
   (`ErrMalformed`, not a silent bad read), narrow race window, salvageable on re-read. A residual
   flake, not a corruption → MINOR (until frequency promotes it — §5 lifecycle).
2b. **ornith DGX reasoning model incompatible with pi harness** (hk-4ir08, open): if the affected path
   is a *non-default* provider the fleet is not currently routing to, and the failure is loud
   (detectable content:null → clear rejection, no silent no-commit), it is a bounded known-issue on an
   opt-in config — NOT a block on the default path. (If it were the default provider, it flips to MAJOR
   — blast radius decides.)
3. **Stale agent worktrees not force-unlocked/age-pruned** (hk-qe736): housekeeping; recoverable by the
   existing prune, no in-flight run corrupted → MINOR.
4. **A cosmetic deploy-readiness report formatting gap, or a log line at the wrong level** (class of
   ergonomic finding the XT fan-out surfaces): no runtime effect → MINOR.
5. **A slower-than-ideal but correct retry/backoff** (latency-only degradation with no wrong result):
   MINOR.
6. **A defect confined to test/scaffold code** (e.g. a flaky scenario in the new testbed itself, not the
   daemon under test): MINOR — it does not gate the product, though it does get its own corpus/fix bead.

---

## 3. Severity → P-level → found-by-bead mapping

The existing deterministic block is stated in P-levels: **"open P0/P1 `found-by:*` beads on the epic
branch = BLOCK."** So the severity rubric must map cleanly onto P-levels; the assessor sets the
P-level when it files the bead, and the P-level IS what the block mechanic reads. No separate severity
field — **P-level is the wire representation of severity.**

| Severity bucket | P-level filed | Blocks the deterministic gate? | Bead disposition |
|---|---|---|---|
| **MAJOR — blocking** | **P0** (critical / fleet-wide / silent-corruption / core-loop break) or **P1** (serious but scoped-major) | **YES** — this is exactly the open-P0/P1-`found-by:*` set | filed `found-by:assessor`, left OPEN + UNASSIGNED; its existence blocks. Cleared only by a fix that closes it (daemon owns the close). |
| **MINOR — known-issue** | **P2** (default), **P3/P4** (cosmetic/backlog) | **NO** — P2+ is below the block threshold by construction | filed `found-by:assessor`, left OPEN + UNASSIGNED as a **tracked known issue**; surfaced in the report, does not hold the action |

Reconciliation with §4b line 53 / §2 org-model:

- The block is **still** "open P0/P1 `found-by:*` on the branch" — unchanged. This framework does not add
  a new gate primitive; it defines the **severity→P-level assignment discipline** so that the P-level is
  trustworthy. MAJOR ⇒ P0/P1 ⇒ block. MINOR ⇒ P2+ ⇒ known-issue, not block. The two are the same
  statement viewed from severity-side vs P-side.
- **`found-by:*` union, not `found-by:assessor` alone:** the block counts *any* `found-by:` source
  (assessor's own findings plus any other-source findings on the branch). The assessor files its own
  under `found-by:assessor`; the block query unions across known sources.
- **The P2 promotion boundary is the safety valve:** if the assessor is unsure whether a finding is
  MAJOR or MINOR, it may file P1 (block) and flag the dispute to the admiral, who can downgrade to P2
  (§6). It is never allowed to file P2 to *dodge* a block on something it believes is a correctness
  break — that is a severity dispute, adjudicated, not a unilateral downgrade.

### Wire-up gotcha (load-bearing — carried from 05-assessor-manifest-notes.md flag 4)

`br list --label` is **exact-match**; **`found-by:*` does NOT glob-expand.** Passing a literal `*`
returns nothing (false PASS — the gate would pass because it "found no blocking beads" when it simply
never queried them). At gate-time the assessor MUST enumerate the block set via **`--label-any` over the
known `found-by:` sources** (currently `found-by:assessor`, plus any other `found-by:<source>` in use),
or query per-source and union in code — never pass `found-by:*` as a label literal. Concretely:

```
br list --status open --priority 0,1 --label-any found-by:assessor,found-by:<other-known-sources>
# scoped to the epic branch. NOT: br list --label "found-by:*"  (matches zero rows → false PASS)
```

The list of known `found-by:` sources is a small, enumerable set; the gate mechanics must keep it
current when a new source is introduced. A missed source is a silent false-PASS, so this enumeration is
itself a corpus/regression concern.

---

## 4. Per-action decision matrix

Three actions the gate governs, per the operator: **merge-to-main**, **release/promote**, and
**local-redeploy** (swap the live daemon binary on a box). Cells: **ALLOW** / **ALLOW-AS-KNOWN-ISSUE** /
**BLOCK**.

| Severity bucket (P-level) | merge-to-main | release / promote | local-redeploy (live binary swap) |
|---|---|---|---|
| **MAJOR — P0/P1** (blocking) | **BLOCK** | **BLOCK** | **BLOCK** |
| **MINOR — P2** (known-issue) | **ALLOW-AS-KNOWN-ISSUE** | **ALLOW-AS-KNOWN-ISSUE** | **ALLOW-AS-KNOWN-ISSUE** |
| **MINOR — P3/P4** (cosmetic/backlog) | **ALLOW** (logged) | **ALLOW** (logged) | **ALLOW** (logged) |
| **DISPUTE / unresolved severity** | **BLOCK** pending adjudication | **BLOCK** pending adjudication | **BLOCK** pending adjudication |

**Reading the cells:**

- **BLOCK** — the action does not proceed. For MAJOR the admiral holds the human epic→main PR / the
  promote / the redeploy until the blocking bead is fixed-and-closed and the gate re-runs green.
- **ALLOW-AS-KNOWN-ISSUE** — the action proceeds; the P2 known-issue bead stays OPEN and is listed in
  the deploy-readiness report the admiral signs off on. The admiral is proceeding *with eyes open* on a
  named residual risk, not on a hidden one.
- **ALLOW (logged)** — proceeds; P3/P4 finding is recorded in the report's backlog section but needs no
  explicit acknowledgment.

### The operator's rule, stated against the matrix

- "A MAJOR regression BLOCKS everything" = the **top row is all-BLOCK across all three columns** — no
  asymmetry. A correctness break, silent failure, unbounded hang, or corpus regression holds merge AND
  release AND redeploy. There is deliberately no "block release but allow redeploy" escape for a MAJOR.
- "A SMALL issue does NOT block; merge/release/redeploy may proceed, recorded as a tracked known issue"
  = the **P2 row is all-ALLOW-AS-KNOWN-ISSUE**.

### On asymmetry (why the top row is intentionally symmetric, and where asymmetry *does* live)

The operator's rule makes the **severity→block** decision symmetric across the three actions: MAJOR
blocks all, MINOR blocks none. The asymmetry between the three actions lives **not in the block
decision** but in **which gate runs and what "green" means**:

- **merge-to-main** and **release/promote** both run the **full merge-gate** (LT+XT+CR on the branch).
  In this fleet `integration→main` is always a human PR and promote moves reviewed work toward the
  target — they gate on the same open-P0/P1 set, so their columns are identical.
- **local-redeploy** runs the **deploy-gate / GATE-0**: the isolated e2e reproducing the *changed
  behavior* must be green, and it is the enforcement point for the **24h reliability rule**. It does
  NOT re-run the whole merge-gate — a redeploy is of an already-merged commit. So a redeploy can BLOCK
  for a reason the merge already passed: a **new** blocking finding discovered post-merge (a corpus
  regression that surfaces only at deploy-repro time, or the 24h-reliability precondition not yet met).
  That is the real asymmetry — same block *rule*, different *evidence* feeding it. A commit that merged
  clean can still be blocked from redeploy by a P0/P1 found-by bead filed against it later, or by an
  unmet deploy precondition (§6, "finding after a known-issue deploy").
- Conversely, a P2 known-issue that was accepted at merge does **not** re-block at redeploy — it is
  already in the ledger; the redeploy inherits it (unless it has since been promoted to blocking — §5).

---

## 5. Known-issue ledger

### Storage — it already exists, it is the bead set

The ledger is **not a new artifact**. It is exactly the set of **OPEN `found-by:assessor` beads at
P2/P3/P4** on the branch/commit. Filing rules (already in operating.md §Merge-gate step 5):

- Filed with `br create ... --label found-by:assessor` at the P-level the rubric assigns.
- Left **OPEN** and **UNASSIGNED** — a known issue is a live, tracked debt, not a resolved one.
- The assessor **never** closes/claims/reopens it (the daemon owns terminal transitions). A known issue
  is closed only when someone actually fixes it and the fix merges.
- Each known-issue bead body records: the finding, the pipeline stage, the **workaround/recovery** (so
  it qualified as MINOR), and — for anything that could bite in production — the trigger conditions.

The **block set (P0/P1)** and the **known-issue ledger (P2+)** are the same `found-by:` bead pool
partitioned by P-level. One query, two partitions.

### How the deploy-readiness report surfaces it

The deploy-readiness report (operating.md §Verdict step 1: *tested / passed / residual risk*) has a
mandatory **Known Issues** section = the P2+ partition, so the admiral authorizes against an explicit
residual-risk list:

```
## Deploy-readiness — <branch/commit> — <PASS|BLOCK>
Tested:        LT <n scenarios> · XT <n probes> · CR <diff summary>
Passed:        <acceptance behaviors confirmed>
Blocking (P0/P1, open found-by:*):   <none | list — this is why BLOCK>
Known issues (P2+, open found-by:assessor, ACCEPTED residual risk):
  - hk-xxxxx (P2): <one-line> · stage:<pipeline-stage> · workaround:<...>
Corpus:        <scenarios added this gate>
```

A PASS verdict with a non-empty Known Issues section is the normal, expected shape — it is the operator
rule made visible: shipping *good enough*, with the residuals named.

### Lifecycle of a known-issue bead

1. **Open (accepted):** filed P2+, listed in the report, action proceeds. Default resting state.
2. **Re-triage on recurrence:** if the same known issue is hit again — especially by a real fleet run,
   not just a probe — the assessor (or admiral) **re-scores** it. Rising frequency, a widening blast
   radius, or loss of the workaround **promotes it to P1 → blocking**. Example: hk-vv10r (ssh-cat
   malformed read) is a P2 flake at low frequency; if it starts eating real verdicts under load it
   promotes to a block.
3. **Promotion path:** promotion is a P-level change on the existing bead (the block query then picks it
   up automatically). Whoever raises it records the new evidence in the bead; a *promotion to blocking*
   is an admiral-authority call (§6) since it can retroactively hold a redeploy.
4. **Demotion:** rare — only if re-investigation shows the finding was mis-scored MAJOR; admiral
   authority, same as any dispute.
5. **Close:** only when the underlying defect is actually fixed and the fix merges (daemon closes it on
   terminal transition). Every closed known-issue that was a real bug **must already have a permanent
   corpus scenario** (operating.md §Grow the regression corpus) so it can never silently return as a
   MAJOR regression.

---

## 6. Edge cases

**Flaky / unreproducible findings.** A finding the assessor cannot reliably reproduce is NOT filed as a
P0/P1 block (a block must rest on a reproducible defect — otherwise the gate wedges on ghosts). It is
filed as a **P2 known-issue tagged flaky/unreproduced**, with whatever repro fragments exist, and it
**counts toward promotion frequency** (§5 step 2): a flake seen repeatedly graduates to a reproducible
block. Rationale: the corpus/gate should never hard-block on noise, but must not forget a recurring
symptom either. (Compare hk-vv10r's mid-write race — real but narrow-window.)

**Severity disputes — who adjudicates.** **The assessor PROPOSES severity; the admiral HOLDS
authority.** This mirrors the existing split (admiral = gate authority, assessor = executor; soul.md
"I do NOT decide when the gate fires or hold the merge/deploy"). Mechanics:

- The assessor files at the P-level its rubric yields and posts the verdict. For any finding it flags as
  a **DISPUTE** (the loud-on-core-loop / silent-but-bounded gray zone in §2), it defaults to the
  **conservative side — file P1 (block)** and name the dispute in the report.
- The admiral may **downgrade P1→P2** (accept as known-issue and let the action proceed) or **uphold**
  the block. The admiral may also **upgrade** a P2 the assessor filed. Either way it is the admiral's
  call recorded on the bead — the assessor does not re-litigate.
- The assessor never unilaterally downgrades a suspected correctness break to dodge a block; that is the
  precise move the dispute path exists to prevent.

**A finding discovered AFTER a known-issue deploy.** Two sub-cases:

- *It's a NEW finding on the already-deployed commit* — file it at true severity. If **MAJOR (P0/P1)**,
  it retroactively makes the live binary a blocking state: the assessor posts BLOCK to the admiral, who
  decides **revert / hotfix-forward** (this is the redeploy-column BLOCK-on-new-evidence asymmetry from
  §4). The deploy having already happened does not launder a MAJOR into a known-issue — a shipped MAJOR
  is an incident, and its fix must add a corpus scenario so it never silently ships again.
- *It's the KNOWN issue biting harder than scored* — this is the §5-step-2 promotion path: re-triage,
  promote P2→P1 if warranted, admiral decides revert-vs-forward. The original accept-as-known-issue
  decision is not "wrong" retroactively; the evidence changed, so the score changes.

**Broken substrate / unverifiable branch.** Out of scope for severity — the assessor cannot assign
severity to what it cannot test. Per soul.md it **escalates to the admiral** (broken scratch substrate,
unbuildable branch, ambiguous gate scope) rather than emitting a PASS or a BLOCK. An unverifiable gate
is neither ALLOW nor a clean BLOCK; it is an escalation.

---

## Known-issue lifecycle: CRITICAL known-issues are ASSIGNED, not parked (operator 2026-07-06)

A worked-around gap does NOT simply sit open in the ledger forever. The operator's rule: an issue may be
marked a known-issue AND the system proceed (merge/release/redeploy) — but if the gap is **critical for
where the project is going**, it must be **passed to a crew to fix**, with a real owning bead, not left as
a passive ledger entry. The known-issue ledger therefore has two partitions:

- **Passive known-issues** — minor/cosmetic, genuinely tolerable indefinitely; stay open, surfaced in the
  deploy-readiness report, no owner required.
- **Assigned known-issues (`known-issue` + a fix bead at true fix-priority)** — worked around NOW for
  throughput, but on a fix track. The assessor/admiral files the fix at its real priority (often P1 even
  though the gate ALLOWED the deploy on the workaround), labels it `known-issue`, and it enters a crew's
  queue. The deploy-readiness report lists these under "Known issues — assigned, with owner + fix bead."

**Canonical worked example (the first one): `hk-lgykq` — per-bead/DOT integration-branch targeting.**
The dead `LandsOn`/`landTaskBranch` path means work can't be directed to a specific integration branch;
everything merges daemon-wide. Operator ruling: the *workaround* (option C — crew commits harness directly
to its integration branch, daemon only executes the matrix) is ALLOWED to proceed, but the capability is
CRITICAL going forward, so: (1) a core-loop-proof harness assertion tests it (REDs today = the known-issue
evidence), (2) it is filed `hk-lgykq` P1 `known-issue`+`found-by:admiral`, (3) it is passed to a crew to
fix. This is the assigned-known-issue pattern in the concrete: proceed on the workaround, but the gap is on
a funded fix track, not parked.

---

*Ends. Wire this in at Phase 1 gate-bootstrap alongside the deterministic found-by block. It changes no
gate primitive — it disciplines the P-level assignment that primitive already reads, and defines the
known-issue ledger the deploy-readiness report already has a slot for.*
