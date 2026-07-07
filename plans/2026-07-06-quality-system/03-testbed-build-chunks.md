# Testbed Build — Medium Chunks for Crew Dispatch

**Date:** 2026-07-06 · **Author:** planning subagent (for the captain) · **Status:** DRAFT

Turns the three-layer daemon-testbed vision (`plans/2026-07-05-agent-world-models/daemon-testbed-design.md`)
plus the quality-process wrapper (`plans/2026-07-05-quality-process/PLAN.md`) into 6 medium, buildable
chunks. Each chunk is an **epic on its own integration branch** (operator rule exception: the testing-crew
builds AND tests in its own worktrees + integration branch, then the integration branch merges to `main`
via one human PR). Smallest-valuable-first; parallelizable where noted.

## Standing constraints baked into every chunk
- **Integration-branch epics.** Each chunk = `epic/<codename>`; beads merge to that branch, `main` reached by
  one human PR at chunk end. The crew live-verifies on its own scratch daemon, never the fleet daemon.
- **24-hour rule.** Build the framework using the **current** live daemon binary. A new daemon build replaces
  the live daemon only *after* being run through this test system. Layers 0–1 are plain tooling / **no Claude
  tokens** — buildable under the token crunch.
- **Non-Claude fleet path.** Prefer pi / deepseek for implementation where feasible; the framework itself must
  not depend on Claude tokens to run.
- **Isolation is proven, not assumed.** Reuse `scripts/scratch-daemon.sh` (`guard_path` +
  `assert_not_supervised`); the production daemon is never stopped.

---

## Chunk order at a glance

| # | Codename | Serial/Parallel | Depends on | Layer/Plan | Size |
|---|---|---|---|---|---|
| 1 | `core-loop-proof` | **first, serial** | — | Layer-1 seam (thin) + Lane-1 LT | M |
| 2 | `scripted-twin` | serial after 1 | 1 | Layer-1 full + first scenario | M |
| 3 | `scratch-substrate` | **parallel with 2** | 1 | Layer-0 Docker + disk dial | M |
| 4 | `twin-replay` | serial after 2 | 2 | Layer-1 replay mode | M |
| 5 | `adversarial-corpus` | serial after 2+3 | 2, 3 | Layer-1 adversarial + corpus | M–L |
| 6 | `chaos-generator` | last, gated | 4, 5 | Layer-2 LLM generator | M |

---

## Chunk 1 — `core-loop-proof` (FIRST, serial, blocks everything)

**Why this is chunk 1, not the scripted twin.** The design's build order opens with "scripted twin +
concurrent-same-file-merge scenario." That is the right *smallest engineering primitive*, but the operator's
stated first proof is broader and more fundamental: **does the real core task-processing loop actually work,
end to end, across every supported harness/model on both substrates?** — "put a bead in the queue → the
harness (claude, codex, pi) starts with the *correct model* → it makes changes that really change things and
can talk to its provider through the sandbox → the DOT reviewer files its verdict back." That loop is the
thing the whole factory rests on and it is currently only ever exercised in production. So chunk 1 is a
**live-verify acceptance harness for the real loop** (Lane-1 LT applied to the loop itself), using real
harnesses on a scratch daemon — NOT a mock. The scripted twin (chunk 2) then makes that same loop
*deterministic and token-free*; it depends on chunk 1 having pinned down exactly what "correct" looks like.

**Scope.** Build a repeatable acceptance run on an isolated scratch daemon that submits a known bead to a
queue and asserts, from the event stream, the full loop for a **matrix** of {harness ∈ claude, codex, pi} ×
{model correctness} × {local, remote substrate}. Assertions: correct harness launched, correct model bound
(the model the bead/queue requested is the model the provider actually saw — closes the "wrong model" gap),
worktree HEAD advanced with a real content change, provider reachable through the sandbox, `review.json`
verdict produced by the DOT reviewer and fed back, bead reaches its terminal transition via the daemon. Seed
the matrix from the `HANDOFF-gb-pr-19.md` case (Claude-version × git-worktree launch gate) as one concrete
row. Pi/codex rows can run token-free-ish; Claude rows are the only ones that spend tokens, so gate them
behind a flag and keep them minimal.

