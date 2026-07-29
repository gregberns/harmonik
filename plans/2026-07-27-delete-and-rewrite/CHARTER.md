# Charter — delete the test mass, then decompose the core

**Stable document. Changes rarely, by operator decision only.**
Read this before `HANDOFF-*.md`. The handoff tells you what happened last session; this tells you what
the program is and what "done" means. If the two disagree about intent, this one wins.

Siblings: [`_plan.md`](_plan.md) — deletion sequencing. [`NEXT_STEPS.md`](NEXT_STEPS.md) — the live
working document (findings, carry-forward, open re-assessments; changes constantly).
[`CARRY-FORWARD.md`](CARRY-FORWARD.md) — 83 facts about the outside world any rewrite must satisfy.
[`../../PRINCIPLES.md`](../../PRINCIPLES.md) — the engineering standard all of this is built to.

Evidence gathered 2026-07-28, kept because it is expensive to re-derive and was otherwise about to be
lost to a session temp directory: [`UNWIRED-INVENTORY.md`](UNWIRED-INVENTORY.md) (what is built but not
connected), [`DECOMPOSITION-MAP.md`](DECOMPOSITION-MAP.md) (what is inside the run machine and where the
real seams are), [`SPEC-TRIAGE.md`](SPEC-TRIAGE.md) (which requirements are trustworthy),
[`FLYWHEEL-ARCHAEOLOGY.md`](FLYWHEEL-ARCHAEOLOGY.md) (what the deleted subsystem was and what replaced
it). **These are findings, not plans — nothing in them is scheduled work.**

---

## 1. Why this program exists

The codebase was not recoverable by extraction and decomposition in place. Two things made every
attempt fail:

- **A test suite of 525,000 lines against ~216,000 lines of product** (488,000 by the time this
  program started, after an earlier deletion), roughly half of it files named
  after a ticket ID that pinned internal function signatures rather than behavior. Any restructuring
  broke hundreds of tests that were never protecting anything. Traced to a real instruction that told
  agents to prefix test *helpers* with a per-ticket name, which agents generalized to whole *files*,
  plus a second rule to write scenario tests and never run them. The helper-prefix rule is removed; the second was amended to close the loophole explicitly.
- **A run machine that generates the bugs.** `internal/daemon/workloop.go` is 6,520 lines with a
  single 2,289-line function carrying ~40 distinct responsibilities and touching every external tool
  the system uses. `reviewloop.go` and `dot_cascade_core.go` are the same disease — all three
  re-implement the identical launch → dispatch → wait → probe → teardown sequence.

Fixing bugs inside that structure produced more debt. The program is to remove the obstruction, then
rebuild the centre.

## 2. The sequence

**Phase 1 — delete. COMPLETE 2026-07-28.** ~225,000 lines removed in four steps: prose-grepping
sensors, ticket-named tests, dead production code, unwired harness types. Zero production behavior
changed, zero new test failures. Details and the exact carve-outs are in `_plan.md` and `NEXT_STEPS.md`.

**Phase 2 — partition.** Config-driven enable/disable of each subsystem, so the system can run as a
small subset. First switch landed 2026-07-28. See §3.

**Phase 3 — decompose and rewrite the core.** `workloop.go` + `reviewloop.go` + `dot_cascade_core.go`
as ONE unit — rewriting one leaves the duplication intact.

**Specs come after, not before** — with the standing caveat that `AGENTS.md` holds specs normative,
so a conflict is adjudicated, never silently ignored.
**Why they come after:** 25% of requirement IDs appear in no Go file; ~28% of those are stale
and seven would actively regress working code if obeyed. `specs/` is not a trustworthy oracle yet.

## 3. The core set — DECIDED, not proposed

The minimum set for the system to accept a unit of work, run an agent on it, and land the result:

> **config → event bus → queue → bead-ledger adapter → worktrees → harness registry + one substrate →
> work loop → merge**

Operator, 2026-07-28: *"Comprehensive but tight."*

**Everything absent from that list is deferred by default rather than by argument.** Adding to it
requires a reason. Outside it: comms, crew, captain, keeper, dashboard, live-state, subscribe, the
sentinel — and the socket listener itself (`harmonik run <bead-id>` already runs work without it).

