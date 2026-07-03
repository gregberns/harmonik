# logmine — Findings iter-10 (2026-06-18) · POST-INCIDENT (the context-cancel saga)

**Crew:** logmine · **Epic:** hk-mhmaw · **Queue:** logmine-q
**Window:** line-anchored from iter-9 high-water `019ed834-01ee-75c4-9043-40f588dd70f6`
→ **566 events** (lines 37437–38002), 2026-06-18T00:49:40Z → 03:56:46Z (~3.1h).
**Mode:** captain-directed POST-INCIDENT harvest — 6-slice fan-out over the frozen snapshot
`/tmp/logmine-window.jsonl`, plus a 7th ADVERSARIAL-VERIFIER pass that overruled three over-stated
slice-6 claims (review-gate discipline). logmine stayed OFF daemon-package files (paul owns them).

---

## HEADLINE — GREEN. Root cause found, fixed, DEPLOYED, and live-validated.

This window is the *resolution* of the DOT/review-loop context-cancel saga that dominated the
previous ~6h. The cancels were **NOT** cold-cache and **NOT** a too-short node deadline — both
hypotheses were proposed AND refuted live. The real bug, isolated via **hk-wths**:
`stalewatch.observe()` skipped events with a **nil run_id**, so a bare `Emit()` left
`agentReadySeen=false` and a reaper that was **never spawned** still FALSE-fired and cancelled
per-run DOT contexts. Fixed in **`960deafc`**, **deployed at 03:53:36Z** (daemon pid 51373 on
binary `81b5de65`), and **live-validated**: run `019ed8e3-39bb` started 04:00:33Z (post-restart),
ran a full multi-node DOT cascade for ~5 min, and `run_completed` 04:05:14Z **with no
context_cancel**. **0% unrecovered work loss** — every one of the 8 context-cancelled runs had
already committed; all work landed on main via salvage-promote.

**The cost (the efficiency lesson):** ~**270 min (~4.5h) of Opus** was burned across the saga —
~123 min of implementer phases discarded in 7 context-cancelled runs, plus ~60 min salvage-promote
overhead and ~30 min hypothesis-validation setup — chasing two refuted hypotheses before the
never-spawned-reaper root cause surfaced. See N1.

---

## The incidents (mined first)

**Incident A — cache-reaper merge-outage → CONFIRMED FIXED & CLEAN.** The pre-window proactive
60-min `go clean -cache` reap (hk-sxlb) raced in-flight merge-builds → `merge_build_failed (go vet:
could not import …)`. Stopgap `5c2276ca` disabled the proactive reap (kept the reactive
10GiB-watermark reap); daemon restarted 00:29:53Z. **Zero** real `merge_build_failed`/go-vet events
by-`.type` after 00:29:53Z (only comms bodies quote the terms). Proper merge-aware guard-fix
**hk-guez** stays OPEN in paul's daemon lane.

**Incident B — DOT context_cancel strand (hk-e3fy) → ROOT-CAUSED & FIXED.** The strand symptom
(`dot: agentic node "implement" failed: context cancelled` leaving the bead `in_progress`) was
fixed two ways: hk-e3fy added the DOT-cascade auto-reset (landed via promote `d0367361`, in HEAD),
and **hk-wths fixed the actual TRIGGER** — the never-spawned-reaper false-cancel (`960deafc`). Both
are in the running binary. 5 strand events occurred this window *before* the deploy (hk-rai2 ×3,
hk-8hr1 ×2), all manually reset by the captain; **0 strands post-deploy**.

---

## The cancel-but-committed saga (slice 1) — 8 run_failed, 100% recovered

| Bead | Run | Cancel @ | Committed first? | Landed on main |
|---|---|---|---|---|
| hk-rai2 i1 | 019ed827-7162 | 01:05:53Z | ✓ | b86a9ea2 / 4a62ff8e / 6afdec00 |
| hk-8hr1 i1 | 019ed827-724b | 01:11:09Z | ✓ | fa98f7c1 (+family) |
| hk-rai2 i2 | 019ed84b-42cb | 01:44:53Z | ✓ | (same set — re-dispatch) |
| hk-8hr1 i2 | 019ed84b-43bd | 01:49:43Z | ✓ | (same set) |
| hk-rai2 i3 | 019ed86c-777c | 01:54:24Z (terminal close-needs-attention) | ✓ | (same set) |
| hk-e3fy   | 019ed897-3b40 | 03:07:53Z | ✓ | d0367361 |
| hk-s8qi   | 019ed897-387a | 03:07:53Z | ✓ | 1d5d737e / 03f72843 |
| hk-s5v3   | 019ed897-3a53 | 03:07:56Z | ✓ | 93b3117b / 44bda383 |

4 `queue_paused` (all `reason=group_failure`: `019ed848-30dc`, `019ed86b-7edf`, `019ed873-67cb`,
`019ed8b3-1cb1`) — the daemon's correct response to each failing group; all recovered (captain
reset / promote). Live queue state: only `main` paused on the **unrelated** hk-tagp.

**Byte-identity proof-of-correctness:** hk-rai2 Phase-B was implemented twice independently
(cold-cache DOT run → `b86a9ea2`; warm-cache review-loop run → `4a62ff8e`); `git diff` between them
was **empty** (identical 291 lines). Two independent runs converging on the same output = a strong
correctness signal → promote, don't re-author. See N3.

---

## The hypothesis-chasing timeline (slice 2) — the meta-lesson

