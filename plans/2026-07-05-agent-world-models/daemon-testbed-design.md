# Daemon test-bed + Claude Code digital twin — design note

*schmidhuber → admiral, for quality-process Lane-1 XT. Grew out of the agent-world-models research: the
productive use is NOT a world model that replaces daemon runs — it's a **controllable test-bed that
keeps the real daemon but mocks the agents and dials the environment**, with an LLM as an optional
generator on top. Operator-endorsed 2026-07-05.*

## Why this exists
Harmonik is nearly untestable at the orchestration level for one reason: **the agents are real Claude
Code sessions** — slow, costly, nondeterministic — and the substrate is a shared base machine that
fights control (noisy disk/CPU, no clean reset). So we can't cheaply ask "does the daemon do the right
thing when the reviewer dies mid-write / two beads touch one file / disk drops under 10GiB / a remote
tunnel flaps?" Almost every incident in our memory bank is one of those, and each was learned the
expensive way — in production, once. This test-bed makes them **cheap, deterministic, and permanent
regression tests.**

## The stack (three layers, increasing sophistication, each useful alone)

### Layer 0 — Dockerized controllable substrate
A dedicated container the daemon runs inside, where the environment is a **dial, not a given**:
- **Disk** — quota/tmpfs sizing to force the ENOSPC → cache-wipe → `merge_build_failed` path on demand
  (the live hk-5uezz reaper-mid-build wipe becomes a repeatable test).
- **CPU** — cgroup limits to reproduce the "CPU saturation masquerades as daemon flakiness" case.
- **Network** — latency/partition injection for the remote-worker tunnel / ControlMaster / agent_ready
  failures.
- **Clock** — controllable time to exercise the 8-min HB-staleness gate, 150s agent_ready, timeouts —
  without real waiting.
- **Clean reset** — every scenario starts from an identical, disposable image. Reproducible concurrency,
  no cross-test contamination (which is exactly what's corrupting the current e2e/scenario runs under
  fleet load).

Value alone: even with *real* agents, a reset-clean controllable substrate fixes the flaky-scenario-test
problem we have right now.

### Layer 1 — Claude Code digital twin (the agent seam)
A stand-in that speaks the **exact protocol the daemon expects of an agent**, so the daemon can't tell
it's not real: occupies a pane (or a mock pane), advances worktree HEAD with commits, emits
`agent_ready` + heartbeats on the event bus, writes `review.json` with a verdict, exits with the right
signals. Where the daemon spawns `claude`, inject the twin instead.

Four modes — this is what makes it a *twin*, not a stub:
1. **Scripted** — fully deterministic: "commit diff X, return REQUEST_CHANGES, then die at step Z."
   The backbone of regression tests.
2. **Replay** — replays a *recorded real Claude Code session* (captured tool calls, timings, outputs, HEAD
   advances). This is the faithful "twin": real behavior, deterministic playback, zero token cost.
3. **Adversarial** — perturbation overlays on scripted/replay: slow responses, malformed `review.json`
   (the ErrMalformed / quit-watchdog-midwrite case), no-HEAD-advance stall, pane death mid-write,
   heartbeat gaps. Each maps to a real incident.
4. **Generated** — LLM-driven (Layer 2).

Value alone: makes the whole orchestration layer deterministically testable, no Claude tokens.

### Layer 2 — LLM generator on top (where the "world model" actually pays off)
Now the world-model idea returns, but in its *strong* role — a **generator whose output the real daemon
verdicts**, never a replacement for the daemon:
- **Realistic, varied agent behavior.** Hand-scripts only test what we thought to write. An LLM playing
  "what would an implementer/reviewer plausibly produce here" generates a *distribution* of realistic
  diffs, verdicts, tool-call sequences, malformed outputs. Hallucination is an ASSET here — it's inputs
  we'd never hand-author; the daemon's real reaction is the ground truth.
- **Adversarial scenario proposal.** An LLM that's seen our failure corpus proposes new break-sequences
  faster than random fuzzing; the harness runs them against the real daemon; the daemon says whether it
  broke. Imperfect proposer, exact judge — so the fidelity gap that kills the "replace the run" use is
  harmless here.

## How it maps to the failure corpus (the seed regression set)
Each memory-bank incident becomes a `(Layer-0 dial + Layer-1 twin script) → expected safe outcome` test:

| Incident | Layer-0 dial | Layer-1 twin behavior | Assert |
|---|---|---|---|
| Concurrent same-file merge race | 2 slots | two twins commit same file | loser fails safe `non_ff_merge`, no corrupt merge |
| Disk cache wipe | disk quota → <10GiB | normal commits | reactive reap + clean retry, not hard fail |
| Reviewer-pane death | — | reviewer twin dies mid-write | 8-min HB gate Kills + re-dispatches, no 40-min hang |
| Stranded in_progress | — | twin exits without terminal event | daemon auto-resets, no claim-skip livelock |
| Concurrent-slot cold-start | 6 remote slots, net latency | twins cold-start slowly | spawn-semaphore + 150s agent_ready hold (hk-5z1f0 regression test) |
| Malformed review.json | — | reviewer twin writes truncated json | ErrMalformed handled, salvage/re-dispatch |
| Mid-flight cancel | — | cancel during run | no phantom RunRegistry blocking resubmit |

## Build order (smallest-first, each independently valuable)
1. **Layer 1 scripted twin + one scenario** (concurrent same-file merge race) running against the daemon
   in a plain sandbox. Proves the agent seam end-to-end. Highest-frequency real incident.
2. **Layer 0 Docker substrate** with the disk dial; port the cache-wipe scenario onto it. Proves the
   controllable-environment payoff and fixes our flaky-scenario-test contamination.
3. **Replay mode** — capture one real Claude Code session, replay it deterministically. Proves the twin
   is faithful.
4. **Adversarial overlays** — port the remaining corpus incidents as perturbations.
5. **Layer 2 LLM generator** — only after 1–4 exist and pay off. This is the world-model layer; gate it
   on the earlier layers proving the harness shape.

## Scope / honesty
- Layers 0–1 are the high-value core and are **plain tooling — no LLM, no Claude tokens.** Build these
  first regardless of the world-model question.
- Layer 2 is where an *imperfect* model is genuinely useful, because it generates and the real daemon
  judges. This is the corrected verdict on agent world models for harmonik: not a run-replacer, a
  **chaos/behavior generator for the test-bed.**
- This IS Lane-1 XT (exploratory break-testing) with a durable regression corpus underneath it —
  recommend routing the whole thing through the quality-process kerf, built on the non-Claude fleet path
  under the token crunch.
