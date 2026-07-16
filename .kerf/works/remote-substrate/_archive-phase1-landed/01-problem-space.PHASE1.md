# remote-substrate — Problem Space (DRAFT v0)

> Status: DRAFT pending operator iteration. This is paul's say-back of the brainstorm
> kickoff (2026-06-14), to be confirmed/edited before we advance to `analyze`.
> Companion docs: `REQUIREMENTS.md`, `BRAINSTORM.md`.

## One-paragraph summary

Today every part of harmonik — the captain session, all crew sessions, and every
bead's implementer Claude Code session + git worktree — runs on a single macOS box
with 16 GB RAM. We are hitting hard RAM and disk ceilings: concurrency is capped not
by useful parallelism but by the one machine's resources (the `--max-concurrent 4`
knee, repeated "no space left" cache blowouts). We want to let harmonik **place
execution on other machines** — starting with two spare Apple-Silicon MacBook Pros
(lots of RAM/disk), and eventually arbitrary remote targets (a dual-boot Linux box, a
VPS, or a cloud sandbox spun up at work). The end state is that harmonik treats
"where a session runs" as a pluggable **execution substrate** rather than always
"local tmux on this host."

## The problem, concretely

- **Resource ceiling.** 16 GB RAM + finite disk on one box caps concurrent beads far
  below what the work queue could absorb. Disk-full has repeatedly masqueraded as
  daemon flakiness; CPU saturation has too.
- **Single point of contention.** Captain, crews, and bead-work compete for the same
  cores, RAM, disk, and build cache on one machine.
- **No isolation.** Every bead's implementer Claude runs against the same host
  filesystem (mitigated by git worktrees, not by true sandboxing). A per-bead or
  per-crew sandbox would give real blast-radius containment.
- **Idle hardware.** Two capable Macs sit unused while box A thrashes.

## Goals (what this work achieves)

1. harmonik can run agent work (at minimum: per-bead implementer sessions) on a
   machine **other than** the one the daemon/captain runs on.
2. The "where it runs" decision is behind a **pluggable substrate interface** — the
   local-tmux path becomes one implementation among several (remote-Mac-over-SSH,
   container, cloud sandbox), not a hardcoded assumption.
3. A spare Mac can be brought online as a worker with a documented, repeatable setup.
4. Preserve harmonik's existing operating model where it still makes sense:
   tmux-inspectability, the comms bus, the worktree→merge→review flow, subscription
   billing (see the auth constraint below).

## The two framing questions to settle FIRST (operator flagged these)

**Q1 — Connect-in vs. spin-up.** Does harmonik *connect into an already-provisioned*
remote environment (someone/something else stands up the box/VM/container; harmonik
just gets a handle and runs work), or does harmonik *itself provision* the
environment (calls Docker/k8s/a cloud API to create a sandbox per bead/crew, runs,
tears down)? Likely answer: a thin provider interface where "connect-only" ships
first and "managed/provisioned" is a later provider — but this must be confirmed.

**Q2 — What is the unit of remote placement?** Options, possibly layered:
  - the **bead-work** (implementer Claude + worktree) — biggest resource win, the
    daemon spawns/monitors a session on a remote host;
  - the **crew** (a long-lived orchestrator Claude) — runs on box B, still talks to
    box A's comms bus over the network;
  - the **whole daemon** and/or the **captain** — heavier, probably out of scope v1.

## Non-goals (explicitly out of scope, at least for v1)

- Building a general-purpose cluster scheduler. (Lean on existing tools.)
- Auto-scaling / elastic cloud fleets. (Manual/declarative worker registration first.)
- Multi-tenant / multi-user isolation hardening beyond single-operator needs.
- Moving **crews, the captain, or the daemon** off box A — DECIDED (D3): all stay on
  box A; only per-bead task work goes remote. (Crews-local is simpler AND preferred.)
- Replacing tmux as the *inspectable* session layer where a worker is a real shell.

## Constraints

- **Auth/billing (suspected make-or-break).** Claude Code subscription auth is
  interactive-login-bound; headless/ephemeral environments may force API-credit
  billing (cf. the credit-burn incident). A persistent remote Mac with a one-time
  interactive login likely preserves subscription billing; ephemeral containers may
  not. *(Research agent R5 is confirming this — it may rule out whole branches.)*
- **Tmux coupling.** The current spawn/keeper path assumes local tmux (`send-keys`,
  `capture-pane`, local PIDs). Remote execution needs an abstraction here.
- **Comms bus reach.** The bus is a local unix socket today — crews on another host
  need network reach (or a relay). *(R6 confirming.)*
- **Code sync.** Repo + worktree state must get to the worker and results (commits)
  must come back race-safely into the one-at-a-time merge flow.
- **Don't regress single-box operation.** Local-tmux must remain a first-class
  substrate; remote is additive.
- **Disk/CPU on box A** is the pain we're solving — the solution must not just move
  the bottleneck or add heavy local overhead.

## Success criteria (concrete, verifiable — DRAFT, sharpen after requirements)

- S1: A bead dispatched by the daemon on box A executes its implementer Claude
  session on box B, commits, and the result merges into main on box A — with no
  manual per-bead intervention.
- S2: With one worker box added, sustained concurrent-bead throughput exceeds the
  single-box `--max-concurrent` knee without box A hitting disk/RAM limits.
- S3: Bringing a fresh spare Mac online as a worker is a documented procedure that
  completes in under [TARGET] minutes.
- S4: Billing for remote sessions lands on the subscription (or the chosen branch's
  billing is an explicit, accepted decision — not an accident).
- S5: A remote worker dying mid-bead is detected and the bead is recovered (re-queued
  or cleanly failed), not silently wedged.

## Decisions locked (operator, 2026-06-14)

- **D-Q1 (connect-in vs spin-up):** ONE provider interface; **connect-in first** (infra is
  already up, harmonik connects). Spin-up is a later provider, not a fork.
- **D-Q2 (unit of placement):** Only **per-bead task work** relocates. Crews + captain +
  daemon + comms bus all stay on box A. (D3 — the big simplifier.)
- **D1 (v1 target):** Anchor v1 on **Phase 1 = remote Mac over SSH, bead-work only**
  (renamed from "Phase 0"). See `PHASE-1-DESIGN.md`.
- **D2 (billing):** Subscription billing is a **hard MUST** — API-credit billing is too
  expensive. Phase 1 → one-time interactive login on the worker. Never set `ANTHROPIC_API_KEY`.
- **D3 (crews):** Crews do **NOT** run remotely. They run on box A and push task work to
  remote workers. Removes the need for network bus transport and remote `--remote-control`
  in v1.
- **Egress:** two-phase / locked-down egress is desirable but is a **Phase 2 (container)**
  capability; some beads need network for tests → egress must be per-bead-configurable.
  Phase 1 (bare remote Mac) has open egress like today.
- **D4 (worker OS):** V1 remote = **macOS** (near-zero setup, identical to box A's model,
  preserves interactive-login subscription, no new env for agents). Linux/containers are a
  deliberate Phase 2 (where the `CLAUDE_CODE_OAUTH_TOKEN` headless-subscription path applies).
- **D5 (scope discipline):** Ship V1 **working → tested → deployed → running** BEFORE any
  Phase-2 work. Governing constraint. V1 = single configured macOS worker over SSH.

### Still-open (downstream, not blocking)
- Setup-burden target per worker (S3 time target).
- NFR latency/throughput numeric targets (S2/S3).
- The Phase-1 design decisions DD1–DD5 (see `PHASE-1-DESIGN.md`) — resolved in analyze/spec.
