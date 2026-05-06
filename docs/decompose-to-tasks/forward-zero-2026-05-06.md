# Forward-Zero Verification — `hk-ahvq.39` (re-verify after waves 3+4)

**Date:** 2026-05-06
**Scope:** all 8 `*-pilot-data.yaml` files under `docs/decompose-to-tasks/`
**Raw grep:** [`forward-zero-grep-2026-05-06.txt`](forward-zero-grep-2026-05-06.txt)
**Prior pass:** [`forward-zero-2026-05-05.md`](forward-zero-2026-05-05.md) (99 stale edges → this pass: 3 stale)
**Triggering patches landed in `f65447f` (Phase-0 closeout wave 4):**
- `.45` EV v0.3.3 → v0.3.4 (3 events added to event-model.md §8.2)
- `.46` CP pilot v0.1.1 → v0.1.2 (10 §3.2-violating yaml entries deleted)

## Resolution (2026-05-06, after wave 2)

**`hk-ahvq.39` CLOSED. Forward-zero achieved.** The diagnosis below was the initial pass. The orchestrator dispatched a follow-up agent that closed the gap:

1. Minted 3 EV-event beads under epic `hk-hqwn`: `hk-hqwn.59.79` (`control_points_registration_started`), `hk-hqwn.59.80` (`verdict_envelope_mismatch`), `hk-hqwn.59.81` (`policy_expression_exceeded_cost`). All carry `scope:bootstrap`.
2. Appended 3 rows to `mnem-maps/ev-mnem-map.csv`.
3. Rewrote `cp-pilot-data.yaml` lines 1244-1246 to drop the `forward:` prefix; bumped `cp-pilot.md` to v0.1.3.
4. Added 3 cross-spec edges via `br dep add` (cp-034b → policy_expression_exceeded_cost, cp-041 → verdict_envelope_mismatch, cp-043 → control_points_registration_started). `br dep cycles` clean.
5. Final forward-zero re-grep: 26 legitimate (PL→ON cycle-break) + **0 stale**.
6. Discipline patched to v0.12 in parallel: F-pilot-PL-4 tag-locus relaxed from edge-level (structurally unsatisfiable) to bead-level (matches `cite:wide-fanout` precedent).

The diagnostic record below is preserved as the audit trail of what was missing and how it was filled.

---

## Initial result (pre-resolution)

**unresolved-survivors — 3 stale CP forward-edges remain.** `hk-ahvq.39` left **OPEN**.

Forward-zero is not yet achieved. The 3 surviving CP `forward:ev-events.*` strings are referenced by `.45`'s commit message as "resolved" — but the actual `.45` patch only added the 3 events to the EV spec body; it did NOT mint mnem-map rows in `mnem-maps/ev-mnem-map.csv` and did NOT rewrite the citing CP yaml entries to drop the `forward:` prefix. Additional bead-creation + yaml-rewrite work is required before `.39` can close.

## Grep summary

| Metric | Count |
|---|---|
| Total edge-form `forward:*` entries (`{from: …, to: "forward:…"}`) | 29 |
| Legitimate per F-pilot-PL-4 cycle-break carve-out (PL → ON, narrative-scoped) | 26 |
| Stale (require yaml rewrite + upstream artifact additions) | 3 |
| Total non-edge `forward:*` mentions (prose / comments / narrative) | ~50 |

(Per-yaml edge-only counts — non-edge prose mentions excluded.)

## Per-pilot table

| Pilot | Edge `forward:*` count | Classification | Notes |
|---|---|---|---|
| `cp-pilot-data.yaml` | 3 | **STALE (b3 missing-row)** | All 3 are `forward:ev-events.<name>`. EV is in CP's `depends-on`, so not a §3.2 violation. The 3 events now exist in the EV spec body (`.45`) but ev-mnem-map.csv has no rows for them, so the yaml strings cannot yet be rewritten to non-`forward:` form. |
| `hc-pilot-data.yaml` | 0 | clean | All 7 prior survivors cleared (yaml-cleanup pass 2026-05-05). |
| `meta-pilot-data.yaml` | 0 | clean | Prose mentions only (describing protocol). |
| `on-pilot-data.yaml` | 0 | clean | All 12 prior survivors cleared (yaml-cleanup pass 2026-05-05); 3 cycle-rejected entries deleted. |
| `pl-pilot-data.yaml` | 26 | **legitimate (F-pilot-PL-4 carve-out)** | All 26 are `forward:on-*`. PL excludes ON from `depends-on` per §9.3 cycle-break NOTE; PL normative prose names ON obligations via §2/§9 named-obligation pattern (PL-008/PL-027 et al). Discipline v0.10/v0.11 §3.2 carve-out recognises this exact pattern. **Caveat below.** |
| `rc-pilot-data.yaml` | 0 | clean | RC was last-loaded; no forward edges ever emitted. |
| `sh-pilot-data.yaml` | 0 | clean | First pilot to ship with zero forward-deferred edges. |
| `wm-pilot-data.yaml` | 0 | clean | All 28 prior survivors cleared (yaml-cleanup pass 2026-05-05); 6 new BI edges added via `br dep add`. |

