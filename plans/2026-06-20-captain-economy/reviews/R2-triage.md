# R2 — Adversarial Triage of Captain-Economy Findings (I1 / I2 / I4)

**Date:** 2026-06-20
**Reviewer:** captain-economy R2 (adversarial / skeptical lens)
**Inputs:** I1 (boot cost), I2 (15 conflicts), I4 (busywork inventory)
**Resolution sources read:** memory `feedback_captain_autonomy_lift`, `feedback_captain_lean_while_operator_away`, `reference_my_comms_identity`, `feedback_keeper_band_no_retune`, **`project_3day_scaleout_directives` (2026-06-19 — most recent, supersedes several)**; source `internal/core/runstartedpayload.go`, `internal/operatornfr/*workflowmode*`, keeper `WarnAbsTokens`/`WarnPct` fields.

> **Adversarial verdict up front:** Two of I2's HIGH conflicts rest on claims my source-read **refuted or weakened**, and one HIGH conflict (lean-vs-fill) was **already lifted by the 2026-06-19 operator directive** — making it currently moot. Details in the table.

---

## (1) Conflict REAL / FALSE-POSITIVE Table

A conflict is **REAL** only if BOTH sides are live instructions a captain reads at boot with no cross-reference reconciling them. **FALSE-POSITIVE** = already resolved by a SUPERSEDES note, a read-only carve-out, or a memory entry that a booting captain also loads. **PARTIAL** = real residual confusion but narrower than I2 framed it, or the premise is partly wrong.