**A rewrite that ends with only the queue and bead processing working is a SUCCESS, not a partial one.**
Operator, 2026-07-28: *"I'm COMPLETELY ok if we get done with part of the re-write and the only thing
that 'works' or is hooked up is the queue + beads processing. Comms, crews, keeper, all can come later."*

**The reviewer is wrongly fused INTO the core** — welded into the work loop instead of being a
switchable stage, which is a large part of why `beadRunOne` is 2,289 lines. Unfusing it is in scope.

**The socket listener stays OUT of the core — operator decision, 2026-07-28.** The daemon opens a Unix
socket and the CLI is a thin client over it; `specs/process-lifecycle.md` PL-003 says clients MUST
communicate with the daemon *exclusively* that way. Operator: *"I'd think it would be best to structure
that out of the core — then the way it communicates with the core is essentially indirect.
`process-lifecycle.md` is a spec that probably needs to be updated. If this is a critical aspect and/or
if it is holding back refactor, then it should probably be extracted."* So the *placement* is decided —
the socket is outside the core, the core is reached indirectly, and the socket becomes one caller among
possible others. The *extraction work* is not scheduled: **extract it when it blocks the decomposition,
and amend PL-003 rather than obey it.**

⚠ **The evidence once offered for this was false and is corrected here.** This section previously said
`harmonik run <bead-id>` "already runs work without it." It does not: `cmd/harmonik/run.go` sets
`ProjectDir` in its `daemon.Config` exactly as `main.go` does, so `bindSocket` runs and the socket
subtree is constructed and bound. (Not quite all of it — `harmonik run` sets no subscription-token
ceiling, so the bandwidth tuner is skipped — but the listener itself is there.) There is no production
path today that runs work without the listener. The decision stands on its own merits; it never rested
on that claim.

**And the sequel is named, not scheduled.** Operator, same day: *"once we have the core system working
again, before we do anything else we probably need to think about how to build a dataplane that
everything else communicates on/through."* That is the next design question after §6's "done" — it is
not part of this program and must not be started early.

**The `$TMUX` fail-fast is CLEARED to come out IF removing it buys real flexibility — operator decision,
2026-07-28, reopening locked decision #4.** The condition is the operator's and is load-bearing: it is
the implementation's job to evaluate it, not to assume it. If the investigation finds tmux is genuinely
load-bearing at dispatch rather than merely checked at boot, reporting that is the correct outcome.
The daemon hard-refuses to boot outside a tmux session. Locked decision #4 says *"Agent runner (S04):
NTM-wrapped Go. Inspectability via tmux is a requirement, not a preference"*, and
`specs/process-lifecycle.md` PL-028b calls a daemon reaching the dispatch loop without `TMUX` a defect.
Operator: *"If we get more flexibility from removing that, then do so. Seems like another 'crossed
wires' issue where the underlying reasoning was lost. Maybe it was from before when not run as a daemon.
Doesn't matter — but we should probably be able to run it any way."*

Read the scope precisely: **the daemon must be able to run without tmux. tmux is not being removed.** It
stays the default and stays the way an operator inspects live agents; what goes is the hard fail-fast
that makes it the only possibility. A capability genuinely unavailable without tmux should be announced
loudly at boot and degraded honestly — never faked, and never deferred to a crash on first dispatch.
Whatever the implementation requires of PL-021b / PL-028b is a **named spec amendment**, adjudicated,
not a silent violation.

This is the second time a constraint here turned out to be an inherited assumption nobody had reopened —
see §5, "a claim that sounds like a blocker gets repeated as one."

Note also that `specs/execution-model.md` EM-061 already defines "Core" conformance as a larger set
than §3's list — §6's "nothing else is required" is this program's target, not a redefinition of that
normative term.

## 4. Standing rules

- **Consolidate by default.** Operator, 2026-07-29: when the same thing is done in three places, that
  is the thing this program exists to remove — so **the default position is to consolidate**, and it is
  keeping the copies that needs the argument. This is the general form of §1's second cause: duplicated
  logic drifts, the compiler stays silent, and the drift is found later as a bug. Note what the default
  costs you if you get it wrong: consolidating wrongly is one visible change you can revert, while
  leaving three copies is an invisible divergence that keeps compiling.
  **Preserving a divergence is a real option, but it is a decision, not a deferral** — it needs a
  written reason, a test that fails when someone tidies it away, and a named trigger for revisiting.
  Beware the shape that already caught us once: a per-site parameter that is a *second* gate in front of
  a real one. A second gate can only subtract, so it can only ever silently disable something the
  configuration says is on.