### CP stale survivors (line-by-line)

| Line | Mnem | Target | Class | Required action |
|---|---|---|---|---|
| 1244 | cp-034b | `forward:ev-events.policy-expression-exceeded-cost` | (b3) missing-row | Mint `ev-events.policy-expression-exceeded-cost` row in `ev-mnem-map.csv`; rewrite yaml to `ev-events.policy-expression-exceeded-cost` (drop `forward:`). Optionally rename to underscore form `policy_expression_exceeded_cost` to match EV §8.2 convention. |
| 1245 | cp-041 | `forward:ev-events.verdict-envelope-mismatch` | (b3) missing-row | Same — mint EV mnem-map row + rewrite CP yaml. |
| 1246 | cp-043 | `forward:ev-events.control-points-registration-started` | (b3) missing-row | Same — mint EV mnem-map row + rewrite CP yaml. |

### PL legitimate-carve-out edges (count 26)

26 PL → ON edges across `pl-pilot-data.yaml` lines 1208-1233. Per discipline v0.11 §3.2 cycle-break carve-out (F-pilot-PL-4), these are recognised as legitimate cross-spec cites where PL's normative prose names ON obligations and the front-matter §9.3 cycle-break NOTE prevents ON appearing in PL's `depends-on`.

**Caveat — tag mechanism gap (class-lane finding for next discipline pass):**

The discipline §3.2 carve-out states: *"The pilot MUST tag each such edge with `cite:cycle-break:<excluded-spec>` (analogous to `cite:wide-fanout` in §3.1.3) so reviewers can distinguish carve-out edges from missed-`depends-on` bugs."*

In the current PL yaml, **0 of 26 edges carry a `cite:cycle-break:operator-nfr` tag** at the edge level. The yaml schema attaches tags to *beads* (`tags: [...]` on a bead entry), not to edge entries (`{from:, to:}`). The bead-level tag carries a different semantic (it labels the bead, not the specific outgoing edge). The carve-out's tag-per-edge requirement therefore cannot be satisfied with the current edge-form schema; either:

1. Extend the yaml edge schema to support per-edge tags: `{from:, to:, tags: [...]}` (a discipline + loader-tooling change), then backfill 26 PL edges + any future cycle-break edges; OR
2. Relax F-pilot-PL-4 to require *bead*-level `cite:cycle-break:<excluded-spec>` tags on the citing bead (matching the existing `cite:wide-fanout` mechanism), and backfill the relevant PL beads.

This is a **class-lane discipline finding** to fold into the next discipline patch (sibling to the v0.10 PL backfill triplet additions in §2.13). The orchestrator-level framing (the prior session's HANDOFF and this re-verify task) treats the 26 PL → ON edges as legitimate via prose-narrative scoping (the PL yaml's narrative comments at lines ~150-200 and ~280 explicitly invoke F-pilot-PL-4); under that framing, stale = 3.

**Filing recommendation:** new bead `hk-ahvq.51` "PL pilot — apply F-pilot-PL-4 cycle-break tags to 26 forward:on-* edges (after discipline patch decides tag scheme)".

## Status decision

**`hk-ahvq.39` left OPEN.** Not flipped to `done`.

**Why:** 3 surviving CP `forward:ev-events.*` strings. The `.45` patch's commit message asserts these are resolved, but the actual landed change does not include the two follow-up steps required to drop the `forward:` prefix:

1. Mint 3 rows in `mnem-maps/ev-mnem-map.csv` (`ev-events.policy-expression-exceeded-cost` → `<assigned_id>`, etc.). The assigned IDs require minting beads in the EV epic (epic `hk-hqwn`) for the new event rows — currently no `br list` row exists for any of the 3 names.
2. Rewrite `cp-pilot-data.yaml` lines 1244-1246 to drop the `forward:` prefix and (if desired) normalize the kebab → underscore form to match EV §8.2 convention.

