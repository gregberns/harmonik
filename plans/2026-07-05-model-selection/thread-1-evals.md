# Thread 1 — Evals: Per-Model Quality per Task Category

**Status:** in progress — **building on the existing eval program, not restarting** · **Reports to:** admiral

## 1.0 There is already a real eval program — extend it

Two kerf works feed this and much is LANDED:
- **`eval-harness`** (2026-07-02) — single-model grading harness. `plans/2026-07-02-eval-harness/DESIGN.md`.
- **`eval-program`** (2026-07-03) — cross-model superset. `plans/2026-07-03-eval-program/01..05*.md`.

Beads carry `codename:eval-harness` / `codename:eval-program`; curated tasks carry `codename:eval`.
**Do NOT design a new harness** — the thread-1 job is to (a) document what exists, (b) drive its open
critical path to a working cross-model report, (c) feed its per-category quality numbers to threads 3/4/5.

## 1.1 Harness architecture (rides existing DOT seams — no daemon change)

A task = a bead labeled `workflow:dot` + `dot:eval-bead` + `harness:<pi|claude-code|codex>`. DOT topology
(`eval-bead.dot`):

```
start → implement  (agentic, MODEL-UNDER-TEST, commit required, NO fix-loop = one-shot capability)
      → grade      (non-agentic shell: restore committed *_test.go read-only, run deterministic check;
                    exit0 → judge, exit≠0 → record-fail; scripts/eval-grade.sh)
      → judge      (agentic reviewer PINNED model=claude-opus-4-8, 5-dim rubric → .harmonik/review.json,
                    UNCONDITIONAL/non-gating — verdict always APPROVE, signal is in scores)
      → record-pass / record-fail (terminal)
```

Deliberate: **no fix-loop** (measures one-shot capability), grade is deterministic-first, judge never gates.
Routing resolvers: `internal/daemon/moderesolve.go`, `harnessresolve.go:53` (`resolveHarness`, 4-tier),
`dot_cascade.go` (per-node `model=`).

## 1.2 Task corpus & categories

- **Location:** `evaltasks/<task_id>/` — self-contained new-file + committed `*_test.go` (tamper-proof:
  model writes only the solution; test restored read-only before grade). Check kinds: `unit-test`, `grep-zero`.
- **This is a CODING eval** — categories are capability/difficulty, not the full orchestration taxonomy:
  implement/greenfield, in-place refactor (`eval-refactor-storage`, `eval-config-thread`), bug-fix
  (`eval-bugfix-rate-limiter`), CLI/stateful (`eval-cli-kv`), concurrency under `-race` (`eval-lru-ttl`),
  parser (`eval-expr-eval`), algorithm (`eval-topo-sort`, `eval-interval-schedule`). **No plan/triage/review
  category** — judge scores review-quality but there's no plan/triage task type.
- **14-task set** = 8 landed easy `codename:eval` beads + 6 hard dirs. `eval-cli-kv` (`hk-p778p`) is **P1 OPEN**.

> ⚠️ **Gap vs the mission's premise.** The mission asks "which model per category: implement / review / plan
> / triage / research / mechanical-edit." The existing eval program only measures **coding-implementation**
> categories. To route *review/plan/triage/research* work we'd need either (a) new eval task types, or (b)
> historical production telemetry (thread 5 Phase 3). **Surface to admiral: is widening the eval taxonomy
> beyond coding in scope, or do we route non-coding categories on production telemetry only?**

## 1.3 Metrics collected (per model, per task)

- **Collector EH1** (`cmd/harmonik/eval_cmd.go` = `harmonik eval collect`, bead CLOSED): reads
  `.harmonik/events/events.jsonl`, groups by `run_id`, emits `.harmonik/eval-results.jsonl`.
  Record adds `task_id, difficulty, pass, check_kind, judge_grade, judge_scores{Q1..Q5}, judge_notes,`
  `implement_time_s, rubric_version, weights`.
- **Always-on cost/token** (`internal/sessiondata`, `.harmonik/session-data.jsonl`): see thread 2.
- **Judge rubric** (`docs/eval-review-schema-v1.md`, normative): Q1 correctness-beyond-tests (.35),
  Q2 completeness (.20), Q3 idiom (.20), Q4 efficiency (.15), Q5 no-over-reach (.10). Objective feeders
  (`harmonik eval metrics` → `.harmonik/metrics.json`): gofmt/vet/gocyclo, TODO/stub, diff-line-budget,
  `expected_big_o`, hidden-test pass, deadcode. **6 anti-gaming guardrails** (G1 verbosity, G2 self-family
  favoritism — model-blind judge, G3 tests-pass-but-wrong, G4 median-of-3 judge noise, G5 metric-gaming,
  G6 rubric drift) in `02-quality-assessment.md §3`.

## 1.4 DONE vs OPEN — the critical path to a working cross-model report

**DONE:** WS1 plumbing (model_selected event, sessiondata hook, codex+pi token extraction, per-node
attribution) · EH1 collector · WS3a rubric · WS3b objective feeders.

**Open critical path** (a cross-model quality-vs-cost report needs these):
1. **WS1f** `hk-eval-prog-repoint-eh1-8qy8s` — re-point collector to consume `session-data.jsonl`.
2. **WS3c** `hk-eval-prog-quality-aggregation-k2180` — judge aggregation (median-of-3, weighted grade).
3. **WS3d** `hk-eval-prog-eval-report-7f1lp` — `harmonik eval report` (per-model × per-dim × per-difficulty).
4. **WS2a/WS2b** `hk-eval-prog-matrix-submitter-vsdzr` / `-openrouter-minimax-eb472` — actually run the model matrix.
5. **EH2–EH5** (`hk-eval-harness-{dot,routing,report,acceptance}-*`) — run the DOT end-to-end.

**Blocked:** WS5 DGX (b/c–e) — SSH to `dgx.local` is down; ornith/qwen3-coder load-ramp gated on SSH restore.

## 1.5 Models already wired for eval runs

- **Claude Sonnet + Opus** — per-DOT-node `model=` (`dot_cascade.go`→`claudelaunchspec.go`); two combos run
  **concurrently** under one daemon. Judge pinned to `claude-opus-4-8` (model-blind on the work).
- **Pi** — daemon-global `harnesses.pi.*`; one model at a time, config-swap + restart to switch:
  OpenRouter/MiniMax (open-weight), DGX vLLM ornith/qwen3-coder (`base_url` + `api:openai-completions`,
  `Ornith-1.0-35B`, currently SSH-blocked).
- **Codex** — model not harmonik-controlled (`$CODEX_HOME/config.toml`); tokens now extracted.

## Proposal to admiral (shannon proposes; admiral dispatches)
Drive the **WS1f → WS3c → WS3d** critical path to get a first real cross-model report on the 14-task set
(Claude Opus vs Sonnet vs Pi/MiniMax). That report *is* the y-axis for threads 3/4/5. It's already-scoped
open beads — no new design. **Decision for admiral:** (a) prioritize this critical path now? (b) widen eval
taxonomy beyond coding (§1.2 gap)?

## Open items
- [ ] Admiral: prioritize WS1f→WS3c→WS3d? widen taxonomy beyond coding?
- [ ] Once report runs: extract per-category (model → pass-rate, judge-grade) table → thread 3/4.
