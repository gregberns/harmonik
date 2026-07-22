# DOT redesign — the coherent model (plain, general over any .dot)

> One correct source. Grounded in the actual code map (`research/01-current-impl/findings.md`) and the two
> adversary reviews. Written to generalize over ANY workflow graph — the implement→review loop is one example.

## The core problem (today)

Every node's document is built by **role-specific code**: `buildAgentTaskContent` for the implementer,
`buildReviewTargetContent` for the reviewer (`internal/workspace/agenttask_chb028.go`). Both are handed the
**whole bead body**, which contains the task *and* the review checklist. So the implementer sees the checklist
and does everything up front. Everything else (can't force a review round-trip, reviewer's own instructions
don't reach it, feedback is bolted on per-tool) follows from that.

## The mechanism (one idea, generalizes)

Four pieces, all general graph features — not special-cased to the default loop:

1. **Variables** — a run has a set of named values. Two origins:
   - set at launch, and
   - **produced by a node as it runs** (e.g. the reviewer produces `verdict` and `notes`).

2. **Per-node visibility** — each node declares which variables it can read. A node sees only what it declares
   (plus a small safe default). The implementer node does not see the `review` variable unless it declares it.
   *(This is the operator's decision: keep variables global, but the graph controls who sees what.)*

3. **Values on edges (producer → consumer)** — a node's produced output is read by a downstream node that
   declares it as an input. There is exactly **one** such pair in the default loop today: the reviewer produces
   `verdict`+`notes`; the implementer, on the bounce-back edge, consumes them. This replaces the
   `.harmonik/reviewer-feedback.iter-N.md` file — that file only exists because there was no input channel.
   The mechanism is general: any graph can have a node produce a value a successor consumes.

4. **One renderer** — replace the two hand-written builders with a single engine that assembles *any* node's
   document from: {that node's declared input variables} + {its `prompt`} + {its `role`}. Same code for every
   node and every tool (claude/pi/codex). Role separation is then structural — the implementer's document
   contains only its declared inputs, so the checklist cannot appear in it. The leak becomes *unexpressible*,
   and checkable when the graph is loaded.

## What deliberately stays out of variables

The **actual code** the implementer writes flows to the reviewer through the **shared git worktree** (the diff
is derived from base/head commit SHAs), exactly as today. It is **not** a variable and **not** an edge payload.

Consequence: we do **not** build a separate store for bulk content (diffs, transcripts). That was the
over-engineered part of the ambitious design the review rejected — it invents a store for bytes that already
live in git. Variables carry only **small instruction/control text**: task, rubric, verdict, notes, (later) a
plan. If a future graph ever needs a genuinely non-git bulk hand-off between agents, that's when a store earns
its place — not now.

## Types — minimal and strict

A declared variable may carry a type: `string` | `number` | `bool` | `enum(...)`, plus required/optional.

- The type that earns its keep immediately is the reviewer's **verdict enum**
  (`APPROVE | REQUEST_CHANGES | BLOCK`). Because the verdict is what edges branch on, the loader can check that
  the graph only routes on verdicts the reviewer can actually emit (catches an `APPROVE` vs `APPROVED` typo
  before any money is spent).
- Task / rubric / notes are just (multi-line) strings. Note: these must travel as **declared input values**,
  not as the old launch-time token splice (`__PARAM__`), because that channel rejects newlines by design
  (`internal/core/templateparams.go:91`, a shell-injection defense) and caps at 8 KB — a real rubric can't fit.
- No heavyweight type lattice, no JSON-embedded-in-DOT. Keep the declaration to one readable line per node.

## Why not the two extremes we tested

- **Ambitious ("type every node as a function"):** types a dataflow that mostly doesn't exist (the code
  hand-off is a git commit, not a value), invents a bulk store for bytes already in git, embeds JSON in DOT
  attributes (unauthorable), and rips out the working review path in one migration. ~5x the work; doesn't even
  improve model control. **Rejected** — but two ideas were extracted: the single renderer, and typing the
  verdict output.
- **Lean ("just use template params"):** technically broken — template params can't carry a multi-line rubric
  (newline ban), its enum types the launch inputs (which nothing branches on) rather than the verdict output,
  and its leak fix is convention (`goal`/`role` are broadcast to every node, so a natural
  `goal="…__RUBRIC__…"` re-leaks). **Rejected as specified** — but its cheap, correct feedback-file reuse and
  load-time checks survive into this model.

This model is the honest middle both reviewers converged on: the **single renderer + declared inputs +
per-node visibility + one produced verdict/notes value on the back-edge + verdict-enum load check** — and
**not** the artifact store, the JSON-in-DOT ports, or the big-bang deletion of the working path.

## The other two legs (unchanged, separate)

- **Models:** flip the priority so a per-bead "use Opus" beats the workflow file's default (make the node's
  `model=` an overridable default with a keep-cheap lock); fix the bug where pi/codex ignore a node's model;
  record the concrete model chosen per step per pass so replays are faithful; fail loud (don't silently
  downgrade) when an explicit escalation can't resolve. **Open:** whole-run vs per-role escalation scope.
- **Testing:** ~80% of an in-process, deterministic cascade test already exists; the missing piece is the
  ability to force a review bounce-back and to check all three tools get the right instructions. The input fix
  above is what makes forcing a bounce-back possible.
