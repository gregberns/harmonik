# Concepts

Harmonik runs AI coding agents on your backlog, unattended. The big idea is a split:
a **deterministic Go skeleton** — the daemon, the queue, the merge pipeline, the review
gate — wraps **probabilistic organs**, the large-language-model agents that actually
write the code. The skeleton is the part you can reason about and trust to behave the
same way every time; the agents are the part that scales to do many things at once. A
useful shorthand for the whole design is "deterministic skeleton, probabilistic organs."

This document explains each moving part in plain English: what it is, why it exists, and
how it fits the whole. The concepts are ordered so each one builds on the last. For the
bird's-eye view and the system's limits, see [OVERVIEW](OVERVIEW.md); to actually run it,
see [QUICKSTART](QUICKSTART.md) and the [OPERATING-GUIDE](OPERATING-GUIDE.md).

---

## Bead

A **bead** is a single, self-contained unit of work — one issue or task, the kind of
thing you'd file as a ticket. "Fix the login redirect" is a bead. Each bead has an
opaque ID that looks like `hk-abc12`. Beads are the atom of everything harmonik does:
you don't tell the system "go improve the app," you give it a bead, and it works that
one bead from start to finish. Beads exist because work has to be discrete and trackable
before a machine can pick it up, run it, and report back whether it succeeded.

## The beads ledger (`br`)

Beads live in a small local database — a SQLite ledger mirrored to a JSONL file checked
into your repo — managed through a command-line tool called `br`. This ledger is the
project's task list: you create beads with `br create`, inspect them with `br show`, and
ask "what's ready to work on?" with `br ready` (which only returns beads whose
dependencies are all met). The ledger is the source of truth for *what work exists* and
*what state each piece is in*. One discipline matters: the daemon, not you and not the
agents, owns the status changes that mark a bead claimed, finished, or reopened. Humans
and agents add comments and labels; the machine moves a bead to "done" only when the
work has actually landed. That separation keeps the ledger honest.

## Daemon

The **daemon** is the long-running background process at the center of everything — one
per project. Its job is to pull beads off a queue and, for each one, launch an AI coding
agent (a Claude Code session) to do the work in isolation. The daemon then commits the
result, merges it, pushes it, and closes the bead. You start it once and leave it
running, typically in a detached terminal session so it survives you closing your laptop.
The daemon is the deterministic skeleton in action: it doesn't write code, it
*orchestrates* — spawning, watching, merging, and recording — so that the unpredictable
part (the agent) runs inside a predictable frame. There is exactly one daemon per
project, enforced by a lock; a second one trying to start will refuse rather than collide.

## Queue (main and named)

The daemon doesn't decide what to work on; you tell it, by submitting beads to a
**queue**. A queue is just an ordered list of beads handed to the daemon. There's a
default queue called **main**, and that's all most projects ever need. But you can also
create **named queues** — separate, parallel work streams that run side by side. Each
named queue gets its own pool of workers, all sharing one global ceiling on how many
agents can run at once (set by `--max-concurrent`). Named queues exist so that two
independent efforts — say, a bug-fixing push and a feature epic — can run in parallel
without stepping on each other or fighting over the same slots. A queue is submitted as a
small file (or a single `--beads` flag); the daemon hands you back a `queue_id`. Queues
come in two shapes: a **wave** is a fixed batch dispatched all at once and frozen after
submit, while a **stream** stays open so you can append more beads to it while it's still
running — the right choice for an ongoing daily loop.

## Worktree and one-at-a-time merge

Every bead runs in its own **git worktree** — an isolated checkout of the repo, separate
from your working copy and from every other bead's. The agent for that bead edits, builds,
and commits entirely inside its own worktree, so concurrent beads never see each other's
half-finished changes. When a bead's work is done, the daemon merges that worktree's
branch back to the target branch **one at a time** — never two merges racing each other.
If a bead's changes would conflict with what's already landed, the daemon doesn't get
stuck or guess; it simply **auto-skips** that bead and moves on, leaving the conflict for
a human or a fresh attempt. This is how harmonik runs many agents in parallel yet keeps a
clean, linear, conflict-free history: parallel work, serialized landing.

## Review-loop

By default, no bead merges on the implementer agent's say-so alone. After the implementer
commits its work, a **separate reviewer agent** reads the change and renders a verdict —
approve, request changes, or block. If it requests changes, the implementer iterates and
tries again before anything merges. This **review-loop** is enforced by the daemon, not
left to good intentions, and it's on by default. It exists because a single agent marking
its own homework is the highest-risk pattern in automated coding; an independent set of
eyes catches the "looked right at the time" mistakes before they reach your branch.

## Epic

An **epic** is a parent bead that groups a set of related child beads — the umbrella over
a coherent body of work, like "ship the new billing flow." The epic itself isn't usually
worked directly; its children are. Epics exist to give a larger initiative a single
handle, so it can be owned, tracked, and reported on as one thing even though it's
dispatched as many small beads. As the next two concepts show, an epic is the unit a crew
takes responsibility for.

