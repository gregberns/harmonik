# ON Pilot — R1 Review Synthesis

`synthesis-version: 1.0` — drafted 2026-04-30 by orchestrator (`hk-ahvq.28`). Combines the three parallel reviewer outputs in this directory.

## Reviewer outputs

- `coverage-r1.md` — **CLEAN** with 2 MINOR cosmetic. All 60 §4 reqs + 8 ON-027 step beads + 4 invariants + 0 schemas + 1 error-taxonomy + 11 test-infra accounted for. STATUS.md drift confirmed: ON's actual count is 60 (F-pilot-ON-1 stands).
- `decomposition-r1.md` — **CLEAN** with 1 MINOR class — F-pilot-ON-5 only. **Verdict: (a) CORRECT AS-IS** for ON-027 split. F8b's "shared function body" correctly collapses PL-005/006/011/027 (cohesive function body) but does NOT capture ON-027's delegating-orchestrator structure (each step body lives in a different subsystem).
- `references-r1.md` — **0 BLOCKER / 2 MAJOR / 3 MINOR**, all local mechanical fixes.

## Findings table

| ID | Severity | Lane | Reviewer | Summary |
|---|---|---|---|---|
| F-cov-ON-1 | MINOR | local | Coverage | `on-error.taxonomy` labeled "23-code" but §8 has 24 numbered rows (codes 0..23 incl. code 0 success). Internal-shorthand drift. |
| F-cov-ON-2 | MINOR | local | Coverage | ON-013b is referenced in spec body lines 306, 520 as forward-reference but has no v0.4.1 header. Pilot correctly treats as placeholder — record for ON r2 attention. |
| F-pilot-ON-5 | MINOR | **class** | Decomposition | **Verdict (a) CORRECT AS-IS**: ON-027 split is right. F8b worked-example pair needed in v0.10 codifying delegating-orchestrator vs cohesive-function-body distinction. **Documentation-only patch — no pilot rewrite.** Resolves D-PL-1 + D-PL-2 from PL r1 (both verdict: PL's collapse is also correct under cohesive-function-body interpretation). |
| F-refs-ON-1 | MAJOR | local | Reference | Missed edge: ON-025 body cites `[event-model.md §8.3]` for `skills_provisioned` event; pilot table promises edge; yaml omits. ADD `{from: on-025, to: ev-events.skills-provisioned}`. |
| F-refs-ON-2 | MAJOR | local | Reference | Missed forward: ON-028 cites `[reconciliation/spec.md §4.2]`; pilot table + §3.2 narrative promise; yaml omits. ADD `{from: on-028, to: forward:rc-NNN}` — patch agent resolves specific NNN. |
| F-refs-ON-3 | MINOR | local | Reference | 2 invented forward edges (`on-040 → forward:rc-014`, `on-045 → forward:rc-018`) lack source cites. REMOVE. Brings forward-deferred count 13→11. |
| F-refs-ON-4 | MINOR | local | Reference | `cite:wide-fanout` count drift: narrative says 4 beads (on-008/013/027/038); yaml has 6 (also on-020/041). UPDATE narrative count. |
| F-refs-ON-5 | MINOR | local→class-strengthening | Reference | F-pilot-ON-6 enumeration error: pilot lists 6 WM cites; spec grep yields 7. Pilot mis-routes #1 (ON-022's WM cite is actually ON-INV-003), and misses ON-ENV-001 (§6.1 Workspace declarative) + ON-015 (§4.7 session-log metadata, load-bearing). 6 of 7 are load-bearing — strengthens F-pilot-ON-6 class signal that WM should join ON's depends-on next revision. |

## Triage

### Pilot-lane (v0.1.1 patch)

1. **F-refs-ON-1** (MAJOR): ADD `{from: on-025, to: ev-events.skills-provisioned}`.
2. **F-refs-ON-2** (MAJOR): RESOLVE specific RC-NNN (read `specs/reconciliation/spec.md` §4.2 for the load-bearing single-owner) and ADD `{from: on-028, to: forward:rc-NNN}`.
3. **F-refs-ON-3** (MINOR): REMOVE 2 invented edges. Audit ON-040 + ON-045 spec body to confirm no RC cite.
4. **F-refs-ON-4** (MINOR): UPDATE narrative wide-fanout count from 4 → 6 (also list on-020 + on-041).
5. **F-refs-ON-5** (MINOR): UPDATE F-pilot-ON-6 finding text in both yaml top-comment and narrative §3 to reflect 7 cites + corrected enumeration. Replace ON-022 reference with ON-INV-003. Add ON-ENV-001 + ON-015.
6. **F-cov-ON-1** (cosmetic): "23-code" → "24-code" wherever it appears.
7. **F-cov-ON-2** (cosmetic): note ON-013b placeholder behavior in narrative §10 or §1 footer.

Bump `pilot-version: 0.1.0 → 0.1.1`. Update top-comment patch block. Update on-pilot.md §10.

### Discipline-lane batch (now collapsed/refined)

Previous discipline batch had 11 findings; ON r1 produces ONE refinement and ONE confirmation:

- **D-PL-1 + D-PL-2 + F-pilot-ON-5 → ONE entry**: F8b worked-example pair for v0.10. Decomposition reviewer's verdict pair (PL collapses correct AS-IS, ON splits correct AS-IS) means no pilot rewrite. Documentation-only.
- F-pilot-ON-3 confirms F-pilot-PL-1 §6-zero-ownership pattern (corpus second).

**Discipline-lane batch is therefore at 11 entries** (one was clarified, not added):
1. F-pilot-CP-7 + F-refs-CP-3 — wide-fanout body-enumerated row-set + section-anchor wide-fanout
2. D-WM-3 — invariant→invariant rule precedence
3. F-pilot-WM-2 / D-WM-6 — §2.11(c) SHAPE-not-COUNT
4. **D-PL-1 + D-PL-2 + F-pilot-ON-5 (RESOLVED to documentation patch)** — F8b worked-example pair
5. F-pilot-PL-4 — §3.2 cycle-break-named-obligation carve-out
6. F-refs-PL-3 — `cite:wide-fanout` mandatory threshold
7. PL backfill: author-label collision pattern (forward:X-NNN may collide with target's actual mnem)
8. PL backfill: reciprocal-cite cycles (one direction must downgrade to mentions)
9. PL backfill: OQ-coordination cites (not edges)
10. F-pilot-ON-3 (CONFIRMATION of F-pilot-PL-1) — corpus pattern; not a discipline patch on its own
11. F-pilot-ON-6 — WM-cites-without-depends-on (sibling of F-pilot-PL-4 named-obligation; possibly bigger pattern)

## Re-run plan

Edges-only mode (`--skip-beads`). Net edge changes:
- ADD: 2 (F-refs-ON-1 + F-refs-ON-2)
- REMOVE: 2 (F-refs-ON-3 invented forwards)
- Net: 0 edge count change; total still 271 ok + 13 forward-deferred = 284 — wait, 13-2 invented removed + 1 added forward (F-refs-ON-2) = 12 forward-deferred. And 271+1 added (F-refs-ON-1) = 272 ok. Total 284 unchanged (since forwards moved between counts).

Cycles MUST remain 0.

## Outcome

ON r1 review **passes** with 5 mechanical pilot-lane fixes for v0.1.1, 2 cosmetic touchups, and 1 class-lane finding RESOLVED to documentation-only discipline patch. F-pilot-ON-5 is the highest-value session output — it RESOLVES the deferred D-PL-1/D-PL-2 question (both pilots' F8b decisions are correct under different applicable patterns). Phase-0 progression unaffected.
