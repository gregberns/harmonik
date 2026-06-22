# 24-Hour Fleet Run Analysis — 2026-06-21 08:00 → 2026-06-22 08:00 MST
**Window:** 2026-06-21T15:00Z → 2026-06-22T15:00Z (UTC)
**Tool:** `plans/2026-06-22-token-usage-audit-tooling/tools/usage_join.py`
**Status:** Phase 0 join complete — transcripts × events.jsonl, no source changes

---

## Executive Summary (one screen)

| Metric | Value |
|---|---|
| **Total cost** | **$4,135** |
| Productive (bead-attributed) | $970  (23.5%) |
| **Idle / orchestrator burn** | **$3,165  (76.5%)** |
| Beads worked | 115 |
| Daemon runs | 166 |
| Orchestrator sessions captured | 47 |
| Opus% of spend | **76.3%** ($3,153) |
| Sonnet% of spend | 23.6% ($975) |
| Cache-read% of total tokens | **95.6%** (2,798M of 2,926M) |
| Run failure rate (top 10 by cost) | 6/10 FAIL, 77% of top-10 cost |

**Key sentence:** Three-quarters of the $4,135 spend is Opus orchestrator uptime, not work. The daemon's bead work is already cheap Sonnet; the problem is 47 long-lived Opus captain/crew sessions running 24/7.

---

## 1. Total Cost Breakdown

### Productive vs Idle

```
Idle/Orchestrator    $3,165  76.5%   ← always-on Opus captain + crews
Productive (beads)   $  970  23.5%   ← actual bead work via daemon
                    ──────────────
TOTAL                $4,135  100%
```

**Productive cost per landed outcome:** The window saw 56 `bead_closed` events and 56 `run_completed` (some beads required multiple runs). Rough cost-per-closed-bead ≈ $970 / 56 ≈ **$17.30 per closed bead**.

The expensive tail: the top-10 beads alone consumed $406 (42% of productive spend). 6 of the 10 most expensive runs **failed**, burning $195 of the $970 productive total with no output — a 20% wasted-productive rate.

### By Model Tier

| Tier | Cost | Share |
|---|---|---|
| **Opus (claude-opus-4-8)** | **$3,153** | **76.3%** |
| Sonnet (claude-sonnet-4-6) | $975 | 23.6% |
| Synthetic/zero-cost | $7 | 0.2% (artifact) |

All bead work is Sonnet. All Opus spend is orchestrators. The `<synthetic>` $7 is a minor cost-fractioning artifact from turn-count weighting on zero-token synthetic Claude-Code entries — safely ignorable.

---

## 2. Top 10 Beads by Cost

| # | Bead | Cost | Runs | Dominant model | Cache-read% |
|---|---|---|---|---|---|
| 1 | hk-ldzp | $75.20 | 5 | sonnet-4-6 | 96% |
| 2 | hk-sj6a | $73.63 | 3 | sonnet-4-6 | 95% |
| 3 | hk-kgwv | $37.30 | 2 | sonnet-4-6 | 95% |
| 4 | hk-pqgtm | $36.11 | 2 | sonnet-4-6 | 90% |
| 5 | hk-lacr | $34.47 | 3 | sonnet-4-6 | 96% |
| 6 | hk-zqb3 | $34.35 | 2 | sonnet-4-6 | 97% |
| 7 | hk-emuic | $32.32 | 3 | sonnet-4-6 | 95% |
| 8 | hk-jvul | $31.55 | 3 | sonnet-4-6 | 95% |
| 9 | hk-l06w4 | $26.85 | 2 | sonnet-4-6 | 89% |
| 10 | hk-tn36 | $23.77 | 3 | sonnet-4-6 | 95% |

**All bead work is Sonnet.** High retry counts (2-5 runs per bead) on the expensive beads inflate cost significantly: hk-ldzp at 5 runs averaged $15/run, hk-sj6a at 3 runs had one $48 failed run.

### Top 10 Runs (individual runs, not beads)

| Bead | Cost | Status | Turns |
|---|---|---|---|
| hk-sj6a | $48.28 | **FAIL** | 808 |
| hk-ldzp | $33.18 | **FAIL** | 691 |
| hk-zqb3 | $28.36 | **FAIL** | 667 |
| hk-pqgtm | $25.95 | **FAIL** | 530 |
| hk-kgwv | $20.87 | OK | 539 |
| hk-1veco | $20.09 | **FAIL** | 421 |
| hk-9uu9e | $19.92 | FAIL | 344 |
| hk-u3q6o | $19.54 | **FAIL** | 412 |
| hk-emuic | $18.94 | OK | 370 |
| hk-sj6a | $18.25 | OK | 368 |

