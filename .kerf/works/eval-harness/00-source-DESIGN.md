# Model-Evaluation Harness — Design

**Date:** 2026-07-02. **Status:** design-only (no production code, no beads). Feeds a 3-agent
consensus review before build.

**Goal:** run a curated series of coding tasks through harmonik against a chosen model
(DGX-hosted *ornith* via the Pi harness, Claude baseline, or any Pi provider/model), and
**grade each solution deterministically** (task's own unit test / grep check → pass/fail) plus
capture **wall-time** from the event log, so we can compare models on **quality + speed** and
later train a **task→model router**. The per-run record schema *is* the router's training signal.

---

## Part 1 — The eval / grading harness

### 1.1 How the existing DOT system works (studied first, with citations)

**The embedded standard workflow** is `internal/daemon/standard-bead.dot`
(topology: `start → implement → commit_gate → review → close`; commit_gate is a shell node,
review is agentic, APPROVE is the sole inbound edge to `close`). Node/edge grammar is the
authoritative reference for the new DOT.

**Node types** (from `standard-bead.dot` + `specs/examples/tool-node.dot`):
- `type="agentic"` + `handler_ref="claude-implementer"` / `"claude-reviewer"` — launches a coding
  or review agent in the run's worktree. Optional `model="..."` attribute
  (`standard-bead.dot:120` sets `model="claude-opus-4-8"` on the review node).
- `type="non-agentic"` + `handler_ref="shell"` + `tool_command="..."` + `timeout="..."` — the
  in-process shell handler runs the command **in the run worktree**, maps exit-code → Outcome
  (`tool-node.dot:6-9`, `standard-bead.dot:104-111`). **exit 0 → `outcome.status=='SUCCESS'`;
  exit ≠ 0 → `FAIL` + `failure_class='deterministic'`; timeout → `FAIL` + `transient`**
  (`internal/daemon/dot_cascade.go:1833-1834`).
- `type="non-agentic"` + `handler_ref="noop"` — entry/terminal markers.

**Edge dialect** (D5 v1 — equality + `&&` only, NO `<`/`>`):
`condition="outcome.status == 'SUCCESS'"`, `condition="outcome.preferred_label == 'APPROVE'"`,
optional `traversal_cap="N"` to bound loops, and a **mandatory unconditional fallback edge that
must appear LAST** among a node's outgoing edges (WG-011 / D-edge-cascade-invariant,
`standard-bead.dot:170-172,190-192`).

**Reviewer verdict surfacing:** a `claude-reviewer` node writes `.harmonik/review.json`; its verdict
becomes `outcome.preferred_label` (APPROVE / REQUEST_CHANGES / BLOCK) which drives the cascade
(`internal/daemon/dot_cascade.go:18-24,1131-1132`). Cross-node combination (e.g. consensus AND)
must happen *inside* a consolidate agent because an edge sees only the current node's outcome
(`specs/examples/two-reviewer-consensus.dot:10-16`).

**Workflow selection — how a bead gets pointed at a custom DOT** (`internal/daemon/moderesolve.go`):
- `resolveWorkflowMode` (moderesolve.go:53) resolves the *mode* (single/review-loop/dot); a bead
  with `workflow:dot` label, or the tier-4 hard fallback, selects DOT mode.
- `resolveWorkflowRef` (moderesolve.go:145) resolves *which .dot file*: a per-bead
  **`dot:<name>` label** → `<name>.dot` resolved against the project dir; else project-level
  `workflow.dot`; else the embedded `standard-bead.dot` (moderesolve.go:130-171). Custom DOTs already
  live at `.harmonik/workflows/*.dot` (e.g. `opus-triple-review.dot`). **So the eval DOT is a file
  drop-in + a bead label — no daemon code change.** Called from `workloop.go:2833`.

**Harness (model-under-test) selection** (`internal/daemon/harnessresolve.go:53`, `resolveHarness`,
4-tier precedence modelled on moderesolve):
- Tier 1 — per-bead **`harness:<agent-type>` label** (`harness:pi`, `harness:codex`,
  `harness:claude-code`; harnessresolve.go:40,61-90).
- Tier 4 — global `Config.DefaultHarness`, falling back to claude-code.
- Every resolution emits a `harness_selected` event (bead_id, agent_type, tier) — observable in
  events.jsonl (harnessresolve.go:20-24).

