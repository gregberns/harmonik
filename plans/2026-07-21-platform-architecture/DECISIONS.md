# Decisions — Platform Architecture (2026-07-21)

## FRAME — ALIGNED
The three-problem decomposition is accepted: **P1 fabric / P2 extraction / P3 distributed
execution**, with the architecture ideas composing as *applications on a platform/kernel*
(operator: "I like thinking of the system like that — applications and a platform/kernel").

## PRIORITY-0 — CODEX FIRST (operator, load-bearing)
Getting **Codex operational as a bead-runner is TOP priority**, above the platform threads.
Rationale: offloading work to Codex stretches Claude tokens, which we'll desperately need to
do all this platform work. Concretely = execute the realignment decisions the SIMPLE way
(local Codex, native sandbox OFF per D3, no ssh per D4, run beads) so Codex crews can then
carry P1/P2/P3. Codex is both the first thing to get working AND the harness the other crews
should run on to conserve Claude.

## Crew structure (operator, for when we execute)
~4-5 crews in parallel: **Codex**, **P2**, **P1**, **P3**, and a **tester checking P1+P3**.
(The platform's whole point — run many crews in parallel. Staff them on Codex once it's up.)

## The six crux decisions

- **C1 — Data vs control plane → CONTROL-PLANE ONLY.** Git stays the artifact plane. Operator:
  "I don't see a good reason for git to change." The fabric carries control/events/addresses,
  not code/diffs/repos.
- **C2 — Kernel now vs later → BUILD P1 NOW, IN PARALLEL WITH P3.** Lay the fabric foundation
  and build P3 on it in tandem, so we discover what P1 must provide AS we build — no ad-hoc
  pipe, no "migrate later." Refinement (operator): build P1 as its OWN fast-iterating thing and
  **use P3 as its test bed**. Governing rule: P1 must do everything P3 needs, with **no hacks
  in either P1 or P3**.