| When | What | By | Outcome |
|---|---|---|---|
| 01:07:33Z | H1: cold-cache blows the DOT deadline (`019ed844-e146`) | captain | mitigated (cache pre-warmed) |
| 01:46:54Z | H1 REFUTED — hk-rai2 failed again on warm cache (`019ed868-e6b8`) | paul | — |
| 01:49:25Z | H2: heavy DOT graph + iter-1 no-op exceeds ~30-min deadline (`019ed86b-354a`) | captain | switched to lighter review-loop graph |
| 03:07:53Z | H2 REFUTED — review-loop also cancelled on real work | paul | — |
| 03:47:05Z | ROOT CAUSE: never-spawned reaper false-fire (nil-run_id skip) (`019ed8d6-f019`) | paul (hk-wths) | fixed `960deafc`, deployed 03:53Z |

---

## FIXED-CONFIRM (the payoff)

| Prior | Evidence this window |
|---|---|
| **hk-wths** (never-spawned reaper false-fire) | ROOT CAUSE. `960deafc` in running binary; validated by clean post-restart DOT run `019ed8e3-39bb`. |
| **hk-e3fy** (DOT strand auto-reset) | Fixed via `d0367361` (in HEAD). 0 strands post-deploy. |
| **Incident A** (cache-reaper merge-outage) | 0 real `merge_build_failed`/go-vet after 00:29:53Z. |
| **F38** (reviewer run_stale FP) | 0 run_stale events. |
| **F57** (review-node agentic timeout) | 0 timeout-class failures; reviewers completed cleanly. |
| **F47** (captain never holds gauge) | 70 `no_gauge` = **100% `foreign_session`** (per-design, post-restart) → **recommend close hk-gfpd**. |
| **F3/F11/F58** (comms dedup / single captain / ad-hoc identity) | clean: unique event_ids, single captain, no `<bead>-impl` recurrence. |

## Keeper cycle (slice 4)

Captain session `849d9e71` ran a clean keeper cycle 01:31–01:35Z: warn@28% → operator restart to
the new 200K/215K band (hk-8hr1) → `handoff_started` (cyc `…013312-000001`) → `clear_unconfirmed`
01:35:02 → `cycle_complete` 01:35:03 (1ms later = clear accepted) → post-restart warn@29% on the
new band. **F45** (warn near threshold) ×2 — feeds keeper-redesign **hk-gffc**; new band is live.

---

## NEW findings (beads filed this window)

- **N1 — Token-burn / detection-invariant meta-lesson** (~270 min Opus on refuted hypotheses).
  Lesson for `major-issue-fanout`: before accepting ANY timing/cache/graph hypothesis, verify the
  **run_id lifecycle is complete** — was a reaper-spawn observed for this run_id? A nil-run_id
  `Emit()` should fail-closed, not silently skip (the exact mechanism hk-wths fixed). → skill-doc
  update (crew:logmine, SAFE) + a daemon-hardening note (paul, likely subsumed by `960deafc`).
- **N2 — Iter-1 harness no-op** — hk-rai2 iter-1 committed ONLY a `.claude/settings.json` edit (no
  code) → REQUEST_CHANGES no-op (reviewer verdict `019ed84f`); iter-2 then implemented full Phase B.
  Cousin of hk-77x1. Daemon/harness lane → crew:paul, digest-only. P2.
- **N3 — Salvage-promote method** — document the reusable recovery (context_cancelled-but-committed
  → verify completeness / byte-identity → `harmonik promote <sha>` → close). → docs/known-workarounds.md
  + harmonik-lifecycle skill. crew:logmine, SAFE doc. P2.
- **N4 — DOT-run / salvage-promote commits skip review-trailer stamping** (recharacterized from a
  false slice-6 "F49 regression"). Salvage cherry-picks (e.g. hk-8hr1 `fa98f7c1`) and DOT
  direct-to-main pushes (hk-l3gs/hk-wths) land without `Reviewed-By:`/`Review-Verdict:` trailers,
  unlike the daemon review-loop merge path. NOT a regression of the normal path (hk-jeby's real
  merge `397c7f15` HAS trailers). Daemon lane → crew:paul, digest-only. P3.
- **N5 — Periodic reconciliation emits no completion event** — 3 in-window hourly
  `reconciliation_started` (`019ed859` 01:29, `019ed890` 02:29, `019ed8c7` 03:29) had NO matching
  `reconciliation_completed`; only the post-restart startup recon (`019ed8dc`) emitted both.
  Instrumentation gap or a hang — needs a daemon-lane look (NOT declared a hang). crew:paul,
  digest-only. P3.

## Still-open / recurring (no new bead)

- **F59** (paused-queue persist) — RECURRING-nominal: 4 in-window `group_failure` pauses, all
  recovered; only `main`/hk-tagp paused live (separate, unrelated bead).
- **F45** (keeper warn near threshold) — feeds hk-gffc; new TA1 band live.
- **hk-guez** (merge-aware cache-reaper guard) — OPEN, paul's lane (proper fix for Incident A).

---

> **Window:** line-anchored from iter-9 high-water `019ed834` → 566 events, lines 37437–38002,
> 2026-06-18T00:49:40Z→03:56:46Z. Adversarial-verifier pass overruled slice-6's F49-regression
> (recharacterized N4), recon-hung (recharacterized N5), and queues-still-paused (refuted) claims.
> high-water: 019ed8df-cdc9-7320-abb4-e40cc8206650  (2026-06-18T03:56:46Z agent_presence — logmine's own comms-join, last line of the frozen iter-10 window; next run resolves THIS event_id to its line and slices forward, per F41a)
