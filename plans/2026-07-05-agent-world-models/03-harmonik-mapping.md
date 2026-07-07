# Mapping agent world models onto harmonik (DRAFT — pending primary-source fidelity numbers)

## What "the environment" is in harmonik

Harmonik is a factory of LLM coding agents. The daemon dispatches a bead (task) into a fresh git
worktree; an **implementer** agent edits code and commits; a **reviewer** agent reads the diff and
emits a verdict; the daemon runs build/test/merge **gates** and, if they pass, merges the branch to
integration → main. So the "environment" an agent acts in is:

- **State:** the repo tree at a commit, the bead spec, open worktrees, daemon queue state.
- **Actions:** file edits, shell commands, commits, tool calls, review verdicts.
- **Observations the agent gets back:** file contents, `go build` / `go test` output, linter
  (golangci) results, the reviewer's APPROVE/REQUEST_CHANGES verdict, and the daemon's merge
  outcome (clean merge vs `non_ff_merge` / `merge_build_failed` / gate timeout).

An *agent world model* for harmonik would be a model that, given (repo state + a proposed action),
**predicts the next observation** — e.g. "will this change build? pass tests? merge clean? draw a
REQUEST_CHANGES?" — without paying for a full live worktree run.

This is a near-exact match to Qwen-AgentWorld's **SWE + Terminal + OS** domains, which predict the
next environment observation after a shell/code action via long chain-of-thought. So the *concept*
transfers cleanly; the open question is **fidelity** (does the predicted build/test/merge outcome
match reality often enough to be useful) and **cost** (is predicting cheaper than just running it).

## The three asks, evaluated honestly

### 1. Testing — simulate the daemon/agent environment to break the system faster
This is the **strongest fit**, and it ties directly into the operator's "spin up the binary and have
agents try to break it" quality goal.

- **Not the LWM's home turf, but adjacent.** Qwen-AgentWorld simulates *the target app's* environment
  (a shell, a browser) so you can train an agent cheaply. Harmonik's testing problem is the reverse:
  we want adversarial *inputs/schedules* that expose daemon bugs (races, wedges, cache-wipe cascades).
- **Where a world model genuinely helps:** a learned model of "given this fleet state + this dispatch
  schedule, what failure mode results" could **generate adversarial scenarios** (concurrent same-file
  beads → merge race; disk <10GiB → cache wipe → merge_build_failed; reviewer-pane death → wedge)
  faster than random fuzzing, by predicting which schedules are likely to break. But we have a large
  corpus of *real* failure signatures already (events.jsonl, the memory bank of ~90 incident notes) —
  a cheaper first step is scenario generation from that corpus, no world model required.
- **Verdict:** the *goal* (agents trying to break the binary) does NOT need an agent world model.
  A scratch-daemon + adversarial scenario harness gets there. A world model is a possible *later*
  optimization to prioritize which scenarios to run.

### 2. Implementation — help agents predict the effect of a change before running it
- **This is exactly what an LWM does** (predict next observation), and it's the most speculative for us.
- An implementer could "dry-run" a change against the world model — predict build/test outcome — before
  committing. But harmonik agents already have the *real* toolchain in their worktree; `go build` is
  seconds. The value of a predicted outcome over just running the real thing is low **unless** the real
  action is expensive or destructive (a full scenario/live-daemon run, which IS minutes and a slot).
- **Verdict:** low marginal value for ordinary code edits (real feedback is cheap and exact). Possible
  value only for the expensive-feedback case (predicting a live-daemon scenario outcome) — which is
  really ask #3.

### 3. Validation — predict a change works without a full live run
- **The economically interesting one.** A live scratch-daemon scenario run costs a worktree + minutes +
  a concurrency slot; the memory bank shows these are the scarce resource. If a world model could
  predict "this daemon change passes the concurrent-dispatch scenario" with high precision, we'd cut
  live runs to only the uncertain cases.
- **The hard truth (compounding error):** every world model in the literature degrades over long
  horizons — small per-step prediction errors compound. Harmonik's failure modes are precisely the
  *long-horizon, multi-agent, timing-dependent* ones (a wedge that appears 20 min into a 6-wide remote
  run). These are the WORST case for a simulator to get right, because they depend on real concurrency,
  real disk, real network — exactly the details a language sim abstracts away.
- **Verdict:** an agent world model is unlikely to replace live validation for harmonik's *daemon-level*
  correctness (races/wedges) any time soon. It could plausibly pre-screen *cheap, deterministic*
  properties (does bead X's diff build/lint/pass unit tests) — but a real `go build && go test` already
  does that faster and exactly.

## Bottom line (draft)
The concept maps cleanly onto harmonik's SWE-like environment, but the two places we'd most want it
(daemon-race validation, adversarial scenario discovery) are exactly where language world models are
weakest (long-horizon, concurrency/timing/disk-dependent, compounding error). The one honest near-term
use is **adversarial scenario generation for the "break the binary" quality goal — and even that is
better served first by a scenario harness mined from our existing real failure corpus, with a world
model as a later prioritizer.** Full assessment + smallest-experiment in RECOMMENDATION.md.