**Pi harness = the model-under-test vehicle.** Per `plans/2026-07-02-pi-sandbox/README.md:156-161`,
**Pi is already fully implemented and harness-blind** (`piharness.go`, `pilaunchspec.go`,
`pijsonlparser.go`; PI-010…050 landed) — a structural clone of codex, selected via the same
`resolveHarness` chain. Its runtime config is the **`harnesses.pi` block** in `.harmonik/config.yaml`:
`provider`, `model`, `api_key_env` are **required, zero baked defaults, refuse-to-start if unset**
(`cmd/harmonik/resolve_pi_config.go:3-28,93-100`; `provider: openrouter`, `model:
openrouter/qwen/qwen3-coder`, `api_key_env: OPENROUTER_API_KEY` in the `--example`, resolve_pi_config.go:230-242).
**Pointing Pi at ornith** = set `harnesses.pi.{provider,model,api_key_env}` to the ornith
provider/model. **There is NO `base_url` in the Pi Go code** — Pi selects the model purely via
`--provider <prov>` + `--model <provider/id>` argv (pilaunchspec.go:242-249); provider routing
(incl. an OpenRouter/OpenAI-compatible or self-hosted endpoint) is Pi's *own* concern, keyed off the
provider string, with the API key env-injected only (never on argv, pilaunchspec.go:13-14). So
reaching a DGX-hosted ornith means picking the right provider string Pi understands for that endpoint
(see resolved decision O2). Claude is the baseline: same DOT, `harness:claude-code` (default).

**Timing / events** (`internal/core/eventtype.go`, `internal/core/event.go`, `internal/daemon/workloop.go`):
- Run lifecycle events: `run_started` (eventtype.go:27), `run_completed` (:31), `run_failed` (:35);
  all durability-class F, appended to **one global** `<project>/.harmonik/events/events.jsonl`
  (`internal/core/jsonlformat_hqwn58.go:15-20`; NOT per-run).
- **Run wall-time.** `emitRunStarted` (workloop.go:5211) writes payload field `started_at`
  (RFC3339, workloop.go:5145,5216); `emitRunCompleted` (workloop.go:5230) writes `ended_at`
  (:5174,5243). **wall_time_s = `run_completed.ended_at − run_started.started_at`**, joined on
  `run_id` (present on both). The envelope `timestamp_wall` (event.go:62) gives the same answer.
- **Per-node timing — IMPORTANT CORRECTION.** The typed `state_entered`/`state_exited` node-bracket
  events are *defined* (eventtype.go:39,43) but **NOT emitted in production** (confirmed: zero
  emission sites). To time the *implement* node specifically, pair `implementer_phase_complete`
  (eventtype.go:115, emitted right after the implementer phase) against `run_started` on the same
  `run_id`. DOT dispatch also emits `node_dispatch_requested`/`node_dispatch_decided`
  (dot_cascade.go:2282,2296) with a `requested_at` timestamp per node — usable for coarser per-node
  timing. So speed is still captured *for free* from the log; we just read the events that are
  actually written, not the state_* pair.

### 1.2 The new DOT — `eval-bead.dot`

Drop it where the project resolves it. Note (from moderesolve.go): `.harmonik/workflows/*.dot` is
just a **palette — no code reads that dir**; the project-level auto-loaded file is repo-root
`workflow.dot`, and per-bead file selection is the **`dot:<name>` label** which resolves `<name>.dot`
against the project dir (moderesolve.go:145-166). So put the file where `dot:eval-bead` resolves (the
project dir, or pass an explicit path via `harmonik run --workflow-ref` — `--workflow-ref` is on
`harmonik run`, *not* on `queue submit`, so for daemon dispatch use the label). Route a task bead
with three labels: **`workflow:dot`** (select DOT mode) + **`dot:eval-bead`** (select the file) +
**`harness:pi`** (or `harness:claude-code` for the baseline). Topology:

```
start
 → implement            (agentic; runs on the MODEL-UNDER-TEST harness; commit required)
 → grade                (non-agentic shell: run the task's own deterministic check)
       SUCCESS (exit 0) → judge          [outcome.status == 'SUCCESS']
       FAIL    (exit≠0) → record-fail     [outcome.status == 'FAIL']  (NO fix-loop — see below)
       fallback         → record-fail
 → judge                (agentic reviewer, model=claude-opus-4-8: rubric quality grade 1-5,
                          writes .harmonik/review.json → preferred_label + notes)
       any verdict      → record-pass     (unconditional; judge NEVER gate-loops in eval)
 → record-pass          (non-agentic shell: append the structured record; terminal)
 → record-fail          (non-agentic shell: append the structured record; terminal)
```

