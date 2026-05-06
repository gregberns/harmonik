# SH Pilot Load Findings — 2026-05-05

`pilot: sh-pilot-data.yaml` v0.1.1 · `loader: scripts/load-pilot.py` v0.1 · `discipline: v0.10` (HC-r1 lockstep release)

This document captures load-time findings from the SH pilot integration into the harmonik Beads workspace. Companion to `bootstrap-subset.md` §7.

## Summary

| Metric | Value |
|---|---:|
| Beads created | 54 (1 epic + 53 children) |
| Edges declared in yaml | 133 (91 intra-spec + 42 cross-spec) |
| Edges accepted into DB | 129 |
| Edges rejected by Beads cycle-detector | 4 |
| Forward-deferred edges | 0 (first pilot in corpus to ship zero) |
| Cross-spec mnemonics resolved | 42 / 42 (all 7 depends-on specs loaded prior) |
| `br dep cycles` (post-load) | clean |
| `scope:bootstrap` labels applied | 54 |

## F-load-SH-1 — 4 cycle-rejected intra-spec edges

The Beads cycle-detector rejected 4 intra-spec edges at `dep add` time. Each pairs with an opposite-direction edge already in the yaml (i.e., the yaml authored both `A→B` and `B→A` between the same two beads, or via a 1-hop transitive path). The loader logged each rejection and continued (per `loader-tooling.md` "edge rejections are recorded as findings, not load-blockers").

| YAML line | Edge (mnem) | Resolves to (id) | Cycle reason |
|---:|---|---|---|
| 639 | `sh-015a → sh-012` | `hk-i0tw.16 → hk-i0tw.12` | yaml also authors `sh-012 → sh-015 → sh-015a` (transitive) |
| 643 | `sh-016a → sh-014` | `hk-i0tw.18 → hk-i0tw.14` | yaml also authors `sh-014 → sh-016a` (direct opposite) |
| 645 | `sh-017 → sh-016a` | `hk-i0tw.19 → hk-i0tw.18` | yaml also authors `sh-016a → sh-017` (direct opposite) |
| 657 | `sh-022 → sh-015a` | `hk-i0tw.24 → hk-i0tw.16` | yaml also authors `sh-015a → sh-022` (direct opposite) |

### Root-cause classification

All 4 are F13 violations (discipline §2.5 / F13 "bidirectional cite — pick a direction"). The pilot author wrote the citing relation in both directions during decomposition (likely from reading the spec text in both `cites X` and `cited-by X` form). The loader's cycle-detector caught all four; no DB corruption.

### Disposition

Defer to v0.2.x SH pilot patch. Each rejected edge needs a 1-line review of the spec text to decide which direction to keep:

- `sh-012 (fixture-up) ↔ sh-015a (snapshot)` — likely keep `sh-015a → sh-012` and drop `sh-012 → sh-015 → sh-015a` ladder (snapshot points at fixture-up; not the other way).
- `sh-014 (event-log) ↔ sh-016a (synthetic project root)` — keep `sh-016a → sh-014` (synthetic root mounts the event-log path).
- `sh-016a (synthetic root) ↔ sh-017 (orchestrator)` — keep `sh-017 → sh-016a` (orchestrator working-dir is the synthetic root).
- `sh-015a (snapshot) ↔ sh-022 (assertion eval)` — keep `sh-022 → sh-015a` (assertions inspect the snapshot).

These choices are tentative; the v0.2.x patch should review the spec text and pick the direction that matches sensor↔impl per discipline §2.5. The cycle-rejected direction in each case is the one to drop.

### No corpus-level impact

The 4 missed edges are intra-spec. They do not affect:
- Cross-spec dependency closure (zero forward-deferred edges, as claimed in pilot synthesis).
- `br dep cycles` (post-load is clean — the rejected edges were never added).
- `scope:bootstrap` labelling (all 54 SH beads received the label).
- Bootstrap subset count (`bootstrap-subset.md` §2 reports 345).

## F-load-SH-2 — concurrent loader runs (operator-only finding)

During this load, two `load-pilot.py` invocations ran concurrently against the same yaml due to a harness output-stream issue that left the operator unsure whether the first run had completed. Both runs were idempotent for `dep add` (rc=0 with `action: already_exists` → recorded as duplicate-not-error) and for bead creation (the second run found all mnemonics in the mnem-map and skipped them). No data corruption observed; no extra DB rows created. This is an operator-process note rather than a tooling bug — the loader's resume-protocol ledger held up under the unintended concurrency. Future loads should single-flight via lockfile or `flock` if the harness output-stream issue persists.

## Status

Both findings are documented. F-load-SH-1 is queued for the v0.2.x SH pilot patch (4-line yaml change). F-load-SH-2 is operator-only and requires no patch.