## Crew

A **crew** is a long-lived orchestrator agent that owns exactly one epic and one named
queue. It watches its epic for ready children, submits them to its own queue, monitors
them to completion, and reports progress — but it never touches the shared main queue, and
it never closes beads itself (the daemon owns that). A crew persists across many beads and
even across restarts. Crews exist so that a big, multi-bead initiative has a dedicated
driver keeping it moving, isolated from other initiatives by its own queue. Think of one
crew per lane of work, each minding its own epic.

## Captain

The **captain** is the top orchestrator that sits above the crews. It organizes the open
backlog into **lanes** — one lane per initiative — and assigns each lane to a crew,
spinning up a crew per lane and handing it a mission. When a crew finishes its epic, the
captain re-tasks it to the next-ranked lane; if a crew goes silent, the captain notices and
surfaces it. The captain exists to keep the whole fleet busy without a human micromanaging
assignments: it consumes an existing priority ranking and staffs the work, only stopping to
ask a person when a genuinely new judgment call comes up (a brand-new initiative nobody has
prioritized, declaring a crew failed, or anything destructive). Captain organizes, crews
drive, the daemon executes.

## Comms bus

When several agent sessions run at once — a captain plus its crews, or multiple
orchestrators — they coordinate through the **comms bus**, the agent-to-agent messaging
surface reached via `harmonik comms`. Agents send directed or broadcast messages, receive
their inbox, announce presence ("who's online?"), and read the conversation log. The bus
exists because concurrent agents need to hand off work, warn each other before touching a
shared resource, and report status without garbling a shared file or racing each other.
One detail every consumer must honor: delivery is *at-least-once*, so each message carries
a unique ID and recipients must ignore any duplicate they've already handled. (See
[CLI-REFERENCE](CLI-REFERENCE.md) for the exact `comms` commands.)

## Human-in-the-loop decisions

Sometimes an agent reaches a point it cannot proceed past without a human call: a risky
migration, a policy question, a choice between options that only the operator can weigh.
The **decisions** surface is the agent→human dual of the comms bus — instead of messaging
another agent, the agent raises a question that a human must answer.

An agent calls `harmonik decisions raise` with a question and a set of allowed options.
The daemon mints a `decision_id`, emits a `decision_needed` event, and returns the id to
the agent. The agent then either polls with `harmonik decisions wait <id>` or passes
`--wait` to `raise` to block inline. Meanwhile the daemon tracks the decision as *open*
until a terminal arrives.

An operator sees every open decision with `harmonik decisions list` — the "what-needs-me"
queue — and resolves one with `harmonik decisions answer <id> <option>`. The daemon
validates that the chosen option is one of the raised options, emits `decision_resolved`,
and wakes the blocked agent. Only the **first** answer counts: resolving an already-answered
decision is a no-op (first-writer-wins), so concurrent operators cannot race each other
into an inconsistent state.

An agent can also cancel its own open decision (`harmonik decisions withdraw`) if it
determines the question is no longer relevant — for example, if the bead that prompted it
was withdrawn.

Two housekeeping rules keep the open-decision list clean. An open decision whose blocked
agent has gone offline for more than ~10 minutes is flagged **orphaned-pending** in the
list output; the **keeper** tick then emits `decision_withdrawn(orphaned)` to reap it.
The displayed flag is read-only — no event is emitted by the list command itself.

Three event types drive the whole surface: `decision_needed` (raise), `decision_resolved`
(answer), and `decision_withdrawn` (withdraw or keeper-orphan-reap). These are durable in
the `events.jsonl` log, so the daemon can reconstruct the open-decision set from scratch
after a restart. (See [CLI-REFERENCE](CLI-REFERENCE.md) for the exact `decisions` commands
and [OPERATING-GUIDE](OPERATING-GUIDE.md) for the operator workflow.)

## Keeper

Any long-running agent eventually fills its context window — the amount of conversation it
can hold at once — and degrades. The **keeper** is a watcher that prevents that. It tracks
an agent's context use and, as it climbs, first injects a warning. For an agent that has
explicitly opted in (marked as *managed*), the keeper goes a step further: when the agent
crosses a higher threshold and is between tasks, it writes a handoff note, clears the
agent's context, and resumes the same session fresh — so the agent picks up where it left
off without the bloat. Today that automatic clear-and-resume cycle runs only for opted-in
agents; for everything else the keeper is warning-only. It's careful either way: it won't
cycle an agent mid-dispatch, it backs off to warning-only while a human is attached to the
session, and a keeper restart is a non-event for the rest of the system, because the
durable state (queues, ledger, comms) survives the refresh. The keeper exists so a captain
or crew can run indefinitely rather than grinding to a halt when its window fills.

