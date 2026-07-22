# 01 — Problem Space: Harden the DOT workflow mechanism

> Seeded from the `2026-07-17-dot-mechanism` spike (its `MODEL.md` is the converged design;
> `CONSOLIDATION.md` is the fuller decision brief; `research/*/findings.md` are code-verified).
> This work carries that decision through the spec-first pipeline to an implementable task list.
> Plain-language mode is ON for this line of work (operator directive).

## What is changing, and why

The DOT workflow engine (the `implement → review → feedback` loop that is the heart of harmonik)
has one structural defect from which several problems follow:

**Every agent's brief is built by role-specific code, and both roles are handed the whole bead
body.** The bead body carries the task *and* the review checklist, and the daemon renders the whole
body into both briefs — implementer (`agent-task.md`) and reviewer (`review-target.md`) — via two
hand-written builders (`buildAgentTaskContent` / `buildReviewTargetContent`,
`internal/workspace/agenttask_chb028.go`). So the implementer sees the reviewer's checklist and
does everything up front; a review round-trip can never be forced; and the whole feedback path
stays untested.

Everything else follows: no forceable round-trip (P2), the reviewer's own node prompt is inert
(P5), feedback in dot mode has no spec'd channel (the `reviewer-feedback` file is review-loop-mode
only per EM §7.5), and feedback delivery is harness-divergent (claude paste-inject vs pi/codex
resume seed — the split that caused the "no commit on resume" bug, commit `8dbe5a17`).

Two adjacent operator asks ride along:
- **Model control:** a bead cannot escalate a hard node past the graph's default — a `.dot` node
  `model=sonnet` overrides a per-bead `model:opus` (`dot_cascade.go:1351`), and per-node `model=`
  is silently ignored for pi/codex (`nodeModelForHarness`, `:1182`).
- **Testing:** there is no in-process test that can force a deterministic review round-trip or
  assert the real per-handler argv, across all three tools.

## The decided design (one line)

Replace the two hand-written brief builders with **one renderer** that assembles any node's brief
from **only that node's declared inputs** (+ its prompt, role, and the run goal). Nodes declare
which named values they can read; a node can produce a value a downstream node consumes (that IS
the reviewer→implementer feedback — a real value on the back-edge, replacing the feedback-file
hack). Role separation becomes structural: the checklist cannot appear in the implementer's brief
because it is not a declared input. Code itself keeps flowing through the shared git worktree, so
there is **no new bulk-data store**. Types stay minimal — the reviewer's verdict enum is the one
that earns its keep (load-time check that edges only branch on verdicts the reviewer can emit).

## Goals — what should be true after this change

1. **No leak, structurally.** The implementer's brief is assembled only from its declared inputs;
   the review checklist is not one of them, so it cannot appear. Checkable at graph-load.
2. **One renderer, all roles, all tools.** A single brief-assembler replaces the two builders. The
   tool adapters (claude/pi/codex) choose only *how* to deliver a finished brief, never *what* it
   says. New roles (qa, planner) need no new builder.
3. **Feedback is a value on the back-edge.** The reviewer produces verdict + notes; the implementer,
   on the bounce-back, consumes them as a declared input — rendered identically across all three
   tools. The dot-mode feedback gap and the harness divergence both close.
4. **Per-node model/effort/tool override lives in the bead** as structured config, addressed by
   node id (names must line up with the graph), overriding the graph's node defaults. The concrete
   (tool, model, effort) chosen at each dispatch is **logged per (node, iteration)**, and a replay
   (rerun) reproduces those exact choices by reading the log — never recomputing from live settings.
5. **A forceable, deterministic round-trip test** exists in-process, across all three tools, plus
   per-handler argv-faithfulness checks — enabled by goal 1 (the leak fix is what makes a
   bounce-back forceable without a fixture hack).

## Operator decisions already locked (inputs to design, not open questions)

- **Variables stay GLOBAL; the graph controls per-node visibility.** A node sees only the variables
  it declares (plus a small safe default). The implementer does not declare — cannot see — the
  review variable.
- **Model priority flips:** a per-bead escalation beats the workflow file's node default. Fix
  pi/codex ignoring a node's model. Per-role reasoning-effort knob: yes.
