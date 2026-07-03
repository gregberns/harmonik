# eval-program — Problem Space

**Status:** design docs authored (2026-07-03); this work packages them into a bead set.

## Summary

The **eval-program** extends the existing single-model `eval-harness` work (the
`eval-bead.dot` + deterministic-grade harness + 8 `codename:eval` task beads + 5 EH build
beads) into a **cross-model evaluation program**: route the SAME curated problem set through
N `(harness, model)` combos, extract normalized per-run session-data/token records for ANY
harness (a general product feature, not eval-only), score subjective quality with a blind
opus judge, expand the problem set with 6 harder tasks, stand up the DGX-hosted ornith model
as a load-scaling target, and calibrate against external benchmarks (Terminal-Bench, Aider).

## Authoritative design docs (READ THESE FIRST — they are normative)

All under `plans/2026-07-03-eval-program/`:

1. `01-run-matrix-and-metrics.md` — the N-combo run matrix (routing seams as they actually
   are) + the always-on `sessiondata.Collect` post-run hook + per-harness token extraction.
   → WS1 (metrics-infra), WS2 (cross-model matrix).
2. `02-quality-assessment.md` — the Q1–Q5 rubric, blind fixed-opus median-of-3 judge,
   collector aggregation + `harmonik eval report` cross-model table, guardrails.
   → WS3 (quality).
3. `03-dgx-swap-and-monitoring.md` — DGX SSH restore (BLOCKED on operator pubkey),
   ornith↔qwen3-coder model-swap runbook, GPU-monitor verification. → WS5 (DGX).
4. `04-dgx-load-scaling.md` — concurrency ramp driver (dgx-load queue Workers 1→2→4→8→16),
   vLLM /metrics + nvidia-smi poller, stop-condition + max-slots write-up. → WS5 (DGX).
5. `05-problem-set-and-tools.md` — the 6 NEW hard tasks + external-benchmark research
   (Terminal-Bench, Aider-polyglot). → WS4 (problem-set), WS6 (tools).

Companion (do NOT duplicate — reference/depend on): `plans/2026-07-02-eval-harness/DESIGN.md`
and its work `eval-harness` (8 `codename:eval` task beads + EH1–EH5 build beads).

## Workstreams

- **WS1 metrics-infra** (`ws:metrics`, P1, foundational) — model on the log, general
  `sessiondata.Collect` hook, Codex/Pi token extraction, per-node attribution, re-point EH1.
- **WS2 cross-model matrix** (`ws:matrix`, P2) — N-sibling submitter + runbook, OpenRouter
  MiniMax profile, optional Tier-2 per-queue harness enabler.
- **WS3 quality** (`ws:quality`, P2) — rubric+schema, feeders, judge, aggregation+report,
  guardrails, per-task expected_big_o/line-budget.
- **WS4 problem-set** (`ws:problems`, P1) — author the 6 NEW hard tasks.
- **WS5 DGX** (`ws:dgx`, P2) — SSH restore (P1 blocker, operator), model-swap runbook,
  monitor verify, load-scaling ramp driver, box specs.
- **WS6 tools** (`ws:tools`, P3) — Terminal-Bench adoption, Aider-polyglot import.

## Cross-workstream dependencies

- **WS1 gates the metrics consumers of WS2/WS3** — the matrix's trustworthy per-row model
  and the quality collector both consume `session-data.jsonl` / the `model` field.
- **DGX-1 SSH restore gates most of WS5** — model-swap, monitor-verify, load-ramp, box-specs
  all need login; SSH is operator-owned (add pubkey).

## Non-goals

- No production code / no runs in this work — planning only (kerf work + beads).
- Do NOT re-author the 8 existing `codename:eval` tasks or the EH1–EH5 build beads.
- Landing Tier-2/Tier-3 harness routing is only a forward-looking enabler (P3 note), not v1.

## Success criteria

- One kerf work `eval-program` with the 5 docs referenced as design artifacts.
- Beads across all 6 workstreams, each labeled `codename:eval-program` + a `ws:*` sub-label,
  with priorities and task→task deps (no epic-parent deps), unassigned, nothing in_progress.