**Advances.** Layer-1 agent seam (thin, real-agent form) + Lane-1 LT (`plans/2026-07-05-quality-process`
P5/Lane-1) + prior-plan Phase-2 orphaned-`smoke` intent.

**Integration branch.** `epic/core-loop-proof`.

**Dependencies.** None. Must precede all others (defines the protocol contract the twin must imitate).

**Size.** Medium.

**Done when.** A single command runs the matrix on a scratch daemon and prints provably-green per-cell
results for pi + codex on local and remote, with correct-model and content-change assertions checked from the
event stream (not stdout scraping); the Claude row passes when explicitly enabled. Red cells file deduped
beads. Reproducible across a clean scratch-daemon reset.

---

## Chunk 2 — `scripted-twin` (serial after 1)

**Scope.** Build the Layer-1 **scripted** digital twin: a stand-in injected where the daemon spawns `claude`,
that speaks the exact protocol chunk 1 nailed down — occupies a (mock) pane, advances worktree HEAD with
scripted commits, emits `agent_ready` + heartbeats, writes a `review.json` verdict, exits with correct
signals. Deterministic scripts ("commit diff X, return REQUEST_CHANGES, die at step Z"). Port the
**concurrent same-file merge race** as the first scenario (2 slots, two twins commit the same file → assert
loser fails safe `non_ff_merge`, no corrupt merge). No Claude tokens.

**Advances.** Layer-1 (design build-order step 1). Makes the whole orchestration layer deterministically
testable token-free.

**Integration branch.** `epic/scripted-twin`.

**Dependencies.** Chunk 1 (protocol contract + assertion library).

**Size.** Medium.

**Done when.** The concurrent-same-file-merge scenario runs green deterministically against the real daemon
with zero Claude tokens, repeatably, and the twin is indistinguishable from a real agent to the daemon
(daemon completes the run without special-casing).

---

## Chunk 3 — `scratch-substrate` (PARALLEL with chunk 2)

**Scope.** Layer-0 Dockerized controllable substrate: a disposable container the daemon runs inside, with the
environment as a dial. Deliver **clean reset** (identical disposable image per scenario — this alone fixes
today's flaky-scenario cross-contamination under fleet load) + the **disk dial** (quota/tmpfs to force
ENOSPC → cache-wipe → `merge_build_failed`). CPU/network/clock dials are stubbed as extension points, not
built here (kept for chunk 5). Port the cache-wipe scenario onto the substrate. No Claude tokens.

**Advances.** Layer-0 (design build-order step 2) + fixes the scenario-test contamination the prior plan
flags.

**Integration branch.** `epic/scratch-substrate`.

**Dependencies.** Chunk 1 (needs the loop + assertion library to have something to run inside the container).
Independent of chunk 2 — **can run in parallel** (different surface: substrate/container vs agent seam).

**Size.** Medium.

**Done when.** The disk-cache-wipe scenario runs green on demand inside the container (disk dial provably
triggers the reap + clean retry, not a hard fail), and two back-to-back scenario runs from a clean reset show
zero cross-test contamination.

---

## Chunk 4 — `twin-replay` (serial after chunk 2)

**Scope.** Layer-1 **replay** mode: capture one real Claude Code session (tool calls, timings, outputs, HEAD
advances) and replay it deterministically through the twin seam — real behavior, deterministic playback, zero
token cost on replay. Includes the capture recorder + the replay driver + one recorded-then-replayed fixture
proving fidelity.

**Advances.** Layer-1 (design build-order step 3). Proves the twin is faithful, not just a plausible stub.

**Integration branch.** `epic/twin-replay`.