- **Segment, then stitch.** Separate subsystems first, compose them back explicitly at a composition
  root. Not entangled-but-documented-as-separate, which is the current state.
- **The queue is the centre.** Where effort is contested, it goes to the queue and the bead path.
- **Composability is the test of the design.** If the system cannot run without a subsystem present,
  that subsystem is entangled with the core and the entanglement is the defect.
- **"Off" means never constructed**, not constructed-and-inert. Inert code still holds the composition
  root hostage — and this codebase already does inert badly.
- **Bad tests are worse than bad production code.** They make the production code harder to fix while
  claiming it is protected. When a test in your path does not earn its keep, deleting it is part of
  the work, not a separate task or a permission you need.
- **We are not fixing bugs.** Operator, 2026-07-28: *"We spent weeks fixing bugs in a rotten system and
  building more and more tech debt."* Defects found along the way are recorded, not chased.
- **Unwired code is NOT deleted by default.** Operator, 2026-07-28: *"I wouldn't want it to be thrown
  away by default — instead that section of code should be identified, enumerated, and we'll go through
  that later."* Roughly 20–25% of the system is built but not wired
  ([`UNWIRED-INVENTORY.md`](UNWIRED-INVENTORY.md)). **Age is the signal:** code written in the last 3–4
  weeks may be refactor work that simply never got connected; older code may be a feature that was never
  finished. Those need different decisions and neither is "delete".
  **The enumeration itself is deferred** — do not spend time cataloguing unless the work demands it. The
  one exception: **if unwired code blocks the rebuild, extract it rather than fight it**, or put an agent
  on working out how.
  The policy / control-point / enforcement layer is the largest cluster and is a runtime no-op today;
  treat it as a candidate to add back to the *configured* system later, not as dead weight.
- **Carry-forward facts are the guardrail.** ~72% of the bug history evaporates in a rewrite because it
  was structure-caused. The remaining ~28% — how tmux, the Claude TUI, codex, pi, `br` and ssh actually
  behave — must survive. `CARRY-FORWARD.md`.
- **Use the enforcement levers that already exist; do not build new ones** without a reason that
  survives being questioned.

## 5. What keeps going wrong — read before trusting any measurement

Every one of these cost real time in this program. They are patterns, not incidents.

- **A mechanical rule that infers intent from a coarse signal, then acts on it.** The movement governor
  scored "no commits" as "stalled" and would have killed the daemon 4,520 times in five weeks. The same
  shape recurs in idle-reaping, bandwidth tuning, and the queue's stop mechanisms. The honest answer is
  always agent judgment or an operator-set number — never an inference.
- **Plan estimates do not survive contact.** Step 3 was planned at 4,276 lines and delivered 1,744;
  step 4 at ~18,000 and delivered 860. Re-derive every number before acting on it.
- **Dead and unfinished look identical from a call graph, and have opposite dispositions.** A 1,005-line
  durable queue substrate reached only from one test is unfinished and load-bearing, not dead.
- **The compiler is not an oracle for deletion.** Three separate near-misses compiled clean: a package
  `TestMain` whose loss broke test isolation, files that only fail under a non-default build tag, and a
  hardware oracle whose `-run` filter silently matched zero tests and exited green.
- **A claim that sounds like a blocker gets repeated as one.** A requirement was cited as forbidding
  subsystem configuration; nobody opened it until the operator asked. It said nothing of the kind.
- **Scope qualifiers license partial work.** A banned term ("MVH") was used to justify implementing
  roughly a quarter of a feature and calling it done. ~20–25% of the system is built-but-unwired.

## 6. What "done" looks like

The queue and bead-processing path runs, is genuinely separable, and is built to `PRINCIPLES.md` —
pure core with effects at the edge, consumer-owned ports, time injected rather than called, one explicit
state machine with a single writer, and tests that fail when behavior breaks. Every other subsystem is
switched off by configuration and absent at runtime, and switching one back on is a one-line change.

Nothing else is required to declare the core done.
