# HC Pilot r1 Synthesis

**Date:** 2026-04-30  
**Bead:** `hk-ahvq.1`  
**Inputs:** `coverage-r1.md`, `decomposition-r1.md`, `references-r1.md`  
**Pilot under review:** `docs/decompose-to-tasks/hc-pilot.md` v0.1.0 + `hc-pilot-data.yaml`  
**Spec:** `specs/handler-contract.md` v0.3.3  
**Discipline:** `discipline.md` v0.8

---

## Outcome at a glance

- **3 BLOCKER findings** — cycle-rejected edges; must fix before re-load.
- **2 MAJOR findings** — narrative/edge cleanup; should fix this round.
- **3 MINOR findings** — cosmetic.
- **1 class-lane finding** — discipline doc gains anti-example for `<spec>-error.taxonomy` direction.

The pilot's coverage is clean (all 65 reqs, 7 invariants, 5 schemas, 7 §8 entries accounted for). The bug class is concentrated in **two patterns**: edge-direction inversion on the §8 taxonomy bead (5 of 6 cycle rejections) and bidirectional-cite resolution (1 cycle rejection plus 1 cited near-miss). Both are pilot-application errors against existing rules; one is recurring across pilots (3rd surfacing) and warrants a discipline anti-example.

---

## Triage table — every BLOCKER / MAJOR

For each finding: probes per protocol §4.1 (Generality / Rule-vs-application / Silence / Reviewer self-tag) → lane.

| # | Finding | Source | Severity | Lane | Probe-1 generality | Probe-2 rule-vs-app | Probe-4 reviewer tags | Lane decision |
|---|---|---|---|---|---|---|---|---|
| 1 | F-load-HC-2: `hc-026a → hc-008a` 3-cycle (chained via hc-026) | Cov F#2, Dec F-dq-HC-3, Ref F#2 | BLOCKER | local | One-off bidirectional pair | App: F-pilot-AR-10 covers it | All 3 say `local` | **pilot-patch** |
| 2 | F-load-HC-3: `hc-044 ↔ hc-007` bidirectional | Cov F#2, Dec F-dq-HC-2, Ref F#3 | BLOCKER | local | One-off; F13 slot-rule covers | App: F13 covers, pilot didn't walk | All 3 say `local` | **pilot-patch** |
| 3 | F-load-HC-4..7: 4 inverted `taxonomy → req` edges | Cov F#2, Dec F-dq-HC-1, Ref F#4–7 | BLOCKER | local + class | **YES** — this is 3rd surfacing of the anti-pattern (EM #15, EV #15, HC #27) | App: §2.6/§2.11(c) covers | Dec flags class-lane recommendation; Cov + Ref say `local` | **pilot-patch + discipline-patch** (doc anti-example) |
| 4 | 6 additional `taxonomy → req` edges (not cycle-rejected but same wrong direction) | Dec F-dq-HC-1 | MAJOR | local | Same as #3 | Same as #3 | Dec only (Ref scoped to cycle list) | **pilot-patch** (rolls into #3 fix) |
| 5 | §5.3 narrative says "22 EV edges" but YAML/loader has 26 | Cov F#1, Ref F#1 | MAJOR | local | One-off arithmetic slip | App: pilot author counted distinct pairs but reported wrong number | All `local` | **pilot-patch** (prose) |
| 6 | `hc-inv-006` predecessor list missing `hc-021` | Dec F-dq-HC-8 | MAJOR | local | One-off; §2.5 source #4 covers term-use single-owner pin | App: pilot bound "protocol mismatch" to HC-009 (first-message rule) instead of HC-021 (the sub-sentinel owner) | Dec only | **pilot-patch** |