**Dependencies.** Chunk 2 (extends the twin seam). One-time capture spends Claude tokens; replay is free.

**Size.** Medium.

**Done when.** A recorded real session replays deterministically and the daemon reaches the same terminal
outcome as the live capture, with zero tokens on the replay run.

---

## Chunk 5 — `adversarial-corpus` (serial after chunks 2 + 3)

**Scope.** Layer-1 **adversarial** perturbation overlays on scripted/replay + port the memory-bank failure
corpus as durable regression tests. Overlays: slow responses, malformed `review.json` (ErrMalformed /
quit-watchdog-midwrite), no-HEAD-advance stall, pane death mid-write, heartbeat gaps. Corpus rows from the
design's table: reviewer-pane death (8-min HB gate), stranded in_progress auto-reset, concurrent-slot
cold-start (150s `agent_ready` hold — needs chunk-3 network/clock dials), mid-flight cancel phantom-run. Also
delivers the Lane-1 **XT** exploratory break-testing harness (3–5 adversarial agents; findings → deduped
beads; P0/P1 block the PR). Build out chunk-3's stubbed CPU/network/clock dials as needed here.

**Advances.** Layer-1 (design step 4) + Lane-1 XT (`plans/2026-07-05-quality-process`).

**Integration branch.** `epic/adversarial-corpus`.

**Dependencies.** Chunk 2 (twin seam to perturb) AND chunk 3 (substrate dials for the timing/network cases).

**Size.** Medium–Large (largest chunk; may be split into a twin-overlay bead-set and a corpus-porting
bead-set on the same branch).

**Done when.** Each ported corpus incident runs green as a deterministic regression test (daemon fails safe
per the expected-outcome column), and the XT harness runs its adversarial agents against a scratch daemon and
files findings as deduped beads.

---

## Chunk 6 — `chaos-generator` (LAST, gated on 4 + 5)

**Scope.** Layer-2 LLM chaos generator — the world-model idea in its *strong* role: an LLM generates a
distribution of realistic agent behaviors (diffs, verdicts, tool-call sequences, malformed outputs) and
proposes new break-sequences from the failure corpus; the **real daemon verdicts them** (imperfect proposer,
exact judge). Never replaces a daemon run. Feeds generated behaviors through the chunk-2/4 twin seam and runs
them on the chunk-3 substrate.

**Advances.** Layer-2 (design step 5). Explicitly gated on layers 0–1 having proven the harness shape.

**Integration branch.** `epic/chaos-generator`.

**Dependencies.** Chunk 4 (twin seam mature) + chunk 5 (adversarial harness + corpus to seed the generator).
Spends tokens — schedule against the 24-hour / token-crunch posture; keep runs bounded.

**Size.** Medium.

**Done when.** The generator produces varied agent behaviors that run through the real daemon on the
substrate, and at least one generator-proposed break-sequence surfaces a daemon reaction that a human
confirms as either a real fail-safe or a genuine new bug (filed as a bead).

---

## Parallelism map

```
Chunk 1 core-loop-proof  ─┬─────────────────────────────────────► (blocks all)
                          │
              ┌───────────┴───────────┐
              ▼                        ▼
     Chunk 2 scripted-twin     Chunk 3 scratch-substrate   (PARALLEL)
              │                        │
              ├────────► Chunk 4 twin-replay                (serial after 2)
              │                        │
              └────────┬───────────────┘
                       ▼
            Chunk 5 adversarial-corpus                     (needs 2 + 3)
                       │
                       ▼
            Chunk 6 chaos-generator                        (needs 4 + 5, gated)
```

- **Serial spine:** 1 → (2 ‖ 3) → 5 → 6, with 4 hanging off 2.
- **Two crews can run concurrently** after chunk 1 lands: one on `scripted-twin` (agent seam), one on
  `scratch-substrate` (container/dials). They touch disjoint surfaces.
- Chunk 4 (replay) can slot in on the twin crew as soon as chunk 2 merges, in parallel with chunk 3.
