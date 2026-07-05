# eval-review-schema-v1 — `.harmonik/review.json` written by the judge node

**Status:** normative (wires harness O4 per plans/2026-07-03-eval-program/02-quality-assessment.md §1)
**Bead:** hk-eval-prog-quality-rubric-i05dc (WS3a)

The `judge` node in `eval-bead.dot` writes `.harmonik/review.json` after every successful grade
(grade FAIL routes to `close-fail` without invoking the judge — Q1 is therefore gated on
`pass==true` structurally). The collector (`harmonik eval collect`, WS3c) will read this file to
populate `judge_scores` on the per-run record.

---

## Schema (version 1)

```json
{
  "schema_version": 1,
  "verdict": "APPROVE",
  "scores": {
    "Q1": 4,
    "Q2": 5,
    "Q3": 3,
    "Q4": 4,
    "Q5": 5
  },
  "evidence": {
    "Q1": "No reachable defect found; nil input correctly returns default.",
    "Q2": "All spec clauses implemented; no TODOs.",
    "Q3": "gofmt clean, go vet clean, gocyclo=2; naming idiomatic.",
    "Q4": "O(1) Get/Put per expected_big_o; no redundant passes.",
    "Q5": "87 added lines vs 90-line budget; no unrequested exports."
  },
  "notes": "Correct and idiomatic; minor: no input validation on negative capacity."
}
```

### Field definitions

| Field | Type | Required | Notes |
|---|---|---|---|
| `schema_version` | integer | yes | Always `1` for this schema. |
| `verdict` | string | yes | Always `"APPROVE"` — the quality judge NEVER gates the eval. |
| `scores` | object | yes | Five integer fields Q1–Q5, each in `[1, 5]`. |
| `scores.Q1` | integer 1–5 | yes | Correctness-beyond-tests (see rubric below). |
| `scores.Q2` | integer 1–5 | yes | Completeness. |
| `scores.Q3` | integer 1–5 | yes | Code quality / idiom. |
| `scores.Q4` | integer 1–5 | yes | Efficiency. |
| `scores.Q5` | integer 1–5 | yes | No dead-code / no over-reach. |
| `evidence` | object | yes | One citation string per Q-dimension (Q1–Q5). |
| `notes` | string | yes | 1–3 sentences, model-blind (no speculation about which system produced the code). |

---

## Rubric (Q1–Q5)

### Q1 — Correctness-beyond-tests (weight 0.35)

Does the solution handle cases the shipped test does **not** cover: boundary values, nil inputs,
overflow, error paths, concurrency hazards?

- **1** — fails on at least one obvious unlisted case the reviewer can construct.
- **3** — handles most edge cases; at least one subtle gap exists.
- **5** — no reachable defect found under adversarial read; all obvious edge cases handled.

**Gate:** Q1 is only scored when `grade=PASS` (i.e. the deterministic check passed). A failing
solution never reaches the judge — `close-fail` is the terminal node for grade failures.

### Q2 — Completeness (weight 0.20)

Does the solution implement the **whole task** as specified, with no stubs, TODOs, FIXMEs, or
silently ignored spec clauses?

- **1** — large portions stubbed or spec clauses ignored.
- **3** — minor omissions or one small spec clause missed.
- **5** — every spec clause implemented; no stubs.

**Objective feeder:** `grep -c 'TODO\|FIXME\|panic("not implemented")'` count from `metrics.json`.

### Q3 — Code quality / idiom (weight 0.20)

Is the code idiomatic, readable, and structurally clean for the target language (Go)?

- **1** — non-idiomatic, unformatted, vet warnings, tangled control flow.
- **3** — mostly idiomatic with some style gaps.
- **5** — idiomatic, clean, passes gofmt/vet, low cyclomatic complexity.

**Objective feeders:** `gofmt -l` result, `go vet` output, `gocyclo` score from `metrics.json`
(treat as ground truth; the judge scores the subjective layer on top).

### Q4 — Efficiency (weight 0.15)

Is the algorithmic complexity appropriate to the task? No obvious waste (redundant passes,
needless allocations, unnecessary synchronisation)?

- **1** — wrong complexity class for the task (e.g. O(n) Get on an LRU that requires O(1)).
- **3** — correct complexity class; minor inefficiencies.
- **5** — meets or beats the expected complexity; no obvious waste.

**Objective feeder:** `expected_big_o` annotation from `metrics.json` (task-level hint, e.g.
`O(1)` for LRU Get/Put). If absent, judge infers from the task spec.

### Q5 — No dead-code / no over-reach (weight 0.10)

Is the solution **minimal**? Every added line earns its place. No unused symbols, no
unrequested abstraction, config, or features.

- **1** — large unused surface or unrequested framework/abstraction added.
- **3** — minor over-engineering or a few unused helpers.
- **5** — focused; no unused symbols; no scope creep.

**Objective feeders:** diff line count vs `reference_line_budget` from `metrics.json`; unused-
symbol scan result. A solution that adds machinery the task did not ask for is **penalised** on
Q5 even if it compiles and the test passes.

---

## Weighted quality score

```
quality_score = 0.35*Q1 + 0.20*Q2 + 0.20*Q3 + 0.15*Q4 + 0.10*Q5
```

`quality_score ∈ [1.0, 5.0]`. Weights are `rubric_version=1` and are stamped on every per-run
record so they can be re-tuned post-hoc without re-running the judge (raw Qk integers are stored).

---

## Model-blind guarantee

The judge prompt (embedded in `eval-bead.dot` judge node `role=`) contains **no** model/harness/
provider/run-id string. The injected fields are task spec, grade result (PASS), objective metrics,
and the unified diff — none of which carry a model identity marker. The judge itself runs on a
**fixed model** (`claude-opus-4-8`) regardless of the model-under-test, so the judge's own
identity is constant across all eval runs.
