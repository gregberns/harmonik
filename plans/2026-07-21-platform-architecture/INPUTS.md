# Inputs — everything on the table (operator, 2026-07-21)

Captured before solutioning, so nothing is lost or re-litigated. Grouped, not ranked.

## 1. Confirmed decisions carried in

- **Scrap ssh-per-node remote execution** (realignment D4). ssh is a bad architecture for
  this. Its wedges are moot.
- **Pull-vs-push:** operator "generally likes" the pull-based-worker direction but wants to
  **think harder and broaden the options** — NOT locked.

## 2. Architecture options on the table (all OPTIONS to investigate — none chosen)

- **Option 1 — Pull-based resident worker (main agent's proposal).** A resident harmonik
  worker on each machine dials OUT to the primary, pulls beads, runs each in a container
  locally, streams status back, delivers results via shared git. (CI-runner model.)
- **Option 2 — Central harmonik server + registered workers/crew (operator).** A harmonik
  server runs SEPARATE from the crew and holds the queues. Crew can run on one machine and
  talk to the central server (co-location optional). Multiple workers call the central node;
  all workers/crew **register** with it. If a worker/crew drops off it can't pull more work
  — OPEN: what to do about its in-flight active work.
- **Option 3 — Minimal harmonik-in-the-container (operator).** A minimal-mode harmonik
  instance starts INSIDE the container with a bead reference + a pointer to a 'central
  harmonik'. It opens a websocket to the server, pulls the bead + its DOT topology/plan,
  executes, and reports progress back as the bead advances.
- **(there are likely more options to enumerate + investigate — operator expects this)**

## 3. The foundational architecture the operator actually wants to target

From `plans/2026-07-15-agent-substrate-v2` — **its premise is WRONG** (it frames this as
"sharing data"; it is really about **a strong underlying architecture to build on**), but
**the architecture is the target**:

- A **data transport layer** hosted by the daemon that can connect multiple machines
  together.
- An **identity / address system** so plugins/components know what other systems are
  available.
- Plugins use the transport layer to **generically move information** across it.
- Provides **general tools with consistent interfaces** — does NOT mush all tooling into a
  bundle that's hard to test and manage.

## 4. Decoupling / modularity concerns (suspected tight coupling that "has burned us")

> CORRECTED 2026-07-21 by the code survey (`research/A4`) + Fable (`research/A5` §4c). The
> operator's INSTINCT is right — coupling has burned us — but the location was mis-stated.
> Correcting it makes the case STRONGER and more actionable, not weaker.

- **keeper and watchdog are actually CLEAN** — the survey found the code CI-bans keeper from
  importing the daemon/workloop; watch is a pure event-bus consumer. The suspected coupling
  here is **unfounded**. (Aim effort elsewhere.)
- **The real damage is concentrated in TWO god packages:** `internal/daemon` (641 files —
  the composition root that also CONTAINS the claude/codex/pi harness implementations, queue
  wiring, ssh transport, crew wiring, and the DOT run-loop) and `internal/core` (501 files —
  an undifferentiated shared-types kernel).
- **Agent-harness coupling is MIXED — and this is the sharp one:** the harness *contract* is
  clean (`Substrate` = one method, zero bead/queue/DOT/crew surface), BUT the concrete
  Claude/Codex/Pi harness *implementations live inside `internal/daemon`*. That's why the
  codex churn thrashed the whole daemon — the seam is **defined but not extracted**.
- General thesis, refined: harmonik is **not a formless monolith** (60+ packages, CI-enforced
  dependency matrix) — the disease is the two god packages + the trapped harness impls. The
  fix is **extraction behind seams that already exist**, not inventing new ones.

## 5. Context to take into account (not necessarily central)

- **Scheduling** work across local / remote / harnesses / providers, etc.
  (`plans/2026-07-09-scheduling-assessment`). Less directly relevant but must be considered.
- **incus** container runtime is set up and available: on gb-mbp there's an Ubuntu VM
  running incus that can run containers (`plans/2026-07-19-incus-container-remote-mode`).

## 6. Prior operator plans to absorb (operator has raised these before, felt dismissed)

- `plans/2026-06-30-distributed-fleet` — the distributed-system vision.
- `plans/2026-07-15-agent-substrate-v2` — the transport/addressing architecture (target).
- `plans/2026-07-19-incus-container-remote-mode` — incus container substrate.
- `plans/2026-07-09-scheduling-assessment` — cross-target scheduling.

## 7. Meta-goal / pattern to break

- Get the architecture CORRECT; build for the future, not a half-assed partial impl.
- **Not over-architecting** — a layered architecture that is easy to test and extend.
- The distributed-architecture discussion previously **dismissed the need for a powerful
  underlying system**; the operator thinks that underestimated it. This plan explicitly
  does not.
