# Eval Program — Problem Set & Tool Research

**Date:** 2026-07-03. **Status:** design-only (read-only research; no beads, no code).
Builds on `plans/2026-07-02-eval-harness/DESIGN.md` (the `eval-bead.dot` + deterministic-grade
harness) and the 8 existing `codename:eval` task beads.

**Locked eval principle (tamper-proof grading).** The deterministic check is a FIXED test suite the
model does NOT write. Each task ships as a bead + a committed test file living under
`evaltasks/<task_id>/`; the implementer writes ONLY the solution file(s), commits the provided
`*_test.go` unchanged, and `grade` runs `go test ./evaltasks/<id>/... -run <T>` (exit 0 = pass) or a
`grep-zero` check. The test is committed-with-the-task and restored-before-the-run so the model can't
edit/delete it to game the score. One-shot: NO fix-loop in `eval-bead.dot` (measures raw capability).

---

## Part 1 — The problem set (~12 tasks, simple → hard)

Existing 8 (from `codename:eval` beads — keep as-is, mostly trivial→medium): `eval-readme-typo`
(trivial, grep-zero), `eval-string-reverse` (trivial), `eval-fizzbuzz` (simple),
`eval-parse-int-safe` (simple), `eval-dedupe-stable` (simple), `eval-lru-cache` (medium),
`eval-json-roundtrip` (medium), `eval-topo-sort` (harder). These discriminate: instruction-following,
UTF-8/rune correctness, basic control flow, error-handling, stable-order dedupe, O(1) data-structure
design, custom (Un)Marshal, and Kahn/cycle-detection.

**ADD 6 harder multi-step tasks** (a strong model can't reliably one-shot these — each has a
subtle invariant + adversarial test cases). All follow the same fixed-test contract.