**Path to clean:**
1. File 3 EV-side beads for the new events (likely under epic `hk-hqwn`'s next-available indices; or a child epic for "EV §8.2 additions wave 4"). Beads MUST be `scope:bootstrap` since CP cites them as hard deps.
2. Append 3 rows to `ev-mnem-map.csv`.
3. Rewrite 3 CP yaml entries (drop `forward:`).
4. Re-run this verification — expected stale count = 0.

These steps are mechanical-ish and could be folded into a single follow-up bead (e.g., `hk-ahvq.52` "Mint 3 EV-event beads + ev-mnem-map rows; rewrite CP yaml to close `.39`").

**Alternative** (orchestrator-deferred): downgrade these 3 `forward:` strings to a tracked TODO comment in the CP yaml narrative + delete the edge entries (treating them as F-pilot-EV-3 informational findings until EV's next bead-mint pass). This preserves forward-zero formally without losing the dependency record. The current shape (forward-deferred edges with no resolvable target) is the worst of both: it's a stale string AND the dependency is not actually loadable into Beads.

## Cross-pass deltas (what changed since 2026-05-05)

| Bucket | 2026-05-05 | 2026-05-06 | Delta | Driver |
|---|---|---|---|---|
| Total edge-form `forward:*` | 99 | 29 | −70 | yaml-cleanup pass + `.46` |
| Stale (b1/b2/b3) | 99 | 3 | −96 | yaml-cleanup pass + `.46` |
| Legitimate cycle-break (F-pilot-PL-4) | 0 (rule didn't exist yet) | 26 | +26 | discipline v0.10 mint of carve-out |
| HC stale | 7 | 0 | −7 | `.23` follow-up yaml-cleanup |
| ON stale | 12 | 0 | −12 | `.37` follow-up yaml-cleanup + 3 cycle deletions |
| PL stale (rc-direction) | 13 | 0 | −13 | `.37` follow-up yaml-cleanup |
| PL stale (on-direction) | 26 | 0 | −26 | discipline v0.10 reclassified as legitimate |
| WM stale | 28 | 0 | −28 | `.16/.23/.30/.37` follow-up yaml-cleanup + new BI→priors edges |
| CP stale (§3.2 violations) | 10 | 0 | −10 | `.46` deletions |
| CP stale (b3 ev-events) | 3 | 3 | 0 | `.45` did spec-side only; mnem-map + yaml rewrite still needed |

## Status flip

**`br update hk-ahvq.39 --status closed`** EXECUTED 2026-05-06 after the wave-2 follow-up landed (3 beads minted, 3 mnem-map rows added, 3 yaml entries rewritten). See the "Resolution" block at the top of this file for the full closure record. The follow-up was performed inline by the orchestrator's wave-2 agent rather than under a separately-filed `hk-ahvq.52` bead — the discipline §2.13 backfill-closure-criteria checklist (yaml mutation at closure) was applied directly.

## Unexpected patterns surfaced (carry forward)

- **`.45` commit-message vs. landed-change gap.** The `.45` commit message asserts that the EV spec patch "resolves the 3 EV-event forward cites in CP pilot." This is true for the spec-text gap (CP §6.5 references events that EV §8 now declares) but not for the pilot-graph gap (the CP yaml's `forward:` strings, which require mnem-map rows + yaml-rewrite). This is the second instance of the "yaml-rewrite gap as a class issue" pattern surfaced in 2026-05-05 (which led to discipline v0.10 §2.13 backfill-closure-criteria). Consider extending §2.13 to include "spec-patch beads MUST include downstream pilot-yaml rewrites in their closure criteria when they introduce/remove cited entities."
- **F-pilot-PL-4 tag-mechanism gap.** The carve-out specifies edge-level tagging but the yaml schema is bead-level. Either the discipline must adapt to the schema or the schema must adapt to the discipline. Class-lane finding for next discipline patch.
- **3 CP yaml entries are zombie edges.** They reference targets (`ev-events.X`) that exist as spec rows but have no Beads bead and no mnem-map entry. The `br dep add` for these would currently fail. This is structurally distinct from prior stale-edge classes (b1 was "edge added in Beads, yaml not rewritten"; b2 was "§3.2 violation"; b3 here is "neither bead nor mnem-map row exists yet, AND yaml not rewritten").

## S07-pending references

**None.** Zero `forward:sh-*` mnemonics. SH was the first pilot to ship with zero forward-deferreds; all pilots loaded after SH (none, since SH was last after waves 1-2) need not concern this caveat.