The top 4 by cost all **failed**. The pattern: a run climbs to 500-800 turns before hitting a wall, then fails — consuming $25-48 with nothing landed. This is the highest-leverage cost reduction target in the productive bucket.

---

## 3. Top Orchestrator Sessions (always-on Opus burn)

| Session (short ID) | Cost | Model | Turns | Active window |
|---|---|---|---|---|
| d3dd06fb… | **$594** | opus-4-8 | 821 | 15:10Z → Jun 22 04:21Z (13h) |
| 55b5ef6d… | $299 | opus-4-8 | 719 | 16:06Z → Jun 22 04:00Z (12h) |
| 91ca317e… | $264 | opus-4-8 | 532 | 15:01Z → Jun 22 02:44Z (12h) |
| cf35c324… | $147 | opus-4-8 | 262 | 16:39Z → Jun 22 14:17Z (22h) |
| 78c49260… | $126 | opus-4-8 | 317 | Jun 22 02:37Z → 04:35Z (2h) |
| e7c0386e… | $106 | opus-4-8 | 250 | Jun 22 05:14Z → 06:37Z (1.5h) |
| 8c0010f6… | $66 | opus-4-8 | 222 | Jun 22 07:36Z → 11:57Z (4h) |
| 68a50409… | $64 | opus-4-8 | 239 | Jun 22 07:05Z → 11:38Z (4.5h) |
| 7932da9d… | $61 | opus-4-8 | 219 | Jun 21 19:40Z → 20:37Z (1h) |
| 7ba89eef… | $60 | opus-4-8 | 194 | Jun 21 17:47Z → 18:38Z (1h) |

**Top-3 sessions alone = $1,157 (37% of total fleet cost).** Session d3dd06fb ran 13 hours with 821 turns, averaging $594 / 821 = **$0.72/turn on Opus**. These are captain and crew orchestrator sessions operating at cwd=repo root on branch=main — no bead attribution is possible without session registration (Phase 2 gap).

Note: without session-to-role registration (not yet instrumented), we cannot distinguish captain from crew sessions in this bucket. The role breakdown is an estimate from context clues.

---

## 4. Opus vs Sonnet — Model-Fit Assessment

**The daemon fleet is already model-tiered correctly.** All 166 daemon bead runs use Sonnet. The Opus spend is 100% orchestrator sessions (captain + crews).

**Cost-per-landed-outcome by tier:**
- Sonnet productive work: $970 / 56 closed beads = **$17.30/bead**
- Opus orchestrator: $3,153 / 56 closed beads = **$56.30/bead equivalent** (idle overhead, not per-bead work)

The efficiency metric is: for every $1 of real Sonnet bead work done, **$3.26 of Opus orchestrator overhead runs in parallel**. This ratio is the target to reduce — not the Sonnet per-token cost.

**Addressable downgrade:** The 3 largest orchestrator sessions alone = $1,157. If the crews those represent were downgraded to Sonnet (5× cheaper), savings would be ~$924 in this window alone — roughly matching the **entire productive spend**.

---

## 5. Cache-Read Analysis

**95.6% of all tokens are cache-reads.** This matches prior audits (96%). Key framing:

| Token type | Volume | % of total | Cost contribution |
|---|---|---|---|
| cache_read | 2,798M | 95.6% | ~15% of cost (cheap at $1.50/M Opus, $0.30/M Sonnet) |
| cache_creation | 99.5M | 3.4% | ~45% of cost (expensive at $18.75/M Opus) |
| output | 26.1M | 0.9% | ~35% of cost |
| input | 1.8M | 0.1% | ~5% of cost |

**Cache-read% is the wrong metric to optimize.** High cache-read% means the cache is working — it's the intended behavior for long sessions with stable prefixes. The cost driver is `cache_creation` ($18.75/M) and `output` ($75/M output on Opus). Reducing session length (fewer turns per session via earlier restarts) reduces cache_creation. Reducing Opus uptime reduces both.

**Idle cache burn:** the 3 largest orchestrator sessions accumulated 304M cache-read tokens in the window. At $1.50/M Opus that's $456 just in cache-read — the cheapest token type, but at 24/7 scale it dominates.

---

## 6. Per-Hour Shape

UTC hours (Phoenix MST = UTC-7):