| id | difficulty | capability it discriminates | deterministic fixed-test check |
|----|-----------|-----------------------------|--------------------------------|
| `eval-interval-schedule` | hard | greedy interval scheduling w/ tie-breaks; max non-overlapping set + edge cases (touching endpoints `[1,2],[2,3]` don't overlap; empty; all-overlap; unsorted input) | `go test ./evaltasks/eval-interval-schedule/... -run TestSchedule` — table of adversarial cases incl. endpoint-touch, negatives, single, dupes |
| `eval-lru-ttl` | hard | concurrency-safe cache: capacity eviction **and** per-entry TTL expiry, thread-safe under `-race` | `go test -race ./evaltasks/eval-lru-ttl/... -run TestLRUTTL` — expiry-then-get miss, TTL refresh on Put, LRU eviction independent of TTL, 100-goroutine concurrent Get/Put must pass under `-race` |
| `eval-expr-eval` | hard | small parser/interpreter: tokenize + recursive-descent eval of `+ - * / ( )` with precedence & assoc; report div-by-zero and malformed input as errors | `go test ./evaltasks/eval-expr-eval/... -run TestEval` — precedence (`2+3*4==14`), parens, left-assoc subtraction, div-by-zero error, unbalanced-paren error, unary minus |
| `eval-bugfix-rate-limiter` | hard | bug-hunt-and-fix in PROVIDED buggy code (token-bucket rate limiter with an off-by-one refill + a lost-update race); solution edits the given file, test is held-out | `go test -race ./evaltasks/eval-bugfix-rate-limiter/... -run TestLimiter` — provided buggy `limiter.go` + committed test; passes only after both bugs fixed (burst never exceeds capacity; concurrent Allow under `-race`) |
| `eval-refactor-storage` | hard | behavior-preserving multi-file refactor: extract an inline map store behind a `Store` interface across 3 files WITHOUT changing observable behavior (the invariant) | `go test ./evaltasks/eval-refactor-storage/... -run TestStore` — golden behavior test committed pre-refactor; still passes post-refactor + a grep-zero check that the old concrete type is no longer referenced by callers |
| `eval-cli-kv` | medium/hard | stateful CLI subcommand: `set/get/del/list` over a JSON file, correct exit codes, idempotent, handles missing-key + malformed-store | `go test ./evaltasks/eval-cli-kv/... -run TestCLI` — driver invokes the built command via `os/exec` / in-process `main`, asserts stdout + exit codes across a `set→get→del→get-miss(exit 1)→list` sequence and a corrupt-store case |

**Final set = 14 tasks** (8 existing + 6 new), spanning trivial→hard, "a couple simple, most
non-trivial." Difficulty mix: 2 trivial, 3 simple, 3 medium, 6 hard — meets "mostly harder,
can't one-shot." Every one is self-contained under `evaltasks/<id>/`, safe to re-run against any
harness, and graded by a committed test the model may not modify. (If a leaner ~12 is wanted, drop
`eval-cli-kv` + one simple; the 6 hard ones are the point.)

---

## Part 2 — Existing benchmarks (web research)

| Benchmark | Measures | License / availability | Runs local + adaptable to our DOT (per-task worktree + FIXED held-out tests + model-agnostic)? | Integration effort |
|-----------|----------|------------------------|-----------------------------------------------------------------------------------|--------------------|
| **SWE-bench / SWE-bench-Verified** | Resolve a real GitHub issue in a real Python repo; Verified = 500 human-vetted, de-flaked instances | Open (MIT), harness `github.com/princeton-nlp/SWE-bench`; Verified set public | Yes — per-instance Docker image, ground-truth held-out `FAIL_TO_PASS`/`PASS_TO_PASS` tests the model never writes. Our DOT would need a shell node that applies the model patch + runs SWE-bench's test spec instead of `go test`. Python-only, repo-scoped (NOT self-contained new-file) | **High** — adopt their Docker/test-spec runner or reimplement patch-apply + pytest-select per instance |
| **SWE-bench-Multimodal** | JS/TS front-end issues requiring image/visual context | Open harness, but **test split is PRIVATE — scored only via the swebench.com hosted API** | Not fully local (gated grading); needs image inputs + JS toolchains | **High** — out of scope for a text-coding eval now |
| **Multi-SWE-bench (ByteDance, 1632)** | SWE-bench task across **7 languages** (Java/TS/JS/Go/Rust/C/C++) | **CC0** (public domain) | Yes — separate harness + config + Docker images, fully local, held-out tests. Best multi-language repo-scale option; includes Go | **Med** |
| **SWE-Gym (2438 / Lite 230)** | Real Python GitHub-issue tasks with runtime + held-out tests — but designed as a **TRAINING env, not a scoreboard** | Apache-2.0 | Yes, SWE-bench-lineage Docker; tests held out. **Keep strictly separate from any SWE-bench eval split** (train/test hygiene) | **Low–Med** (as a corpus, not an eval) |
| **Aider polyglot** | 225 Exercism exercises across 6 langs (C++, Go, Java, JS, Python, Rust); one-shot + one-feedback edit | Open (Apache-2.0), `github.com/Aider-AI/polyglot-benchmark` | Yes, and closest fit — each exercise = a stub file + a HELD-OUT unit test the model may not edit; Docker runner; language-native `go test`/pytest. Maps almost 1:1 onto our `evaltasks/<id>/ + committed test` contract | **Low–Med** — import exercises as eval beads; grade node runs the exercise's own test |
| **LiveCodeBench** | Fresh competitive-programming problems (LeetCode/AtCoder/Codeforces) time-windowed AFTER model cutoff → contamination-resistant | Open (`github.com/LiveCodeBench/LiveCodeBench`), rolling releases | Local; held-out hidden test cases (stdin/stdout judge). Model-agnostic. Algorithmic single-file, not repo/agentic | **Med** — wrap the stdin/stdout judge as a grade shell node; value = anti-contamination discriminator |
| **Terminal-Bench (2.0)** | Hard realistic CLI/sysadmin/infra tasks in a real shell (scripting, process/fs mgmt) | Open harness (`terminal-bench.ai`, Stanford/Laude) | Yes — per-task Docker container + a held-out test script (model never writes it). Model-agnostic via an "agent adapter." Tests *terminal agency*, complementary to code-gen | **Med** — adopt its container+test-script pattern; our harness would drive it as one more DOT harness |
| **Commit0** | Build 54 real Python libraries from scratch to pass their full unit-test suites (lite=16) | Open (`github.com/commit-0/commit0`), MIT | Yes — per-library Docker + lint/typecheck/coverage feedback. **Caveat: tests are VISIBLE to the agent (they're the spec) → gameable; weakest tamper-proofing story.** Long-horizon, Python-only | **High** — heavy multi-hour tasks; costly per model |
| **EvalPlus (HumanEval+/MBPP+) · BigCodeBench** | Function-level correctness with 80×-augmented tests (EvalPlus) / practical library-use tasks (BigCodeBench) | Open, pip-installable | Yes, trivially local; held-out augmented tests. But function-level = the EASY end; high contamination risk on base HumanEval | **Low** — useful only as cheap smoke, overlaps our simple tier |

**Key findings.** (1) *Held-out vs gameable tests:* SWE-bench (family), Multi-SWE-bench, SWE-Gym, Aider-polyglot, LiveCodeBench, and **Terminal-Bench** keep grading tests OUT of the agent's writable env (Terminal-Bench even ships a canary GUID + forbids tests in the Dockerfile) — matching our locked "model doesn't write the test" principle. **Commit0 is the exception: its tests ARE the spec, visible + gameable — weakest tamper-proofing.** (2) *Contamination:* base HumanEval/EvalPlus reuse 2021 prompts → leakage-inflated; SWE-bench issues are public GitHub (~33% solution leakage, ~31% weak tests reported, and some public containers even leak post-`base_commit` git history — sanitize the worktree). **LiveCodeBench is the purpose-built fix** (date-stamped, `--start_date/--end_date` window to post-cutoff), alongside our own private hand-rolled set. (3) *Gated:* only SWE-bench-Multimodal (private test split, hosted-API grading); everything else runs fully local. (4) *pip + Docker out of the box:* `swebench`, `evalplus`, `bigcodebench`, `commit0`, LiveCodeBench (`lcb_runner`), Terminal-Bench (`tb run --agent X --model provider/model` — the cleanest native model-agnostic adapter). (5) *Model-agnostic seam:* all decouple the agent via an adapter → pointing at Pi/ornith vs Claude is the same seam our `harness:<x>` label already provides. (6) *Cost / shape:* SWE-bench-Verified/Commit0 are minutes-to-hours per task (repo-scale); our hand-rolled + Aider exercises are seconds-to-minutes — far cheaper for a router training-signal sweep. Multi-SWE-bench (CC0) is the best multi-language repo-scale option and is the only external one that already includes **Go**. RepoBench is a poor fit (grades by string similarity, no test execution) — excluded.

## Recommendation — **HYBRID (hand-rolled core + Aider-polyglot import), phased**

1. **Phase 1 (now): ship the hand-rolled 14-task set.** It already rides our exact seams (`evaltasks/<id>/` + committed test + `eval-bead.dot`), is fast/cheap to sweep across every candidate model, is fully private (zero contamination), and we control difficulty distribution precisely for the router's training rows. No external harness dependency, no license friction, runs today.
2. **Phase 2: import Aider-polyglot** as additional eval beads — it is the lowest-effort external adopt (its exercise = stub + held-out test = our contract), adds multi-language breadth (Rust/JS/Java) our Go-only set lacks, and lets us cross-calibrate our private scores against a public leaderboard.
3. **Phase 3 (as budget allows): add SWE-bench-Verified + LiveCodeBench + Terminal-Bench** as a *separate, heavier* eval track (own DOT, own runner). Verified = real-repo agentic capability (Python); LiveCodeBench = contamination-proof discriminator (date-windowed) that stops a model gaming the private set over time; **Terminal-Bench** = the cleanest native model-agnostic drop-in (`tb run --agent … --model …` + canary hygiene) for terminal/ops agency. If multi-language repo-scale is wanted, prefer **Multi-SWE-bench (CC0)** over Aider for Go/Rust/TS coverage. Do NOT use SWE-Gym as a scoreboard (it's a training corpus — keep it out of any eval split).

**Why not adopt-only:** SWE-bench/Commit0 are Python-only, repo-scale, and slow — wrong shape for a fast per-model router sweep and a heavier lift; Commit0's tests are also gameable. **Why not hand-rolled-only:** a purely private set can't be externally calibrated and risks slow contamination; the Aider (and later Multi-SWE-bench) import fixes both. The hybrid keeps the fast controllable core while borrowing external credibility and anti-gaming where it's cheap.

## Sources
- SWE-bench: [FAQ](https://www.swebench.com/SWE-bench/faq/) · [pypi swebench](https://pypi.org/project/swebench/) · [Verified dataset (HF)](https://huggingface.co/datasets/SWE-bench/SWE-bench_Verified) · [repo](https://github.com/SWE-bench/SWE-bench)
- Multi-SWE-bench: [dataset (HF, CC0)](https://huggingface.co/datasets/ByteDance-Seed/Multi-SWE-bench) · [repo](https://github.com/multi-swe-bench/multi-swe-bench)
- SWE-bench leakage / weak tests: [arXiv 2410.06992](https://arxiv.org/pdf/2410.06992) · [SWE-bench Pro public](https://labs.scale.com/leaderboard/swe_bench_pro_public)
- Aider polyglot: [leaderboards](https://aider.chat/docs/leaderboards/) · [repo](https://github.com/Aider-AI/polyglot-benchmark)
- LiveCodeBench: [site](https://livecodebench.github.io/) · [repo](https://github.com/livecodebench/livecodebench) · [arXiv 2403.07974](https://arxiv.org/pdf/2403.07974)
- Terminal-Bench: [site](https://tbench.ai/) · [repo](https://github.com/harbor-framework/terminal-bench) · [2.0 announcement](https://www.tbench.ai/news/announcement-2-0)
- Commit0: [site](https://commit-0.github.io/) · [repo](https://github.com/commit-0/commit0) · [arXiv 2412.01769](https://arxiv.org/abs/2412.01769)
- SWE-Gym: [repo](https://github.com/SWE-Gym/SWE-Gym) · [arXiv 2412.21139](https://arxiv.org/abs/2412.21139)
- EvalPlus: [repo](https://github.com/evalplus/evalplus) · BigCodeBench: [repo](https://github.com/bigcode-project/bigcodebench) · RepoBench: [repo](https://github.com/Leolty/repobench)
- Overview guides: [digitalapplied 2026 guide](https://www.digitalapplied.com/blog/swe-bench-terminal-bench-benchmark-guide-2026) · [benchlm.ai leaderboard](https://benchlm.ai/coding)