- **C3 — Where "central" lives → SETTLED.** Two layers, answered separately: **transport =
  leaderless/resilient** (no single point of failure — the peer instinct), **work-dispatch =
  one central owner, but only as an application on top of the transport** (not a network
  property; queue-holder dies → restart it, don't rebuild the network). Operator's pragmatic
  first cut: since plugins are applications, **configure ONE instance as primary/hub and the
  rest as spokes/workers; for now the workers' config just names who the primary is** (static
  config, not dynamic discovery yet — that's a later capability). This is the operator's
  original "central harmonik server holds the queues," with the clarification that the network
  underneath has no single point of failure.
- **C4 — Sequencing → ALL THREE IN PARALLEL.** Operator: "Seems like all 3 in parallel. That's
  the whole point of this platform." P2 extraction runs as a **steady stream** (see below).
- **C5 — Dispatch unit + failure semantics → KEEP IT SIMPLE NOW.** Build a basic mechanism to
  hand beads to containers; defer scheduling. Operator: "keep things really simple — just build
  a basic mechanism, then we can dig into scheduling later." NON-SKIPPABLE minimum even in the
  simple version: a dead/timed-out container's in-flight bead must not strand as `in_progress`
  — a heartbeat/timeout that marks it failed and requeues. (That's not scheduling; it's the one
  piece of today's pain we can't reintroduce. Full leases/orphan-recovery = later.)
  **Liveness controls (operator's model — the deterministic set, simple now):**
  (1) worker pulls a bead, starts the container, and KNOWS every bead running on it;
  (2) worker periodically checks the container process is alive (deterministic, modest
  expectations); on actual death → restart / notify / flag;
  (3) **per-DOT-node timeout** — if a run is stuck on the same DOT node too long, kill it and
  flag it back to the main system (catches HANGS, which a liveness check can't);
  (4) when the worker starts the container it passes container info back to the primary — which
  later enables an AGENT-based deep inspection ("reach into the box and inspect"), a LATER
  capability, not the deterministic first cut.
  Operator: "there are probably a handful of these controls we should think through" → OPEN
  P3-design sub-task: enumerate the full liveness/health control set. Core deterministic
  controls (1-3) are the simple-now baseline.
- **C6 — Plugin boundary → IN-PROC LIBRARY (for now).** Operator: "a plugin is going to be
  in-proc… just a library in this system with particular interfaces it can interface with the
  kernel with." So plugin↔kernel is a **Go interface, in-process**, NOT a separate binary/gRPC.
  IMPLICATION — CONFIRMED by operator: the KERNEL is the network-aware/cross-machine layer;
  plugins are always local to their daemon and reach other machines THROUGH the kernel's
  transport (plugin↔kernel = Go interface in-proc; kernel↔kernel = network). Matches "don't
  want many deploy artifacts," and the operator notes it "makes testing way easier" (in-proc
  interfaces mock cleanly vs cross-process gRPC).

## P2 execution style (operator)
A big refactor already landed last week (stronger platform). Keep going, but as a **steady
stream**: no big-bang release, no trickle — break the whole thing down, take it **component by
component, test and release as each is separated out.**

## Guardrail (proposed — to ratify)
Adopt substrate-v2's **kill-criteria + boundary tests as shared, pre-agreed ground rules**, so
scope disputes are settled by a test both sides agreed to in advance — not by "over-
engineering" vs "you're dismissing me." (Prevents both over-build and under-build.)

## Kernel design notes (for P1)

- **Plugin resource APIs (operator, 2026-07-21) — reserve, don't build yet.** Plugins are
  in-proc libraries (C6); when a plugin eventually needs STATE, the kernel should offer a
  standard mechanism rather than every app inventing its own storage. Expect a small set of
  distinct "resource APIs" the kernel exposes to plugins (state/storage first; others later).
  Design the seam for this now (so it's anticipated), but **do not build it until a plugin
  actually needs it.** Goal: no app designs its own storage mechanism.

## Review resolutions (2026-07-21) — the 3 operator decisions from REVIEW.md

- **OQ-1 (guardrail) → REFRAMED as PRINCIPLES, not a bolt-on package.** Do NOT create a separate
  "kill-criteria" construct. Instead fold these into the platform's **Principles we follow while
  building** — improve the existing set (we have good ones already) and **make sure they're
  referenced from the AGENTS file** (operator: if not referenced there, it should be). The
  boundary tests (dep-allowlist, `[]byte` payload, "two plugins needing a new kernel verb =
  stop," P2 freeze tripwire) become principle-backed checks. → TASK: locate + consolidate the
  existing principles, improve them, reference from AGENTS.md.
- **OQ-2 (liveness) → LEAN, reactive, not a fixed L1-L9 spec.** Don't over-plan controls now.
  During kerf, at the KEY failure seams, wire in the ABILITY to add checks — **start with just
  timeouts + feeding that signal back centrally.** Add more checks REACTIVELY once we run
  everything and find the real failure points. Put basic checks where key failure points will
  obviously be; don't get bogged down enumerating a full set up front. (Supersedes "adopt all
  nine.")
- **OQ-3 (reviewer) → keep Claude reviewer NOW; real fix = config-driven per-node model/harness.**
  See codex-first `_plan.md` §7.1. The reviewer being Claude is a HARD-CODING bug; the fix is
  making DOT nodes take model/harness as a config parameter (implement/review/any node),
  processed from a DOT config — a **fast-follow right after Codex is fully supported.** Likely
  related to a recent "rerun" discussion (~`plans/2026-07-19-ralph-autonomous-loop/`).

## Design note — "Resource APIs" naming (operator likes it)
The kernel's plugin resource seam is named **Resource APIs** — operator's mental model is the
**Android ecosystem**: a set of system-provided interfaces a plugin/app calls to get data/
capability from the system. Keep this framing in P1's kernel design (state/storage first;
others added as methods, never transport changes).

## Fast-follow queue (after Codex)
1. **Config-driven per-node model/harness in the DOT** (de-hard-code the reviewer; any node's
   model/harness set by config; relates to the "rerun" idea). Pursue immediately after Codex.

## Building principles — PRINCIPLES, not rules (operator, 2026-07-21)

Operator: agents work much better off **principles (directions to travel)** than **rules (laws)**.
Rules get obeyed literally and make agents do goofy things; principles guide and preserve thinking.
So the "guardrail" (OQ-1) is expressed as principles, not hard laws. Consolidate + improve the
existing principles and **reference them from AGENTS.md** (captain task). The four for the platform:

- **The kernel carries signals, not meaning.** It moves bytes between named channels and stays out
  of the domain; the urge to teach it about a bead/DOT node is the signal that logic belongs in a
  plugin.
- **A healthy kernel stops growing.** Recurring pressure to extend it = the boundary is misplaced;
  treat it as a smell to investigate, not a request to satisfy.
- **The fabric moves references, not cargo.** Heavy things (code/diffs/repos) travel by reference —
  git stays the artifact plane. (A size ceiling may exist as a tripwire that prompts the question,
  not a law.)
- **Code lives in the component that owns it; the composition root wires, it doesn't accumulate.**
  New code wanting to land in the daemon → ask which owned component it belongs to.

Mechanical CI checks may still exist, but as **signals that prompt thought, not laws that stop it.**

## P2 ordering refinement (operator, 2026-07-21)
**Pull everything out of the daemon FIRST, then start killing dead code.** Extract-then-delete —
don't interleave deletion with extraction. (Confirms cold-first; the Codex-first plan's local dead-
code cleanup is fine as a small local exception.)

## Clarifications (2026-07-21, round 2)

- **"Park the core split" ≠ don't refactor core.** It means do it LAST and deliberately.
  `internal/core` is imported by ~everything (fan-in 35), so splitting it is the single
  riskiest refactor — a wrong move = repo-wide merge storm. Extract harness/crew/queue/DOT from
  the daemon FIRST; then split core surgically by type-family, informed by what actually hurt.
  Core WILL be refactored, just not as a big up-front rename. (Same cold-first/steady-stream
  discipline the operator set.)
- **Container "dumb box" design — DEFERRED with diagrams.** Operator wants to see exactly what
  runs where, with diagrams, but not now — get through the smaller decisions first. TODO: a
  diagrammed container-execution design when we reach P3.
- **Daemon auto-dispatch on boot — REMOVE IT ENTIRELY (not default-off).** Booting the daemon
  auto-ran the backlog. Operator decision (2026-07-21): **completely remove the code** that lets
  the daemon automatically start processing beads on boot — do NOT just default it off. Rationale:
  it's coupling the operator dislikes; the daemon shouldn't start up and start running; it's a
  dumb execution substrate and **only AGENTS should decide what runs through the system** — not an
  out-of-date set of beads arbitrarily pulled in. → CAPTAIN CREW TASK: delete the auto-dispatch-
  on-boot path. (Machinery context: pause-queues + skip-on-paused gate hk-kac8g exist; this goes
  further — remove the self-start entirely.)

## Status
**Frame + ALL SIX crux decisions (C1-C6) SETTLED; review's 3 operator decisions RESOLVED above.** Problem-alignment phase complete.
Still open: (a) ratify the kill-criteria guardrail; (b) enumerate the full C5 liveness-control
set (P3-design sub-task, not now). Next: turn this into the concrete plan structure — the three
threads (P1/P2/P3) + Codex-first sequencing + the time-boxed delivery scaffold.
