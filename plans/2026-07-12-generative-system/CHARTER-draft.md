# Charter — first cut (admiral draft, 2026-07-13 · to be torn apart)

> A first cut of the thing everything derives from. Deliberately SHORT. Each principle is
> stated two ways — **reason-from** (so an agent applies it where no rule anticipated the
> situation) and **checkable** (so we can tell if it was honored). If a principle can't be
> stated checkably, it isn't a principle yet — it's a mood. This draft's job is to be
> argued with, cut, and sharpened — not adopted.

## Purpose (one line)

**Build a system that builds — one whose structure carries its own correctness, so a
thousand varied agent sessions stay pointed the same way and the system gets better without
getting heavier.**

The tool (harmonik) is the encoding of this system's attributes. If the system is right, the
tool it builds is maintainable; if the tool rots, the system was wrong.

---

## Principles (candidate set — 7)

### P1 — Correctness rides in the structure, not in vigilance.
**Reason-from:** prefer a shape where the wrong thing is hard to express over a rule that
asks an agent to remember not to do it. Push guarantees into types, boundaries, and gates —
so building it correctly *is* most of the proof (the Haskell "if it compiles, it works" pull).
**Checkable:** for each invariant, name the mechanism that enforces it (a boundary, a lint, a
type, a gate). "An agent will remember" is not a mechanism. Count of invariants guarded by
structure vs. by hope — the latter should trend to zero.

### P2 — Nothing exists that a person can't hold in their head.
**Reason-from:** bounded blast radius beats cleverness. A unit of work, a function, a module
should fit in one head. The 2300-line function is the anti-pattern this principle exists to kill.
**Checkable:** hard ceilings that *refuse* the violation (function/file/param/fan-out limits) —
not a review that hopes to catch it. The gate fails the build; no agent judgment required.

### P3 — Prove it before prod, off the path you're proving.
**Reason-from:** a thing under repair cannot be its own oracle. Evidence that it works must
not route through the mechanism being tested. Testability is a requirement *on* the design,
not a suite bolted beside it.
**Checkable:** every expectation has an out-of-band check (a diff/git/filesystem assertion, a
coverage number, an injected fault producing a terminal signal). No expectation is "done" on
self-report or a single green run.

### P4 — Silence is a defect.
**Reason-from:** the failures that hurt us (resume-hang, false-close) all *went quiet*. A
correct component makes its own state observable — it emits output, or `stale`, or an honest
error. Never nothing.
**Checkable:** every boundary emits a signal on both success and failure; a stall produces a
terminal event within a bounded time. A silent path is a bug, gated the same as a crash.

### P5 — Align by the environment, not by the briefing.
**Reason-from:** you can't script coherence into 1000 sessions. Put the direction and the
signals in the shared medium (repo, comms, beads, specs) where an agent *senses* them
locally — ants and pheromone, not a manual read once and hoped-obeyed. Where a principle can
be ambient or structural, it must not be an instruction.
**Checkable:** for each principle, ask "is this felt at the point of action, or only stated in
a brief?" Brief-only alignment is a known-weak mechanism and is flagged, not relied on.

### P6 — Remove before you add; keep the principle-set small.
**Reason-from:** the reflex on a new lesson is *fold it into an existing principle or delete
something* — not mint a new rule. A rule is a liability until proven load-bearing across many
sessions. Scarcity of principles is the feature that keeps them alignable.
**Checkable:** the principle-set has a hard cap (this set is 7; adding an 8th means retiring
one or proving the new one carries weight several couldn't). Net rule count trends flat or
down, not up.

### P7 — The system improves by selection, and prunes what's stale.
**Reason-from:** improvement is not authorship. What *repeatedly* produces sound, maintainable,
provable results distills up into a principle; a one-off scar dies. The metabolism — promote
what generalizes, garbage-collect what fired once — is what makes this a living system and not
a nicer rulebook. (This principle is the new construction; the others are things to encode.)
**Checkable:** there is an actual loop that (a) surfaces recurring patterns as principle
candidates and (b) flags principles/rules unused for N cycles for pruning. If that loop
doesn't exist and run, the system is static — a finding.

---

## The recursion (the constraint on all of the above)

The system that **builds** the tool must **run on these same principles.** If the builder
accretes stale rules (violating P6/P7) or ships a god-function in its own process (P2), it
cannot produce a tool that embodies them. Builder and built are one principle-set. Every
principle above is a claim about *both* harmonik-the-tool and the-agents-that-build-it.

---

## Next steps (a couple — where I think we go)

**Step A — Sharpen this set with the operator, then freeze it as v0.**
Cut, merge, rename. The goal is a set small enough to hold and sharp enough to check. Likely
tensions to resolve: is P3 ("prove before prod") really distinct from P1, or a corollary?
Is P4 ("silence is a defect") a principle or an instance of P1? Argue it down to the
load-bearing few. **Output:** `CHARTER.md` v0 — the frozen artifact everything derives from.

**Step B — Prove ONE principle against ONE real carve slice.**
Take the sharpest, most falsifiable principle — I nominate **P2 (nothing a person can't hold)** —
and encode it three ways: a structural gate that *refuses* an over-limit function, a review
lens, and a kerf requirement. Then run a real carve slice (the M3 daemon-god-function
extraction is the natural target) through it. **Falsifiable question:** does encoding P2
measurably change what the agents produce — does the 2300-line shape become impossible rather
than merely discouraged? If encoding a principle doesn't change outcomes, we learned that
cheaply, before betting the system on it.

**Step C — (after A/B) build the P7 metabolism loop.**
The one genuinely new construction. Everything else is re-derivation of tools we have; this is
the part that makes it *generative*. Design it only once A proves the principles hold and B
proves encoding changes behavior — otherwise we're automating a loop over principles we haven't
validated.

**Held open (the honest caution):** do NOT assume kerf / dot / crew / captain / admiral survive
as-is. They are hypotheses about coordination; re-derive the roles from this charter (gradient +
local rules + shared medium + selection), don't retrofit the charter onto the roles. Admiral's
real job may *be* the P7 metabolism.

---

## Status
- First cut. Not adopted. Freeze holds; nothing dispatches.
- Next action: operator tears this apart → we converge on the load-bearing few → freeze v0.