| # | I2 sev | Verdict | Rationale (one line) |
|---|--------|---------|----------------------|
| 1 | HIGH | **REAL** | Three live numbers (30/35, 25/30, 200K/215K) sit in STARTUP/SKILL/keeper with no cross-ref; the abs-vs-pct lever genuinely differs. Operator's 200K/215K (band_no_retune + 3day) is canonical but the skills still print stale pct. |
| 2 | HIGH | **FALSE-POSITIVE (currently moot)** | `project_3day_scaleout_directives` (2026-06-19) **explicitly LIFTS** lean-park ("ONE-AT-A-TIME RETIRED… LEAN-park stance is LIFTED for this window"). Fill-every-lane is the live rule now; the lean memory is self-marked SUPERSEDED. Latent only when the 3-day window closes. |
| 3 | MED | **PARTIAL** | Not doc-vs-doc — both sides ("keep working/restart-now" + "never self-quit") agree. Real residual = unverified compiled `on_demand_warn_text` could be the fatal `/quit`. A latent footgun, not a contradiction. Keep as a verify-step add. |
| 4 | MED | **FALSE-POSITIVE** | `feedback_keeper_band_no_retune` UPDATE (2026-06-17) directional caveat + 3day both resolve: HARD-NO is WIDENING only, LOWERING is operator-directed. Memory loads at boot. Skills are STALE (worth flattening) but the captain reading memory is NOT mis-instructed. |
| 5 | HIGH | **FALSE-POSITIVE** | The no-sub-agent rule is already carved out: STARTUP Anti-pattern C exempts "read-only PLANNING/RESEARCH/triage sub-agents," and the consensus agents ARE read-only. `feedback_captain_delegation_discipline` ("inline OK = … sub-agent") confirms. Reconciliation exists; making it explicit is polish, not a defect. |
| 6 | MED | **REAL** | The `br close` exception (SHUTDOWN) vs prohibition (SKILL/beads-cli) vs locked-decision-reversal (captain-lanes) genuinely splits across 3 files with no single reconciling cross-ref; the promote-cherry-pick-lacks-trailer exception-to-exception is a live foot-gun. |
| 7 | MED | **PARTIAL** | Freshness question I2 itself flags. Crews own dispatch, not the captain; captain only hits it on lull-deploy/canary. Real but mostly-not-captain; low captain-facing severity. |
| 8 | HIGH | **PARTIAL — premise partly REFUTED** | `workflow_mode` **DOES exist** on the run_started payload (`runstartedpayload.go:67-71`, `json:"workflow_mode,omitempty"`, spec event-model §8.1). I4/the memory's "field does not exist" is WRONG. Real defects survive: it's `omitempty` (absent when nil → null on old runs), and the jq must read `.payload.workflow_mode` not top-level. The 2026-06-19 directive RELIES on this exact field (STARTUP 5c) — so "always false all-clear" overstates it. Downgrade HIGH→MED. |
| 9 | MED | **REAL** | SKILL §0.5 arms `run_stale,heartbeat` subscribe; STARTUP explicitly forbids exactly that as the named context-burn failure. Both live, no cross-ref. Corroborated by I4 #3 (eliminate 600s heartbeat). |
| 10 | LOW | **REAL (dup of 1)** | Self-inconsistent within keeper/SKILL.md (cites inert pct + flags pct inert + wrong 25/30-vs-30/35 cross-ref). Genuine, but folds entirely into issue 1. |
| 11 | LOW | **REAL** | SKILL §A and captain-lanes.md are two live snapshots-of-record that disagree; SHUTDOWN writes §A while STARTUP reads captain-lanes.md → guaranteed drift. |
| 12 | HIGH | **PARTIAL** | `reference_my_comms_identity` resolves the *principle* and SKILL §10 already says `<your-lane>` not hardcoded. But STARTUP:37 + most of SKILL/SHUTDOWN still hardcode `--from captain` with only the Step-0 echo as guard — internally inconsistent within the skill. Real residual; downgrade HIGH→MED (a captain loading the memory won't freeze the fleet, but the literals invite it). |
| 13 | MED | **REAL** | Genuine reliability gap: `--wake` to `captain` fails (pane-name mismatch, per memory) yet skills treat comms as the operator channel. Memory documents the failure but the SKILL does not flag the re-arm-`--follow`-is-load-bearing consequence. |
| 14 | HIGH | **REAL** | restart-earlier (200K, 3day + band_no_retune) × "re-ground via full STARTUP / do NOT trust handoff" pull opposite. 3day even says keeper is unreliable + cheap-resume is load-bearing (`hk-n3w1`). The single deepest economy tension; no doc reconciles "trust tier-2/3 as input" with "do NOT trust, re-derive." |
| 15 | LOW | **FALSE-POSITIVE** | I2 itself concludes "minor and reconcilable… already mostly clear" — terse-ack is WARN-scoped, surfacing is event-scoped. A one-clause clarification at most; not a live contradiction. |

### Tally
- **REAL: 6** — #1, #6, #9, #10, #11, #13
- **PARTIAL: 4** — #3, #7, #8, #12
- **FALSE-POSITIVE: 4** — #2 (moot under 3day), #4, #5, #15

(#10 is a true-but-subsumed dup of #1; counted REAL for fidelity but merges in dedup.)

---

## (2) Deduplicated Master Issue List

Merging cross-finding duplicates. Corroboration across ≥2 findings ⇒ higher confidence. `[I1]/[I2]/[I4]` tags show which findings reported each.

| ID | Issue (merged) | Reported by | Conf. | Severity | Effort | Doc vs Code |
|----|----------------|-------------|-------|----------|--------|-------------|
| **M1** | **Keeper band: stale/inert pct flags across STARTUP+SKILL+keeper; canonical = `--warn-abs-tokens 200000 --act-abs-tokens 215000`** (folds I2 #1, #4, #10) | I2 | High | reliability-breaking (captain arms inert flags → no early restart) | skill rewrite (≥3 files) + verify config.yaml `keeper:` block | **pure-doc** |
| **M2** | **Broken/ambiguous review-gate quality-check** — jq reads top-level `workflow_mode` (must be `.payload.workflow_mode`); field is `omitempty` so absent on old runs. Replace with `reviewer_verdict`-per-`run_id`. NOTE: field EXISTS (refutes "does not exist"); 2026-06-19 directive relies on it via STARTUP 5c | **I2 #8 + I4 #4** (2-finding corroboration) | High | reliability-breaking (silent review-bypass undetected) | skill edit + move to Sonnet ops-monitor; **verify against current daemon binary** | **pure-doc** (fix); code only if ops-monitor script added |
| **M3** | **600s subscribe heartbeat / `run_stale,heartbeat` standing subscribe** — SKILL §0.5 arms it, STARTUP forbids it (context-burn). Drop from §0.5 (folds I2 #9 + I4 #3) | **I1 (§C framing) + I2 #9 + I4 #3** (3-finding) | High | efficiency (Opus no-op wakes = cache-read burn) | one-section skill edit | **pure-doc** |
| **M4** | **restart-earlier vs heavy full-STARTUP re-grounding every resume** — lower band + "do NOT trust handoff, re-derive" thrash. Lean on boot-digest for keeper-restart resume; trust tier-2/3 as input (folds I2 #14 + I1's boot-cost thesis + I4 cache-read finding) | **I1 + I2 #14 + I4** (3-finding) | High | reliability-breaking (perceived captain unreliability; defeats the lower band) | skill rewrite of resume path; make boot-digest mandatory | **pure-doc** + minor script (mandate digest) |
| **M5** | **Boot context bloat** — STARTUP⇄SKILL duplication (~5-6k); full AGENT_INDEX/STATUS/TASKS re-read (~9k); full keeper SKILL.md auto-injected (~6k); digest framed optional w/ raw commands still inline (double-run) | **I1 (primary) + I4 #6** | High | efficiency (~22-26k tokens; root of "100k after boot") | multi-file skill rewrite + cheatsheet extraction | **pure-doc** + 2 small scripts (verify-fleet, context-digest) |
| **M6** | **`/loop 12m` tick is mostly no-op Opus wakes** — deterministic slices (daemon-up, paused-queue, crew-freshness, review-gate, backlog/lull detection) should move to a Sonnet ops-monitor; only judgment slices wake Opus | I4 (#2,7,8,9) | High | efficiency (recurring Opus cache-read burn) | **code change** — new Sonnet ops-monitor on `ops-q` (design.md D4/D6) + skill edit | **code** (worktree+review) |
| **M7** | **`br close` exception split across 3 files** — SHUTDOWN sanctions it, SKILL/beads-cli forbid it, captain-lanes says raw-close reverses a locked decision; promote-cherry-picks lack the reconcile trailer | I2 #6 | Med | reliability-breaking (wrong raw-close strands bead / reverses locked decision) | consolidate into SKILL §8 (name the ONE exception + the exception-to-it) | **pure-doc** |
| **M8** | **Hardcoded `--from captain` literals vs lane-identity guard** — STARTUP:37 + SHUTDOWN hardcode while SKILL §10 + memory say `<your-lane>`; Step-0 echo is the only guard | I2 #12 | Med | reliability-breaking (uncommissioned `--from captain` freezes fleet) | replace literals w/ `$HARMONIK_AGENT`; make Step-0 guard normative | **pure-doc** |
| **M9** | **§A lane snapshot duplicates+contradicts captain-lanes.md** — two snapshots-of-record; SHUTDOWN writes §A but STARTUP reads captain-lanes.md → drift; bead IDs months stale | I2 #11 | Cosmetic→Efficiency | efficiency (stale context confuses + adds tokens) | reduce §A to the model; point at captain-lanes.md; fix SHUTDOWN target | **pure-doc** |
| **M10** | **Captain unreachable by `--wake` (pane-name mismatch)** yet treated as comms-reachable; re-arming `comms recv --follow` after `/clear`/PARK is load-bearing and unflagged | I2 #13 | Med | reliability-breaking (stalled captain unrousable) | add reliability note to SKILL; file a bead for the `--wake` pane gap | **pure-doc** (note) + **code** (the `--wake` fix itself) |
| **M11** | **Latent `/quit` warn-text footgun** — verify compiled `on_demand_warn_text` injects captain-safe restart-now text, not the shared fatal `/quit` | I2 #3 | Med | reliability-breaking IF mis-deployed | add one verify-step to STARTUP §6 (`keeper doctor`) | **pure-doc** (verify) |
| **M12** | **Stream-vs-wave concurrency guidance opposes operational memory** (`--wave` for concurrency vs "concurrent REAL beads wedge, go serial") — mostly crew-facing, freshness-gated | I2 #7 | Cosmetic (captain), Med (crew) | efficiency | add cross-ref note; **verify hk-3j50y/hk-h8u7p status first** | **pure-doc** |
| — | **M13 (dropped/no-op)** | I2 #5, #15, #2 | — | — | These are FALSE-POSITIVE per the table — already resolved (carve-out / scope / 3day-lift). NO action this window; revisit #2 when the 3-day window closes. | n/a |

### Severity rollup (deduplicated, actionable issues M1–M12)
- **Reliability-breaking: 6** — M1, M2, M4, M7, M8, M10 (+ M11 conditional)
- **Efficiency: 5** — M3, M5, M6, M9, M12
- **Cosmetic: 0** standalone (M9/M12 graded efficiency)

### Effort rollup
- **Pure-doc (safe, fast — no worktree/review needed):** M1, M2 (fix only), M3, M4, M5 (+scripts), M7, M8, M9, M11, M12 — **10 of 12**
- **Code (needs worktree + review):** M6 (Sonnet ops-monitor), M10 (the `--wake` pane fix), and the optional scripts under M2/M5.

### Highest-confidence (multi-finding corroborated) — do these first
- **M2** (I2+I4), **M3** (I1+I2+I4), **M4** (I1+I2+I4), **M5** (I1+I4) — all corroborated by ≥2 findings AND all but M6's monitor are pure-doc.

---

## Adversarial notes for the synthesizer
1. **Do NOT carry "workflow_mode does not exist" forward.** It exists (`runstartedpayload.go:67-71`; spec event-model §8.1; operator-nfr ON-004a). The 2026-06-19 operator directive uses it as a live check. The real bug is jq nesting + `omitempty` absence on legacy runs — re-verify the check against the *current* daemon binary, not the 2026-06-17 memory.
2. **Conflict 2 (lean-vs-fill) is moot right now** — `project_3day_scaleout_directives` lifted lean-park. Don't "fix" SKILL to add a lean carve-out that contradicts the current standing directive; instead make the source-of-truth `.harmonik/context/project.yaml operator_directives` and have the skill defer to it.
3. **Three conflicts (#4, #5, #15) are already reconciled in artifacts a booting captain loads** (memory + STARTUP carve-out). Flattening the stale skill text is cheap hygiene, not a reliability fix — bucket as M-low, don't inflate the severity count.
