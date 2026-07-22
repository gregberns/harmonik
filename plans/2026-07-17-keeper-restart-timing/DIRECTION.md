# Direction memo — keeper restart timing

Plan: `plans/2026-07-17-keeper-restart-timing/` · item 5 · author: xray
· drafted 2026-07-18 · **revised same day to record the operator's chosen direction.**

> **What this is.** The research is done (see `_plan.md`, `C6-findings.md`). This memo
> now records the **direction the operator chose on 2026-07-18** and hands it to a
> follow-on plan that will turn it into actual work. It does not build anything, touch
> keeper code, or move any threshold. The keeper's restart limits stay exactly where
> they are.

---

## The one fact that anchors everything

The keeper (the background process that restarts a full session) works like this today:
it drops a "please write a handoff" note into the agent's terminal, then **watches the
handoff file for exactly 5 minutes** for the agent to write it. If the agent takes longer
than 5 minutes — because it's finishing a task, or it's a crew that only wakes up once an
hour — the keeper **gives up**. The agent then writes its handoff to a keeper that has
stopped listening, and just sits there on a bloated context, never restarted.
(`cycle.go`, 300-second `DefaultHandoffTimeout`.)

That single fact is why every message the keeper sends must also **hand the agent the
command to restart itself.** The keeper's watch is a nicety; the self-run command is the
guarantee. Everything below follows from this.

---

## Two separate problems, two separate tracks

The research proved the leader sessions (the captain and admiral — the ones the operator
talks to) and the worker sessions (the crews) have **different** problems and need
**different** fixes. Don't merge them.

- **Captain / admiral** get interrupted mid-conversation, and the way the keeper types its
  note into the terminal steps on whatever the operator is typing. This is a **timing and
  delivery** problem.
- **Crews** don't have that — nobody's typing into a crew's terminal, and crews already
  restart at natural pauses. Their problem is that the restart **fails outright about one
  time in five** (a dead watcher, a handoff that gets thrown away, a killed job leaking
  stray processes). This is a **reliability** problem.

---

## Track A — captain / admiral (the operator's direction)

The keeper's nudge to a leader session should become **a comms message, not a keystroke
paste.** Right now the keeper types its note directly into the terminal, which lands on top
of whatever the operator is half-way through typing and submits it by accident. Sending the
nudge as a comms message instead — the same channel agents already use to talk to each
other — puts it into the agent's reading queue without touching the operator's input line.
The agent reads it at its next turn, the operator's typing is untouched.

The message itself should tell the agent to **hold off** in two cases, and always **carry
the escape hatch**:

1. **Deliver it as a comms message, not a terminal paste** — so it never steps on the
   operator's typing. *(This is the net-new piece: the keeper doesn't touch comms today.
   For it to land as a real prompt the agent must have its comms inbox actively armed —
   the captain/admiral do. Worth confirming that armed-inbox assumption holds before
   building on it.)*
2. **"If you're talking to the operator right now, wait."** The agent knows it's mid-
   conversation better than the keeper can detect from the outside; let the message tell it
   to finish the exchange first.
3. **"If you have work in flight, finish it, then hand off."** Same idea — land the restart
   at a real pause, not in the middle of something.
4. **"…and here's the command to restart yourself."** Because of the 5-minute-timeout fact
   above: if the agent takes its time (which points 2 and 3 explicitly invite), the keeper
   will have stopped watching by the time the handoff is written. The command lets the agent
   pull its own restart regardless of whether the keeper is still listening.

Two things to keep in mind while building this (both already established in the research):

- The self-run restart command **already mostly exists** (`restartnow.go`) — the leader
  sessions use a version of it today. This is largely wiring it in as the default and
  writing the message, not inventing a mechanism.
- **What counts as "a good pause" is something only the agent can judge, not the keeper.**
  The keeper can see "the agent finished a turn"; it cannot see "the agent is mid-plan."
  So the message must ask the agent to judge it, using a plain test (written up in
  `_plan.md` under Q3): *a good pause is when everything you'd need to continue is already
  saved to disk / the task ledger / a short handoff — nothing important lives only in your
  head.* Wire that test into the message so "good stopping point" isn't left vague.

---

## Track B — crews (the operator's direction)

Keep this **lighter and separate** from Track A. Two parts:

**1. The same "finish, then self-restart" message may be worth giving crews too — if it can
be made reliable.** A crew should be allowed to finish what it's doing and then run the
restart command itself. The self-run command matters even more for crews than for leaders:
a crew parked to wake once an hour can't possibly answer the keeper's 5-minute watch, so
self-restart-on-wake is the only thing that works for it. So the messaging idea is *not*
dropped for crews — it's kept, on the condition that the reliability holes below get fixed
first, or it'll just fail the same way.

**2. Fix the four reliability bugs (from `C6-findings.md`).** These are ordinary bugs, not
redesign:

- **The keeper's watcher can die silently with nothing to bring it back.** One crew's
  watcher died and nobody noticed until someone ran a manual health check. A restart can't
  happen if the thing that triggers it is dead — this needs detection and auto-revive.
- **A restart can throw away a handoff the crew actually wrote.** One crew wrote a perfectly
  good handoff and the reboot never read it ("no handoff on record") — it cold-started
  instead. If a crew pays the cost to write a handoff, the reboot must use it.
- **Restarting kills in-flight jobs uncleanly and leaks stray processes.** One crew's
  restart killed a running job mid-flight and left an orphaned process polluting the machine
  for 40+ minutes — it even corrupted that crew's own later debugging. Restarts should wind
  jobs down cleanly, not hard-kill them.
- **The handoff instruction the keeper types is garbled** — the file path and the
  instruction run together with no space between them. Cosmetic (crews figured it out every
  time) but real.

Plus one gap that isn't a bug: **a crew parked to wake once an hour is unreachable by a
5-minute watch.** Either the keeper should notice a parked crew and wait, or parking should
drop the context low enough that no restart is due. (The self-run command in part 1 largely
covers this too.)

---

## Keeper accuracy — ideas to make it aim better

Not a decision — a list of where the keeper currently mis-reads the situation, for the
follow-on plan to weigh:

- **It can't tell a slow-typing operator from an absent one.** It decides "is the operator
  here?" from a 5-minute-wide activity signal, so an operator composing a long message with
  pauses reads as *gone*, and a remote / phone operator never registers at all
  (`tmuxresolve.go`). Sharpening this signal is the single highest-value accuracy fix,
  because it's the root of the "typed on top of the operator" pain.
- **It checks "is the operator here?" only once, at the very start of a restart**, then runs
  a paste-and-submit and up to a 5-minute wait — an operator who starts typing *after* that
  one check gets run over. Re-checking during the wait would close that hole.
- **It equates "finished a turn" with "at a good pause."** Those aren't the same (see the
  Q3 note above). Any move toward a real task-boundary signal — even the simple version
  where a crew that stops just below the limit hands off *then*, rather than being cut at the
  limit — makes the aim better.
- **It doesn't check whether the agent is even reachable before trying.** The dead-watcher
  and parked-crew failures above are both "the keeper fired into the void." A liveness /
  reachability check before starting a cycle would turn silent failures into deferrals.

---

## Testing — build this in as part of the initiative

The operator's call: this work ships with **real testing, not just unit tests.** Unit tests
already exist for the keeper's threshold logic. What's thin — and what this initiative must
add — is testing that exercises the whole restart from end to end:

- **Integration and end-to-end tests** that drive a real handoff → clear → reboot cycle and
  assert it actually completed (and, for the failure cases, that it deferred cleanly instead
  of firing into the void).
- **Scenario tests using the twin** — the twin is a scripted stand-in for a real Claude
  session (`harmonik-twin-claude`) that lets a test play out a full restart without a live
  model. This is the right tool for reproducing the exact failures the research found: the
  operator-typing-collision, the 5-minute-timeout-then-late-handoff, the parked crew, the
  dead watcher, the discarded handoff, the orphaned process. Each of those should become a
  scenario test that fails today and passes after the fix.
- A note for the follow-on: there's an existing twin-vs-real parity audit
  (`docs/twin-parity-audit-2026-05-14.md`) worth reading first, so the new scenario tests
  cover the paths the twin actually reaches.

---

## What still needs the operator

Almost nothing — the direction above *is* the operator's. Two small confirmations for the
follow-on plan to pick up:

1. **The comms-delivery assumption.** Track A point 1 depends on the leader session having
   its comms inbox actively armed so a comms message lands as a real prompt. That's true for
   the captain/admiral today; the follow-on should confirm it before building on it.
2. **Filing the crew bugs.** The four Track-B reliability bugs are real and ready to file as
   issues now. I held off pending your say-so. Say the word and I'll file them (checking for
   duplicates first), or the follow-on plan can own them.

## Handoff to the follow-on plan

- **Track A** (leader sessions): build the comms-delivered nudge with the four message
  properties, defaulting to the agent's own restart command. Wire in the plain "good pause"
  test. Sharpen the operator-present detection.
- **Track B** (crews): fix the four reliability bugs; then, once reliable, extend the
  finish-then-self-restart message to crews.
- **Testing**: add integration / end-to-end / twin-scenario coverage for every failure the
  research named — each should fail before the fix and pass after.
- **Guardrails**: don't naively rip out the keeper's retry logic on the terminal (it's there
  because a single keystroke was getting dropped — `restartnow`/hk-89g), and **change none of
  the restart thresholds.**

Grounding for all of this: `_plan.md` (the full map, the case list, the Q1/Q2/Q3 answers)
and `C6-findings.md` (the crew reliability data).