- **Escalation is addressed per node id**, carried in the bead as **structured config** (the exact
  serialization is a Design-pass decision — NOT the throwaway `implement@model=…` string the
  operator sketched; pick a proper structured form). A simple/bare form MAY still mean run-wide.
  **This is task configuration in the bead, explicitly NOT version pinning** (version pinning =
  hardcoding a tool/model version in the codebase; a model alias in a bead is per-task config and
  is fine). Model names remain advisory aliases with graceful degradation — except an explicit
  escalation that cannot resolve for the target tool must fail loud, not silently downgrade.
- **Replay is the confirmation mechanism.** The per-(node, iteration) concrete-selection log is what
  makes "did the system use what I configured?" answerable and stable across catalog edits.

## Non-goals (explicitly out of scope — deferred until a real need appears)

- **No artifact/bulk-value store.** Diffs, transcripts, rubrics already have durable homes (git
  worktree + base/head SHAs; the harness; on-disk briefs). Do not invent a `ref<KIND>` store.
- **No edge-scoped typed dataflow binder / general type lattice / JSON-in-DOT ports.** harmonik runs
  one dominant graph shape (one producer→consumer pair). Defer until a second pair actually exists.
- **No CSS `model_stylesheet` / selector engine** (already deferred, hk-1xzg3). Node attr + bead
  config + per-role knob suffice at current scale.
- **No new edge-condition operators** (`<`, `>`, `||`). Equality dialect stays as-is.
- **No deletion of the review-loop Go path in this work** in a big-bang migration. Generalize dot
  mode additively; retire the separate path only once dot-mode feedback is proven.
- **No hard version pins** anywhere in code/config for tools or models.

## Constraints / things that must not change

- **Spec-first.** The spec is normative; code follows. All contract-level changes land as spec text
  first (`specs/workflow-graph.md`, `specs/execution-model.md`, `specs/handler-contract.md`).
- **Template-param substitution security ordering (WG-046)** is preserved: parse before substitute,
  per-attribute, `tool_command` shell-quoted. Note: task/rubric text must travel as **declared
  inputs**, not as launch-time template params — that channel rejects newlines (`templateparams.go:91`,
  a shell-injection defense) and caps at 8 KB, so a real multi-line rubric cannot fit.
- **The unconditional-fallback edge invariant (WG-011)**, the five-step cascade (EM-041), the closed
  4-node type enum (WG-001), and the review-floor (WG-050, `close` has exactly one APPROVE inbound)
  are unchanged.
- **Beads are OFF this phase** — process tracking is in `plans/2026-07-13-code-revamp/COORD.md`, not
  the bead ledger. Task beads produced by this work are the *deliverable*, not live-dispatched now.

## Success criteria (what the specs must describe when this work is done)

1. `workflow-graph.md` defines **declared per-node inputs/outputs** (named values, with per-node
   read visibility), a typed **verdict output enum**, and the **load-time checks** (declared-input
   present; edges branch only on emittable verdict values; back-edges carry a traversal cap;
   override clauses name real nodes).
2. `execution-model.md` defines the **single brief-renderer contract** (brief assembled only from a
   node's declared inputs + prompt + role + goal), the **value-on-back-edge feedback** channel
   (replacing the review-loop-only file prohibition in EM §7.5), the **model-resolution ladder flip**,
   and the **per-(node, iteration) concrete-selection seal + replay-reads-the-seal** rule.
3. `handler-contract.md` defines the harness adapter as **transport-only** (delivers the assembled
   brief; never re-derives content), **per-tool model-alias resolution** (fixing the pi/codex
   silent-ignore, with fail-loud on an unresolvable explicit escalation), and the **testing seams**
   (real launch-spec builder + argv recorder + handler-faithful twin) needed to force a round-trip
   and assert per-handler argv.
4. A **task breakdown** (`07-tasks.md`) sequences the change so the input-model/renderer lands first
   (it unblocks everything), then feedback-on-edge, then model control, then the test harness.

## Preliminary spec areas affected

- `specs/workflow-graph.md` (WG) — node I/O declarations, visibility, verdict enum, override attrs,
  load checks.
- `specs/execution-model.md` (EM) — renderer contract, feedback-on-edge (EM §7.5 reconciliation),
  model-resolution flip + seal + replay.
- `specs/handler-contract.md` (HC) — transport-only adapter, per-tool model resolution, test seams.

(Possible touch: `specs/examples/standard-bead.dot` sidecar, if the default graph gains declared
I/O. Confirmed or dropped in Decompose.)
