# CP Pilot — R1 Review Synthesis

`synthesis-version: 1.0` — drafted 2026-04-30 by orchestrator (`hk-ahvq.7`). Combines the three parallel reviewer outputs in this directory. Lane-assignment uses pilot-review-protocol.md §4.1 four-probe triage.

## Reviewer outputs

- `coverage-r1.md` — **CLEAN.** 0 findings. All 55 §4 reqs, 3 active invariants (CP-INV-004/005 confirmed demoted in spec §5 NOTE lines 500–501), 24 schemas, §8-routes-to-EM decision all accounted for. Tally arithmetic and version reference both correct.
- `decomposition-r1.md` — **2 MINOR / 0 MAJOR / 0 BLOCKER**, all `local`.
- `references-r1.md` — **1 MAJOR / 2 MINOR / 0 BLOCKER**.

## Findings table

| ID | Severity | Lane | Reviewer | Summary |
|---|---|---|---|---|
| F-refs-CP-1 | MAJOR | local | Reference | `cp-008` missing AR §4.8 role-taxonomy edges to `ar-039`, `ar-040`, `ar-041`, `ar-042`. Peer beads cp-017/028/029/030/039 emit the same cluster — pilot inconsistency, not a rule gap. |
| F-decomp-CP-1 | MINOR | local | Decomposition | `cp-schema.hook-verdict-record` description says "7-field" but lists 8 (spec §6.1.6 declares 8). Off-by-one in count prefix; field list correct. |
| F-decomp-CP-2 | MINOR | local | Decomposition | ~~`cp-034b` description omits the `bound_fired ∈ {ast_steps, wall_clock}` discriminator~~ **REJECTED on inline verification.** Yaml line 455 already contains: "Event payload carries `bound_fired ∈ {ast_steps, wall_clock}` discriminator + per-abort `io_determinism` tag." Reviewer likely read the title only and missed the description body. No fix needed. |
| F-refs-CP-2 | MINOR | local | Reference | `cp-048` missing `cite:wide-fanout` tag on its `[event-model.md §8]` section-anchor cite. Identical shape to `cp-013` which IS tagged — internal inconsistency. |
| F-refs-CP-3 | MINOR | **class** | Reference | `cp-024`'s `[event-model.md §8.9]` resolves to `ev-005`; defensible but EV §8.9 is acceptance-criteria meta-prose with no specific req housed there. Should carry `cite:wide-fanout` and possibly edge to `ev-008` too. |

## Triage — four probes per finding

### F-refs-CP-1 (MAJOR, local)

1. Generality? **No.** Peer beads cp-017/028/029/030/039 emit the cluster; CP-008 is the inconsistent case, not a discipline gap.
2. Rule-vs-application? **Application error.** §3.1 step 5 (term-use single-owner pin) is correct; pilot just missed one application case.
3. Silence? **No.** Discipline §3.1 covers role-taxonomy term-use.
4. Reviewer self-classification? **`local`.**

→ **Pilot lane.** Action: add 4 edges in v0.1.1 patch.

### F-decomp-CP-1, F-decomp-CP-2, F-refs-CP-2 (3× MINOR, local)

All cosmetic / internal-consistency. Reviewer self-tagged `local`. Per §4.2 MINOR is "at author's discretion."

→ **Pilot lane (discretionary).** Action: fold into v0.1.1 patch — marginal cost, cleaner record for downstream pilots.

### F-refs-CP-3 (MINOR, class)

1. Generality? **Yes.** Other specs cite `[event-model.md §8.N]` section anchors; the wide-fanout-tag rule applies broadly.
2. Rule-vs-application? Mixed — discipline §3.1 step 3 routes wide-fanout to load-bearing single owner, but doesn't explicitly say "and tag the bead `cite:wide-fanout`." The pilot author treated `cite:wide-fanout` as conditional documentation.
3. Silence? **Partial.** Discipline names the tag but doesn't specify when it's mandatory.
4. Reviewer self-classification? **`class`.**

→ **Discipline lane (deferred).** Per §4.2: "MINOR class findings at author's discretion. May be batched into the next discipline patch rather than triggering one." Action: NOT gating CP load. Track as F-pilot-CP-7-adjacent (the yaml's existing F-pilot-CP-7 already flags wide-fanout body-enumerated row-set as a discipline-patch candidate; F-refs-CP-3 is a sibling). Batch with F-pilot-CP-7 for the next discipline revision (likely v0.10 after WM pilot lands).

## v0.1.1 patch plan

Apply to `docs/decompose-to-tasks/cp-pilot-data.yaml`:

1. **F-refs-CP-1 fix.** Add 4 cross-spec edges to `cp-008` (Hook is the §4.3 Kind for trigger-driven side-effect/observability): `ar-039`, `ar-040`, `ar-041`, `ar-042`. Match the pattern in cp-017/028/029/030/039.
2. **F-decomp-CP-1 fix.** In `cp-schema.hook-verdict-record` description, change "7-field" → "8-field".
3. **F-decomp-CP-2 fix.** In `cp-034b` description, append: "Event payload carries `bound_fired ∈ {ast_steps, wall_clock}` discriminator + per-abort `io_determinism` tag per spec §4.7."
4. **F-refs-CP-2 fix.** Add `cite:wide-fanout` tag to `cp-048` (mirroring `cp-013`).
5. **Bump version.** `pilot-version: 0.1.0` → `0.1.1`. Update top-comment with patch summary.
6. **Update revision history** in `cp-pilot.md` §10.

## Re-run plan

After patch: `python3 scripts/load-pilot.py docs/decompose-to-tasks/cp-pilot-data.yaml --skip-beads` (edges-only mode; resume against the existing mnem-map). Expected: 4 new edges added, 273 reported `already_exists`. `br dep cycles` clean.

## Discipline-lane batch

`F-pilot-CP-7` + `F-refs-CP-3` are both wide-fanout discipline-patch candidates. Defer to the post-WM discipline review (likely discipline v0.10). Not gating CP load.

## Re-review trigger

Per protocol §2: patches change one cross-spec edge cluster (4 new edges on cp-008) and three cosmetic descriptions. **Edge change re-triggers Reference reviewer at minimum.** Coverage and Decomposition need NOT re-run (no structure change beyond the 4 new edges, which the patch agent verifies inline).

For efficiency, the v0.1.1 patch will spot-check Reference's specific complaint (cp-008 → ar-039/040/041/042) inline rather than re-spawning the full reference reviewer agent. If the patch agent has any concerns it can't resolve, it will flag for re-review.

## Lane-assignment summary

- Pilot lane: F-refs-CP-1 + 3× MINOR (all folded into v0.1.1)
- Discipline lane (deferred): F-refs-CP-3, batched with existing F-pilot-CP-7 for post-WM discipline patch

## Outcome

CP r1 review **passes** with one pilot-lane patch (v0.1.1) and one deferred discipline-lane batch. Patch can run immediately. Phase-0 progression unaffected.