**Lane summary:** 5 findings strictly pilot-patch, 1 finding (the recurring taxonomy anti-pattern) gets BOTH a pilot-patch (the data-level fix) AND a discipline-patch (a documentation-only anti-example so CP/WM/PL/ON/RC pilots don't repeat the mistake).

---

## Resolved divergence between reviewers

**On F-load-HC-3 (`hc-044 ↔ hc-007` direction):** Decomposition says keep `hc-007 → hc-044` (drop `hc-044 → hc-007`); Reference says keep `hc-044 → hc-007` (drop `hc-007 → hc-044`). I read the spec to decide.

- HC-007 (§4.2): wire-protocol message-stream contract — what runs over the socket.
- HC-044 (§4.10): socket-path declaration `.harmonik/daemon.sock` — owns the socket file definition + parent-child rule.

Per discipline §2.7 F13 slot-rule heuristic: HC-044 owns the slot (the socket file as IPC channel); HC-007 is the content (messages over the slot). **Content blocks-on slot.** Therefore `hc-007 → hc-044` is the keep edge; the cycle-closing `hc-044 → hc-007` must drop. **Decomposition reviewer is correct;** Reference reviewer's reasoning ("HC-007 establishes the existence of the socket") got the ownership backwards.

The pilot's existing line 623 `{from: hc-007, to: hc-044}` stays. Line 678 `{from: hc-044, to: hc-007}` is removed.

---

## Patch list (in execution order)

### A. Discipline-patch lane (do first per §4.2)

**discipline.md v0.8 → v0.9** — add anti-example clarifying `<spec>-error.taxonomy` direction.

The anti-pattern has surfaced 3× now (EM r1 finding #15, EV r1 finding #15, HC r1 — this synthesis F-dq-HC-1). Each time, a pilot author sees the §8 error-taxonomy as a "structural carve-out / slot" (per F13) and emits `taxonomy → <req>` edges. The correct framing: the taxonomy is a **schema-shaped vocabulary owner** per §2.6 / §2.11(c.1); consumers (§4 reqs that cite a sentinel name) `block-on` the taxonomy bead.

Patch §2.11 with an explicit anti-example block: "The `<spec>-error.taxonomy` bead is NOT a slot rule in the F13 sense. It is a vocabulary-owner: §4 reqs that *use* sentinel names (`ErrTransient`, `ErrStructural`, etc.) are CONSUMERS and `block-on` the taxonomy bead, exactly like a schema cite per §2.6. Edges run `<req> → <spec>-error.taxonomy`, never the inverse."

### B. Pilot-patch lane

Apply to `hc-pilot-data.yaml`:

1. **Delete the 10 inverted `hc-error.taxonomy → <req>` intra-spec edges** (lines 705–714). Lines 702–704 (schema → taxonomy) stay; lines 824, 850, 851 (cross-spec edges to EM/EV) stay.

2. **Add `<req> → hc-error.taxonomy` edges** for the §4 reqs that cite sentinels in normative prose. Per the decomposition reviewer's enumeration plus my spec walk: HC-004, HC-008, HC-008a, HC-009, HC-011a, HC-013, HC-013a, HC-016a, HC-018, HC-020, HC-021, HC-022, HC-023, HC-024, HC-024a, HC-026, HC-043, HC-044a, HC-048, HC-048a (≈20 edges).

3. **Delete `{from: hc-026a, to: hc-008a}`** at line 654 (F-load-HC-2 fix).

4. **Delete `{from: hc-044, to: hc-007}`** at line 678 (F-load-HC-3 fix; keep line 623 `hc-007 → hc-044`).

5. **Add `hc-021` to `hc-inv-006`'s predecessor list** (F-dq-HC-8 fix).

Apply to `hc-pilot.md`:

6. **§5.3 narrative:** change "Total emitted cross-spec edges to EV: 22" to "26" with a sentence noting the count reflects loadable rows (not de-duplicated citing-bead × target pairs).

7. **§9 revision history:** add v0.1.0 → v0.1.1 row capturing the 5 patches above.

### C. DB cleanup (after YAML patch, before re-run)

The 6 wrong-direction `hc-error.taxonomy → <req>` edges that DID load (hc-007, hc-008, hc-009, hc-024, hc-044a, hc-048 — the ones that didn't close cycles) are already in `.beads/`. The loader's resume mode appends, doesn't reconcile, so these need explicit removal:

```
br dep remove hc-error.taxonomy hc-007 -t blocks
br dep remove hc-error.taxonomy hc-008 -t blocks
br dep remove hc-error.taxonomy hc-009 -t blocks
br dep remove hc-error.taxonomy hc-024 -t blocks
br dep remove hc-error.taxonomy hc-044a -t blocks
br dep remove hc-error.taxonomy hc-048 -t blocks
```

(The 4 cycle-rejected edges hc-008a, hc-024a, hc-026, hc-048a never loaded; no DB cleanup needed for those.)

Also: `hc-026a → hc-008a` and `hc-044 → hc-007` were both rejected at load (in the F-load-HC-2 / F-load-HC-3 lines), so no DB cleanup for those either.

### D. Re-run loader

```
python3 scripts/load-pilot.py docs/decompose-to-tasks/hc-pilot-data.yaml
```

Resume mode: bead creates skip; only new/missing edges retry. Verify:
- All previously-rejected 6 edges (after fixes) either no longer attempted (F-load-HC-2/HC-3) or now accept (F-load-HC-4..7 inverted).
- New `<req> → hc-error.taxonomy` edges accept.
- New `hc-021 → hc-inv-006` edge accepts.
- `br dep cycles` returns clean across the AR + EM + EV + HC + BI union.

---

## MINOR findings (author discretion)

- **Cov #1 sub-finding** ("62" first-class-req derivation): Pilot's §1 says "65 active − 3 coalesces = 62." Should be "65 − 2 = 63" (only 2 coalesce clusters: HC-007/007a/007b → 1 bead [3→1, saving 2]; HC-046/047 → 1 bead [2→1, saving 1] = 3 reqs absorbed into coalesce comments, not 3 coalesces). Cosmetic; the "81 total" tally still adds up via different intermediate.
- **F-dq-HC-11**: `hc-inv-001` sensor description doesn't name a specific verification mechanism. Apply if convenient.
- **F-dq-HC-12**: `hc-schema.launch-spec` title says "13-field" but spec has 14 fields. One-character fix.
- **Ref Finding #7** (`cite:wide-fanout` tag verification on hc-007 / hc-026 / hc-033): trivial check; apply if missing.

---

## What's NOT being patched

- **Forward-deferred PL edges** (Ref Finding #8). Correctly designed per F-pilot-EM-2 Option B; reciprocal direction materializes at PL pilot.
- **Non-depends-on cite scan** (Ref Finding #9). Confirmed only WM/CP/RC/ON cites are in informative §2 (Out of Scope), not normative §4–§8. No additional forward-deferred entries needed.
- **§7.1 silent-hang FSM split** (Dec F-dq-HC-6) and **§7.2 launch handshake split** (Dec F-dq-HC-7). Correctly NOT split per §2.2 F8b shared-function-body; no change.
- **HC-008+HC-008a, HC-026+HC-026a coalesce near-misses** (Dec F-dq-HC-21, F-dq-HC-22). Correctly kept separate per §2.3 three-AND test failures; no change.

---

## Re-review trigger?

Per protocol §2 "after pilot patch":
- Patch class = bead structure changes (new `<req> → taxonomy` edges, new `hc-021 → hc-inv-006`). **All three reviewers should re-run** at r2 after the patch lands.
- However: the changes are mechanical inversions/additions, not new decomposition decisions. The discipline-doc patch (anti-example) is documentation-only. **Recommend a single targeted re-run of the Reference reviewer at r2** (verify all new edges trace to inline cites and no cycles remain), and skip Coverage + Decomposition r2 unless the load surfaces a new structural bug.

The author makes the final call; documenting this here so the next session can decide.

---

## Ready for promotion?

After patches A–D land and the re-load is clean, hk-ahvq.1 is **complete**. The next bead in the meta-graph is `hk-ahvq.2` (apply HC r1 patches and re-run loader) — which executes patches B–D above. Patch A is ahead-of-the-graph (it lives in the discipline lane and unblocks all subsequent pilots, not just HC) so it should land before B–D start.