**Key design choices (and why they differ from standard-bead.dot):**
1. **No fix-loop.** standard-bead loops `commit_gate FAIL → implement` (cap 3) to *ship* the bead.
   An eval must measure the model's **one-shot** capability, so `grade FAIL` routes straight to
   `record-fail` — a fix-loop would contaminate the pass/fail + wall-time signal. (A future
   `eval-bead-assisted.dot` could add a capped fix-loop to measure *iterated* capability — a
   distinct metric; keep them separate DOTs.)
2. **`grade` is deterministic-first.** It is a plain shell node — `tool_command` runs the task's
   own unit test or grep=0 check (see §1.4 template). Exit 0 = pass, exit≠0 = fail. This is the
   objective spine; it reuses the exact exit-code→Outcome contract standard-bead already relies on.
3. **`judge` is a secondary quality signal, non-gating.** An LLM-judge reviewer scores the solution
   on a fixed rubric (correctness-beyond-tests, idiom, clarity, over-engineering) 1-5 and writes
   notes to `.harmonik/review.json`. It runs on a **fixed judge model** (`model="claude-opus-4-8"`,
   held constant across all models-under-test so the quality axis is comparable) and **never routes
   back** — its verdict is *recorded*, not *acted on*. Every judge path is unconditional → record-pass.
4. **`.git` isolation / repeatability.** Tasks are self-contained (create-new-file + its test), run
   in the standard per-run worktree, so the same task can be re-run against many models repeatedly
   and safely. (When the Pi sandbox from `plans/2026-07-02-pi-sandbox/` lands, eval runs inherit it
   automatically — same substrate seam.)

### 1.3 The results record (the router's training signal)

`record-pass` / `record-fail` are shell nodes whose `tool_command` appends **one JSON line** per
run to a results file. **Deterministic fields come from the shell node itself** (task id, pass/fail,
check output); **timing + model come from the event log** (a tiny collector reads events.jsonl by
`run_id`). One flat record per run:

```json
{
  "schema_version": 1,
  "run_id":        "019f...-...",          // ties back to events.jsonl
  "bead_id":       "hk-...",
  "task_id":       "eval-fizzbuzz",         // stable curated-task id (§2)
  "difficulty":    "simple",               // trivial | simple | medium | harder
  "model":         "openrouter/qwen/qwen3-coder",  // harnesses.pi.model, or "claude-opus-4-8"
  "harness":       "pi",                    // pi | claude-code | codex  (from harness_selected)
  "pass":          true,                    // deterministic check exit==0  (THE primary label)
  "check_kind":    "unit-test",             // unit-test | grep-zero
  "wall_time_s":   214.7,                   // run_completed - run_started (mono-nsec)
  "implement_time_s": 191.2,               // state_exited - state_entered for the implement node
  "judge_grade":   4,                       // 1-5 LLM-judge quality (secondary; null if judge skipped)
  "judge_notes":   "correct; idiomatic; minor: no input validation",
  "commit_sha":    "abcd123",
  "timestamp":     "2026-07-02T22:14:03Z"
}
```

**Collection / aggregation.** Two options (O3 below): (a) each `record-*` shell node appends the
deterministic slice to `.harmonik/eval-results.jsonl`, and a small post-run **collector** enriches
it with timing/model from events.jsonl keyed on `run_id`; or (b) a single collector reads *everything*
from events.jsonl + the check's stdout (`outcome_emitted` payload) and writes the whole record —
no shell-node file writes at all. Aggregation is then trivial: `jq` group-by `(model, difficulty)`
→ pass-rate, median wall_time_s, mean judge_grade. **This table is the router's training set**:
`features = {task_id, difficulty, task-type tags}` → `label = which model(s) passed, how fast, at
what quality`. A first router is a lookup ("route difficulty≤simple to the cheap/fast model that
passes them; route harder to Claude"); a learned router trains on the same rows once enough accrue.

### 1.4 Deterministic-check template (portable across models)

Each curated task ships as a **bead + a self-contained check**. The check is what `grade` runs. Two
kinds, both exit-code-clean:
- **`unit-test`**: the task bead's description tells the implementer to create `solution/foo.go` (or
  `.py`), and the repo already contains `solution/foo_test.go`. `grade.tool_command` =
  `go test ./solution/... -run TestFoo` (exit 0 = pass). Test file is committed *with the task*, the
  implementer only writes the solution — so the test can't be gamed by the model.