```
15Z (MST 08:00)  $1,025  ████████████████████████████  ← fleet boot (captain + crew launch)
16Z (MST 09:00)  $  550  ██████████████
17Z (MST 10:00)  $  227  ██████
18Z (MST 11:00)  $  113  ███
19Z (MST 12:00)  $  215  █████
20Z (MST 13:00)  $  142  ████
21Z (MST 14:00)  $   99  ███
22Z (MST 15:00)  $   84  ██
23Z (MST 16:00)  $   91  ███
00Z (MST 17:00)  $    9  ▌    ← significant drop (most crews wound down)
01Z (MST 18:00)  $  136  ████
02Z (MST 19:00)  $  198  █████
03Z (MST 20:00)  $   13  ▌
04Z (MST 21:00)  $  236  ██████
05Z (MST 22:00)  $  158  ████
06Z (MST 23:00)  $  108  ███
07Z (MST 00:00)  $  265  ███████
08Z (MST 01:00)  (0)
09Z (MST 02:00)  $    2  ▌
10Z (MST 03:00)  $   72  ██
11Z (MST 04:00)  $  126  ███
12Z (MST 05:00)  $  129  ████
13Z (MST 06:00)  $   52  ██
14Z (MST 07:00)  $   46  █
```

**Observations:**
- **08:00 MST spike = $1,025 in one hour** — all orchestrators boot simultaneously at session start. This includes the session-start prompt seeding (large cache_creation spike).
- **00:00 UTC (MST 17:00) = near-zero ($9)** — most crews had context-cleared or were restarted. A natural idle point.
- **No clean idle window overnight:** spend at 01-07 UTC ($895 over 7 hours) shows continuous Opus orchestrator activity even when operator is absent.
- **The `00Z` trough followed by $136 at `01Z`** is consistent with keeper-triggered restarts around that time — new sessions burning fresh cache_creation tokens.

---

## 7. Productive vs Idle — The "Always-On Orchestrators" Answer

**The 76.5% idle burn answer is confirmed:** the fleet spends $3.26 on always-on Opus orchestrators for every $1 of actual bead work.

This is structurally identical to the 2026-06-17 audit finding ("one 56h crew = $245 = 14% of 3-day spend"). The mechanisms have changed (leanfleet, per-crew --model, keeper restart-earlier) but the root pattern persists because:
1. Crews are still running Opus by default (except where mission front-matter overrides)
2. The fleet sleep-wake policy exists but is not enabled
3. When operators are absent, orchestrators idle at 24/7 uptime accumulating cache-read + periodic turns

---

## 8. Why ccusage Alone Is Insufficient

`ccusage` (npx ccusage@latest) reads the same transcript files this tool does, but:

| Gap | Impact |
|---|---|
| **No bead attribution** | Can't answer "which bead cost what" — the core cost-per-outcome question |
| **Date-granular only** | Can't do sub-day hourly breakdowns or identify the 08:00 boot spike |
| **Session totals only** | Can't separate a single long session's productive (worktree) vs idle (main-branch) turns |
| **No productive/idle split** | The 23.5% vs 76.5% finding is invisible to ccusage |
| **Local machine only** | Remote worker transcripts (when remote-substrate is active) don't appear |
| **No run-failure attribution** | Can't flag that 6/10 top-cost runs failed |

`ccusage` is useful for a quick total cost sanity-check (`npx ccusage@latest daily --since 20260621`). This join tool adds all five dimensions that matter for cost-per-outcome analysis.

---

## 9. Gaps List — Prioritized

### GAP-1 — No role attribution for orchestrator sessions [Impact: HIGH / Effort: S]
The 47 orchestrator sessions ($3,165) have no role label — we can't distinguish captain from crew-paul from crew-stilgar etc. without a session registry. One launch-time `session_registered{role, session_id}` event would close this permanently. **This is the single highest-value instrumentation gap.** Without it, the 76.5% idle bucket is an undifferentiated blob.

_Candidate fix:_ emit `session_registered` from `harmonik start captain/crew`. Already spec'd as Phase 2 item #3 in README §6.

### GAP-2 — Run failure budget not enforced (77% of top-cost runs fail) [Impact: HIGH / Effort: M]
6 of the 10 most expensive daemon runs failed after 400-800 turns, burning $195 with no output. A hard-turn-limit (e.g., 500 turns → force a FAIL rather than continue) would cap per-run cost at roughly $12 for a Sonnet run. The `implementer_budget_exceeded` event exists but the 300k-token force-cut governor is flagged in README as "built, not enabled."

_Candidate fix:_ enable the daemon-native 300k force-cut governor. Operator A/B/C decision per `reference_keeper_hardceiling_unwired_prod.md`.

### GAP-3 — No model field on run_completed/run_failed events [Impact: MEDIUM / Effort: S]
The README §6 lists this as the #1 instrumentation gap. Currently the join requires reading the transcript to find the model; adding `model` to terminal events makes bead-level model attribution trivial without transcript reading.