## Integration branch

By default the daemon merges and pushes finished work straight to `main`. That's fine for a
personal or throwaway repo, but dangerous for a real product repo with branch protections.
An **integration branch** is the safety valve: you point the daemon at a separate target
branch (for example `integration`) so it merges and pushes *there* instead, and you mark
`main` as protected so the daemon fail-closes — it refuses to push a protected branch under
any condition, checked at startup, at every dispatch, and during merge. Promoting the
integration branch into `main` then stays a deliberate human step, usually via a pull
request. This concept exists so harmonik can run against repos where auto-pushing `main`
is simply not allowed. (Configuration details are in [CONFIGURATION](CONFIGURATION.md).)

## DOT workflows

So far every bead has followed the same built-in path: implementer, then reviewer, then
merge. A **DOT workflow** lets you define a richer, multi-step process for a bead as an
explicit graph, written in the Graphviz DOT format. The graph's nodes are the steps — an
AI step, a plain scripted step, a decision gate, or a call into a sub-workflow — and its
edges route between them, including branching on how a step turned out. DOT workflows exist
for work that doesn't fit the default one-shot shape: when a bead needs a sequence of
distinct phases with conditional routing, you describe that shape once as a graph and the
daemon walks it. It's the extension point that turns the fixed implement-review-merge loop
into an arbitrary, author-defined process.

This is an early, emerging capability rather than the everyday path. The built-in
implement-review-merge loop is what virtually all work runs on today; DOT workflows are the
forward-looking way to go beyond it, and the surface is still settling. Reach for the
default loop first, and treat DOT workflows as the place harmonik is growing into.

## Scheduled jobs

Most of what the daemon does is reactive — it works the beads you put in front of it. A
**scheduled job** lets the daemon also act on its own clock: a recurring job that fires at a
set time without anyone submitting anything. A scheduled job is a simple pairing of two
things — a **schedule** (when to fire) and an **action** (what to do when it does). It's
deliberately generic: harmonik itself owns the firing, checking on each pass of the daemon's
normal work loop whether any job is due, so a schedule is not tied to crews or to any one
kind of task — it's a plain recurring trigger the daemon honors.

There are two kinds of action a job can take. A **command** action runs an ordinary shell
command — a script, a maintenance task, anything you'd run from a terminal. A **spawn-crew**
action starts a crew, exactly as if you'd launched one by hand, which means a scheduled crew
spawn is billed and guarded the same way every other crew spawn is. Starting a crew is just
*one* thing a schedule can do, not the reason schedules exist; the primitive knows nothing
about crews beyond it being one available action.

The cadence in this first version is daily: a job fires once a day at a wall-clock time you
choose, in your local timezone or a named one. Two sensible behaviors keep it well-mannered.
First, because the daemon can't fire while it's switched off, a job that misses its time
while the daemon was down will catch up — but only once, and only if the missed fire was
recent (within the last day). A daemon that was off for a week therefore fires a single
catch-up run on restart, never a week's backlog. Second, if a job's previous run is somehow
still going when its next fire comes due, the daemon skips the new fire rather than stacking
a second copy on top. Both behaviors can be turned off per job if you want the raw cadence.

You manage scheduled jobs with the `harmonik schedule` command — adding, listing, enabling,
disabling, removing, and firing one on demand. The schedule list lives in a small file the
daemon reads, so changes you make take effect on its next pass whether or not it was running
when you made them. *Available after the next daemon rebuild.* (See
[CLI-REFERENCE](CLI-REFERENCE.md) for the exact `schedule` commands.)

---

## How it all fits together

Work starts as **beads** in the **`br` ledger**. You submit a batch of beads to a
**queue** — the default **main** queue, or a **named queue** for a parallel stream — and
the one-per-project **daemon** picks them up. For each bead the daemon spawns an AI agent
in an isolated **worktree**; an implementer writes the code and a **review-loop** has a
second agent check it before the daemon merges the result **one bead at a time** to the
target branch — `main` by default, or a protected **integration branch** when auto-pushing
`main` isn't safe. For larger efforts, an **epic** groups related beads, a **crew** owns
each epic and drives it on its own queue, and a **captain** organizes the whole backlog
into lanes and staffs a crew per lane. These concurrent agents coordinate over the **comms
bus**, and a **keeper** keeps each long-running agent fresh as its context fills. When an
agent reaches a decision it cannot make on its own, it raises a **human-in-the-loop
decision** and blocks; the operator answers via `harmonik decisions answer` and the agent
continues. When a bead needs more than the default implement-review-merge path, a **DOT
workflow** defines that process as a graph. Recurring work — a periodic harvest, a nightly
job — runs as a **scheduled job** the daemon fires on its own clock. Throughout, the
deterministic Go skeleton — daemon, queue, merge pipeline, review gate — is what makes the
probabilistic agents safe to turn loose at scale.
