# Assessor "good-enough" release-bar principles

*The normative release bar the `assessor` measures a branch against when it forms
its reasoned PASS/BLOCK. This is the standard the verdict is held to — not a new
mechanic. It is referenced by the handoff schema
(`specs/assessor-handoff-schema.md` — WS5-2) and by the admiral's gate authority
(WS5-6). The reasoning model itself is owned by `soul.md` + `operating.md`
(WS5-1); the severity rubric by `plans/2026-07-06-quality-system/07-assessor-severity-framework.md`.
This doc states WHERE the bar sits; those own HOW a finding is graded.*

---

## 1. What "good enough" means here

A branch is **good enough to release** when the assessor can honestly attest, on
its own reasoned judgment, that the work does what it claims and hides no
ship-blocking defect — measured against the four requirements in §2, at the
hardness §3 (risk tier) demands. "Good enough" is deliberately NOT "perfect":
minor, bounded, or recoverable defects are recorded as known issues and do not
hold the gate (`07` §1). The bar cuts between *"bad enough to hold everything"*
and *"a real bug, but shipping is still the right call, tracked."*

The verdict is a **reasoned PASS/BLOCK**, not a bead-count query (schema v2, §9).
The bar below is the standard that judgment is exercised against; it is never a
mechanical row count that can be gamed by an empty ledger.

---

## 2. The release bar — four requirements

All four MUST hold for a **PASS**. Any one failing at or above the §3 risk floor
is a **BLOCK**.

### 2.1 LT — local-tester matrix green (including the required cells)

The live-verify leg (`operating.md` §Merge-gate step 2) drives the real
task-processing loop on the isolated scratch daemon. The **acceptance-behavior
matrix the epic claims must fold green** — and the cells the gate declares
**required** (e.g. the required-harness cell a milestone names) MUST be among the
green ones, not skipped or stubbed. A matrix that is green only because a required
cell was not exercised does **not** clear the bar. A red required cell is a BLOCK.

### 2.2 XT — no unmitigated critical (exploratory-tester)

The exploratory break-testing leg (`operating.md` §Merge-gate step 3) runs the
adversarial fan-out and the failure-corpus scenarios. **No critical
(MAJOR-severity, `07` §2) defect may remain unmitigated** — correctness break on
the core loop, silent failure / false-green, unbounded hang / wedge, fleet-wide
blast radius, or a regressed corpus scenario. A critical is "mitigated" only when
it is genuinely neutralized (fixed, or provably out of the change's reach), not
merely filed. A regression of a previously-fixed bug (a corpus cell going red) is
always critical and always blocks.

### 2.3 CR — no BLOCK-class defect (cold-review)

The independent code-review leg (`operating.md` §Merge-gate step 4 — the assessor
reads the diff cold, having not built it) must surface **no BLOCK-class defect**:
a correctness, safety, or spec-alignment fault that on its own makes the change
unfit to ship. REQUEST_CHANGES-class review notes (idiom, tidiness, non-blocking
concerns) are recorded, not gating. A BLOCK-class review finding is a BLOCK.

### 2.4 Claimed-done reconciles to reality

The assessor MUST reconcile every **claimed-done** unit of work against the
**actual commits, diffs, tests, and reviews** on the branch (`operating.md`
§Merge-gate; schema §7 worked example: *"the COORD-log claims are a lead, not
proof"*). A claim that is not backed by a real commit/diff, that has no test
exercising it, or that skipped its review gate does **not** count as done.
Unreconciled claimed-done — work asserted complete but unsupported by the branch's
own artifacts — is a BLOCK; the assessor grades what the branch actually contains,
never what a log says it contains.

---

## 3. Risk tiering sets how hard the bar is (D2 — path-glob risk floor)

The bar in §2 is not uniform across all changes. Per **D2 of the code-revamp plan
(the path-glob risk floor)**, a change's **risk tier is set by which paths it
touches**: path globs establish a **risk floor** — a change touching a
higher-risk path (core dispatch, daemon lifecycle, the commit/merge gate, the
remote throughput path) is floored at a higher tier no matter how small the diff
looks. The risk tier then sets **how hard the bar is applied**:

- **Higher risk tier → the bar tightens.** Required LT cells expand, the XT
  fan-out probes deeper, CR is read with less benefit of the doubt, and the
  claimed-done reconciliation is exhaustive. A defect that would be a tolerable
  known-issue on a low-risk change is treated as gating on a high-risk one
  (silent failure and blast-radius aggravators, `07` §2, escalate faster here).
- **Lower risk tier → the bar is proportionate.** A narrowly-scoped, low-blast
  change is held to its own acceptance behavior and cold review; it is not blocked
  on defects outside its reach.

The path-glob floor is a **floor, not a ceiling**: the assessor's judgment may
raise a change's effective tier when the evidence warrants (e.g. a small diff with
outsized blast radius), but it may never lower a change below the floor its paths
establish. When in doubt about scope or tier, the assessor escalates to the
admiral rather than guessing (`soul.md` §escalate).

---

## 4. Deploy gate (GATE-0) addendum

For a `gate: deploy` handoff the same four requirements apply to the named
`commit`, with two additions the deploy gate enforces (`operating.md`
§Deploy-gate): the isolated e2e reproducing the changed behavior MUST be green,
and the deploy-readiness **preconditions the mission names** (the 24h-reliability
rule) MUST be met. Green e2e + preconditions + a clean claimed-vs-actual
reconciliation → PASS; otherwise BLOCK with the `found-by:assessor` beads that
explain why.

---

## 5. How this is referenced

- **WS5-2 — handoff schema** (`specs/assessor-handoff-schema.md`): the assessor
  "forms a reasoned PASS/BLOCK … measured against the good-enough principles"
  (schema §1, §7). This doc is that standard; the schema carries the gate inputs
  (`branch`, `gate`, `epic_id`, `found_by_sources`) the bar is applied over, and
  the verdict is posted to the admiral over `--topic gate`.
- **WS5-6 — admiral authority**: the admiral **owns the gate decision**; the
  assessor is the executor that applies this bar and reports. The admiral
  adjudicates severity/tier disputes, makes the critical-for-direction call on a
  known-issue, and holds the final release / epic→main-PR decision. This doc gives
  the admiral the shared, written standard the assessor's PASS/BLOCK is grounded
  in, so an override is a reasoned exception to a stated bar, not an ad-hoc call.

---

## 6. Non-goals

- This doc does **not** grade individual findings — severity and P-level are
  `07-assessor-severity-framework.md`; disposition labels are
  `09-remediation-loop-design.md`.
- It does **not** re-open the schema field contract or the reasoning model — those
  are WS5-2 and WS5-1 respectively.
- It does **not** hold the merge/deploy — the admiral does (WS5-6). The assessor
  measures against this bar, posts, and self-terminates.