- **`grep-zero`**: for mechanical/text tasks, `grade.tool_command` = a `grep -c 'pattern' file;
  test $(...) -eq 0` style check (the memory-noted deterministic done-check pattern). Exit 0 = clean.

Portability: because tasks create a *new* file + carry their *own* test, they don't depend on deep
repo context and are safe to run repeatedly against any harness. This is deliberate (§2).

### 1.5 What's code vs config/DOT

**Config + DOT only (no daemon change) — the whole harness rides existing seams:**
- `eval-bead.dot` — a new file resolved by the `dot:eval-bead` label (uses only existing node/edge
  grammar; no daemon change — the embed/parse/dispatch path is unchanged).
- Routing — per-bead labels `workflow:dot` + `dot:eval-bead` + `harness:pi` (existing
  `resolveWorkflowMode` / `resolveWorkflowRef` / `resolveHarness`).
- Ornith target — `harnesses.pi.{provider,model,api_key_env}` in `.harmonik/config.yaml` (existing
  `ResolvePiConfig`); Claude baseline needs no config.
- The `grade` / `record-*` shell commands — plain `tool_command` strings.
- The curated task beads + their committed test files (§2) — repo content, not code.

**New code (small, out-of-band — none of it is on the daemon hot path):**
- The **results collector** (~a script or `harmonik eval collect`): read events.jsonl by run_id,
  join timing + `harness_selected` + `outcome_emitted`, emit/enrich the record JSONL. Deterministic,
  read-only over the log. (This is the only genuinely new logic; the DOT + config do everything else.)
- Optional **`harmonik eval report`** — `jq`-style aggregation → the comparison table. Could just be
  a shell one-liner for v1.
- Optional: a `judge`-reviewer prompt/skill variant that emits a 1-5 rubric grade into review.json
  (or reuse the existing reviewer with an eval-specific role string — likely no code).

### 1.6 Open decisions (flag for 3-agent consensus before build)

- **O1 — grade node reads WHICH test?** Committed-with-task test file (can't be gamed, but the
  implementer could `git rm` it) vs. a test injected by the daemon post-implement (safer, needs a
  pre-`grade` shell node to copy it in). Lean: commit-with-task + `grade` restores the test from a
  read-only path before running, so the model can't delete/edit it. **Needs consensus.**
- **O2 — ornith endpoint plumbing (RESOLVED to a build-time verify).** Pi has **no `base_url`
  config** — model routing is the `--provider`/`--model` argv only, and provider resolution
  (endpoint, key) is internal to Pi. So the real question is: *does Pi support a provider string that
  targets the DGX-hosted ornith endpoint* (e.g. an OpenAI-compatible/vLLM provider Pi already knows,
  or a custom-endpoint provider)? If yes → pure config (`harnesses.pi.provider/model/api_key_env`),
  no harmonik code. If Pi has no way to point at a custom endpoint → a small Pi-side or config change
  is needed. **This is the one thing that gates tonight's run — verify Pi's provider list against the
  ornith endpoint FIRST.**
- **O3 — record assembly: shell-node-append + collector, or pure post-run collector?** (§1.3 a vs b).
  Pure-collector keeps the DOT clean and avoids concurrent-file-write footguns across parallel eval
  runs; shell-append is simpler to eyeball live. Lean: pure collector. **Needs consensus.**
- **O4 (minor) — judge model & rubric.** Fixed judge = `claude-opus-4-8` held constant is the
  proposal (comparable quality axis). Confirm we accept the judge-token cost per eval run, or make
  judge opt-in via a `judge` label so pure speed/pass runs skip it.

---

## Part 2 — Initial curated test-task set (immediate ornith suite)

All self-contained (create a new file + carry its own deterministic check), portable across models,
safe to re-run. Each is one bead → `dot:eval-bead` + `harness:pi`. `grade` runs the listed check;
exit 0 = pass. IDs are stable (they become `task_id` in the record).