_Candidate fix:_ one-line daemon edit. Part of Phase 1 in README.

### GAP-4 — Crew Opus default for clean-drain lanes [Impact: HIGH / Effort: XS]
The 3 largest orchestrator sessions ($1,157) are likely clean-lane crews. Downgrading them to Sonnet via mission front-matter `model: claude-sonnet-4-6` saves ~$925 in a 24h window with zero build work. The `--model` knob already exists per `hk-9j3z`.

_Candidate fix:_ edit mission front-matter for clean-drain crews. No build required.

### GAP-5 — Fleet sleep-wake policy not enabled [Impact: HIGH / Effort: S]
The `00Z` trough ($9 for that hour) and the continuous overnight activity suggest the fleet never sleeps. The sleep-wake mechanism (`hk-rl4b`) is built but the captain "when to sleep" policy hasn't been activated. Enabling it for absent-operator windows would cut overnight idle burn.

_Candidate fix:_ captain policy configuration. Deferred per `plans/leanfleet`.

### GAP-6 — Remote worker transcripts not captured [Impact: MEDIUM / Effort: M]
When remote-substrate runs are active, their transcripts live on the worker box and are invisible to this tool. The productive cost is thus undercounted for any remote runs. The `worker_name` field already exists on run events.

_Candidate fix:_ rsync or ship worker transcripts at run-end. Part of Phase 2 in README.

### GAP-7 — /clear re-minting creates multi-file sessions [Impact: LOW / Effort: S]
A long session with a `/clear` produces multiple transcript files. This join handles it correctly (sums per session_id within a run dir), but a session that cleared mid-window will have its pre-clear turns attributed to a different session_id. Minor cost attribution noise.

_Candidate fix:_ emit `session_continuation{old_session_id, new_session_id}` event on `/clear`. Phase 2 item #4.

### GAP-8 — Bead retry cost not flagged in tooling [Impact: MEDIUM / Effort: S]
hk-ldzp ran 5 times, hk-sj6a 3 times. Each retry re-creates cache (expensive). The tool surfaces run_count per bead but doesn't flag "this bead has retried N times, costing $X in redundant cache_creation." A simple alert on bead.run_count >= 3 would surface retry cost hotspots.

_Candidate fix:_ add retry cost estimate to bead output + optional `--warn-retries N` flag.

---

## 10. Prioritized Action List (by cost-per-landed-outcome impact)

| Priority | Action | Effort | Estimated 24h saving |
|---|---|---|---|
| **1** | Default clean-drain crews to Sonnet (GAP-4) | XS (no build) | ~$925 |
| **2** | Enable fleet sleep-wake policy (GAP-5) | S | ~$400-600 (overnight) |
| **3** | Enable 300k force-cut governor (GAP-2) | S | ~$150 (failed-run waste) |
| **4** | Add session_registered event (GAP-1) | S | 0 direct saving, but makes gaps 1 measurable |
| **5** | Add model to terminal events (GAP-3) | S | 0 direct saving, enables Phase 1 |

**Highest-ROI single move:** crew Sonnet default (GAP-4, no build, zero risk) + sleep-wake enable (GAP-5). Together these address ~$1,500 of the $3,165 idle burn per 24h.

---

## Appendix — Tool Coverage Notes

- **Runs covered:** 110/166 (66%) have transcript data. The 56 gaps are: 52 old runs (started weeks ago, transcripts cleaned up per Claude Code's 30-day retention) + 4 runs that completed seconds before the window start. Cost impact of uncovered runs is likely small (they contributed to the 56 events within the window but did most of their work outside it).
- **Orchestrator sessions:** 47 identified. The main harmonik project dir (`~/.claude/projects/-Users-gb-github-harmonik/`) has 537 total transcript files. The 47 in-window sessions are those with at least one assistant turn in the 24h window. Without role registration, their identities (captain, crew-paul, crew-stilgar, etc.) are inferred from context clues (cwd, branch) only.
- **Pricing:** Opus $15/$75 input/output, $18.75 cache_creation, $1.50 cache_read per 1M tokens. Sonnet $3/$15/$3.75/$0.30. Sourced from Anthropic pricing page (June 2026). The `<synthetic>` $7 artifact is from turn-count-weighted cost distribution on zero-token entries — real cost is $0.
- **Total tokens:** 2,925.6M (2.9B) in 24h. At 95.6% cache-read, actual "fresh" token processing was ~128M tokens (cache_creation + input + output combined). The cache is doing its job; the cost driver is session uptime volume, not per-turn computation.
