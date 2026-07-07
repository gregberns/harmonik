# quality-system — Problem Space (Phase 1 chunk: `core-loop-proof`)

**Codename:** `quality-system` · **Phase 1 chunk:** `core-loop-proof` (FIRST, blocks all other chunks)
**Source:** `plans/2026-07-06-quality-system/{00-SYNTHESIS,02-bug-corpus-classification,03-testbed-build-chunks}.md`
**Scope note:** this doc plans **`core-loop-proof` ONLY**. The other five chunks (`scripted-twin`,
`scratch-substrate`, `twin-replay`, `adversarial-corpus`, `chaos-generator`) and Phases 2–3 are owned by
the admiral and are explicitly out of scope here.

## Summary

Build a repeatable **acceptance harness that live-verifies the real task-processing loop** on an isolated
scratch daemon — before any new daemon binary can replace the live one. A known bead is submitted to a
queue and the harness asserts, **from the event stream**, the whole loop end-to-end: bead → queue →
correct harness launches → **correct model bound** → real content change committed → provider reached
**through the sandbox** → DOT reviewer verdict fed back → bead reaches its terminal transition via the
daemon. It runs as a **matrix** of `{harness ∈ claude, codex, pi} × {substrate ∈ local, remote(tcp://)}`.
Today this loop is only ever exercised in production; an entire week of model-leak bugs and the PR-19
fleet outage shipped because nothing asserts it out-of-band.

## Goals

- Make the core loop provable on demand on a scratch daemon (never the fleet daemon), reproducibly across
  a clean reset, with a single command that prints per-cell green/red.
- Close the top-5 task-processing coverage gaps (§Success criteria) with e2e assertions read from the
  event stream, not stdout scraping.
- Produce the reusable **assertion library + matrix runner** that chunk 2 (`scripted-twin`) later imitates
  — this chunk pins down exactly what "correct" looks like for the protocol contract.
- Emit red cells as **deduped beads** back into the main repo (reuse `scratch-daemon.sh feedback`).

## Non-goals (explicitly out of scope)

- The other five build chunks and everything Layer-0/Layer-2: no Docker substrate, no scripted/replay
  twin, no adversarial overlays, no LLM chaos generator. (Phases 2–3, admiral-owned.)
- The `assessor` manifest / merge-gate / deploy-gate mechanics — that is the sibling admiral-owned track
  of Phase 1, not this chunk's build queue.
- Closing `remote-test-pyramid` or deciding `testing-strategy-uplift` — separate Phase-1 admin items.
- Fixing any bug the harness surfaces. This chunk **proves** the loop and **files** reds; it does not fix
  them (fixes are their own beads).
- Broad model-eval / `eval-program` work — that is the model-eval lane, not software quality.

## Constraints

- **Token crunch.** Non-Claude rows (pi, codex) are token-cheap and run by default; the **Claude rows are
  flag-gated** (opt-in, minimal) so a normal run spends ~no Claude tokens. The harness itself must not
  depend on Claude tokens to run. Prefer the non-Claude fleet path (pi/deepseek) to *build* the chunk.
- **Build-in-own-worktree rule.** The crew builds AND live-verifies in its own worktrees on the
  `epic/core-loop-proof` integration branch; `main` is reached by one human PR at chunk end. The crew
  never stops or touches the fleet daemon.
- **24-hour rule.** Build the harness against the **current** live daemon binary. A new daemon build
  replaces the live daemon only after passing through this harness.
- **Isolation is proven, not assumed.** Reuse `scripts/scratch-daemon.sh` (`guard_path` +
  `assert_not_supervised`, per-project socket/tmux/pid keyed off scratch path). Never `pkill harmonik`.
- **Remote row needs a real tcp:// worker.** The remote(tcp://) cells require an available remote runner
  (gb-mbp / a stand-in). If none is reachable at run time the remote cells must **skip loudly**, not
  false-green.
- **Event-stream assertions only.** All correctness checks read typed events (`harmonik subscribe --json`
  / `events.jsonl` via jq), never stdout/pane scraping (memory: worker activity lives in events, not
  stderr).

## Success criteria (the 5 acceptance gaps, as concrete verifiable checks)

Each gap = one assertion the matrix runner makes from the event stream. A gap is "closed" when its check
runs green for the non-Claude cells (pi/codex on local and remote) and passes on the flag-gated Claude
cell.

1. **Model reaches the harness per family (closes C4 — the pi-model-leak class, zero coverage today).**
   For each `{claude, codex, pi}`, submit a bead whose configured model for that family is known, and
   assert the model the harness/provider actually saw == the configured model for THAT family — including
   a per-node `model=` pin NOT leaking across families (a claude pin must not hit the pi row).
2. **remote(tcp://) path == local path (closes C2 — gb-mbp critical path).** The same bead run through the
   remote runner reaches the same terminal outcome via the SAME code seam as local: no sandbox-wrap
   misapplied to tcp runs, DOT brief routed through the worker runner, verdict read intact. Assert
   local-cell and remote-cell event sequences are equivalent for the same input.
3. **Provider comms through the sandbox (closes C3-provider / C6).** Assert the chain
   harness → sandbox → provider → `tool_call` → commit is actually driven: a real content change lands on
   the worktree HEAD, and a provider response with `content:null` / no `tool_call` is detected as an
   explicit failure, not a silent no-commit.
4. **queue-submit → dispatch field fidelity (closes C7).** Submit a fully-specified item
   (`workflow_ref`, `workflow_mode`, `model`, `harness`) and assert the worker was dispatched with those
   exact fields — no rpc-rebuild dropping fields, no hardcoded review-loop default overriding the item.
5. **Real Claude-worktree startup → agent_ready (closes C8 — PR-19).** A real git-worktree Claude launch
   (flag-gated) clears every startup gate (folder-trust, permissions-consent, onboarding) to `agent_ready`
   — the version×worktree interaction that unit/spec tests miss.

**Chunk done when:** one command runs the matrix on a scratch daemon and prints provably-green per-cell
results for pi + codex on local and remote with the correct-model and content-change assertions checked
from the event stream; the Claude row passes when explicitly enabled; red cells file deduped beads; the
run reproduces across a clean scratch-daemon reset.