| id | title | difficulty | deterministic check | why it discriminates |
|---|---|---|---|---|
| `eval-readme-typo` | Fix a one-word typo in a fixture doc | trivial | `grep -c 'recieve' fixture.md; test == 0` (grep-zero) | floor test — any working harness passes; isolates *harness plumbing* failures from *capability* failures |
| `eval-string-reverse` | `Reverse(s string) string` (UTF-8 rune-correct) | trivial | `go test -run TestReverse` incl. a multi-byte-emoji case | catches naive byte-reversal — the emoji case separates a real understanding from a copy-paste |
| `eval-fizzbuzz` | Classic FizzBuzz to a slice | simple | `go test -run TestFizzBuzz` (boundary 15/30) | baseline "can it follow a precise spec" — cheap models usually pass; a fail here means the model is unusable |
| `eval-parse-int-safe` | `ParseIntOr(s string, def int) int` (no panic on garbage) | simple | `go test -run TestParseIntOr` incl. empty/overflow/non-numeric | tests error-path discipline + no-panic; weaker models forget the default-on-error branch |
| `eval-dedupe-stable` | `Dedupe([]int) []int` preserving first-seen order | simple | `go test -run TestDedupe` incl. order-preservation asserts | order-preserving vs set-based is a subtle spec point; discriminates spec-reading care |
| `eval-lru-cache` | Fixed-capacity LRU `Get/Put` (O(1)) | medium | `go test -run TestLRU` incl. eviction-order + update-refresh cases | classic data-structure — needs map+list coordination; separates competent from strong models |
| `eval-config-thread` | Add a `MaxRetries int` field to a small `Config` struct + thread it into an existing `doWork` in the same fixture file | medium | `go test -run TestConfigThread` asserting retry count honored | small *refactor-in-place* (not greenfield) — tests reading existing code + threading a value, the router's "mechanical Go" bucket |
| `eval-json-roundtrip` | Struct with custom `MarshalJSON`/`UnmarshalJSON` for a `time.Duration`-as-string field | medium | `go test -run TestJSONRoundtrip` (marshal→unmarshal identity + a fixed wire-format assert) | custom (un)marshal is idiom-heavy; discriminates Go-stdlib fluency |
| `eval-topo-sort` | Kahn's-algorithm topological sort; detect + error on cycles | harder | `go test -run TestTopoSort` incl. a cyclic-graph error case + a valid ordering-validity check | genuine algorithm + a correctness *and* error-detection axis; strong signal for capability ceiling |
| `eval-interval-merge` | Merge overlapping intervals, sorted, edge-touching handled | harder | `go test -run TestMergeIntervals` incl. adjacent-touching + full-contained cases | edge-heavy algorithm; the touching/contained cases are where weaker models produce off-by-one |
| `eval-mini-expr-eval` | Recursive-descent evaluator for `+ - * /` with precedence + parens | harder | `go test -run TestEvalExpr` incl. precedence + nested-paren + div-by-zero-error cases | multi-concern (parsing + precedence + error) — the top of the ladder; only strong models pass clean |
| `eval-two-file-visitor` | Add a new `Visitor` impl across a 2-file fixture (interface in `a.go`, register in `b.go`) | harder | `go test -run TestVisitor` asserting the new visitor is dispatched | the only *multi-file* task — tests whether a model can navigate more than one file coherently; the harness's portability ceiling |

**Coverage rationale:** 2 trivial (floor / plumbing isolation), 3 simple (spec-following), 3 medium
(data-structure + in-repo refactor + idiom), 4 harder (algorithm + error-handling + multi-file).
The trivial→harder spread is what lets the router learn a *difficulty→model* boundary; the
`grep-zero` vs `unit-test` mix exercises both check kinds; the in-place-refactor
(`eval-config-thread`) and multi-file (`eval-two-file-visitor`) tasks specifically probe the
capabilities a cheap model most often lacks, which is exactly the routing decision that matters.

**Tonight:** (0) verify Pi can point at the ornith endpoint (O2 — the gating check); author the 12
beads + their committed `*_test.go` / fixtures under a `solution/` (or `eval/`) tree; drop
`eval-bead.dot` where `dot:eval-bead` resolves; set `harnesses.pi` at ornith; label each bead
`workflow:dot dot:eval-bead harness:pi` and run all 12; then a second pass swapping `harness:pi` →
`harness:claude-code` for the baseline. Run the collector over events.jsonl → `eval-results.jsonl`;
that JSONL is the first comparison table.
