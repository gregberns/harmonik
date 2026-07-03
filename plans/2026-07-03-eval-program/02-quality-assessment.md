# Cross-Model Quality Assessment — Design

**Date:** 2026-07-03. **Status:** design-only (no code, no beads).
**Builds on:** `plans/2026-07-02-eval-harness/DESIGN.md` — the `judge` node (agentic reviewer,
`model="claude-opus-4-8"`, non-gating, writes `.harmonik/review.json` → `preferred_label` + notes,
every path → `record-pass`). That design left the rubric itself as open decision **O4** ("fixed judge
= opus, held constant; confirm token cost / opt-in label"). This doc **fills O4**: it specifies the
rubric, the judge design, aggregation, and guardrails, changing **nothing** in the harness/DOT plumbing.

## Why quality (beyond pass/fail)

The deterministic `grade` node gives one bit: does the task's own test pass. That bit does **not**
distinguish a correct-but-sloppy solution from a correct-and-idiomatic one, nor catch a solution that
passes the shipped test but is wrong on cases the test misses, nor flag dead code / over-reach. Two
models can both go green on `eval-lru-cache` while one is O(1) idiomatic and the other is an O(n) scan
with a leaked goroutine. The operator needs to see **that gap**. This is the subjective axis the opus
judge already exists to score — we just make it a **rigorous, comparable, blind** measurement.

**Deterministic-first ordering (unchanged principle from the harness):** objective signals lead;
the LLM-judge fills only the *subjective residue* the objective signals can't reach.

---

## Part 1 — The quality rubric

Five dimensions, each scored **1–5** (integers only). Total is **not** a bare sum — see §1.3.
Each dimension has an objective feeder (deterministic signal, computed *before* the judge runs and
handed to it as evidence) plus the judge's subjective read. This is the "deterministic-first,
LLM-judge for the gap" split the operator asked for.

| # | Dimension | What it measures | Objective feeder (deterministic, pre-computed) | 1 (worst) | 5 (best) |
|---|---|---|---|---|---|
| Q1 | **Correctness-beyond-tests** | Right on cases the shipped test *doesn't* cover (boundaries, nil, overflow, concurrency, error paths) | shipped test = PASS (gate — Q1 only scored if `pass==true`); optional **hidden-test** pass-count if a `hidden_test.go` is supplied | passes shipped test but visibly wrong on an obvious unlisted case | no reachable defect found under adversarial read; all obvious edge cases handled |
| Q2 | **Completeness** | Solves the *whole* task as specified, nothing stubbed/TODO/partial | `grep -c 'TODO\|panic("not implemented")\|FIXME'` in the diff | large parts stubbed or spec clauses ignored | every spec clause implemented, no stubs |
| Q3 | **Code quality / idiom** | Language-idiomatic, readable, correct naming/structure for Go | `gofmt -l` (unformatted?), `go vet`, cyclomatic complexity via `gocyclo` on changed funcs | non-idiomatic, unformatted, vet warnings, tangled | idiomatic, clean, passes vet+fmt, low complexity |
| Q4 | **Efficiency** | Appropriate algorithmic complexity + no obvious waste (redundant passes, needless allocs) | complexity annotation from the task's `expected_big_o` field (e.g. LRU expects O(1)); allocation smell markers | wrong complexity class for the task (e.g. O(n) LRU Get) | meets/beats the expected complexity, no obvious waste |
| Q5 | **No dead-code / no over-reach** | Only what the task needs — no unused symbols, no unrequested abstraction/config/features, no scope creep | diff **line count** vs a reference-solution line budget; `deadcode`/unused-symbol scan on the diff | large unused surface or unrequested framework/abstraction added | minimal, focused; every added line earns its place |

**Notes on the feeders:**
- Feeders are computed by a small **pre-judge shell step** (a new non-agentic node `metrics` inserted
  between `grade` and `judge`, or folded into the collector — see §Beads / O-Q3). Output is a compact
  `metrics.json` in the run worktree. The judge is *given* this file as evidence and must *cite* it.
- The judge does **not** re-derive the objective facts; it reads them and scores the **subjective**
  layer on top (e.g. Q3 feeder says "gofmt clean, gocyclo=4"; judge still down-scores if the code is
  correct-but-cryptic). This keeps the deterministic signal authoritative and the LLM confined to the gap.
- Q1 is **gated on `pass==true`**: a solution that fails the shipped test scores the whole rubric as
  `null` (a fail is a fail; we don't quality-grade broken code). `record-fail` therefore never invokes
  the judge — matching the existing DOT (`grade FAIL → record-fail`, judge only on the SUCCESS path).

### 1.1 The judge prompt (verbatim template)

The reviewer node's role string. `{{...}}` are injected per-run by the harness; the judge sees **only**
these — never the model name, harness, provider, or any run metadata that could reveal identity.

```
You are a strict, impartial code-quality judge. You are scoring ONE candidate solution to a
programming task. You do NOT know and MUST NOT guess which system or model produced it — judge only
the code in front of you. Verbosity is NOT quality; a shorter correct solution outscores a longer one.

## TASK (what was asked)
{{task_spec}}

## SHIPPED TEST RESULT
{{grade_result}}          # PASS or FAIL + the test command's stdout/stderr

## OBJECTIVE METRICS (already computed — treat as ground truth, cite them)
{{metrics_json}}          # gofmt/vet/gocyclo, TODO/stub count, diff line count vs budget,
                          # expected_big_o, hidden-test pass-count if any, unused-symbol scan

## THE DIFF UNDER JUDGEMENT (the only code you may consider)
{{unified_diff}}

## SCORE each dimension 1-5 (integers). For EACH, cite the specific line(s)/metric that justify it.
Q1 correctness-beyond-tests: right on cases the shipped test does NOT cover (boundaries/nil/overflow/
   error paths/concurrency). If you cannot construct a failing input, score toward 5.
Q2 completeness: whole task done, no stub/TODO/partial.
Q3 code-quality/idiom: idiomatic, readable; the metrics tell you fmt/vet/complexity — judge the rest.
Q4 efficiency: complexity class appropriate to the task (expected_big_o in metrics); no obvious waste.
Q5 no-dead-code/no-over-reach: minimal; no unused symbols, no unrequested abstraction/features/config.
   A solution that adds machinery the task did not ask for is PENALIZED here even if it works.

Output ONLY this JSON (schema for review.json):
{ "schema_version": 1,
  "verdict": "APPROVE",                     // always APPROVE — quality judge NEVER gates the eval
  "scores": { "Q1": n, "Q2": n, "Q3": n, "Q4": n, "Q5": n },
  "evidence": { "Q1": "…cite…", "Q2": "…", "Q3": "…", "Q4": "…", "Q5": "…" },
  "notes": "1-3 sentences, blind (no model/system speculation)" }
```

`verdict` stays `APPROVE` on every path so the existing `judge → record-pass` unconditional edge is
untouched; the *signal* lives in `scores`. The collector reads `scores` out of `review.json`.

### 1.2 Model-blind guarantee

The judge template above contains **no** model/harness/provider/run-id string. Enforced structurally:
the injected fields are `task_spec`, `grade_result`, `metrics_json`, `unified_diff` — the diff is the
**staged patch text only** (`git diff` of the run's commit), which carries no author-model marker.
The harness already runs the judge on a **fixed model** (opus) regardless of the model-under-test, so
the judge's *own* identity is constant. Blindness is a property of the **inputs**, and the inputs are
enumerable and model-free. (Guardrail G2 below adds a scrub check on the diff for stray self-identifying
comments like `// generated by <model>`.)

### 1.3 Per-run quality score

`quality_score = weighted mean of {Q1..Q5}`, weights **fixed and published** so every run is scored
identically. Proposed weights (correctness dominates; over-reach and efficiency are real but secondary):

```
Q1 correctness-beyond-tests  0.35
Q2 completeness              0.20
Q3 code-quality/idiom        0.20
Q4 efficiency                0.15
Q5 no-dead-code/no-over-reach 0.10
```

`quality_score ∈ [1.0, 5.0]`, `null` when `pass==false`. Stored on the per-run record from the harness
design as a new field `judge_scores` (the 5 raw ints) **plus** `judge_grade` (the weighted mean —
supersedes the harness's single 1-5 `judge_grade` placeholder). Raw ints are kept so weights can be
re-tuned post-hoc without re-running the judge.

---

## Part 2 — Comparability across the 6 model combos

The eval runs **6 model/harness combos** on the **same problem set**. For scores to be comparable, all
of the following are held **identical** across combos (only the model-under-test varies):

- **Same judge model** — always `claude-opus-4-8` (harness `model=` on the judge node, constant).
- **Same rubric + same weights** — §1 above, versioned as `rubric_version` on every record.
- **Same inputs to the judge** — task_spec, grade_result, metrics_json, unified_diff. The judge sees
  **only the diff + task + test result + objective metrics**, exactly as the operator specified.
- **Same temperature / no few-shot drift** — judge run with fixed decoding; the prompt is the only variable input.

**Score stability (the one real threat to comparability):** an LLM judge is noisy at ±1 on a 5-point
scale. Mitigation: score each diff **3× and take the median** per dimension (odd count → no ties), a
cheap ~3× judge-token cost that collapses most jitter. Record `judge_runs=3`. (Opt-out via a
`judge:single` label for pure speed passes, per O4.) For a same-problem cross-model comparison this
median-of-3 is what makes a 3.7-vs-4.3 gap *trustworthy* rather than noise.

### 2.1 Aggregation → per-model quality score

The collector (harness §1.5, "the only genuinely new logic") already joins per-run records from
events.jsonl. Extend it to compute, grouped by `model`:

```
per_model_quality = {
  pass_rate:            passes / attempts,                         # deterministic spine
  mean_quality:        mean(judge_grade over PASSING runs),        # subjective, PASS-only
  quality_by_dim:      mean(Qk) for k in 1..5, PASS-only,          # where the gap lives
  quality_by_difficulty: mean(judge_grade) per {trivial,simple,medium,harder},
  n:                   attempts, n_pass: passes
}
```

`mean_quality` is computed **over passing runs only** so it measures "when it works, how good" — kept
orthogonal to `pass_rate` ("how often it works"). Reporting both prevents a model that fails often but
is pretty-when-it-passes from looking better than a reliable-but-plain one.

### 2.2 Cross-model comparison table

`harmonik eval report` (harness's optional `jq`-aggregation, extended) emits per problem-set:

```
model                 pass_rate  quality  Q1   Q2   Q3   Q4   Q5   trivial simple medium harder
opus (baseline)         12/12     4.6    4.7  4.9  4.6  4.4  4.5    5.0    4.8    4.6    4.3
sonnet                  12/12     4.3    4.4  4.7  4.3  4.1  4.2    5.0    4.6    4.2    3.9
qwen3-coder (ornith)    10/12     3.8    3.6  4.3  3.9  3.5  3.9    4.8    4.1    3.6    3.1
…                        …         …      …    …    …    …    …      …      …      …      …
```

The **per-dimension columns are the payload**: two models with the same `pass_rate` and close
`quality` can still split hard on Q4 (efficiency) or Q5 (over-reach) — that split is exactly the
quality gap the operator is hunting, invisible to pass/fail alone. **A per-problem drill-down**
(`--by-task`) shows where a cheaper model's quality collapses (usually `harder` + Q1/Q4), which is
*also* the router training signal from the harness design.

---

## Part 3 — Failure modes & mitigations

| # | Failure mode | Why it corrupts the comparison | Mitigation |
|---|---|---|---|
| **G1 — verbosity bias** | LLM judges reliably over-reward longer/more-commented solutions; a verbose model wins on style points it didn't earn | **Q5 penalizes over-reach directly**; feeder = diff line count vs a per-task **reference line budget**; judge prompt states "verbosity is NOT quality; shorter correct outscores longer." Report Q5 separately so bias is visible. |
| **G2 — self-family favoritism** | Opus judge may favor Claude-family diffs (self-preference is a documented LLM-judge bias); baseline model = judge family | **Blindness (§1.2)**: judge never sees the model. **G2 scrub**: strip/flag self-identifying strings in the diff. **Calibration probe**: periodically feed the judge a *known-good* reference solution unlabeled — its score is the yardstick; if opus's own diffs systematically beat an equally-correct non-Claude diff on Q1/Q3 with no metric backing, that's detectable drift. Optionally cross-check a sample with a **second judge model** (different family) on the same diffs; large systematic per-family deltas → investigate. |
| **G3 — tests-pass-but-wrong** | Shipped test is thin; a wrong solution goes green and inherits a high pass + a high Q1 the judge can't refute from a passing test | **Q1 is adversarial**: judge must *try to construct* a failing input and cite it; "cannot construct" is required to reach 5. **Hidden tests**: optional `hidden_test.go` (not shown to implementer, run in `metrics`) gives an *objective* beyond-the-shipped-test signal feeding Q1. Strengthen shipped tests over time (harness §2 already picks edge-heavy cases). |
| **G4 — judge noise** | ±1 jitter makes a real cross-model gap indistinguishable from randomness | **Median-of-3** (§2). Report is comparative on the *same* problem set, so correlated noise partly cancels. Keep raw per-run scores to compute variance and flag low-confidence rows. |
| **G5 — metric gaming** | Model deletes/edits the shipped test, or strips TODOs to fool Q2 feeder | Test restored read-only before `grade` (harness O1); feeders run on the **diff**, and a diff that touches the test file is flagged. Q5's unused-symbol scan is on committed code, not self-reported. |
| **G6 — rubric drift over time** | Re-tuning weights or prompt mid-program makes old and new runs incomparable | `rubric_version` + `weights` stamped on **every** record; a report only compares runs sharing a `rubric_version`. Re-tuning weights uses stored **raw** Qk ints — no re-judge needed. |

---

## Beads (proposed — not created)

- **Q1** — Author the rubric prompt + `review.json` schema-v1-with-scores; wire the judge node's role
  string (config/DOT only, no daemon change). *(fills harness O4)*
- **Q2** — `metrics` pre-judge step: gofmt/vet/gocyclo/TODO-count/diff-line-budget/unused-scan →
  `metrics.json`; decide node-vs-collector placement *(open: O-Q3 below)*.
- **Q3** — Extend the collector to read `judge_scores`, compute weighted `judge_grade`, stamp
  `rubric_version`/`weights`; median-of-3 aggregation.
- **Q4** — `harmonik eval report` per-model + per-dimension + per-difficulty table + `--by-task` drill-down.
- **Q5** — Guardrails: G2 diff-scrub, optional second-judge cross-check sample, optional `hidden_test.go`
  support in `metrics` for Q1.
- **Q6** — Per-task `expected_big_o` + `reference_line_budget` fields on the 12 curated task beads
  (feeders for Q4/Q5).

**Open decisions to confirm before build:**
- **O-Q1** — weights (§1.3): correctness-weighted 0.35/0.20/0.20/0.15/0.10. Confirm or re-tune.
- **O-Q2** — median-of-3 judge cost (~3× judge tokens/run) vs single-shot noise. Lean: 3× on the
  comparison pass, `judge:single` label for throwaway speed passes.
- **O-Q3** — `metrics` as a DOT node vs folded into the collector (mirrors harness O3: pure-collector
  avoids parallel-write footguns; a node makes evidence visible live). Lean: collector for v1.
- **O-Q4** — second-judge cross-check (G2): sample-only in v1, or every run? Lean: sample (cost).
