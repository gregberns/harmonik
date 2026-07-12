# crew-harness-select — Pi crew-orchestrator feasibility (MR1 Pi half)

> codename:crew-harness-select · hk-cpaf2 · parent hk-q3ovr
> Companion to `docs/design/crew-harness-select.md` (the Codex half, hk-fijwi).
> FINDING — answers the feasibility question; does not itself build anything.

## The question

MR1 is "run crews on Codex OR Pi." The Codex path is scoped (hk-l63b9 routing,
lrf30 spec, nzzos sidecar; decision recorded in the Codex half of this doc).
The Pi path was unscoped. Given a Pi **worker** harness already exists
(`default-harness=pi` via ornith/openai-completions, `internal/daemon/piharness.go`,
`specs/pi-harness.md` §1–8), does a Pi crew **orchestrator** need the same
resident-substrate work as Codex, or can it ride the existing Claude-CLI
crew-launch harness (`crewlaunchspec.go`) with just a model/provider swap?

## Finding: no, Pi needs the same class of resident-substrate work — and the spec already says so

The Pi worker harness is spawn-per-turn and self-terminating:

```
pi --mode json --provider <prov> --model <prov/id> "<seed>"   # first turn
pi --mode json --session <id> "<feedback>"                    # resume
```

(`specs/pi-harness.md` PI-011/012/020). `Completion()` is
`CompletionProcessExit` — structurally the same one-shot-API-call shape as
`codex exec`, not a resident session. Pi's exit is unreliable enough that
harmonik carries an extra `agent_end` NDJSON watcher (PI-014) just to force-kill
hung processes — this is *less* robust than Codex's `exec`, not more.

Today there is **no** channel analogous to Claude's `--remote-control` paste
path. The spec already anticipates the crew-orchestrator question directly in
§9 (PI-080/081) and is explicit that it is a **design spike, not a build
contract**:

> "The crew binary ... is a **resident interactive harness** (one process
> across many turns) — NOT a per-turn exec, and NOT a thin 'shim' (it is a new
> ~Claude-Code-equivalent interactive program; the `--remote-control` protocol
> is the easy part)." (PI-080)

So the spec's own framing already answers this task's feasibility question:
**Pi-as-crew-orchestrator is not "worker harness + swap."** It requires
building a new resident, multi-turn, remote-control-analogous surface for Pi —
the same category of problem MR1's Codex half solved by *inverting the loop*
(Option B: daemon holds the loop, `codex exec resume` supplies bounded
decision turns) rather than by building a persistent Codex TUI (Option A,
rejected).

Pi additionally carries risks the Codex spike didn't have to solve, per
PI-085:

1. **RPC-mode unknown.** Whether `pi --mode rpc` is actually a resident
   multi-turn server, or just another per-turn invocation with state rebuilt
   from a session id, is **unverified**. Codex's equivalent question (does
   `codex exec resume` cleanly thread a `thread_id`) was already answered
   affirmatively before Option B was adopted; Pi's has not been.
2. **Keeper is blind on a non-Claude pane.** The context-fill watcher reads
   Claude's context% and drives Claude's `/clear`+`/session-resume`. A Pi
   orchestrator needs its own token-tracking and self-restart handling from
   scratch — Codex's Option B sidesteps this entirely by making the daemon's
   wake loop *be* the keeper (no client-side context window to gauge). A
   Pi orchestrator that stays one-shot-per-turn (a Pi analogue of Option B)
   would inherit that same simplification "for free"; a Pi orchestrator that
   goes resident (a Pi analogue of Option A) would not.
3. **No app-server-equivalent surface** is documented for Pi/ornith
   (Codex's Option C existed as a real, if not-recommended, fallback).

## Recommendation: treat Pi-as-crew like Codex's Option B, not Option A

Given (1)-(3), and given PI-082 already restricts Phase-1 Pi crews to a single
narrow mechanical lane (not judgment/design lanes, which stay Claude), the
lowest-risk path — consistent with the Codex decision already adopted — is:

- **Do not** pursue a resident/interactive Pi TUI (Option A analogue). It
  requires building an entire new interactive harness from nothing (no
  existing `--remote-control`-equivalent to build on, unlike Codex which at
  least has a human-facing "Codex Remote" to point at and reject), and
  duplicates the same keeper-blindness and RPC-mode unknowns.
- **Prefer** a bounded per-turn Pi invocation with the daemon holding the crew
  loop (comms membership, queue subscription, wake triggers) — the direct Pi
  analogue of Codex's adopted Option B — *if and only if* PI-085 unknown (1)
  resolves in favor of clean multi-turn resume via `--session <id>` (no RPC
  server needed). If unknown (1) instead shows session resume loses too much
  state per turn to carry a crew's operating loop context, Phase-1 Pi-as-crew
  should be deferred rather than forced.
- **Keep** PI-082's fence: Phase-1 Pi crew orchestration stays pilot-scoped to
  one deterministic/mechanical lane; captain and judgment lanes remain Claude
  unconditionally (PI-082, PI-090).

## Recommended bead breakdown

All beads below are new work under parent `hk-q3ovr`, gated behind PI-085
resolving. None of this is "nearly free" — the premise in the task
description (that Pi-as-crew is nearly free once l63b9 routing exists) is
**not supported** by the current spec or code; PI-080/081 are explicitly
marked as goals pending a spike, not a fallout of routing work.

1. **`pi-crew-spike`** — resolve PI-085(1): verify whether `pi --mode rpc` is
   a resident multi-turn server. Blocks everything below. Small, time-boxed
   research bead; output is a go/no-go verdict, not code.
2. **`pi-crew-keeper-analogue`** — design the Pi-side equivalent of "daemon
   wake loop is the keeper" (Option-B-style) if (1) resolves per-turn, OR a
   from-scratch token-tracking/self-restart mechanism if (1) resolves
   resident. Depends on (1).
3. **`pi-crew-shim-liveness`** — PI-085(4): add a post-spawn readiness probe
   for a Pi crew (today crew-start returns success with no check at all).
   Independent of (1)/(2); can start in parallel.
4. **`pi-crew-handlerbinary-wiring`** — PI-085(5): resolve `HandlerBinary`
   config-wiring (`daemon.go:116-123`) so Phase-1 Pi crew launch doesn't
   depend on unshipped Phase-2 (PI-090) plumbing — narrow global-binary
   override, or move the wiring earlier. Independent; can start in parallel.
5. **`pi-crew-pilot-lane`** — PI-082: once (1)-(4) land, pilot on the single
   named narrow mechanical lane (gurney, 2 deterministic beads) as a spike
   datapoint only — explicitly not evidence that "weak model can run a
   crew" in general.

Sequencing: 1 blocks 2 and 5; 3 and 4 are file-disjoint from 1/2 and from each
other, so they can run alongside the spike. 5 is the only bead that touches
live crew orchestration and should not start until 1-4 all land.

## Sources

- `specs/pi-harness.md` §1 (PI-011/012/014/020, worker harness shape), §9
  (PI-080/081/082/085, Phase-1 crew shim, spike-gated), §10 (PI-090, Phase-2
  provider abstraction)
- `internal/daemon/piharness.go`, `internal/daemon/pilaunchspec.go` (current
  spawn-per-turn worker implementation)
- `docs/design/crew-harness-select.md` (Codex half — Option A/B/C survey and
  the adopted Option B decision this doc mirrors)
- `internal/daemon/crewlaunchspec.go` (`buildCrewLaunchSpec`, the Claude
  `--remote-control` crew-launch harness being compared against)
